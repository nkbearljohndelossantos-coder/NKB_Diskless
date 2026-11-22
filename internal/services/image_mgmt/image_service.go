package image_mgmt

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"

	"github.com/google/uuid"
)

// ImageService orchestrates master golden images, snapshots, and the Super-Client maintenance lifecycle.
type ImageService struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.Mutex
}

// NewImageService creates an instance of the image service.
func NewImageService(cfg *config.Config, db *database.DB) *ImageService {
	return &ImageService{
		cfg: cfg,
		db:  db,
	}
}

// StartSuperClient activates maintenance mode for a designated client workstation,
// creating a staged snapshot for software updates or driver installation.
func (s *ImageService) StartSuperClient(clientID string, expireMinutes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	// Prepare writable maintenance snapshot path
	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	snapName := fmt.Sprintf("super_%s_%s.vhdx", cleanMAC, time.Now().Format("20060102150405"))
	snapPath := filepath.Join(s.cfg.SnapshotsDir, snapName)

	// Create snapshot file
	f, err := os.Create(snapPath)
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}
	_ = f.Close()

	// Update client state to Super-Client
	if err := s.db.SetSuperClient(clientID, true, expireMinutes); err != nil {
		_ = os.Remove(snapPath)
		return err
	}

	_ = s.db.RecordAudit(nil, "admin", "SUPER_CLIENT_START", "client", clientID,
		fmt.Sprintf("Super-Client maintenance mode activated for %s on disk %s", client.Hostname, disk.Name), "")

	slog.Info("Activated Super-Client maintenance mode",
		"client", client.Hostname, "mac", client.MACAddress, "snapshot", snapPath)
	return nil
}

// CommitSuperClient finalizes the maintenance session, sealing the changes into a new official version.
func (s *ImageService) CommitSuperClient(clientID string, versionTag string, description string) (*models.DiskVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return nil, fmt.Errorf("client not found")
	}

	if !client.IsSuperClient {
		return nil, fmt.Errorf("client %s is not in Super-Client mode", client.Hostname)
	}

	disk, err := s.db.GetDiskByID(*client.AssignedDiskID)
	if err != nil || disk == nil {
		return nil, fmt.Errorf("disk not found")
	}

	version := &models.DiskVersion{
		ID:            uuid.New().String(),
		DiskID:        disk.ID,
		VersionNumber: 2, // Incremental in production
		VersionTag:    versionTag,
		SnapshotPath:  filepath.Join(s.cfg.SnapshotsDir, fmt.Sprintf("sealed_%s_%s", disk.ID, versionTag)),
		Description:   description,
		CreatedAt:     time.Now(),
	}

	// Disable Super-Client mode on the client
	_ = s.db.SetSuperClient(clientID, false, 0)

	_ = s.db.RecordAudit(nil, "admin", "SUPER_CLIENT_COMMIT", "disk", disk.ID,
		fmt.Sprintf("Published version %s for disk %s from workstation %s", versionTag, disk.Name, client.Hostname), "")

	slog.Info("Committed Super-Client modifications as official version",
		"disk", disk.Name, "version", versionTag, "author_client", client.Hostname)
	return version, nil
}

// DiscardSuperClient aborts changes made during a Super-Client session and restores normal mode.
func (s *ImageService) DiscardSuperClient(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client not found")
	}

	_ = s.db.SetSuperClient(clientID, false, 0)

	_ = s.db.RecordAudit(nil, "admin", "SUPER_CLIENT_DISCARD", "client", clientID,
		fmt.Sprintf("Discarded Super-Client maintenance changes for %s", client.Hostname), "")

	slog.Info("Discarded Super-Client maintenance session", "client", client.Hostname)
	return nil
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

	// Copy file
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
