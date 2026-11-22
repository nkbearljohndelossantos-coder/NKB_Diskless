package database_test

import (
	"os"
	"path/filepath"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
)

func TestDatabaseMigrationAndCRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	// Find migrations folder relative to workspace
	migrationsDir := filepath.Join("..", "database", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("migrations")
	}

	err = db.Migrate(migrationsDir)
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Test Group Creation
	group := &models.ClientGroup{
		Name:               "Manufacturing Floor Line A",
		Description:        "Assembly computers for Line A",
		BootTimeoutSeconds: 3,
		WriteCachePolicy:   "volatile",
		AllowSuperClient:   true,
	}
	err = db.CreateGroup(group)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// Test Virtual Disk Creation
	disk := &models.VirtualDisk{
		Name:        "Windows 11 Enterprise LTSC 2024",
		Description: "Golden base image for Line A workstations",
		FilePath:    filepath.Join(tempDir, "disks", "win11_ltsc.vhdx"),
		SizeBytes:   64424509440, // 60 GB
		DiskFormat:  "VHDX",
		IsGolden:    true,
		IsLocked:    false,
		OSType:      "Windows 11 x64",
	}
	err = db.CreateDisk(disk)
	if err != nil {
		t.Fatalf("failed to create disk: %v", err)
	}

	// Test Client Creation
	client := &models.Client{
		MACAddress:     "00:1A:2B:3C:4D:5E",
		IPAddress:      "192.168.10.101",
		Hostname:       "NKB-PC-01",
		GroupID:        &group.ID,
		AssignedDiskID: &disk.ID,
		Status:         "offline",
		BootMode:       "UEFI",
		WriteCacheMode: "volatile",
		HardwareInfo:   `{"cpu":"Intel Core i5-12400","ram_mb":16384,"nic":"Intel I219-V"}`,
	}
	err = db.CreateClient(client)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test Client Retrieval by MAC
	retrieved, err := db.GetClientByMAC("00-1A-2B-3C-4D-5E")
	if err != nil {
		t.Fatalf("failed to get client by MAC: %v", err)
	}
	if retrieved == nil {
		t.Fatalf("expected client, got nil")
	}
	if retrieved.Hostname != "NKB-PC-01" {
		t.Fatalf("expected hostname 'NKB-PC-01', got '%s'", retrieved.Hostname)
	}

	// Test Audit Logging
	err = db.RecordAudit(nil, "admin", "CLIENT_CREATE", "client", client.ID, "Created client NKB-PC-01", "127.0.0.1")
	if err != nil {
		t.Fatalf("failed to record audit log: %v", err)
	}

	logs, err := db.ListAuditLogs(10)
	if err != nil {
		t.Fatalf("failed to list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected at least 1 audit log, got 0")
	}
}
