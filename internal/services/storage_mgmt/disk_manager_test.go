package storage_mgmt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/storage_mgmt"
)

func TestLocalVHDXManagement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_disk_test_*")
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

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	dm, err := storage_mgmt.NewDiskManager(cfg, db)
	if err != nil {
		t.Fatalf("failed to create disk manager: %v", err)
	}

	// 1. Create VHDX
	disk, err := dm.CreateVHDX("Win11 Factory Golden", 1024*1024*1024, true, "Windows 11 x64", "Golden base image")
	if err != nil {
		t.Fatalf("failed to create VHDX: %v", err)
	}
	if disk.Name != "Win11 Factory Golden" {
		t.Fatalf("name mismatch: %s", disk.Name)
	}
	if _, err := os.Stat(disk.FilePath); os.IsNotExist(err) {
		t.Fatalf("disk file was not created on disk: %s", disk.FilePath)
	}

	// 2. Verify SHA-256 Integrity
	valid, hash, err := dm.VerifyIntegrity(disk.ID)
	if err != nil {
		t.Fatalf("failed to verify integrity: %v", err)
	}
	if !valid || hash == "" {
		t.Fatalf("expected valid integrity and non-empty hash, got valid=%v, hash=%s", valid, hash)
	}
	t.Logf("Disk SHA-256 Checksum: %s", hash)

	// 3. Assign to Workstation
	client := &models.Client{
		MACAddress: "00:AA:BB:CC:DD:EE",
		Hostname:   "NKB-STATION-01",
		Status:     "online",
	}
	if err := db.CreateClient(client); err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	err = dm.AssignWorkstation(disk.ID, client.ID, "local_vhdx")
	if err != nil {
		t.Fatalf("failed to assign workstation: %v", err)
	}

	updatedClient, _ := db.GetClientByID(client.ID)
	if updatedClient.AssignedDiskID == nil || *updatedClient.AssignedDiskID != disk.ID {
		t.Fatalf("assigned disk mismatch on client: %+v", updatedClient)
	}

	// 4. Export Disk
	exportDir := filepath.Join(tempDir, "exports")
	exportedPath, err := dm.ExportDisk(disk.ID, exportDir)
	if err != nil {
		t.Fatalf("failed to export disk: %v", err)
	}
	if _, err := os.Stat(exportedPath); os.IsNotExist(err) {
		t.Fatalf("exported file not found: %s", exportedPath)
	}

	// 5. Allocate & Purge Client Write Cache
	cachePath, err := dm.AllocateClientWriteCache(client.ID, disk.ID, 1024*1024)
	if err != nil {
		t.Fatalf("failed to allocate write-cache: %v", err)
	}
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("write-cache file does not exist: %s", cachePath)
	}

	err = dm.PurgeClientWriteCache(client.ID)
	if err != nil {
		t.Fatalf("failed to purge write-cache: %v", err)
	}
}

// TestPhysicalDiskSafety_ModificationsBlocked verifies Checkpoint B:
// All physical disk modification operations are unconditionally rejected with DISABLED_BY_SAFETY_POLICY.
func TestPhysicalDiskSafety_ModificationsBlocked(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_physical_disk_test_*")
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

	dm, err := storage_mgmt.NewDiskManager(cfg, db)
	if err != nil {
		t.Fatalf("failed to create disk manager: %v", err)
	}

	// 1. Direct call to ModifyPhysicalDisk is rejected
	err = dm.ModifyPhysicalDisk(0)
	if err == nil || !strings.Contains(err.Error(), "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected ModifyPhysicalDisk to return DISABLED_BY_SAFETY_POLICY, got: %v", err)
	}

	// 2. Attempting to import raw physical device path is rejected
	_, err = dm.ImportDisk(`\\.\PhysicalDrive0`, "Raw Drive", "Windows 11")
	if err == nil || !strings.Contains(err.Error(), "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected raw PhysicalDrive path import to return DISABLED_BY_SAFETY_POLICY, got: %v", err)
	}

	// 3. Attempting to import non-virtual disk file extension is rejected
	_, err = dm.ImportDisk(filepath.Join(tempDir, "test.exe"), "Executable", "Windows 11")
	if err == nil || !strings.Contains(err.Error(), "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected non-vdisk extension import to return DISABLED_BY_SAFETY_POLICY, got: %v", err)
	}
}

// TestCreateVHDX_SanitizationAndInjectionDefense verifies that injection payloads and path traversals in disk names are rejected.
func TestCreateVHDX_SanitizationAndInjectionDefense(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_injection_test_*")
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

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	dm, err := storage_mgmt.NewDiskManager(cfg, db)
	if err != nil {
		t.Fatalf("failed to create disk manager: %v", err)
	}

	injectionPayloads := []string{
		`test" maximum=100 type=fixed`,
		"test\nclean\ncreate vdisk",
		"../../windows/system32/evil",
		`..\..\boot.ini`,
		"name; rm -rf /",
		"",
		"   ",
		"test/disk",
		"test\\disk",
		"disk|pipe",
		"disk`backtick`",
	}

	for _, payload := range injectionPayloads {
		_, err := dm.CreateVHDX(payload, 1024*1024*1024, true, "Windows 11", "payload test")
		if err == nil {
			t.Errorf("expected payload %q to be rejected, but it succeeded", payload)
		}
	}
}
