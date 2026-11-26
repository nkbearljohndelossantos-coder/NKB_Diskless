package storage_mgmt

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

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
)

// DiskManager handles local VHDX operations, sparse allocation, differential caches, and version trees.
type DiskManager struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.RWMutex
}

// NewDiskManager initializes storage manager with configured paths.
func NewDiskManager(cfg *config.Config, db *database.DB) (*DiskManager, error) {
	dirs := []string{cfg.DisksDir, cfg.CacheDir, cfg.SnapshotsDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("failed to create storage directory %s: %w", d, err)
		}
	}

	return &DiskManager{
		cfg: cfg,
		db:  db,
	}, nil
}

// validateVirtualDiskPath ensures storage operations exclusively target project-contained virtual disk files (.vhdx / .vhd / .delta)
// and strictly rejects any attempt to target physical disks, raw block devices, or volume partitions.
func validateVirtualDiskPath(path string) error {
	cleanPath := filepath.Clean(path)
	lower := strings.ToLower(cleanPath)

	if strings.Contains(lower, "physicaldrive") || strings.HasPrefix(lower, `\\.\`) || strings.HasPrefix(lower, `\\?\`) {
		return fmt.Errorf("DISABLED_BY_SAFETY_POLICY: physical drive operations are prohibited")
	}

	ext := filepath.Ext(lower)
	if ext != ".vhdx" && ext != ".vhd" && ext != ".delta" {
		return fmt.Errorf("DISABLED_BY_SAFETY_POLICY: only virtual disk images (.vhdx, .vhd) are permitted, got: %s", ext)
	}

	return nil
}

// ModifyPhysicalDisk unconditionally rejects any physical disk modification during Phase 2 PoC.
func (dm *DiskManager) ModifyPhysicalDisk(diskNumber int) error {
	return fmt.Errorf("DISABLED_BY_SAFETY_POLICY: physical disk modification is prohibited")
}

// CreateVirtualDisk provisions a new virtual disk file on storage (dynamic sparse allocation).
func (dm *DiskManager) CreateVirtualDisk(name string, sizeBytes int64, format string, osType string, description string) (*models.VirtualDisk, error) {
	return dm.CreateVHDX(name, sizeBytes, true, osType, description)
}

// CreateVHDX provisions a dynamically expanding or fixed VHDX image.
func (dm *DiskManager) CreateVHDX(name string, sizeBytes int64, isDynamic bool, osType string, description string) (*models.VirtualDisk, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, fmt.Errorf("disk name cannot be empty")
	}
	// Enforce strict character whitelist to prevent diskpart command injection and path traversal
	for _, ch := range trimmedName {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == ' ' || ch == '_' || ch == '-' || ch == '.') {
			return nil, fmt.Errorf("invalid disk name: contains forbidden characters: %q", name)
		}
	}
	if strings.Contains(trimmedName, "..") || strings.Contains(trimmedName, "/") || strings.Contains(trimmedName, "\\") || strings.Contains(trimmedName, "\"") || strings.Contains(trimmedName, "\n") {
		return nil, fmt.Errorf("invalid disk name: path traversal or command injection sequence detected: %q", name)
	}

	sanitizedName := strings.ToLower(strings.ReplaceAll(trimmedName, " ", "_"))
	filename := fmt.Sprintf("%s.vhdx", sanitizedName)
	diskPath := filepath.Join(dm.cfg.DisksDir, filename)

	if _, err := os.Stat(diskPath); err == nil {
		return nil, fmt.Errorf("disk file already exists at %s", diskPath)
	}

	// If running on Windows, we can use diskpart to initialize proper VHDX header
	if runtime.GOOS == "windows" {
		diskType := "expandable"
		if !isDynamic {
			diskType = "fixed"
		}
		maxMB := sizeBytes / (1024 * 1024)
		if maxMB <= 0 {
			maxMB = 50 * 1024 // 50GB default
		}

		diskpartScript := fmt.Sprintf("create vdisk file=\"%s\" maximum=%d type=%s\nselect vdisk file=\"%s\"\nattach vdisk\nconvert gpt\ncreate partition efi size=260\nformat quick fs=fat32 label=\"System\"\ncreate partition primary\nformat quick fs=ntfs label=\"NKB_OS\"\nassign\ndetach vdisk\n", diskPath, maxMB, diskType, diskPath)
		cmd := exec.Command("diskpart")
		cmd.Stdin = strings.NewReader(diskpartScript)
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Warn("diskpart create vdisk failed (elevation required), falling back to sparse disk allocation", "err", err, "out", string(out))
			file, createErr := os.Create(diskPath)
			if createErr != nil {
				return nil, fmt.Errorf("failed to create virtual disk file: %w", createErr)
			}
			if sizeBytes > 0 {
				_ = file.Truncate(sizeBytes)
			}
			_ = file.Close()
		}
	} else {
		// Non-Windows fallback (sparse file)
		file, err := os.Create(diskPath)
		if err != nil {
			return nil, err
		}
		if sizeBytes > 0 {
			_ = file.Truncate(sizeBytes)
		}
		_ = file.Close()
	}

	disk := &models.VirtualDisk{
		Name:        name,
		Description: description,
		FilePath:    diskPath,
		SizeBytes:   sizeBytes,
		DiskFormat:  "VHDX",
		IsGolden:    true,
		IsLocked:    false,
		OSType:      osType,
	}

	if err := dm.db.CreateDisk(disk); err != nil {
		_ = os.Remove(diskPath)
		return nil, fmt.Errorf("failed to register disk in database: %w", err)
	}

	slog.Info("Created virtual disk image", "name", name, "path", diskPath, "size_gb", sizeBytes/(1024*1024*1024))
	return disk, nil
}

// MountVHDX attaches the virtual disk locally in Windows.
func (dm *DiskManager) MountVHDX(diskID string) (string, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return "", fmt.Errorf("disk not found: %s", diskID)
	}

	if err := validateVirtualDiskPath(disk.FilePath); err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		script := fmt.Sprintf("select vdisk file=\"%s\"\nattach vdisk readonly\n", disk.FilePath)
		cmd := exec.Command("diskpart")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to attach vdisk: %v (%s)", err, string(out))
		}
		slog.Info("Mounted VHDX disk", "name", disk.Name, "path", disk.FilePath)
		return disk.FilePath, nil
	}

	return disk.FilePath, nil
}

// DismountVHDX detaches the virtual disk locally in Windows.
func (dm *DiskManager) DismountVHDX(diskID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return fmt.Errorf("disk not found: %s", diskID)
	}

	if runtime.GOOS == "windows" {
		script := fmt.Sprintf("select vdisk file=\"%s\"\ndetach vdisk\n", disk.FilePath)
		cmd := exec.Command("diskpart")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to detach vdisk: %v (%s)", err, string(out))
		}
		slog.Info("Dismounted VHDX disk", "name", disk.Name, "path", disk.FilePath)
	}

	return nil
}

// ImportDisk registers an existing VHD/VHDX into the repository.
func (dm *DiskManager) ImportDisk(sourcePath string, name string, osType string) (*models.VirtualDisk, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if err := validateVirtualDiskPath(sourcePath); err != nil {
		return nil, err
	}

	fi, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("source file does not exist: %w", err)
	}

	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(sourcePath), "."))
	if ext != "VHD" && ext != "VHDX" && ext != "RAW" {
		ext = "VHDX"
	}

	disk := &models.VirtualDisk{
		Name:        name,
		Description: fmt.Sprintf("Imported from %s", sourcePath),
		FilePath:    sourcePath,
		SizeBytes:   fi.Size(),
		DiskFormat:  ext,
		IsGolden:    true,
		IsLocked:    false,
		OSType:      osType,
	}

	if err := dm.db.CreateDisk(disk); err != nil {
		return nil, err
	}

	slog.Info("Imported virtual disk into NKB repository", "name", name, "path", sourcePath)
	return disk, nil
}

// ExportDisk copies a virtual disk to an external export directory.
func (dm *DiskManager) ExportDisk(diskID string, destDir string) (string, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return "", fmt.Errorf("disk not found: %s", diskID)
	}

	if err := validateVirtualDiskPath(disk.FilePath); err != nil {
		return "", err
	}

	_ = os.MkdirAll(destDir, 0755)
	destPath := filepath.Join(destDir, filepath.Base(disk.FilePath))

	srcFile, err := os.Open(disk.FilePath)
	if err != nil {
		return "", fmt.Errorf("cannot open source disk: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("cannot create destination disk: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("failed to copy disk data: %w", err)
	}

	slog.Info("Exported virtual disk", "name", disk.Name, "dest", destPath)
	return destPath, nil
}

// AssignWorkstation binds a virtual disk to a specific workstation.
func (dm *DiskManager) AssignWorkstation(diskID string, clientID string, deploymentType string) error {
	client, err := dm.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client %s not found", clientID)
	}

	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return fmt.Errorf("disk %s not found", diskID)
	}

	client.AssignedDiskID = &diskID
	if err := dm.db.UpdateClient(client); err != nil {
		return err
	}

	_ = dm.db.RecordAudit(nil, "admin", "DISK_ASSIGN", "client", clientID,
		fmt.Sprintf("Assigned disk %s to workstation %s (Deployment: %s)", disk.Name, client.Hostname, deploymentType), "")

	slog.Info("Assigned virtual disk to workstation", "client", client.Hostname, "disk", disk.Name)
	return nil
}

// VerifyIntegrity calculates SHA256 of the master disk image and compares against stored hash.
func (dm *DiskManager) VerifyIntegrity(diskID string) (bool, string, error) {
	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return false, "", fmt.Errorf("disk not found")
	}

	file, err := os.Open(disk.FilePath)
	if err != nil {
		return false, "", fmt.Errorf("failed to open disk file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, "", fmt.Errorf("checksum calculation failed: %w", err)
	}

	currentHash := hex.EncodeToString(hasher.Sum(nil))
	if disk.ChecksumSHA256 == "" {
		disk.ChecksumSHA256 = currentHash
		_ = dm.db.UpdateDisk(disk)
		return true, currentHash, nil
	}

	match := strings.EqualFold(currentHash, disk.ChecksumSHA256)
	return match, currentHash, nil
}

// AllocateClientWriteCache ensures a client has a differential write-cache delta file.
func (dm *DiskManager) AllocateClientWriteCache(clientID string, diskID string, allocatedBytes int64) (string, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	client, err := dm.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return "", fmt.Errorf("client %s not found", clientID)
	}

	cacheFilename := fmt.Sprintf("wc_%s_%s.delta", strings.ReplaceAll(client.MACAddress, ":", ""), diskID)
	cachePath := filepath.Join(dm.cfg.CacheDir, cacheFilename)

	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		f, err := os.Create(cachePath)
		if err != nil {
			return "", fmt.Errorf("failed to create write-cache file: %w", err)
		}
		defer f.Close()

		if allocatedBytes <= 0 {
			allocatedBytes = 20 * 1024 * 1024 * 1024 // 20GB default
		}
		_ = f.Truncate(allocatedBytes)
		slog.Info("Allocated write-cache for client", "mac", client.MACAddress, "path", cachePath, "size_gb", allocatedBytes/(1024*1024*1024))
	}

	return cachePath, nil
}

// PurgeClientWriteCache discards the client's differential write-cache delta (for volatile mode).
func (dm *DiskManager) PurgeClientWriteCache(clientID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	client, err := dm.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client not found: %s", clientID)
	}

	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	pattern := filepath.Join(dm.cfg.CacheDir, fmt.Sprintf("wc_%s_*.delta", cleanMAC))
	matches, _ := filepath.Glob(pattern)

	for _, match := range matches {
		f, err := os.OpenFile(match, os.O_TRUNC|os.O_WRONLY, 0644)
		if err == nil {
			_ = f.Truncate(20 * 1024 * 1024 * 1024)
			_ = f.Close()
			slog.Info("Flushed volatile write-cache for client", "mac", client.MACAddress, "file", match)
		}
	}

	return nil
}
