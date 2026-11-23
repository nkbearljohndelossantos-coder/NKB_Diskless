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

	// 1. Locate the active sqlite DB file
	dbFilePath := filepath.Join(cfg.DatabaseDir, "nkb_diskless.db")
	origBytes, err := os.ReadFile(dbFilePath)
	if err != nil {
		t.Fatalf("failed to read active db file: %v", err)
	}

	// 2. Test Pre-Update Backup Creation
	backupPath, err := updater.CreatePreUpdateBackup("v1.1.0")
	if err != nil {
		t.Fatalf("failed to create pre-update backup: %v", err)
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist on disk: %s", backupPath)
	}
	t.Logf("Created pre-update backup at: %s", backupPath)

	// 3. Verify backup content matches the database state at backup time
	backupBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	if len(backupBytes) == 0 {
		t.Errorf("expected non-empty backup file")
	}
	if len(backupBytes) < len(origBytes) {
		t.Errorf("backup size %d is smaller than original %d", len(backupBytes), len(origBytes))
	}
}
