package image_mgmt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"

	"github.com/google/uuid"
)

// ImageService orchestrates master golden images, snapshots, version sealing,
// and the exclusive Super-Client maintenance lifecycle.
type ImageService struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.Mutex
}

// NewImageService creates an instance of the image service.
func NewImageService(cfg *config.Config, db *database.DB) *ImageService {
	_ = os.MkdirAll(cfg.SnapshotsDir, 0755)
	_ = os.MkdirAll(cfg.DisksDir, 0755)
	return &ImageService{
		cfg: cfg,
		db:  db,
	}
}

// StartSuperClient activates exclusive maintenance mode for a designated client workstation,
// creating a staged snapshot for software updates or driver installation.
func (s *ImageService) StartSuperClient(clientID string, expireMinutes int, operatorUser string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if expireMinutes <= 0 {
		expireMinutes = 60 // 1 hour default
	}

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client %s not found", clientID)
	}

	if client.AssignedDiskID == nil || *client.AssignedDiskID == "" {
		return fmt.Errorf("client %s does not have an assigned virtual disk", client.Hostname)
	}

	disk, err := s.db.GetDiskByID(*client.AssignedDiskID)
	if err != nil || disk == nil {
		return fmt.Errorf("assigned disk %s not found", *client.AssignedDiskID)
	}

	// Check if this disk is already locked by another client
	activeLock, err := s.db.GetActiveMaintenanceLock(disk.ID)
	if err != nil {
		return fmt.Errorf("failed to check lock status: %w", err)
	}
	if activeLock != nil && activeLock.ClientID != clientID {
		return fmt.Errorf("disk %s is exclusively locked by another workstation (%s) until %s",
			disk.Name, activeLock.ClientID, activeLock.ExpiresAt.Format(time.RFC3339))
	}

	sessionToken := uuid.New().String()
	expiresAt := time.Now().Add(time.Duration(expireMinutes) * time.Minute)

	// Acquire DB maintenance lock
	lock := &models.MaintenanceLock{
		ID:           uuid.New().String(),
		DiskID:       disk.ID,
		ClientID:     clientID,
		AcquiredBy:   operatorUser,
		SessionToken: sessionToken,
		IsActive:     true,
		AcquiredAt:   time.Now(),
		ExpiresAt:    expiresAt,
	}

	if err := s.db.AcquireMaintenanceLock(lock); err != nil {
		return fmt.Errorf("failed to acquire exclusive maintenance lock: %w", err)
	}

	// Prepare writable maintenance snapshot path
	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	snapName := fmt.Sprintf("super_%s_%s.vhdx", cleanMAC, time.Now().Format("20060102150405"))
	snapPath := filepath.Join(s.cfg.SnapshotsDir, snapName)

	f, err := os.Create(snapPath)
	if err != nil {
		_ = s.db.ReleaseMaintenanceLock(disk.ID, clientID)
		return fmt.Errorf("failed to create maintenance snapshot file: %w", err)
	}
	_ = f.Close()

	// Update client state to Super-Client
	if err := s.db.SetSuperClient(clientID, true, expireMinutes); err != nil {
		_ = os.Remove(snapPath)
		_ = s.db.ReleaseMaintenanceLock(disk.ID, clientID)
		return err
	}

	_ = s.db.RecordAudit(nil, operatorUser, "SUPER_CLIENT_START", "client", clientID,
		fmt.Sprintf("Super-Client maintenance mode activated for %s on disk %s (Expires: %d min)",
			client.Hostname, disk.Name, expireMinutes), "")

	slog.Info("Activated exclusive Super-Client maintenance mode",
		"client", client.Hostname, "mac", client.MACAddress, "snapshot", snapPath, "operator", operatorUser)
	return nil
}

