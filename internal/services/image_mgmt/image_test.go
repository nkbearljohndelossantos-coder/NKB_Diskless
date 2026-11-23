package image_mgmt_test

import (
	"os"
	"path/filepath"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/image_mgmt"
)

func TestGoldenImageManagementAndSuperClient(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nkb_image_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig(tmpDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	imageSvc := image_mgmt.NewImageService(cfg, db)

	// 1. Create a Golden Image file
	diskPath := filepath.Join(cfg.DisksDir, "win11_pro_golden.vhdx")
	initialContent := []byte("NKB_WIN11_ENTERPRISE_GOLDEN_IMAGE_PAYLOAD_V1")
	if err := os.WriteFile(diskPath, initialContent, 0644); err != nil {
		t.Fatalf("Failed to write golden disk payload: %v", err)
	}

	disk := &models.VirtualDisk{
		Name:        "Win11 Pro Manufacturing",
		Description: "Base manufacturing golden image",
		FilePath:    diskPath,
		SizeBytes:   int64(len(initialContent)),
		DiskFormat:  "VHDX",
		IsGolden:    true,
		IsLocked:    false,
		OSType:      "Windows 11 Pro x64",
	}
	if err := db.CreateDisk(disk); err != nil {
		t.Fatalf("Failed to register disk: %v", err)
	}

	// Initial validation
	ok, checksum, err := imageSvc.ValidateImage(disk.ID)
	if err != nil || !ok {
		t.Fatalf("Initial image validation failed: ok=%v, err=%v", ok, err)
	}
	t.Logf("Initial image verified. SHA256: %s", checksum)

	// 2. Register Workstation 1 and Workstation 2
	client1 := &models.Client{
		Hostname:   "NKB-STATION-01",
		MACAddress: "00:11:22:33:44:01",
		IPAddress:  "192.168.99.101",
		Status:     "online",
	}
	if err := db.CreateClient(client1); err != nil {
		t.Fatalf("Failed to create client 1: %v", err)
	}

	client2 := &models.Client{
		Hostname:   "NKB-STATION-02",
		MACAddress: "00:11:22:33:44:02",
		IPAddress:  "192.168.99.102",
		Status:     "online",
	}
	if err := db.CreateClient(client2); err != nil {
		t.Fatalf("Failed to create client 2: %v", err)
	}

	// 3. Assign disk to Workstation 1
	assignment, err := imageSvc.AssignDisk(client1.ID, disk.ID, "network_iscsi", "", "admin")
	if err != nil {
		t.Fatalf("Failed to assign disk: %v", err)
	}
	if assignment.DeploymentType != "network_iscsi" {
		t.Errorf("Expected network_iscsi, got %s", assignment.DeploymentType)
	}

	// Also assign to Workstation 2
	_, _ = imageSvc.AssignDisk(client2.ID, disk.ID, "network_iscsi", "", "admin")

	// 4. Start Super-Client maintenance mode on Workstation 1
	err = imageSvc.StartSuperClient(client1.ID, 30, "admin")
	if err != nil {
		t.Fatalf("Failed to start Super-Client: %v", err)
	}

	// Verify Workstation 1 is now marked as Super-Client
	updatedClient1, _ := db.GetClientByID(client1.ID)
	if !updatedClient1.IsSuperClient {
		t.Errorf("Client 1 should be in Super-Client mode")
	}

	// 5. Test Mutual Exclusion: Client 2 MUST NOT be able to start Super-Client on the same disk
	err = imageSvc.StartSuperClient(client2.ID, 30, "admin")
	if err == nil {
		t.Fatalf("Expected exclusive lock collision error for Client 2, but it succeeded!")
	}
	t.Logf("Exclusive lock properly blocked Client 2: %v", err)

	// 6. Commit Super-Client updates: publishing Version 1.1
	ver, err := imageSvc.CommitSuperClient(client1.ID, "v1.1", "Added AutoCAD 2026 and CNC driver update", "admin")
	if err != nil {
		t.Fatalf("Failed to commit Super-Client: %v", err)
	}
	if ver.VersionTag != "v1.1" {
		t.Errorf("Expected tag v1.1, got %s", ver.VersionTag)
	}
	t.Logf("Sealed version %s with SHA256: %s", ver.VersionTag, ver.ChecksumSHA256)

	// Workstation 1 should no longer be in Super-Client mode
	postCommitClient1, _ := db.GetClientByID(client1.ID)
	if postCommitClient1.IsSuperClient {
		t.Errorf("Client 1 should no longer be in Super-Client mode after commit")
	}

	// 7. Test Discard Super-Client session on Workstation 2
	err = imageSvc.StartSuperClient(client2.ID, 15, "admin")
	if err != nil {
		t.Fatalf("Client 2 should now be able to acquire lock after Client 1 committed: %v", err)
	}
	err = imageSvc.DiscardSuperClient(client2.ID, "admin")
	if err != nil {
		t.Fatalf("Discard failed: %v", err)
	}
	postDiscardClient2, _ := db.GetClientByID(client2.ID)
	if postDiscardClient2.IsSuperClient {
		t.Errorf("Client 2 should not be in Super-Client mode after discard")
	}

	// 8. Test Rollback to prior version
	rollbackVer, err := imageSvc.RollbackToVersion(disk.ID, ver.ID, "admin")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	t.Logf("Successfully rolled back to %s (Snapshot: %s)", rollbackVer.VersionTag, rollbackVer.SnapshotPath)

	// 9. Test Unassign Disk
	err = imageSvc.UnassignDisk(client1.ID, "admin")
	if err != nil {
		t.Fatalf("Unassign disk failed: %v", err)
	}
	postUnassign, _ := db.GetDiskAssignment(client1.ID)
	if postUnassign != nil {
		t.Errorf("Expected nil assignment after unassign, got active assignment")
	}
}
