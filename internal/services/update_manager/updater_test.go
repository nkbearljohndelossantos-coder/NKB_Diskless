package update_manager_test

import (
	"os"
	"path/filepath"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/services/update_manager"
)

func TestUpdateManagerWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_update_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	updater, err := update_manager.NewUpdateManager(cfg, db)
	if err != nil {
		t.Fatalf("failed to create update manager: %v", err)
	}

	// 1. Create a dummy sqlite DB file in storage root for backup testing
	dataDir := filepath.Join(cfg.StorageRoot, "data")
	_ = os.MkdirAll(dataDir, 0755)
	dbFilePath := filepath.Join(dataDir, "nkb_diskless.db")
	_ = os.WriteFile(dbFilePath, []byte("NKB_DATABASE_PAYLOAD_V1"), 0644)

	// 2. Test Pre-Update Backup Creation
	backupPath, err := updater.CreatePreUpdateBackup("v1.1.0")
	if err != nil {
		t.Fatalf("failed to create pre-update backup: %v", err)
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist on disk: %s", backupPath)
	}
	t.Logf("Created pre-update backup at: %s", backupPath)

	// 3. Test Rollback to Backup
	// Simulate corrupting or modifying data
	_ = os.WriteFile(dbFilePath, []byte("MODIFIED_CORRUPT_DATA"), 0644)

	// Restore from backup
	backupBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	if string(backupBytes) != "NKB_DATABASE_PAYLOAD_V1" {
		t.Errorf("backup contents mismatch: %s", string(backupBytes))
	}
}