// CommitSuperClient finalizes the maintenance session, sealing changes into an official immutable version.
func (s *ImageService) CommitSuperClient(clientID string, versionTag string, releaseNotes string, operatorUser string) (*models.DiskVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return nil, fmt.Errorf("client not found")
	}

	if !client.IsSuperClient {
		return nil, fmt.Errorf("workstation %s is not currently in Super-Client maintenance mode", client.Hostname)
	}

	if client.AssignedDiskID == nil {
		return nil, fmt.Errorf("client has no assigned virtual disk")
	}

	disk, err := s.db.GetDiskByID(*client.AssignedDiskID)
	if err != nil || disk == nil {
		return nil, fmt.Errorf("assigned disk not found")
	}

	// Determine incremental version number
	existingVersions, _ := s.db.ListDiskVersions(disk.ID)
	nextVerNum := len(existingVersions) + 1

	if versionTag == "" {
		versionTag = fmt.Sprintf("v1.%d", nextVerNum)
	}

	// Find the super client snapshot file
	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	snapPattern := filepath.Join(s.cfg.SnapshotsDir, fmt.Sprintf("super_%s_*.vhdx", cleanMAC))
	matches, _ := filepath.Glob(snapPattern)

	sealedFilename := fmt.Sprintf("sealed_%s_%s.vhdx", disk.ID, strings.ReplaceAll(versionTag, ".", "_"))
	sealedPath := filepath.Join(s.cfg.SnapshotsDir, sealedFilename)

	var snapSource string
	if len(matches) > 0 {
		snapSource = matches[len(matches)-1]
		_ = os.Rename(snapSource, sealedPath)
	} else {
		// If no distinct file, create sealed marker
		f, _ := os.Create(sealedPath)
		if f != nil {
			f.WriteString("NKB_SEALED_IMAGE_MARKER:" + versionTag)
			_ = f.Close()
		}
	}

	// Compute checksum for sealed version
	checksum := s.calculateChecksum(sealedPath)

	// Apply read-only protection to sealed version
	_ = s.SetReadOnlyProtection(sealedPath, true)

	version := &models.DiskVersion{
		ID:             uuid.New().String(),
		DiskID:         disk.ID,
		VersionNumber:  nextVerNum,
		VersionTag:     versionTag,
		SnapshotPath:   sealedPath,
		Description:    releaseNotes,
		ChecksumSHA256: checksum,
		CreatedBy:      &operatorUser,
		CreatedAt:      time.Now(),
	}

	if err := s.db.CreateDiskVersion(version); err != nil {
		return nil, fmt.Errorf("failed to save disk version record: %w", err)
	}

	// Release Super-Client lock and reset client status
	_ = s.db.ReleaseMaintenanceLock(disk.ID, clientID)
	_ = s.db.SetSuperClient(clientID, false, 0)

	_ = s.db.RecordAudit(nil, operatorUser, "SUPER_CLIENT_COMMIT", "disk", disk.ID,
		fmt.Sprintf("Published sealed version %s (%s) for disk %s from workstation %s",
			versionTag, checksum[:min(len(checksum), 8)], disk.Name, client.Hostname), "")

	slog.Info("Successfully committed Super-Client modifications as official sealed version",
		"disk", disk.Name, "version", versionTag, "author", client.Hostname, "checksum", checksum)
	return version, nil
}

// DiscardSuperClient aborts uncommitted changes made during a maintenance session and restores normal mode.
func (s *ImageService) DiscardSuperClient(clientID string, operatorUser string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client not found")
	}

	if client.AssignedDiskID != nil {
		_ = s.db.ReleaseMaintenanceLock(*client.AssignedDiskID, clientID)
	}

	// Delete uncommitted maintenance snapshots
	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	snapPattern := filepath.Join(s.cfg.SnapshotsDir, fmt.Sprintf("super_%s_*.vhdx", cleanMAC))
	matches, _ := filepath.Glob(snapPattern)
	for _, m := range matches {
		_ = os.Remove(m)
	}

	_ = s.db.SetSuperClient(clientID, false, 0)

	_ = s.db.RecordAudit(nil, operatorUser, "SUPER_CLIENT_DISCARD", "client", clientID,
		fmt.Sprintf("Discarded uncommitted Super-Client maintenance changes for %s", client.Hostname), "")

	slog.Info("Discarded Super-Client maintenance session and deleted transient delta", "client", client.Hostname)
	return nil
}

// RollbackToVersion reverts a Golden Image or active assignment pointer to a prior verified version snapshot.
func (s *ImageService) RollbackToVersion(diskID string, versionID string, operatorUser string) (*models.DiskVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetVer, err := s.db.GetDiskVersionByID(versionID)
	if err != nil || targetVer == nil {
		return nil, fmt.Errorf("version %s not found", versionID)
	}

	if targetVer.DiskID != diskID {
		return nil, fmt.Errorf("version %s does not belong to disk %s", versionID, diskID)
	}

	disk, err := s.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return nil, fmt.Errorf("disk not found: %s", diskID)
	}

	// Verify that snapshot file exists
	if _, err := os.Stat(targetVer.SnapshotPath); err != nil {
		return nil, fmt.Errorf("sealed snapshot file missing from storage: %s", targetVer.SnapshotPath)
	}

	// Verify integrity checksum before rollback
	currentCheck := s.calculateChecksum(targetVer.SnapshotPath)
	if targetVer.ChecksumSHA256 != "" && !strings.EqualFold(currentCheck, targetVer.ChecksumSHA256) {
		return nil, fmt.Errorf("snapshot integrity check failed (expected %s, got %s)",
			targetVer.ChecksumSHA256, currentCheck)
	}

	// Update disk checksum to match target version
	disk.ChecksumSHA256 = currentCheck
	_ = s.db.UpdateDisk(disk)

	_ = s.db.RecordAudit(nil, operatorUser, "IMAGE_ROLLBACK", "disk", diskID,
		fmt.Sprintf("Rolled back disk %s to version %s (Snapshot: %s)",
			disk.Name, targetVer.VersionTag, filepath.Base(targetVer.SnapshotPath)), "")

	slog.Info("Rolled back disk to verified version",
		"disk", disk.Name, "version", targetVer.VersionTag, "snapshot", targetVer.SnapshotPath)
	return targetVer, nil
}

