package storage_mgmt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
)

// DiskManager handles physical disk files, sparse allocation, differential caches, and version trees.
type DiskManager struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.RWMutex
}

// NewDiskManager initializes storage manager with configured paths.
func NewDiskManager(cfg *config.Config, db *database.DB) (*DiskManager, error) {
	// Ensure storage directories exist
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

// CreateVirtualDisk provisions a new virtual disk file on storage.
func (dm *DiskManager) CreateVirtualDisk(name string, sizeBytes int64, format string, osType string, description string) (*models.VirtualDisk, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	format = strings.ToUpper(format)
	if format == "" {
		format = "VHDX"
	}

	sanitizedName := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	filename := fmt.Sprintf("%s.%s", sanitizedName, strings.ToLower(format))
	diskPath := filepath.Join(dm.cfg.DisksDir, filename)

	if _, err := os.Stat(diskPath); err == nil {
		return nil, fmt.Errorf("disk file already exists at %s", diskPath)
	}

	// Create sparse file
	file, err := os.Create(diskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk file: %w", err)
	}
	defer file.Close()

	if sizeBytes > 0 {
		// Truncate to desired virtual capacity (sparse on NTFS/ext4)
		if err := file.Truncate(sizeBytes); err != nil {
			_ = os.Remove(diskPath)
			return nil, fmt.Errorf("failed to truncate virtual disk size: %w", err)
		}
	}

	disk := &models.VirtualDisk{
		Name:        name,
		Description: description,
		FilePath:    diskPath,
		SizeBytes:   sizeBytes,
		DiskFormat:  format,
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
		// Truncate back to empty or re-initialize
		f, err := os.OpenFile(match, os.O_TRUNC|os.O_WRONLY, 0644)
		if err == nil {
			_ = f.Truncate(20 * 1024 * 1024 * 1024)
			_ = f.Close()
			slog.Info("Flushed volatile write-cache for client", "mac", client.MACAddress, "file", match)
		}
	}

	return nil
}

// ComputeChecksum calculates SHA256 of the master disk image for verification.
func (dm *DiskManager) ComputeChecksum(diskID string) (string, error) {
	disk, err := dm.db.GetDiskByID(diskID)
	if err != nil || disk == nil {
		return "", fmt.Errorf("disk not found")
	}

	file, err := os.Open(disk.FilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	disk.ChecksumSHA256 = sum
	_ = dm.db.UpdateDisk(disk)

	return sum, nil
}
