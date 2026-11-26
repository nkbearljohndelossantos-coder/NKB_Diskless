package iscsi_mgmt

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
)

// SessionInfo represents an active iSCSI block session.
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	InitiatorIQN string    `json:"initiator_iqn"`
	InitiatorIP  string    `json:"initiator_ip"`
	TargetIQN    string    `json:"target_iqn"`
	ConnectedAt  time.Time `json:"connected_at"`
	IsSuperMode  bool      `json:"is_super_mode"`
}

// ClientBootConfig aggregates all parameters required for a workstation to boot via iSCSI.
type ClientBootConfig struct {
	ClientID       string `json:"client_id"`
	Hostname       string `json:"hostname"`
	MACAddress     string `json:"mac_address"`
	TargetIQN      string `json:"target_iqn"`
	PortalIP       string `json:"portal_ip"`
	PortalPort     int    `json:"portal_port"`
	LUN            int    `json:"lun"`
	DiskImagePath  string `json:"disk_image_path"`
	WriteCachePath string `json:"write_cache_path"`
	IsSuperClient  bool   `json:"is_super_client"`
}

// TargetManager orchestrates kernel iSCSI target services (Microsoft iSCSI Target Server / Linux LIO).
// Crucially, it manages the control plane; raw block I/O is handled by the kernel driver.
type TargetManager struct {
	cfg *config.Config
	db  *database.DB
	mu  sync.RWMutex
}

// NewTargetManager constructs an iSCSI target manager instance.
func NewTargetManager(cfg *config.Config, db *database.DB) *TargetManager {
	return &TargetManager{
		cfg: cfg,
		db:  db,
	}
}

// BuildTargetIQN formats a compliant iSCSI Qualified Name for a workstation.
func (tm *TargetManager) BuildTargetIQN(mac string, isSuperClient bool) string {
	cleanMAC := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	prefix := tm.cfg.ISCSIIQNPfx
	if prefix == "" {
		prefix = "iqn.2026-03.com.nkb"
	}
	if isSuperClient {
		return fmt.Sprintf("%s:super-%s", prefix, cleanMAC)
	}
	return fmt.Sprintf("%s:client-%s", prefix, cleanMAC)
}

// ProvisionClientTarget configures a target LUN mapping for a registered workstation.
func (tm *TargetManager) ProvisionClientTarget(clientID string) (*ClientBootConfig, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	client, err := tm.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return nil, fmt.Errorf("client not found: %s", clientID)
	}

	if client.AssignedDiskID == nil || *client.AssignedDiskID == "" {
		return nil, fmt.Errorf("workstation %s has no assigned virtual disk", client.Hostname)
	}

	disk, err := tm.db.GetDiskByID(*client.AssignedDiskID)
	if err != nil || disk == nil {
		return nil, fmt.Errorf("assigned disk %s not found", *client.AssignedDiskID)
	}

	targetIQN := tm.BuildTargetIQN(client.MACAddress, client.IsSuperClient)
	cleanMAC := strings.ReplaceAll(client.MACAddress, ":", "")
	cachePath := fmt.Sprintf("%s/wc_%s.delta", tm.cfg.CacheDir, cleanMAC)

	portalIP := tm.cfg.ISCSIPortal
	if portalIP == "" {
		portalIP = tm.cfg.BindAddress
		if portalIP == "" || portalIP == "0.0.0.0" {
			portalIP = "127.0.0.1"
		}
	}

	bootConfig := &ClientBootConfig{
		ClientID:       client.ID,
		Hostname:       client.Hostname,
		MACAddress:     client.MACAddress,
		TargetIQN:      targetIQN,
		PortalIP:       portalIP,
		PortalPort:     tm.cfg.ISCSIPort,
		LUN:            0,
		DiskImagePath:  disk.FilePath,
		WriteCachePath: cachePath,
		IsSuperClient:  client.IsSuperClient,
	}

	// For Phase 2 PoC: Physical target provisioning via PowerShell/kernel driver is strictly disabled by safety policy.
	// Target control plane runs entirely in simulated mode to protect host environment.
	slog.Info("iSCSI Target mapping provisioned in simulated mode (physical PowerShell execution DISABLED_BY_SAFETY_POLICY for Phase 2 PoC)",
		"client", client.Hostname, "target", targetIQN, "initiator_ip", client.IPAddress)

	return bootConfig, nil
}

// DeprovisionClientTarget dismantles the target mapping when workstation unassigns or shuts down.
func (tm *TargetManager) DeprovisionClientTarget(clientID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	client, err := tm.db.GetClientByID(clientID)
	if err != nil || client == nil {
		return fmt.Errorf("client not found")
	}

	targetIQN := tm.BuildTargetIQN(client.MACAddress, client.IsSuperClient)

	// For Phase 2 PoC: Physical target removal via PowerShell is disabled by safety policy.
	slog.Info("Deprovisioned iSCSI Target in simulated mode (physical execution DISABLED_BY_SAFETY_POLICY)", "client", client.Hostname, "iqn", targetIQN)
	return nil
}

// CreatePhysicalTarget rejects any request to modify the host iSCSI kernel subsystem during Phase 2 PoC.
func (tm *TargetManager) CreatePhysicalTarget(targetName string) error {
	return fmt.Errorf("DISABLED_BY_SAFETY_POLICY: Physical iSCSI target creation is disabled during Phase 2 PoC")
}

// GenerateNDISRegistryFix produces the registry script required to prevent Windows kernel
// bugcheck 0x7B (INACCESSIBLE_BOOT_DEVICE) caused by NIC miniport link resets during initialization.
func (tm *TargetManager) GenerateNDISRegistryFix() string {
	return `Windows Registry Editor Version 5.00

; NKB Diskless System - NDIS Link Reset Stability Patch
; Prevents BugCheck 0x7B during kernel TCP network driver re-initialization

[HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\NetworkProvider]
"RestoreConnection"=dword:00000000

[HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters]
"TcpWindowSize"=dword:00040000
"GlobalMaxTcpWindowSize"=dword:00040000
"Tcp1323Opts"=dword:00000001
"DisableTaskOffload"=dword:00000000

[HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Session Manager\Memory Management]
"PagingFiles"=hex(7):00,00
`
}