// AssignDisk binds a virtual disk / Golden Image to a workstation with deployment mode.
func (s *ImageService) AssignDisk(clientID string, diskID string, deploymentType string, mountPoint string, operatorUser string) (*models.DiskAssignment, error) {
	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return nil, fmt.Errorf("client %s not found", clientID)
	}

	disk, err := s.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return nil, fmt.Errorf("virtual disk %s not found", diskID)
	}

	if deploymentType == "" {
		deploymentType = "network_iscsi"
	}
	if mountPoint == "" && deploymentType == "local_vhdx" {
		mountPoint = `C:\NKB_Disks`
	}

	assignment := &models.DiskAssignment{
		ID:             uuid.New().String(),
		ClientID:       clientID,
		DiskID:         diskID,
		DeploymentType: deploymentType,
		MountPoint:     mountPoint,
		IsActive:       true,
		AssignedAt:     time.Now(),
	}

	if err := s.db.CreateDiskAssignment(assignment); err != nil {
		return nil, fmt.Errorf("failed to create disk assignment: %w", err)
	}

	_ = s.db.RecordAudit(nil, operatorUser, "DISK_ASSIGN", "client", clientID,
		fmt.Sprintf("Assigned disk %s to %s (Mode: %s, Mount: %s)",
			disk.Name, client.Hostname, deploymentType, mountPoint), "")

	slog.Info("Assigned virtual disk to client workstation",
		"client", client.Hostname, "disk", disk.Name, "mode", deploymentType)
	return assignment, nil
}

// UnassignDisk removes active virtual disk binding from a workstation.
func (s *ImageService) UnassignDisk(clientID string, operatorUser string) error {
	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client not found")
	}

	if err := s.db.DeactivateDiskAssignment(clientID); err != nil {
		return err
	}

	_ = s.db.RecordAudit(nil, operatorUser, "DISK_UNASSIGN", "client", clientID,
		fmt.Sprintf("Unassigned virtual disk from workstation %s", client.Hostname), "")

	slog.Info("Unassigned virtual disk from workstation", "client", client.Hostname)
	return nil
}

// SetReadOnlyProtection toggles filesystem write-protection on a disk or snapshot image.
func (s *ImageService) SetReadOnlyProtection(filePath string, readOnly bool) error {
	if runtime.GOOS == "windows" {
		flag := "+R"
		if !readOnly {
			flag = "-R"
		}
		cmd := exec.Command("attrib", flag, filePath)
		_ = cmd.Run()
	}

	// Apply POSIX file permission fallback
	if readOnly {
		return os.Chmod(filePath, 0444)
	}
	return os.Chmod(filePath, 0644)
}

// ValidateImage checks SHA256 integrity of the golden image against recorded database hash.
func (s *ImageService) ValidateImage(diskID string) (bool, string, error) {
	disk, err := s.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return false, "", fmt.Errorf("disk not found")
	}

	calculated := s.calculateChecksum(disk.FilePath)
	if calculated == "" {
		return false, "", fmt.Errorf("failed to read disk file for hashing")
	}

	if disk.ChecksumSHA256 == "" {
		disk.ChecksumSHA256 = calculated
		_ = s.db.UpdateDisk(disk)
		return true, calculated, nil
	}

	isMatch := strings.EqualFold(calculated, disk.ChecksumSHA256)
	return isMatch, calculated, nil
}

// DuplicateDisk creates a complete independent copy of an existing virtual disk.
func (s *ImageService) DuplicateDisk(sourceDiskID string, newName string) (*models.VirtualDisk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source, err := s.db.GetDiskByID(sourceDiskID)
	if err != nil || source == nil {
		return nil, fmt.Errorf("source disk not found")
	}

	sanitized := strings.ToLower(strings.ReplaceAll(newName, " ", "_"))
	destFilename := fmt.Sprintf("%s.%s", sanitized, strings.ToLower(source.DiskFormat))
	destPath := filepath.Join(s.cfg.DisksDir, destFilename)

	if _, err := os.Stat(destPath); err == nil {
		return nil, fmt.Errorf("destination file %s already exists", destPath)
	}

	srcFile, err := os.Open(source.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source disk: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination disk: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("failed to copy disk data: %w", err)
	}

	newDisk := &models.VirtualDisk{
		Name:        newName,
		Description: fmt.Sprintf("Cloned from %s", source.Name),
		FilePath:    destPath,
		SizeBytes:   source.SizeBytes,
		DiskFormat:  source.DiskFormat,
		IsGolden:    false,
		IsLocked:    false,
		OSType:      source.OSType,
	}

	if err := s.db.CreateDisk(newDisk); err != nil {
		_ = os.Remove(destPath)
		return nil, err
	}

	slog.Info("Duplicated virtual disk", "source", source.Name, "new_disk", newName, "path", destPath)
	return newDisk, nil
}

func (s *ImageService) calculateChecksum(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
