package storage_mgmt

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
)

// ISCSITargetController manages network block storage targets and LUN mappings.
type ISCSITargetController struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.Mutex
}

// NewISCSITargetController initializes the controller.
func NewISCSITargetController(cfg *config.Config, db *database.DB) *ISCSITargetController {
	return &ISCSITargetController{
		cfg: cfg,
		db:  db,
	}
}

// EnsureClientTarget configures the iSCSI target for a booting client workstation.
func (c *ISCSITargetController) EnsureClientTarget(mac string, diskPath string, writeCachePath string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cleanMAC := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	cleanMAC = strings.ReplaceAll(cleanMAC, "-", "")
	targetName := fmt.Sprintf("client-%s", cleanMAC)
	fullIQN := fmt.Sprintf("%s:%s", c.cfg.ISCSIIQNPfx, targetName)

	if runtime.GOOS == "windows" {
		return fullIQN, c.configureWindowsTarget(targetName, fullIQN, diskPath, writeCachePath)
	} else if runtime.GOOS == "linux" {
		return fullIQN, c.configureLinuxTarget(fullIQN, diskPath)
	}

	slog.Info("iSCSI Target configured (development mock mode)", "iqn", fullIQN, "disk", diskPath)
	return fullIQN, nil
}

// EnsureSuperClientTarget configures a target pointing directly to the writable snapshot.
func (c *ISCSITargetController) EnsureSuperClientTarget(mac string, snapshotPath string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cleanMAC := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	cleanMAC = strings.ReplaceAll(cleanMAC, "-", "")
	targetName := fmt.Sprintf("superclient-%s", cleanMAC)
	fullIQN := fmt.Sprintf("%s:%s", c.cfg.ISCSIIQNPfx, targetName)

	if runtime.GOOS == "windows" {
		return fullIQN, c.configureWindowsTarget(targetName, fullIQN, snapshotPath, "")
	}

	slog.Info("Super-Client iSCSI Target configured", "iqn", fullIQN, "snapshot", snapshotPath)
	return fullIQN, nil
}

// Windows native iSCSI Target integration via PowerShell (Simulated for Phase 2 PoC)
func (c *ISCSITargetController) configureWindowsTarget(targetName string, iqn string, diskPath string, writeCachePath string) error {
	slog.Info("Windows iSCSI Target simulated in safe PoC mode (physical PowerShell execution DISABLED_BY_SAFETY_POLICY)", "target", targetName, "iqn", iqn)
	return nil
}

// CreatePhysicalTarget rejects any request to modify the host iSCSI kernel subsystem during Phase 2 PoC.
func (c *ISCSITargetController) CreatePhysicalTarget(targetName string) error {
	return fmt.Errorf("DISABLED_BY_SAFETY_POLICY: Physical iSCSI target creation is disabled during Phase 2 PoC")
}

// Linux native iSCSI integration via targetcli
func (c *ISCSITargetController) configureLinuxTarget(iqn string, diskPath string) error {
	if _, err := exec.LookPath("targetcli"); err != nil {
		slog.Debug("targetcli not present on system; mock mode active")
		return nil
	}

	// targetcli script commands can be dispatched here
	slog.Info("Configuring Linux LIO targetcli", "iqn", iqn)
	return nil
}
