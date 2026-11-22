package pxe_boot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/pxe_boot"
)

func TestDynamicScriptGeneration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_pxe_test_*")
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
	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("..", "database", "migrations")
	}
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		// Absolute path fallback
		cwd, _ := os.Getwd()
		t.Logf("cwd: %s", cwd)
	}
	err = db.Migrate(migrationsDir)
	if err != nil {
		t.Fatalf("failed to migrate db in test: %v", err)
	}

	// Create a test disk
	disk := &models.VirtualDisk{
		Name:       "Production Line Windows 10 Image",
		FilePath:   filepath.Join(tempDir, "disks", "prod10.vhdx"),
		SizeBytes:  50000000000,
		DiskFormat: "VHDX",
		IsGolden:   true,
	}
	_ = db.CreateDisk(disk)

	// Create test client
	client := &models.Client{
		MACAddress:     "AA:BB:CC:DD:EE:FF",
		IPAddress:      "192.168.1.50",
		Hostname:       "LINE-01-STATION",
		AssignedDiskID: &disk.ID,
		Status:         "online",
		BootMode:       "UEFI",
		WriteCacheMode: "volatile",
	}
	_ = db.CreateClient(client)

	req := pxe_boot.BootRequest{
		MAC:         "AA:BB:CC:DD:EE:FF",
		IP:          "192.168.1.50",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.1.10",
		HTTPPort:    8080,
		ISCSIPort:   3260,
		ISCSIPrefix: "iqn.2026-11.com.nkb",
	}

	// 1. Test Normal Boot Script
	script, err := pxe_boot.GenerateScript(req, db)
	if err != nil {
		t.Fatalf("failed to generate script: %v", err)
	}
	if !strings.Contains(script, "LINE-01-STATION") {
		t.Errorf("expected script to contain hostname 'LINE-01-STATION'")
	}
	if !strings.Contains(script, "sanboot") || !strings.Contains(script, "iqn.2026-11.com.nkb:client-aabbccddeeff") {
		t.Errorf("expected script to contain proper sanboot command with client target IQN")
	}

	// 2. Test Super-Client Mode Script
	_ = db.SetSuperClient(client.ID, true, 60)
	superScript, err := pxe_boot.GenerateScript(req, db)
	if err != nil {
		t.Fatalf("failed to generate super client script: %v", err)
	}
	if !strings.Contains(superScript, "SUPER-CLIENT MAINTENANCE MODE") {
		t.Errorf("expected super client banner in script")
	}
	if !strings.Contains(superScript, "iqn.2026-11.com.nkb:superclient-aabbccddeeff") {
		t.Errorf("expected super client target IQN in script")
	}

	// 3. Test Unregistered Computer Script
	unregisteredReq := pxe_boot.BootRequest{
		MAC:         "11:22:33:44:55:66",
		IP:          "192.168.1.99",
		Platform:    "efi",
		Arch:        "x86_64",
		ServerIP:    "192.168.1.10",
		HTTPPort:    8080,
		ISCSIPort:   3260,
		ISCSIPrefix: "iqn.2026-11.com.nkb",
	}
	unregScript, err := pxe_boot.GenerateScript(unregisteredReq, db)
	if err != nil {
		t.Fatalf("failed to generate unreg script: %v", err)
	}
	if !strings.Contains(unregScript, "UNREGISTERED WORKSTATION") {
		t.Errorf("expected unregistered station prompt in script")
	}
}
