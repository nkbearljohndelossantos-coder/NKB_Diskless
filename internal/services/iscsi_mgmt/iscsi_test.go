package iscsi_mgmt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/iscsi_mgmt"
)

func TestISCSITargetManagement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_iscsi_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"
	cfg.ISCSIPortal = "192.168.99.1"
	cfg.ISCSIPort = 3260
	cfg.ISCSIIQNPfx = "iqn.2026-03.com.nkb"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	mgr := iscsi_mgmt.NewTargetManager(cfg, db)

	// 1. Create a Virtual Disk
	disk := &models.VirtualDisk{
		Name:        "Win11 Production Base",
		Description: "Base OS image",
		FilePath:    filepath.Join(tempDir, "disks", "win11.vhdx"),
		SizeBytes:   40 * 1024 * 1024 * 1024,
		DiskFormat:  "VHDX",
		IsGolden:    true,
	}
	if err := db.CreateDisk(disk); err != nil {
		t.Fatalf("failed to register disk: %v", err)
	}

	// 2. Create a Client Workstation
	client := &models.Client{
		Hostname:       "NKB-STATION-01",
		MACAddress:     "00:11:22:33:44:55",
		IPAddress:      "192.168.99.101",
		AssignedDiskID: &disk.ID,
		Status:         "online",
		BootMode:       "UEFI",
		WriteCacheMode: "volatile",
	}
	if err := db.CreateClient(client); err != nil {
		t.Fatalf("failed to register client: %v", err)
	}

	// 3. Test Normal Boot Provisioning
	bootCfg, err := mgr.ProvisionClientTarget(client.ID)
	if err != nil {
		t.Fatalf("failed to provision target: %v", err)
	}

	expectedIQN := "iqn.2026-03.com.nkb:client-001122334455"
	if bootCfg.TargetIQN != expectedIQN {
		t.Errorf("expected IQN %s, got %s", expectedIQN, bootCfg.TargetIQN)
	}
	if bootCfg.PortalIP != "192.168.99.1" || bootCfg.PortalPort != 3260 {
		t.Errorf("unexpected portal endpoint: %s:%d", bootCfg.PortalIP, bootCfg.PortalPort)
	}

	// 4. Test Super-Client Mode Target
	_ = db.SetSuperClient(client.ID, true, 30)
	superCfg, err := mgr.ProvisionClientTarget(client.ID)
	if err != nil {
		t.Fatalf("failed to provision super client target: %v", err)
	}
	expectedSuperIQN := "iqn.2026-03.com.nkb:super-001122334455"
	if superCfg.TargetIQN != expectedSuperIQN {
		t.Errorf("expected Super IQN %s, got %s", expectedSuperIQN, superCfg.TargetIQN)
	}

	// 5. Test NDIS Reset Stability Registry Patch
	regPatch := mgr.GenerateNDISRegistryFix()
	if !strings.Contains(regPatch, "TcpWindowSize") || !strings.Contains(regPatch, "RestoreConnection") {
		t.Errorf("NDIS registry patch missing required TCP/IP stabilization parameters")
	}

	// 6. Test Deprovisioning Target
	if err := mgr.DeprovisionClientTarget(client.ID); err != nil {
		t.Fatalf("failed to deprovision target: %v", err)
	}
}
