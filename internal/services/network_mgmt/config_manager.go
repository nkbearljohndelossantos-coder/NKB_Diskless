package network_mgmt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NetworkConfigManager handles loading, saving, schema validation, versioning,
// backups, rollback, transaction journaling, and adapter reconciliation for network profiles.
type NetworkConfigManager struct {
	storageRoot string
	configFile  string
	backupDir   string
	snapshotDir string
	scriptsDir  string
	journal     *TransactionJournal
	mu          sync.RWMutex
}

// NewNetworkConfigManager constructs a manager for network configurations.
func NewNetworkConfigManager(storageRoot string) (*NetworkConfigManager, error) {
	if storageRoot == "" {
		storageRoot = `C:\NKB-PoC\`
	}
	configDir := filepath.Join(storageRoot, "config")
	backupDir := filepath.Join(configDir, "backups")
	snapshotDir := filepath.Join(configDir, "snapshots")
	scriptsDir := filepath.Join(configDir, "scripts")

	for _, dir := range []string{backupDir, snapshotDir, scriptsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	journal, err := NewTransactionJournal(storageRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transaction journal: %w", err)
	}

	return &NetworkConfigManager{
		storageRoot: storageRoot,
		configFile:  filepath.Join(configDir, "network_profile.json"),
		backupDir:   backupDir,
		snapshotDir: snapshotDir,
		scriptsDir:  scriptsDir,
		journal:     journal,
	}, nil
}

// Journal returns the active transaction journal manager.
func (m *NetworkConfigManager) Journal() *TransactionJournal {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.journal
}

// ConfigFile returns the path to the network profile JSON file.
func (m *NetworkConfigManager) ConfigFile() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.configFile
}

// SnapshotDir returns the path to the Windows network snapshots directory.
func (m *NetworkConfigManager) SnapshotDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotDir
}

// ScriptsDir returns the path to generated staging and rollback scripts.
func (m *NetworkConfigManager) ScriptsDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scriptsDir
}

// DefaultProfile returns a safe initial network configuration profile.
func DefaultProfile(storageRoot string) *NetworkProfile {
	return &NetworkProfile{
		Version:     1,
		ProfileID:   "profile_default",
		ProfileName: "NKB Zero-Purchase Isolated Staging",
		NetworkMode: ModeIsolatedStandalone,
		SelectedAdapter: AdapterMatch{
			PersistentKey: "",
			Alias:         "Ethernet",
			Description:   "",
			Index:         0,
			GUID:          "",
			MAC:           "",
		},
		IPv4Config: IPv4Config{
			IPAddress:      "192.168.50.1",
			PrefixLength:   24,
			DefaultGateway: "",
			DNSServers:     []string{},
		},
		DHCPConfig: DHCPConfig{
			Enabled:              true,
			PoolStart:            "192.168.50.100",
			PoolEnd:              "192.168.50.200",
			LeaseDurationSeconds: 120, // 2-minute diagnostic lease
			SubnetMask:           "255.255.255.0",
		},
		PXEConfig: PXEConfig{
			EnableStandaloneDHCP: false, // Safe default: stopped
			EnableProxyDHCP:     false,
			EnableTFTP:          false,
			EnableHTTPBoot:      true,
			TFTPPort:            69,
			ProxyDHCPPort:       4011,
			HTTPBootPort:        8080,
			BIOSBootloader:      "bios/undionly.kpxe",
			UEFIBootloader:      "uefi/ipxe.efi",
		},
		StorageRoot:      storageRoot,
		Status:           StatusDiscovered,
		LastVerification: "NOT_TESTED",
		LastUpdatedAt:    time.Now().UTC(),
	}
}

// LoadProfile reads the persistent network configuration or returns safe defaults without overwriting.
func (m *NetworkConfigManager) LoadProfile() (*NetworkProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, err := os.Stat(m.configFile); os.IsNotExist(err) {
		// File does not exist yet; return default without overwriting
		return DefaultProfile(m.storageRoot), nil
	}

	data, err := os.ReadFile(m.configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read network profile: %w", err)
	}

	var profile NetworkProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("network profile json malformed: %w", err)
	}

	// Schema migration if needed
	if profile.Version < 1 {
		profile.Version = 1
	}

	return &profile, nil
}

// SaveProfile creates a timestamped backup, validates the schema, and saves atomically.
func (m *NetworkConfigManager) SaveProfile(profile *NetworkProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile.LastUpdatedAt = time.Now().UTC()

	// 1. Create backup if current file exists
	if _, err := os.Stat(m.configFile); err == nil {
		existingData, _ := os.ReadFile(m.configFile)
		timestamp := time.Now().Format("20060102_150405")
		backupPath := filepath.Join(m.backupDir, fmt.Sprintf("network_profile_%s.json", timestamp))
		_ = os.WriteFile(backupPath, existingData, 0644)
	}

	// 2. Marshal and save atomically
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	tmpPath := m.configFile + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0644); err != nil {
		return fmt.Errorf("failed to write tmp config: %w", err)
	}

	if err := os.Rename(tmpPath, m.configFile); err != nil {
		return fmt.Errorf("failed to commit config file: %w", err)
	}

	return nil
}

// Rollback restores the most recent backup profile.
func (m *NetworkConfigManager) Rollback() (*NetworkProfile, error) {
	return m.RollbackProfile()
}

// RollbackProfile restores the most recent backup profile JSON.
func (m *NetworkConfigManager) RollbackProfile() (*NetworkProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.backupDir)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("no configuration backups available for rollback")
	}

	var backupFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "network_profile_") && strings.HasSuffix(e.Name(), ".json") {
			backupFiles = append(backupFiles, filepath.Join(m.backupDir, e.Name()))
		}
	}

	if len(backupFiles) == 0 {
		return nil, fmt.Errorf("no valid backup files found in %s", m.backupDir)
	}

	sort.Strings(backupFiles)
	latestBackup := backupFiles[len(backupFiles)-1]

	data, err := os.ReadFile(latestBackup)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup %s: %w", latestBackup, err)
	}

	var profile NetworkProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("backup corrupted: %w", err)
	}

	// Overwrite active configuration file with restored backup
	if err := os.WriteFile(m.configFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to restore backup to active config: %w", err)
	}

	return &profile, nil
}

// BuildSnapshotFromAdapter extracts live Windows network settings into a WindowsAdapterSnapshot model.
func (m *NetworkConfigManager) BuildSnapshotFromAdapter(a *NetworkAdapter) *WindowsAdapterSnapshot {
	now := time.Now().UTC()
	snapID := fmt.Sprintf("snap_%s_%s", a.ID, now.Format("20060102_150405"))

	var ipEntries []IPv4AddressEntry
	for _, ipStr := range a.IPv4Addresses {
		parts := strings.Split(ipStr, "/")
		ip := parts[0]
		prefix := 24
		if len(parts) > 1 {
			if p, err := strconv.Atoi(parts[1]); err == nil {
				prefix = p
			}
		}
		ipEntries = append(ipEntries, IPv4AddressEntry{
			IPAddress:    ip,
			PrefixLength: prefix,
		})
	}

	var ip6Entries []IPv6AddressEntry
	for _, addr := range a.IPv6Addresses {
		parts := strings.Split(addr, "/")
		ip := parts[0]
		prefix := 64
		if len(parts) > 1 {
			if p, err := strconv.Atoi(parts[1]); err == nil {
				prefix = p
			}
		}
		ip6Entries = append(ip6Entries, IPv6AddressEntry{
			IPAddress:    ip,
			PrefixLength: prefix,
		})
	}

	var routes []RouteEntry
	var routeMetrics []RouteMetricEntry
	for _, gw := range a.DefaultGateways {
		clean := strings.TrimSpace(gw)
		if clean != "" && clean != "0.0.0.0" {
			routes = append(routes, RouteEntry{
				DestinationPrefix: "0.0.0.0/0",
				NextHop:           clean,
				RouteMetric:       10,
				AddressFamily:     "IPv4",
			})
			routeMetrics = append(routeMetrics, RouteMetricEntry{
				DestinationPrefix: "0.0.0.0/0",
				Metric:            10,
			})
		}
	}

	category := a.NetworkCategory
	if category == "" {
		category = "Private"
	}
	hostIdentity := os.Getenv("COMPUTERNAME")
	if hostIdentity == "" {
		hostIdentity, _ = os.Hostname()
	}

	snap := &WindowsAdapterSnapshot{
		SnapshotID:                     snapID,
		Timestamp:                      now,
		PersistentKey:                  a.ID,
		InterfaceGUID:                  a.InterfaceGUID,
		InterfaceIndex:                 a.InterfaceIndex,
		InterfaceAlias:                 a.InterfaceAlias,
		MACAddress:                     a.MACAddress,
		HardwareDescription:            a.InterfaceDescription,
		Description:                    a.InterfaceDescription,
		Status:                         a.Status,
		DhcpEnabled:                    a.DHCPEnabled,
		Dhcpv4Enabled:                  a.DHCPEnabled,
		Dhcpv6Enabled:                  a.DHCPv6Enabled,
		IPv4Addresses:                  ipEntries,
		IPv6Addresses:                  a.IPv6Addresses,
		IPv6AddressesDetailed:          ip6Entries,
		DefaultGateways:                a.DefaultGateways,
		IPv4DefaultGateways:            a.DefaultGateways,
		IPv6DefaultGateways:            []string{},
		Routes:                         routes,
		RouteMetrics:                   routeMetrics,
		InterfaceMetricIPv4:            a.InterfaceMetricIPv4,
		InterfaceMetricIPv6:            a.InterfaceMetricIPv6,
		AutomaticMetricIPv4:            a.AutomaticMetricIPv4,
		AutomaticMetricIPv6:            a.AutomaticMetricIPv6,
		DNSServers:                     a.DNSServers,
		IPv4DNSServers:                 a.DNSServers,
		IPv6DNSServers:                 []string{},
		DNSFromDHCP:                    a.DHCPEnabled,
		DNSConfigurationSource:         a.DNSConfigurationSource,
		RegisterThisConnectionsAddress: a.RegisterThisConnectionsAddress,
		NetworkCategory:                category,
		IsSynthetic:                    false,
		Provenance:                     "LIVE_WINDOWS_CIM",
		HostIdentity:                   hostIdentity,
	}

	snap.IntegrityHash = ComputeSnapshotHash(snap)
	return snap
}

// ValidateSnapshotIntegrity inspects a WindowsAdapterSnapshot for required fields and checksum consistency.
func (m *NetworkConfigManager) ValidateSnapshotIntegrity(snap *WindowsAdapterSnapshot) error {
	if snap == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if strings.TrimSpace(snap.SnapshotID) == "" {
		return fmt.Errorf("missing snapshot ID")
	}
	if strings.TrimSpace(snap.PersistentKey) == "" {
		return fmt.Errorf("missing persistent key")
	}
	if strings.TrimSpace(snap.MACAddress) == "" {
		return fmt.Errorf("missing MAC address")
	}
	if strings.TrimSpace(snap.InterfaceGUID) == "" {
		return fmt.Errorf("missing interface GUID")
	}
	if !snap.Dhcpv4Enabled && len(snap.IPv4Addresses) == 0 {
		return fmt.Errorf("invalid static configuration snapshot: no IPv4 addresses recorded")
	}
	if snap.IntegrityHash != "" {
		expected := ComputeSnapshotHash(snap)
		if snap.IntegrityHash != expected {
			return fmt.Errorf("snapshot integrity check failed: payload checksum mismatch (corrupted snapshot)")
		}
	}
	return nil
}

// ValidateSnapshotForRollback verifies that a snapshot is safe to use for host restoration.
// It strictly rejects synthetic fixtures, missing identifiers, checksum corruption, and foreign host snapshots.
func (m *NetworkConfigManager) ValidateSnapshotForRollback(snap *WindowsAdapterSnapshot) error {
	if snap == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if snap.IsSynthetic || snap.Provenance == "SYNTHETIC_TEST_FIXTURE" {
		return fmt.Errorf("synthetic snapshot fixture cannot be used for live host rollback (provenance: %s)", snap.Provenance)
	}
	if strings.TrimSpace(snap.IntegrityHash) == "" {
		return fmt.Errorf("snapshot missing mandatory integrity checksum (hash required for rollback)")
	}
	expectedHash := ComputeSnapshotHash(snap)
	if snap.IntegrityHash != expectedHash {
		return fmt.Errorf("snapshot integrity verification failed: payload checksum mismatch (expected %s, found %s)", expectedHash, snap.IntegrityHash)
	}
	currentHost := os.Getenv("COMPUTERNAME")
	if currentHost != "" && snap.HostIdentity != "" {
		hostPrefix := strings.Split(snap.HostIdentity, "|")[0]
		if !strings.EqualFold(hostPrefix, currentHost) && !strings.EqualFold(snap.HostIdentity, currentHost) {
			return fmt.Errorf("snapshot host identity mismatch: snapshot belongs to %q but current host is %q", snap.HostIdentity, currentHost)
		}
	}
	return m.ValidateSnapshotIntegrity(snap)
}

// VerifyRestoration compares the restored adapter against the reference snapshot.
func (m *NetworkConfigManager) VerifyRestoration(snap *WindowsAdapterSnapshot, current *NetworkAdapter) (bool, []string) {
	var mismatches []string

	if current == nil {
		return false, []string{"current adapter is nil"}
	}

	// 1. Verify IPv4 DHCP mode
	if snap.Dhcpv4Enabled != current.DHCPEnabled {
		mismatches = append(mismatches, fmt.Sprintf("IPv4 DHCP state mismatch: expected %t, found %t", snap.Dhcpv4Enabled, current.DHCPEnabled))
	}

	// 2. Verify IPv6 DHCP mode (when snap has explicit IPv6 DHCP recorded)
	if snap.Dhcpv6Enabled && !current.DHCPv6Enabled {
		mismatches = append(mismatches, fmt.Sprintf("IPv6 DHCP state mismatch: expected %t, found %t", snap.Dhcpv6Enabled, current.DHCPv6Enabled))
	}

	// 3. If static IPv4, verify all addresses and prefixes
	if !snap.Dhcpv4Enabled {
		for _, exp := range snap.IPv4Addresses {
			found := false
			for _, curr := range current.IPv4Addresses {
				parts := strings.Split(curr, "/")
				if parts[0] == exp.IPAddress {
					found = true
					break
				}
			}
			if !found {
				mismatches = append(mismatches, fmt.Sprintf("Missing expected static IPv4 address: %s", exp.IPAddress))
			}
		}
	}

	// 4. If static IPv6, verify detailed addresses
	if !snap.Dhcpv6Enabled && len(snap.IPv6AddressesDetailed) > 0 {
		for _, expV6 := range snap.IPv6AddressesDetailed {
			found := false
			for _, currV6 := range current.IPv6Addresses {
				parts := strings.Split(currV6, "/")
				if strings.EqualFold(parts[0], expV6.IPAddress) {
					found = true
					break
				}
			}
			if !found {
				mismatches = append(mismatches, fmt.Sprintf("Missing expected static IPv6 address: %s", expV6.IPAddress))
			}
		}
	}

	// 5. Verify default gateways
	for _, expGW := range snap.IPv4DefaultGateways {
		if strings.TrimSpace(expGW) == "" || expGW == "0.0.0.0" {
			continue
		}
		found := false
		for _, currGW := range current.DefaultGateways {
			if strings.TrimSpace(currGW) == expGW {
				found = true
				break
			}
		}
		if !found {
			mismatches = append(mismatches, fmt.Sprintf("Missing expected default gateway: %s", expGW))
		}
	}

	// 6. Verify DNS servers
	for _, expDNS := range snap.IPv4DNSServers {
		found := false
		for _, currDNS := range current.DNSServers {
			if strings.TrimSpace(currDNS) == expDNS {
				found = true
				break
			}
		}
		if !found {
			mismatches = append(mismatches, fmt.Sprintf("Missing expected DNS server: %s", expDNS))
		}
	}

	// 7. Verify Dynamic DNS registration
	if snap.RegisterThisConnectionsAddress != current.RegisterThisConnectionsAddress {
		mismatches = append(mismatches, fmt.Sprintf("Dynamic DNS registration mismatch: expected %t, found %t", snap.RegisterThisConnectionsAddress, current.RegisterThisConnectionsAddress))
	}

	// 8. Verify NetworkCategory if set
	if snap.NetworkCategory != "" && current.NetworkCategory != "" && !strings.EqualFold(snap.NetworkCategory, current.NetworkCategory) {
		mismatches = append(mismatches, fmt.Sprintf("Network category mismatch: expected %s, found %s", snap.NetworkCategory, current.NetworkCategory))
	}

	// 9. Verify Interface Metric (if explicitly configured > 0)
	if snap.InterfaceMetricIPv4 > 0 && current.InterfaceMetricIPv4 > 0 && snap.InterfaceMetricIPv4 != current.InterfaceMetricIPv4 {
		mismatches = append(mismatches, fmt.Sprintf("IPv4 interface metric mismatch: expected %d, found %d", snap.InterfaceMetricIPv4, current.InterfaceMetricIPv4))
	}

	return len(mismatches) == 0, mismatches
}

// SaveWindowsSnapshot persists the Windows network snapshot to config/snapshots and updates rollback scripts.
func (m *NetworkConfigManager) SaveWindowsSnapshot(snap *WindowsAdapterSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if snap.IntegrityHash == "" {
		snap.IntegrityHash = ComputeSnapshotHash(snap)
	}

	if err := m.ValidateSnapshotIntegrity(snap); err != nil {
		return fmt.Errorf("snapshot integrity error: %w", err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	snapFile := filepath.Join(m.snapshotDir, fmt.Sprintf("%s.json", snap.SnapshotID))
	tmpFile := snapFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot temp file: %w", err)
	}
	if err := os.Rename(tmpFile, snapFile); err != nil {
		return fmt.Errorf("failed to commit snapshot: %w", err)
	}

	// Also generate and save the corresponding rollback script
	rollbackScript := m.GenerateRollbackScript(snap)
	rollbackPath := filepath.Join(m.scriptsDir, "rollback_adapter.ps1")
	_ = os.WriteFile(rollbackPath, []byte(rollbackScript), 0644)

	// Save an archived copy with timestamp
	archivedRollback := filepath.Join(m.snapshotDir, fmt.Sprintf("rollback_%s.ps1", snap.SnapshotID))
	_ = os.WriteFile(archivedRollback, []byte(rollbackScript), 0644)

	return nil
}

// ListWindowsSnapshots enumerates all saved Windows adapter snapshots.
func (m *NetworkConfigManager) ListWindowsSnapshots() ([]WindowsAdapterSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.snapshotDir)
	if err != nil {
		return nil, err
	}

	var snapshots []WindowsAdapterSnapshot
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "snap_") && strings.HasSuffix(e.Name(), ".json") {
			filePath := filepath.Join(m.snapshotDir, e.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			var s WindowsAdapterSnapshot
			if err := json.Unmarshal(data, &s); err == nil {
				snapshots = append(snapshots, s)
			}
		}
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp.After(snapshots[j].Timestamp)
	})

	return snapshots, nil
}

// GetLatestWindowsSnapshot returns the newest snapshot matching persistentKey (or newest overall).
func (m *NetworkConfigManager) GetLatestWindowsSnapshot(persistentKey string) (*WindowsAdapterSnapshot, error) {
	snapshots, err := m.ListWindowsSnapshots()
	if err != nil || len(snapshots) == 0 {
		return nil, fmt.Errorf("no Windows adapter snapshots found")
	}

	for _, s := range snapshots {
		if persistentKey == "" || s.PersistentKey == persistentKey {
			return &s, nil
		}
	}

	return &snapshots[0], nil
}

// PreflightStaging executes complete non-destructive pre-flight verification before any staging script is run.
func (m *NetworkConfigManager) PreflightStaging(p *NetworkProfile, adapters []NetworkAdapter) (*PreflightResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := &PreflightResult{
		Eligible: false,
		Status:   StatusBlocked,
		Checks:   []PreflightCheck{},
	}

	// 1. Locate target adapter in live inventory
	var target *NetworkAdapter
	for i, a := range adapters {
		if (p.SelectedAdapter.PersistentKey != "" && a.ID == p.SelectedAdapter.PersistentKey) ||
			(p.SelectedAdapter.MAC != "" && strings.EqualFold(a.MACAddress, p.SelectedAdapter.MAC)) ||
			(p.SelectedAdapter.Alias != "" && a.InterfaceAlias == p.SelectedAdapter.Alias) {
			target = &adapters[i]
			break
		}
	}

	if target == nil {
		result.Status = StatusMissing
		result.Message = fmt.Sprintf("Target adapter '%s' (MAC: %s) is physically missing from the system.",
			p.SelectedAdapter.Alias, p.SelectedAdapter.MAC)
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Hardware Discovery",
			Passed:  false,
			Blocked: true,
			Detail:  result.Message,
		})
		p.Status = StatusMissing
		return result, nil
	}

	result.AdapterAlias = target.InterfaceAlias
	result.AdapterMAC = target.MACAddress
	result.AdapterGUID = target.InterfaceGUID
	result.CurrentRole = target.SafetyRole

	result.Checks = append(result.Checks, PreflightCheck{
		Name:    "Hardware Verification",
		Passed:  true,
		Blocked: false,
		Detail:  fmt.Sprintf("Matched %s (%s, GUID: %s, Index: %d)", target.InterfaceAlias, target.MACAddress, target.InterfaceGUID, target.InterfaceIndex),
	})

	// 2. Hardware Eligibility Check
	if target.IsWireless {
		result.Status = StatusBlocked
		result.Message = fmt.Sprintf("Adapter '%s' is a wireless (Wi-Fi) interface. Network boot staging strictly prohibited.", target.InterfaceAlias)
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Physical Ethernet Verification",
			Passed:  false,
			Blocked: true,
			Detail:  result.Message,
		})
		p.Status = StatusBlocked
		return result, nil
	}

	if target.SafetyRole == RoleVirtualBlocked {
		result.Status = StatusBlocked
		result.Message = fmt.Sprintf("Adapter '%s' is a virtual or loopback adapter. Physical PXE boot requires physical hardware.", target.InterfaceAlias)
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Physical Hardware Verification",
			Passed:  false,
			Blocked: true,
			Detail:  result.Message,
		})
		p.Status = StatusBlocked
		return result, nil
	}

	result.Checks = append(result.Checks, PreflightCheck{
		Name:    "Physical Hardware Verification",
		Passed:  true,
		Blocked: false,
		Detail:  "Physical Gigabit Ethernet adapter confirmed.",
	})

	// 3. Active Default Route & Management Independence Check
	hasTargetDefaultGateway := false
	for _, gw := range target.DefaultGateways {
		if strings.TrimSpace(gw) != "" && strings.TrimSpace(gw) != "0.0.0.0" {
			hasTargetDefaultGateway = true
			result.ActiveGateway = gw
			break
		}
	}
	result.HasActiveDefaultRoute = hasTargetDefaultGateway

	// Policy check: Require separate management NIC
	if p.ManagementPolicy.RequireSeparateManagementNIC {
		altMgmt := FindActiveManagementAdapter(adapters, target.ID, target.MACAddress, target.InterfaceAlias)
		if altMgmt == nil {
			result.Status = StatusBlocked
			result.Eligible = false
			result.ManagementRouteSecured = false
			result.Message = "Management safety policy requires a separate active management adapter (e.g. Wi-Fi or secondary Ethernet). None found."
			result.Checks = append(result.Checks, PreflightCheck{
				Name:    "Management Safety Policy Enforcement",
				Passed:  false,
				Blocked: true,
				Detail:  result.Message,
			})
			p.Status = StatusBlocked
			p.ManagementRouteSecured = false
			return result, nil
		}
	}

	if hasTargetDefaultGateway {
		// Check if another active adapter secures host management
		altMgmt := FindActiveManagementAdapter(adapters, target.ID, target.MACAddress, target.InterfaceAlias)
		if altMgmt == nil {
			result.Status = StatusBlocked
			result.Eligible = false
			result.ManagementRouteSecured = false
			result.Message = fmt.Sprintf("Target adapter '%s' currently holds the host's sole default gateway (%s). Staging is BLOCKED because removing this route would sever server internet and management connectivity. Transition requires a separate active management connection (e.g. Wi-Fi or secondary Ethernet adapter).",
				target.InterfaceAlias, result.ActiveGateway)
			result.Checks = append(result.Checks, PreflightCheck{
				Name:    "Management Route Independence",
				Passed:  false,
				Blocked: true,
				Detail:  result.Message,
			})
			p.Status = StatusBlocked
			p.ManagementRouteSecured = false
			return result, nil
		}

		result.ManagementRouteSecured = true
		result.ManagementInterfaceAlias = altMgmt.InterfaceAlias
		p.ManagementRouteSecured = true
		p.AlternateManagementNIC = altMgmt.InterfaceAlias
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Management Route Independence",
			Passed:  true,
			Blocked: false,
			Detail:  fmt.Sprintf("Alternate default gateway confirmed on management interface '%s' (%s). Host connectivity will remain secured.", altMgmt.InterfaceAlias, strings.Join(altMgmt.DefaultGateways, ", ")),
		})
	} else {
		result.ManagementRouteSecured = true
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Management Route Independence",
			Passed:  true,
			Blocked: false,
			Detail:  "Target adapter does not hold host default gateway. Air-gap isolation compliant.",
		})
	}

	// 4. Subnet Overlap & IP Conflict Check
	targetCIDR := fmt.Sprintf("%s/%d", p.IPv4Config.IPAddress, p.IPv4Config.PrefixLength)
	overlapDetected := false
	var overlapDetails string
	for _, a := range adapters {
		if a.ID == target.ID || strings.EqualFold(a.MACAddress, target.MACAddress) {
			continue
		}
		for _, addr := range a.IPv4Addresses {
			if SubnetsOverlap(targetCIDR, addr) {
				overlapDetected = true
				overlapDetails = fmt.Sprintf("Proposed test subnet %s overlaps with active interface '%s' (%s - %s)",
					targetCIDR, a.InterfaceAlias, a.InterfaceDescription, addr)
				break
			}
		}
		if overlapDetected {
			break
		}
	}

	result.SubnetOverlapDetected = overlapDetected
	result.SubnetOverlapDetails = overlapDetails

	if overlapDetected {
		result.Status = StatusBlocked
		result.Message = overlapDetails
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Subnet Non-Interference Check",
			Passed:  false,
			Blocked: true,
			Detail:  overlapDetails,
		})
		p.Status = StatusBlocked
		return result, nil
	}

	result.Checks = append(result.Checks, PreflightCheck{
		Name:    "Subnet Non-Interference Check",
		Passed:  true,
		Blocked: false,
		Detail:  fmt.Sprintf("Subnet %s is isolated and does not conflict with any host interface.", targetCIDR),
	})

	// 5. DHCP Pool & Addressing Validation
	report, err := m.ValidateProfile(p, adapters)
	if err != nil || report.HasConflict {
		result.Status = StatusError
		result.Message = fmt.Sprintf("Profile configuration validation failed: %v", report.Errors)
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "Addressing & DHCP Pool Validation",
			Passed:  false,
			Blocked: true,
			Detail:  strings.Join(report.Errors, "; "),
		})
		p.Status = StatusError
		return result, nil
	}

	result.Checks = append(result.Checks, PreflightCheck{
		Name:    "Addressing & DHCP Pool Validation",
		Passed:  true,
		Blocked: false,
		Detail:  fmt.Sprintf("Server IP %s and DHCP pool %s - %s verified inside /%d subnet.", p.IPv4Config.IPAddress, p.DHCPConfig.PoolStart, p.DHCPConfig.PoolEnd, p.IPv4Config.PrefixLength),
	})

	// All Preflight checks succeeded
	result.Eligible = true
	result.Status = StatusReadyToStage
	result.Message = fmt.Sprintf("Preflight validation PASSED. Adapter '%s' (%s) is fully eligible for transactional staging to %s.",
		target.InterfaceAlias, target.MACAddress, targetCIDR)
	p.Status = StatusReadyToStage

	return result, nil
}

// ValidateProfile validates network profile parameters and detects conflicts with host adapters.
func (m *NetworkConfigManager) ValidateProfile(p *NetworkProfile, adapters []NetworkAdapter) (*ConflictReport, error) {
	report := &ConflictReport{
		Warnings: []string{},
		Errors:   []string{},
	}

	// 1. Validate Server IP & Prefix
	ip := net.ParseIP(p.IPv4Config.IPAddress).To4()
	if ip == nil {
		report.Errors = append(report.Errors, fmt.Sprintf("Invalid server IPv4 address: %q", p.IPv4Config.IPAddress))
	}
	if p.IPv4Config.PrefixLength < 8 || p.IPv4Config.PrefixLength > 30 {
		report.Errors = append(report.Errors, fmt.Sprintf("Invalid prefix length /%d (must be between 8 and 30)", p.IPv4Config.PrefixLength))
	}

	serverCIDR := fmt.Sprintf("%s/%d", p.IPv4Config.IPAddress, p.IPv4Config.PrefixLength)
	_, serverNet, _ := net.ParseCIDR(serverCIDR)

	// 2. Validate Gateway in Isolated Mode
	if p.NetworkMode == ModeIsolatedStandalone && strings.TrimSpace(p.IPv4Config.DefaultGateway) != "" {
		report.Warnings = append(report.Warnings, "Gateway configured in isolated mode. Air-gap isolation requires zero default gateway.")
		report.GatewayConflict = true
	}

	// 3. Validate DHCP Pool
	if p.DHCPConfig.Enabled {
		startIP := net.ParseIP(p.DHCPConfig.PoolStart).To4()
		endIP := net.ParseIP(p.DHCPConfig.PoolEnd).To4()

		if startIP == nil {
			report.Errors = append(report.Errors, fmt.Sprintf("Invalid DHCP pool start IP: %q", p.DHCPConfig.PoolStart))
		}
		if endIP == nil {
			report.Errors = append(report.Errors, fmt.Sprintf("Invalid DHCP pool end IP: %q", p.DHCPConfig.PoolEnd))
		}

		if startIP != nil && endIP != nil {
			startVal := binary.BigEndian.Uint32(startIP)
			endVal := binary.BigEndian.Uint32(endIP)

			if startVal > endVal {
				report.Errors = append(report.Errors, fmt.Sprintf("DHCP pool start (%s) cannot be greater than pool end (%s)", p.DHCPConfig.PoolStart, p.DHCPConfig.PoolEnd))
			}

			// Verify pool is inside Server Subnet
			if serverNet != nil {
				if !serverNet.Contains(startIP) {
					report.Errors = append(report.Errors, fmt.Sprintf("DHCP pool start %s is outside server subnet %s", p.DHCPConfig.PoolStart, serverCIDR))
				}
				if !serverNet.Contains(endIP) {
					report.Errors = append(report.Errors, fmt.Sprintf("DHCP pool end %s is outside server subnet %s", p.DHCPConfig.PoolEnd, serverCIDR))
				}
			}

			// Ensure Server IP is NOT within the client DHCP pool
			if ip != nil {
				serverVal := binary.BigEndian.Uint32(ip)
				if serverVal >= startVal && serverVal <= endVal {
					report.Errors = append(report.Errors, fmt.Sprintf("IP Conflict: Server IP %s cannot be inside the DHCP dynamic lease pool (%s - %s)", p.IPv4Config.IPAddress, p.DHCPConfig.PoolStart, p.DHCPConfig.PoolEnd))
					report.IPConflict = true
				}
			}
		}
	}

	// 4. Overlap & Conflict Check against Other Host Adapters
	if serverNet != nil {
		for _, a := range adapters {
			// Skip the selected test adapter itself
			isCurrentSelected := false
			if p.SelectedAdapter.MAC != "" && strings.EqualFold(a.MACAddress, p.SelectedAdapter.MAC) {
				isCurrentSelected = true
			} else if p.SelectedAdapter.Alias != "" && a.InterfaceAlias == p.SelectedAdapter.Alias {
				isCurrentSelected = true
			}
			if isCurrentSelected {
				continue
			}

			// Check if another adapter shares this subnet or server IP
			for _, cidr := range a.IPv4Addresses {
				if SubnetsOverlap(serverCIDR, cidr) {
					report.OverlappingSubnet = true
					msg := fmt.Sprintf("Subnet Collision: Proposed test subnet %s overlaps with active adapter '%s' (%s - %s)",
						serverCIDR, a.InterfaceAlias, a.InterfaceDescription, cidr)
					if a.IsWireless || a.IsPrimaryInternet {
						report.Errors = append(report.Errors, "[CRITICAL] "+msg)
					} else {
						report.Warnings = append(report.Warnings, msg)
					}
				}

				// Check exact IP duplicate
				parts := strings.Split(cidr, "/")
				if len(parts) > 0 && parts[0] == p.IPv4Config.IPAddress {
					report.IPConflict = true
					report.Errors = append(report.Errors, fmt.Sprintf("[FATAL] IP %s is already assigned to adapter '%s'", p.IPv4Config.IPAddress, a.InterfaceAlias))
				}
			}
		}
	}

	// 5. Selected Adapter Safety Check
	if p.SelectedAdapter.Alias != "" || p.SelectedAdapter.MAC != "" {
		for _, a := range adapters {
			isTarget := false
			if p.SelectedAdapter.MAC != "" && strings.EqualFold(a.MACAddress, p.SelectedAdapter.MAC) {
				isTarget = true
			} else if p.SelectedAdapter.Alias != "" && a.InterfaceAlias == p.SelectedAdapter.Alias {
				isTarget = true
			}
			if isTarget {
				if a.IsWireless {
					report.ProtectedAdapter = true
					report.Errors = append(report.Errors, fmt.Sprintf("Safety Violation: Adapter '%s' is a wireless interface. Cannot be adopted for PXE boot.", a.InterfaceAlias))
				} else if a.SafetyRole == RoleProtectedInternet && !p.ManagementRouteSecured {
					report.ProtectedAdapter = true
					report.Errors = append(report.Errors, fmt.Sprintf("Safety Violation: Adapter '%s' holds an active default gateway (%s). Transition requires a verified alternate management route.", a.InterfaceAlias, strings.Join(a.DefaultGateways, ", ")))
				}
				if a.SafetyRole == RoleVirtualBlocked {
					report.ProtectedAdapter = true
					report.Errors = append(report.Errors, fmt.Sprintf("Safety Violation: Adapter '%s' is a virtual adapter. Physical PXE boot requires physical hardware.", a.InterfaceAlias))
				}
				break
			}
		}
	}

	report.HasConflict = len(report.Errors) > 0

	if len(report.Errors) > 0 {
		return report, fmt.Errorf("configuration validation failed with %d error(s)", len(report.Errors))
	}
	return report, nil
}

// ReconcileAdapter inspects current discovered adapters against the profile's adopted adapter.
// Updates runtime index/alias if changed, or sets status to MISSING if physically absent.
func (m *NetworkConfigManager) ReconcileAdapter(p *NetworkProfile, adapters []NetworkAdapter) (status string, matched *NetworkAdapter) {
	if p.SelectedAdapter.PersistentKey == "" && p.SelectedAdapter.MAC == "" && p.SelectedAdapter.Alias == "" {
		p.Status = StatusDiscovered
		return p.Status, nil
	}

	// 1. Try matching by Persistent Key (MAC + GUID + Description)
	for i, a := range adapters {
		if a.ID == p.SelectedAdapter.PersistentKey {
			p.SelectedAdapter.Index = a.InterfaceIndex
			p.SelectedAdapter.Alias = a.InterfaceAlias
			p.SelectedAdapter.GUID = a.InterfaceGUID
			p.SelectedAdapter.MAC = a.MACAddress
			p.SelectedAdapter.Description = a.InterfaceDescription
			p.Status = StatusSelected
			return p.Status, &adapters[i]
		}
	}

	// 2. Try matching by normalized MAC Address (survives alias renames and driver reinstalls)
	for i, a := range adapters {
		if p.SelectedAdapter.MAC != "" && strings.EqualFold(a.MACAddress, p.SelectedAdapter.MAC) {
			p.SelectedAdapter.PersistentKey = a.ID
			p.SelectedAdapter.Index = a.InterfaceIndex
			p.SelectedAdapter.Alias = a.InterfaceAlias
			p.SelectedAdapter.GUID = a.InterfaceGUID
			p.SelectedAdapter.Description = a.InterfaceDescription
			p.Status = StatusSelected
			return p.Status, &adapters[i]
		}
	}

	// 3. Adapter physically missing
	p.Status = StatusMissing
	return p.Status, nil
}

// AdoptAdapter binds a newly selected adapter into the profile.
func (m *NetworkConfigManager) AdoptAdapter(p *NetworkProfile, newAdapter NetworkAdapter) error {
	if newAdapter.SafetyRole == RoleProtectedInternet || newAdapter.IsWireless {
		return fmt.Errorf("cannot adopt protected corporate/wireless adapter '%s'", newAdapter.InterfaceAlias)
	}
	if newAdapter.SafetyRole == RoleVirtualBlocked {
		return fmt.Errorf("cannot adopt virtual adapter '%s' for physical PXE testing", newAdapter.InterfaceAlias)
	}

	p.SelectedAdapter = AdapterMatch{
		PersistentKey: newAdapter.ID,
		Alias:         newAdapter.InterfaceAlias,
		Description:   newAdapter.InterfaceDescription,
		Index:         newAdapter.InterfaceIndex,
		GUID:          newAdapter.InterfaceGUID,
		MAC:           newAdapter.MACAddress,
	}
	p.Status = StatusSelected
	return m.SaveProfile(p)
}

// GenerateStagingPreview constructs a side-by-side diff preview of current vs proposed adapter settings.
func (m *NetworkConfigManager) GenerateStagingPreview(p *NetworkProfile, adapter *NetworkAdapter, conflicts ConflictReport) StagingPreview {
	preview := StagingPreview{
		AdapterName:     p.SelectedAdapter.Alias,
		AdapterMAC:      p.SelectedAdapter.MAC,
		AdapterDesc:     p.SelectedAdapter.Description,
		ProposedIPv4:    fmt.Sprintf("%s/%d", p.IPv4Config.IPAddress, p.IPv4Config.PrefixLength),
		ProposedGateway: p.IPv4Config.DefaultGateway,
		ProposedDNS:     p.IPv4Config.DNSServers,
		ProposedPool:    fmt.Sprintf("%s - %s", p.DHCPConfig.PoolStart, p.DHCPConfig.PoolEnd),
		NetworkMode:     p.NetworkMode,
		Conflicts:       conflicts,
	}

	if adapter != nil {
		preview.AdapterName = adapter.InterfaceAlias
		preview.AdapterMAC = adapter.MACAddress
		preview.AdapterDesc = adapter.InterfaceDescription
		preview.CurrentIPv4 = adapter.IPv4Addresses
		preview.CurrentGateway = adapter.DefaultGateways
		preview.CurrentDNS = adapter.DNSServers
	}

	return preview
}

// GenerateRollbackScript generates a standalone PowerShell script that restores an adapter
// from a saved WindowsAdapterSnapshot.
func (m *NetworkConfigManager) GenerateRollbackScript(snap *WindowsAdapterSnapshot) string {
	buf := new(bytes.Buffer)
	buf.WriteString("# ==============================================================================\n")
	buf.WriteString("# NKB DISKLESS - WINDOWS NETWORK RESTORATION SCRIPT (ROLLBACK)\n")
	buf.WriteString(fmt.Sprintf("# Snapshot ID: %s | Captured: %s\n", snap.SnapshotID, snap.Timestamp.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("# Target Adapter: %s (%s, MAC: %s)\n", snap.InterfaceAlias, snap.Description, snap.MACAddress))
	buf.WriteString("# Configuration-Driven, Snapshot-Driven, Parameterized\n")
	buf.WriteString("# Requires Elevated Administrator Privileges\n")
	buf.WriteString("# ==============================================================================\n\n")

	// param block MUST be the very first statement in PowerShell
	buf.WriteString("[CmdletBinding()]\n")
	buf.WriteString("param(\n")
	buf.WriteString(fmt.Sprintf("    [Parameter(Mandatory=$false)]\n    [string]$SnapshotPath = \"config/snapshots/%s.json\",\n", snap.SnapshotID))
	buf.WriteString("    [Parameter(Mandatory=$false)]\n    [switch]$AllowSyntheticFixture\n")
	buf.WriteString(")\n\n")

	buf.WriteString("$ErrorActionPreference = 'Stop'\n\n")

	// Reference snapshot metadata payload (retained for verification and audit trail, but never silently substituted)
	snapJsonBytes, _ := json.MarshalIndent(snap, "", "  ")
	buf.WriteString(fmt.Sprintf("# Snapshot reference payload (audit trail):\n$embeddedSnapshotJson = @'\n%s\n'@\n\n", string(snapJsonBytes)))

	buf.WriteString("Write-Host '======================================================================' -ForegroundColor Cyan\n")
	buf.WriteString("Write-Host ' NKB DISKLESS CONFIGURATION-DRIVEN ROLLBACK' -ForegroundColor Cyan\n")
	buf.WriteString("Write-Host '======================================================================' -ForegroundColor Cyan\n\n")

	buf.WriteString("# Locate snapshot file on disk (fail-closed if missing)\n")
	buf.WriteString("$snap = $null\n")
	buf.WriteString("if ($SnapshotPath -and (Test-Path $SnapshotPath)) {\n")
	buf.WriteString("    Write-Host \"Loading snapshot from: $SnapshotPath\" -ForegroundColor White\n")
	buf.WriteString("    try {\n")
	buf.WriteString("        $rawJson = Get-Content -Raw -Path $SnapshotPath -ErrorAction Stop\n")
	buf.WriteString("        $snap = $rawJson | ConvertFrom-Json\n")
	buf.WriteString("    } catch {\n")
	buf.WriteString("        Write-Host \"[ROLLBACK_FAILED] Corrupted snapshot file: $_\" -ForegroundColor Red\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("    }\n")
	buf.WriteString("} else {\n")
	buf.WriteString("    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path\n")
	buf.WriteString("    $candidateDirs = @(\n")
	buf.WriteString("        $scriptDir,\n")
	buf.WriteString("        (Join-Path $scriptDir '..\\snapshots'),\n")
	buf.WriteString("        'config\\snapshots',\n")
	buf.WriteString("        'C:\\NKB-PoC\\config\\snapshots'\n")
	buf.WriteString("    )\n")
	buf.WriteString("    $foundPath = $null\n")
	buf.WriteString("    if ($SnapshotPath) {\n")
	buf.WriteString("        $leaf = Split-Path -Leaf $SnapshotPath\n")
	buf.WriteString("        foreach ($d in $candidateDirs) {\n")
	buf.WriteString("            $candidate = Join-Path $d $leaf\n")
	buf.WriteString("            if (Test-Path $candidate) {\n")
	buf.WriteString("                $foundPath = $candidate\n")
	buf.WriteString("                break\n")
	buf.WriteString("            }\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if ($foundPath) {\n")
	buf.WriteString("        Write-Host \"Loading snapshot from discovered path: $foundPath\" -ForegroundColor White\n")
	buf.WriteString("        $snap = (Get-Content -Raw -Path $foundPath) | ConvertFrom-Json\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("        Write-Host \"[ROLLBACK_FAILED] Snapshot file not found at '$SnapshotPath' and no matching snapshot found in candidate directories.\" -ForegroundColor Red\n")
	buf.WriteString("        Write-Host \"Rollback is fail-closed. Staging baseline cannot be restored without a valid snapshot.\" -ForegroundColor Red\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Safety Check 1: Provenance and synthetic fixture rejection\n")
	buf.WriteString("if (($snap.is_synthetic -or $snap.provenance -eq 'SYNTHETIC_TEST_FIXTURE') -and -not $AllowSyntheticFixture) {\n")
	buf.WriteString("    Write-Host \"[ROLLBACK_FAILED] Snapshot is marked as SYNTHETIC_TEST_FIXTURE. Production rollback refused. Use -AllowSyntheticFixture only in isolated tests.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Safety Check 2: Mandatory integrity hash verification\n")
	buf.WriteString("if (-not $snap.integrity_hash) {\n")
	buf.WriteString("    Write-Host \"[ROLLBACK_FAILED] Snapshot is missing mandatory integrity checksum (integrity_hash). Rollback refused.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Safety Check 3: Host identity cross-system verification\n")
	buf.WriteString("if ($snap.host_identity -and $snap.host_identity -ne $env:COMPUTERNAME) {\n")
	buf.WriteString("    $snapHostPrefix = ($snap.host_identity -split '\\|')[0]\n")
	buf.WriteString("    if ($snapHostPrefix -ne $env:COMPUTERNAME) {\n")
	buf.WriteString("        Write-Host \"[ROLLBACK_FAILED] Snapshot host identity mismatch: snapshot belongs to '$($snap.host_identity)' but current host is '$($env:COMPUTERNAME)'. Rollback refused.\" -ForegroundColor Red\n")
	buf.WriteString("        exit 1\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Re-identify target adapter by MAC or Interface GUID (Fail-closed on 0 or >1 matches)\n")
	buf.WriteString("Write-Host \"`n[1/4] Re-identifying target adapter (MAC: $($snap.mac_address), GUID: $($snap.interface_guid))...\" -ForegroundColor Cyan\n")
	buf.WriteString("$adapters = Get-NetAdapter -ErrorAction SilentlyContinue\n")
	buf.WriteString("$targetMacClean = ($snap.mac_address -replace '[-:]', '').ToUpper()\n")
	buf.WriteString("$matchedAdapters = @()\n")
	buf.WriteString("foreach ($a in $adapters) {\n")
	buf.WriteString("    $currMac = ($a.MacAddress -replace '[-:]', '').ToUpper()\n")
	buf.WriteString("    if ($currMac -eq $targetMacClean -or ($snap.interface_guid -and $a.InterfaceGuid -eq $snap.interface_guid)) {\n")
	buf.WriteString("        $matchedAdapters += $a\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("if ($matchedAdapters.Count -eq 0) {\n")
	buf.WriteString("    Write-Host \"[ROLLBACK_FAILED] Target adapter not found (Expected MAC: $($snap.mac_address), GUID: $($snap.interface_guid)).\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n")
	buf.WriteString("if ($matchedAdapters.Count -gt 1) {\n")
	buf.WriteString("    Write-Host \"[ROLLBACK_FAILED] Ambiguous adapter match! $($matchedAdapters.Count) adapters matched MAC $($snap.mac_address) or GUID $($snap.interface_guid). Refusing modification to prevent misconfiguration.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n\n")

	buf.WriteString("$adapter = $matchedAdapters[0]\n")
	buf.WriteString("$currIdx = $adapter.InterfaceIndex\n")
	buf.WriteString("$currAlias = $adapter.Name\n")
	buf.WriteString("Write-Host \"Matched adapter: '$currAlias' (InterfaceIndex: $currIdx, Status: $($adapter.Status))\" -ForegroundColor Green\n\n")

	buf.WriteString("Write-Host \"`n[2/4] Clearing staged static IP addresses and default routes on '$currAlias'...\" -ForegroundColor Cyan\n")
	buf.WriteString("Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue\n")
	buf.WriteString("Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false\n\n")

	buf.WriteString("Write-Host \"`n[3/4] Restoring original Windows configuration from snapshot...\" -ForegroundColor Cyan\n")
	buf.WriteString("$isDhcp = [bool]$snap.dhcpv4_enabled\n")
	buf.WriteString("if ($null -eq $snap.dhcpv4_enabled) { $isDhcp = [bool]$snap.dhcp_enabled }\n\n")

	buf.WriteString("if ($isDhcp) {\n")
	buf.WriteString("    Write-Host \" - Enabling DHCP dynamic configuration on '$currAlias'...\" -ForegroundColor White\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Enabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("    Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ResetServerAddresses -ErrorAction SilentlyContinue\n")
	buf.WriteString("} else {\n")
	buf.WriteString("    Write-Host \" - Restoring static IP configuration on '$currAlias'...\" -ForegroundColor White\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Disabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("    if ($snap.ipv4_addresses) {\n")
	buf.WriteString("        foreach ($ip in $snap.ipv4_addresses) {\n")
	buf.WriteString("            $prefix = if ($ip.prefix_length) { $ip.prefix_length } else { 24 }\n")
	buf.WriteString("            New-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $ip.ip_address -PrefixLength $prefix -ErrorAction SilentlyContinue | Out-Null\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    $gws = @()\n")
	buf.WriteString("    if ($snap.ipv4_default_gateways) { $gws = @($snap.ipv4_default_gateways) }\n")
	buf.WriteString("    elseif ($snap.default_gateways) { $gws = @($snap.default_gateways) }\n")
	buf.WriteString("    foreach ($gw in $gws) {\n")
	buf.WriteString("        if ($gw -and $gw -ne '0.0.0.0') {\n")
	buf.WriteString("            $gwMetric = 10\n")
	buf.WriteString("            if ($snap.route_metrics) {\n")
	buf.WriteString("                $mEntry = $snap.route_metrics | Where-Object { $_.destination_prefix -eq '0.0.0.0/0' } | Select-Object -First 1\n")
	buf.WriteString("                if ($mEntry -and $mEntry.metric) { $gwMetric = $mEntry.metric }\n")
	buf.WriteString("            }\n")
	buf.WriteString("            New-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -NextHop $gw -RouteMetric $gwMetric -ErrorAction SilentlyContinue | Out-Null\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if ($snap.routes) {\n")
	buf.WriteString("        foreach ($r in $snap.routes) {\n")
	buf.WriteString("            if ($r.destination_prefix -and $r.next_hop -and $r.destination_prefix -ne '0.0.0.0/0') {\n")
	buf.WriteString("                $mVal = if ($r.route_metric) { $r.route_metric } else { 10 }\n")
	buf.WriteString("                New-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix $r.destination_prefix -NextHop $r.next_hop -RouteMetric $mVal -ErrorAction SilentlyContinue | Out-Null\n")
	buf.WriteString("            }\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    $dns = @()\n")
	buf.WriteString("    if ($snap.ipv4_dns_servers) { $dns += @($snap.ipv4_dns_servers) }\n")
	buf.WriteString("    elseif ($snap.dns_servers) { $dns += @($snap.dns_servers) }\n")
	buf.WriteString("    if ($snap.ipv6_dns_servers) { $dns += @($snap.ipv6_dns_servers) }\n")
	buf.WriteString("    if ($dns.Count -gt 0) {\n")
	buf.WriteString("        Set-DnsClientServerAddress -InterfaceIndex $adapter.InterfaceIndex -ServerAddresses $dns -ErrorAction SilentlyContinue\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Restore IPv6 state\n")
	buf.WriteString("if ($snap.dhcpv6_enabled) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Enabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("} elseif ($snap.ipv6_addresses_detailed) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Disabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("    foreach ($v6 in $snap.ipv6_addresses_detailed) {\n")
	buf.WriteString("        New-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $v6.ip_address -PrefixLength $v6.prefix_length -ErrorAction SilentlyContinue | Out-Null\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Restore interface metrics\n")
	buf.WriteString("if ($snap.automatic_metric_ipv4) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -AutomaticMetric Enabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("} elseif ($snap.interface_metric_ipv4 -and $snap.interface_metric_ipv4 -gt 0) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -InterfaceMetric $snap.interface_metric_ipv4 -ErrorAction SilentlyContinue\n")
	buf.WriteString("}\n")
	buf.WriteString("if ($snap.automatic_metric_ipv6) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -AutomaticMetric Enabled -ErrorAction SilentlyContinue\n")
	buf.WriteString("} elseif ($snap.interface_metric_ipv6 -and $snap.interface_metric_ipv6 -gt 0) {\n")
	buf.WriteString("    Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -InterfaceMetric $snap.interface_metric_ipv6 -ErrorAction SilentlyContinue\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Restore Dynamic DNS registration\n")
	buf.WriteString("if ($null -ne $snap.register_this_connections_address) {\n")
	buf.WriteString("    Set-DnsClient -InterfaceIndex $adapter.InterfaceIndex -RegisterThisConnectionsAddress:$snap.register_this_connections_address -ErrorAction SilentlyContinue\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Restore Network Category\n")
	buf.WriteString("if ($snap.network_category) {\n")
	buf.WriteString("    Set-NetConnectionProfile -InterfaceIndex $adapter.InterfaceIndex -NetworkCategory $snap.network_category -ErrorAction SilentlyContinue\n")
	buf.WriteString("}\n\n")

	buf.WriteString("Write-Host \"`n[4/4] Verifying exact restoration against snapshot...\" -ForegroundColor Cyan\n")
	buf.WriteString("$verified = $true\n")
	buf.WriteString("$errs = @()\n")
	buf.WriteString("if ($isDhcp) {\n")
	buf.WriteString("    $check = Get-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue\n")
	buf.WriteString("    if (-not $check -or $check.Dhcp -ne 'Enabled') {\n")
	buf.WriteString("        $verified = $false\n")
	buf.WriteString("        $errs += 'DHCP state was not restored to Enabled.'\n")
	buf.WriteString("    }\n")
	buf.WriteString("} else {\n")
	buf.WriteString("    if ($snap.ipv4_addresses) {\n")
	buf.WriteString("        foreach ($expIP in $snap.ipv4_addresses) {\n")
	buf.WriteString("            $check = Get-NetIPAddress -InterfaceIndex $adapter.InterfaceIndex -IPAddress $expIP.ip_address -ErrorAction SilentlyContinue\n")
	buf.WriteString("            if (-not $check) {\n")
	buf.WriteString("                $verified = $false\n")
	buf.WriteString("                $errs += \"Static IP $($expIP.ip_address) not found on adapter.\"\n")
	buf.WriteString("            }\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if ($gws.Count -gt 0) {\n")
	buf.WriteString("        $gwCheck = Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue\n")
	buf.WriteString("        if (-not $gwCheck) {\n")
	buf.WriteString("            $verified = $false\n")
	buf.WriteString("            $errs += 'Default gateway route not found on adapter.'\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("if ($snap.network_category) {\n")
	buf.WriteString("    $catCheck = (Get-NetConnectionProfile -InterfaceIndex $adapter.InterfaceIndex -ErrorAction SilentlyContinue).NetworkCategory\n")
	buf.WriteString("    if ($catCheck -and $catCheck.ToString() -ne $snap.network_category) {\n")
	buf.WriteString("        $verified = $false\n")
	buf.WriteString("        $errs += \"NetworkCategory mismatch: expected $($snap.network_category), found $catCheck.\"\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("if ($verified) {\n")
	buf.WriteString("    Write-Host '======================================================================' -ForegroundColor Green\n")
	buf.WriteString("    Write-Host \" [PASS] Windows network adapter '$currAlias' restored successfully from snapshot.\" -ForegroundColor Green\n")
	buf.WriteString("    Write-Host '======================================================================' -ForegroundColor Green\n")
	buf.WriteString("    exit 0\n")
	buf.WriteString("} else {\n")
	buf.WriteString("    Write-Host \"[ROLLBACK_FAILED] Restoration verification failed:\" -ForegroundColor Red\n")
	buf.WriteString("    $errs | ForEach-Object { Write-Host \" - $_\" -ForegroundColor Red }\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n")

	return buf.String()
}

// GenerateElevatedStagingScript generates the complete transactional PowerShell staging script with preflight, snapshot, try/catch apply, and automated rollback.
func (m *NetworkConfigManager) GenerateElevatedStagingScript(p *NetworkProfile) string {
	buf := new(bytes.Buffer)
	buf.WriteString("# ==============================================================================\n")
	buf.WriteString(fmt.Sprintf("# NKB DISKLESS - TRANSACTIONAL NETWORK STAGING SCRIPT: %s\n", p.ProfileName))
	buf.WriteString(fmt.Sprintf("# Target Adapter: %s (%s, MAC: %s)\n", p.SelectedAdapter.Alias, p.SelectedAdapter.Description, p.SelectedAdapter.MAC))
	buf.WriteString(fmt.Sprintf("# Dedicated IP: %s/%d (Zero Default Gateway)\n", p.IPv4Config.IPAddress, p.IPv4Config.PrefixLength))
	buf.WriteString("# Configuration-Driven, Snapshot-Driven, Parameterized\n")
	buf.WriteString("# Requires Elevated Administrator Privileges\n")
	buf.WriteString("# ==============================================================================\n\n")

	buf.WriteString("[CmdletBinding()]\n")
	buf.WriteString("param(\n")
	buf.WriteString("    [Parameter(Mandatory=$false)]\n    [string]$ProfilePath = \"config/network_profile.json\",\n")
	buf.WriteString("    [Parameter(Mandatory=$false)]\n    [string]$SnapshotDir = \"config/snapshots\"\n")
	buf.WriteString(")\n\n")

	buf.WriteString("$ErrorActionPreference = 'Stop'\n\n")

	buf.WriteString(fmt.Sprintf("$alias = \"%s\"\n", p.SelectedAdapter.Alias))
	buf.WriteString(fmt.Sprintf("$mac = \"%s\"\n", p.SelectedAdapter.MAC))
	buf.WriteString(fmt.Sprintf("$guid = \"%s\"\n", p.SelectedAdapter.GUID))
	buf.WriteString(fmt.Sprintf("$targetIP = \"%s\"\n", p.IPv4Config.IPAddress))
	buf.WriteString(fmt.Sprintf("$prefixLength = %d\n", p.IPv4Config.PrefixLength))
	buf.WriteString(fmt.Sprintf("$persistentKey = \"%s\"\n\n", p.SelectedAdapter.PersistentKey))

	buf.WriteString("Write-Host '======================================================================' -ForegroundColor Cyan\n")
	buf.WriteString("Write-Host ' NKB DISKLESS TRANSACTIONAL STAGING' -ForegroundColor Cyan\n")
	buf.WriteString("Write-Host \" Target: $alias ($mac) -> $targetIP/$prefixLength\" -ForegroundColor Cyan\n")
	buf.WriteString("Write-Host '======================================================================' -ForegroundColor Cyan\n\n")

	// PHASE 1: PREFLIGHT
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("# PHASE 1: PREFLIGHT VALIDATION & MANAGEMENT ROUTE INDEPENDENCE CHECK\n")
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("Write-Host \"`n[PHASE 1/4] Preflight verification...\" -ForegroundColor White\n")
	buf.WriteString("$adapters = Get-NetAdapter -ErrorAction SilentlyContinue\n")
	buf.WriteString("$cleanTargetMac = ($mac -replace '[-:]', '').ToUpper()\n")
	buf.WriteString("$matchedAdapters = @()\n")
	buf.WriteString("foreach ($a in $adapters) {\n")
	buf.WriteString("    $currMac = ($a.MacAddress -replace '[-:]', '').ToUpper()\n")
	buf.WriteString("    if ($currMac -eq $cleanTargetMac -or ($guid -and $a.InterfaceGuid -eq $guid)) {\n")
	buf.WriteString("        $matchedAdapters += $a\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n\n")

	buf.WriteString("if ($matchedAdapters.Count -eq 0) {\n")
	buf.WriteString("    Write-Host \"[BLOCKED] Target adapter not found (MAC: $mac, GUID: $guid). Aborting.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n")
	buf.WriteString("if ($matchedAdapters.Count -gt 1) {\n")
	buf.WriteString("    Write-Host \"[BLOCKED] Ambiguous target adapter match! $($matchedAdapters.Count) adapters matched MAC $mac or GUID $guid. Fail-closed.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n")
	buf.WriteString("$adapter = $matchedAdapters[0]\n\n")

	buf.WriteString("# Reject wireless interfaces\n")
	buf.WriteString("if ($adapter.PhysicalMediaType -eq 'Native 802.11' -or $adapter.Name -like '*Wi-Fi*' -or $adapter.InterfaceDescription -like '*Wireless*') {\n")
	buf.WriteString("    Write-Host \"[BLOCKED] Target adapter '$($adapter.Name)' is a wireless interface. Staging prohibited.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 1\n")
	buf.WriteString("}\n\n")

	buf.WriteString("# Verify management route independence (Task 2 multi-point verification)\n")
	buf.WriteString("$targetDefaultRoutes = Get-NetRoute -InterfaceIndex $adapter.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue\n")
	buf.WriteString("$hasTargetGW = ($null -ne $targetDefaultRoutes -and $targetDefaultRoutes.Count -gt 0)\n")
	buf.WriteString("if ($hasTargetGW) {\n")
	buf.WriteString("    Write-Host \"Target adapter holds default gateway. Verifying alternate management route independence...\" -ForegroundColor Yellow\n")
	buf.WriteString("    $altCandidates = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object {\n")
	buf.WriteString("        $_.InterfaceIndex -ne $adapter.InterfaceIndex -and $_.Status -eq 'Up'\n")
	buf.WriteString("    }\n")
	buf.WriteString("    $validAlt = $null\n")
	buf.WriteString("    foreach ($cand in $altCandidates) {\n")
	buf.WriteString("        $candIPs = Get-NetIPAddress -InterfaceIndex $cand.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {\n")
	buf.WriteString("            $_.IPAddress -notlike '169.254.*' -and $_.IPAddress -ne '0.0.0.0' -and ($_.AddressState -eq 'Preferred' -or -not $_.AddressState)\n")
	buf.WriteString("        }\n")
	buf.WriteString("        if (-not $candIPs) { continue }\n\n")
	buf.WriteString("        $candRoutes = Get-NetRoute -InterfaceIndex $cand.InterfaceIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object {\n")
	buf.WriteString("            $_.NextHop -and $_.NextHop -ne '0.0.0.0'\n")
	buf.WriteString("        }\n")
	buf.WriteString("        if (-not $candRoutes) { continue }\n\n")
	buf.WriteString("        $probeTarget = $candRoutes[0].NextHop\n")
	buf.WriteString("        $reachable = $false\n")
	buf.WriteString("        try {\n")
	buf.WriteString("            $pingOk = Test-Connection -ComputerName $probeTarget -Count 2 -Quiet -ErrorAction SilentlyContinue\n")
	buf.WriteString("            if ($pingOk) {\n")
	buf.WriteString("                $reachable = $true\n")
	buf.WriteString("            } else {\n")
	buf.WriteString("                $neighbor = Get-NetNeighbor -InterfaceIndex $cand.InterfaceIndex -IPAddress $probeTarget -ErrorAction SilentlyContinue | Where-Object {\n")
	buf.WriteString("                    $_.State -ne 'Unreachable' -and $_.State -ne 'None'\n")
	buf.WriteString("                }\n")
	buf.WriteString("                if ($neighbor) { $reachable = $true }\n")
	buf.WriteString("            }\n")
	buf.WriteString("        } catch {\n")
	buf.WriteString("            $reachable = $false\n")
	buf.WriteString("        }\n\n")
	buf.WriteString("        if ($reachable) {\n")
	buf.WriteString("            $validAlt = $cand\n")
	buf.WriteString("            break\n")
	buf.WriteString("        }\n")
	buf.WriteString("    }\n\n")
	buf.WriteString("    if (-not $validAlt) {\n")
	buf.WriteString("        Write-Host \"[BLOCKED] Target adapter holds the host default gateway and no verified, reachable alternate management interface was found.\" -ForegroundColor Red\n")
	buf.WriteString("        Write-Host \"Staging is BLOCKED to prevent severing host internet connectivity.\" -ForegroundColor Red\n")
	buf.WriteString("        exit 2\n")
	buf.WriteString("    }\n")
	buf.WriteString("    Write-Host \"Alternate management interface '$($validAlt.Name)' (Index: $($validAlt.InterfaceIndex)) verified Up with reachable gateway. Host connectivity secured.\" -ForegroundColor Green\n")
	buf.WriteString("}\n\n")

	// PHASE 2: PERSISTENT SNAPSHOT
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("# PHASE 2: CAPTURE PERSISTENT WINDOWS CONFIGURATION SNAPSHOT (LIVE CIM)\n")
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("Write-Host \"`n[PHASE 2/4] Capturing persistent Windows adapter configuration snapshot...\" -ForegroundColor White\n")
	buf.WriteString("if (-not (Test-Path $SnapshotDir)) { New-Item -ItemType Directory -Path $SnapshotDir -Force | Out-Null }\n\n")

	buf.WriteString("$idx = $adapter.InterfaceIndex\n")
	buf.WriteString("$ipConf = Get-NetIPConfiguration -InterfaceIndex $idx -ErrorAction SilentlyContinue\n")
	buf.WriteString("$v4Int = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue\n")
	buf.WriteString("$v6Int = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv6 -ErrorAction SilentlyContinue\n")
	buf.WriteString("$connProfile = Get-NetConnectionProfile -InterfaceIndex $idx -ErrorAction SilentlyContinue\n")
	buf.WriteString("$dnsClient = Get-DnsClient -InterfaceIndex $idx -ErrorAction SilentlyContinue\n\n")

	buf.WriteString("$dhcpV4 = ($v4Int -and $v4Int.Dhcp -eq 'Enabled')\n")
	buf.WriteString("$dhcpV6 = ($v6Int -and $v6Int.Dhcp -eq 'Enabled')\n")
	buf.WriteString("$metric4 = if ($v4Int) { [int]$v4Int.InterfaceMetric } else { 0 }\n")
	buf.WriteString("$metric6 = if ($v6Int) { [int]$v6Int.InterfaceMetric } else { 0 }\n")
	buf.WriteString("$autoMetric4 = if ($v4Int) { [bool]($v4Int.AutomaticMetric -eq 'Enabled') } else { $false }\n")
	buf.WriteString("$autoMetric6 = if ($v6Int) { [bool]($v6Int.AutomaticMetric -eq 'Enabled') } else { $false }\n")
	buf.WriteString("$regDns = if ($dnsClient) { [bool]$dnsClient.RegisterThisConnectionsAddress } else { $true }\n")
	buf.WriteString("$netCat = if ($connProfile -and $connProfile.NetworkCategory) { [string]$connProfile.NetworkCategory } else { 'Private' }\n\n")

	buf.WriteString("$snapIPs = @()\n")
	buf.WriteString("if ($ipConf -and $ipConf.IPv4Address) {\n")
	buf.WriteString("    $snapIPs = @($ipConf.IPv4Address | ForEach-Object { @{ ip_address = $_.IPAddress; prefix_length = [int]$_.PrefixLength } })\n")
	buf.WriteString("}\n")
	buf.WriteString("$snapV6 = @()\n")
	buf.WriteString("if ($ipConf -and $ipConf.IPv6Address) {\n")
	buf.WriteString("    $snapV6 = @($ipConf.IPv6Address.IPAddress)\n")
	buf.WriteString("}\n")
	buf.WriteString("$snapGWs = @()\n")
	buf.WriteString("if ($ipConf -and $ipConf.IPv4DefaultGateway) {\n")
	buf.WriteString("    $snapGWs = @($ipConf.IPv4DefaultGateway.NextHop)\n")
	buf.WriteString("}\n")
	buf.WriteString("$snapDNS = @()\n")
	buf.WriteString("if ($ipConf -and $ipConf.DNSServer) {\n")
	buf.WriteString("    $snapDNS = @($ipConf.DNSServer.ServerAddresses)\n")
	buf.WriteString("}\n\n")

	buf.WriteString("$nowTimestamp = Get-Date -Format 'yyyyMMdd_HHmmss'\n")
	buf.WriteString("$snapId = \"snap_${persistentKey}_$nowTimestamp\"\n")
	buf.WriteString("$snapshotPath = Join-Path $SnapshotDir \"$snapId.json\"\n\n")

	buf.WriteString("$machineGuid = (Get-ItemProperty -Path 'HKLM:\\SOFTWARE\\Microsoft\\Cryptography' -ErrorAction SilentlyContinue).MachineGuid\n")
	buf.WriteString("$hostIdentity = if ($machineGuid) { \"$($env:COMPUTERNAME)|$machineGuid\" } else { $env:COMPUTERNAME }\n\n")

	buf.WriteString("$ipStrs = @()\n")
	buf.WriteString("if ($snapIPs) { $ipStrs = @($snapIPs | ForEach-Object { \"$($_.ip_address)/$($_.prefix_length)\" } | Sort-Object) }\n")
	buf.WriteString("$v6Sorted = @($snapV6 | Sort-Object)\n")
	buf.WriteString("$gwSorted = @($snapGWs | Sort-Object)\n")
	buf.WriteString("$dnsSorted = @($snapDNS | Sort-Object)\n")
	buf.WriteString("$cleanMac = ($adapter.MacAddress -replace '[-:]', '').ToUpper()\n")
	buf.WriteString("$guidUpper = if ($adapter.InterfaceGuid) { $adapter.InterfaceGuid.ToUpper() } else { '' }\n\n")

	buf.WriteString("$canonStr = \"ID:$snapId|KEY:$persistentKey|GUID:$guidUpper|MAC:$cleanMac|INDEX:$idx|DHCP4:$dhcpV4|DHCP6:$dhcpV6|IPS:$(-join ($ipStrs, ','))|IPV6:$(-join ($v6Sorted, ','))|GWS:$(-join ($gwSorted, ','))|DNS:$(-join ($dnsSorted, ','))|METRIC4:$metric4|METRIC6:$metric6|REGDNS:$regDns|CAT:$netCat|SYNTH:False|PROV:LIVE_WINDOWS_CIM|HOST:$hostIdentity\"\n")
	buf.WriteString("$hasher = [System.Security.Cryptography.SHA256]::Create()\n")
	buf.WriteString("$hashBytes = $hasher.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($canonStr))\n")
	buf.WriteString("$calcIntegrityHash = \"sha256:\" + (-join ($hashBytes | ForEach-Object { \"{0:x2}\" -f $_ }))\n\n")

	buf.WriteString("$snapshotObj = [PSCustomObject]@{\n")
	buf.WriteString("    snapshot_id = $snapId\n")
	buf.WriteString("    timestamp = (Get-Date).ToUniversalTime().ToString('o')\n")
	buf.WriteString("    persistent_key = $persistentKey\n")
	buf.WriteString("    interface_guid = $adapter.InterfaceGuid\n")
	buf.WriteString("    interface_index = $idx\n")
	buf.WriteString("    interface_alias = $adapter.Name\n")
	buf.WriteString("    mac_address = $adapter.MacAddress\n")
	buf.WriteString("    hardware_description = $adapter.InterfaceDescription\n")
	buf.WriteString("    description = $adapter.InterfaceDescription\n")
	buf.WriteString("    status = $adapter.Status\n")
	buf.WriteString("    dhcp_enabled = $dhcpV4\n")
	buf.WriteString("    dhcpv4_enabled = $dhcpV4\n")
	buf.WriteString("    dhcpv6_enabled = $dhcpV6\n")
	buf.WriteString("    ipv4_addresses = $snapIPs\n")
	buf.WriteString("    ipv6_addresses = $snapV6\n")
	buf.WriteString("    default_gateways = $snapGWs\n")
	buf.WriteString("    ipv4_default_gateways = $snapGWs\n")
	buf.WriteString("    ipv4_dns_servers = $snapDNS\n")
	buf.WriteString("    dns_servers = $snapDNS\n")
	buf.WriteString("    interface_metric_ipv4 = $metric4\n")
	buf.WriteString("    interface_metric_ipv6 = $metric6\n")
	buf.WriteString("    automatic_metric_ipv4 = $autoMetric4\n")
	buf.WriteString("    automatic_metric_ipv6 = $autoMetric6\n")
	buf.WriteString("    register_this_connections_address = $regDns\n")
	buf.WriteString("    network_category = $netCat\n")
	buf.WriteString("    is_synthetic = $false\n")
	buf.WriteString("    provenance = \"LIVE_WINDOWS_CIM\"\n")
	buf.WriteString("    host_identity = $hostIdentity\n")
	buf.WriteString("    integrity_hash = $calcIntegrityHash\n")
	buf.WriteString("}\n\n")

	buf.WriteString("$snapshotJson = $snapshotObj | ConvertTo-Json -Depth 5\n")
	buf.WriteString("$tmpFile = \"$snapshotPath.tmp\"\n")
	buf.WriteString("$snapshotJson | Set-Content -Path $tmpFile -Encoding UTF8 -Force\n")
	buf.WriteString("Move-Item -Path $tmpFile -Destination $snapshotPath -Force\n\n")

	buf.WriteString("# Verify snapshot integrity on disk\n")
	buf.WriteString("if (-not (Test-Path $snapshotPath) -or (Get-Item $snapshotPath).Length -eq 0) {\n")
	buf.WriteString("    Write-Host \"[BLOCKED] Snapshot file could not be written to disk! Aborting before changes.\" -ForegroundColor Red\n")
	buf.WriteString("    exit 2\n")
	buf.WriteString("}\n")
	buf.WriteString("Write-Host \"Persistent live CIM snapshot saved and verified: $snapshotPath\" -ForegroundColor Green\n\n")

	// PHASE 3: APPLY WITH TRY/CATCH AUTO-ROLLBACK
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("# PHASE 3: APPLY STAGING CONFIGURATION WITH TRANSACTIONAL ROLLBACK\n")
	buf.WriteString("# ------------------------------------------------------------------------------\n")
	buf.WriteString("Write-Host \"`n[PHASE 3/4] Applying static staging configuration...\" -ForegroundColor White\n")
	buf.WriteString("try {\n")
	buf.WriteString("    Write-Host \" - Clearing existing default routes on '$($adapter.Name)'...\" -ForegroundColor Gray\n")
	buf.WriteString("    Get-NetRoute -InterfaceIndex $idx -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue\n\n")

	buf.WriteString("    Write-Host \" - Clearing existing IPv4 addresses on '$($adapter.Name)'...\" -ForegroundColor Gray\n")
	buf.WriteString("    Get-NetIPAddress -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false\n\n")

	buf.WriteString("    Write-Host \" - Assigning dedicated static IP $targetIP/$prefixLength (Zero Gateway)...\" -ForegroundColor Gray\n")
	buf.WriteString("    # Enforce zero default gateway: -DefaultGateway $null (air-gap isolation requires zero default gateway)\n")
	buf.WriteString("    New-NetIPAddress -InterfaceIndex $idx -IPAddress $targetIP -PrefixLength $prefixLength -ErrorAction Stop | Out-Null\n\n")

	buf.WriteString("    Write-Host \" - Enforcing empty DNS and Private network profile...\" -ForegroundColor Gray\n")
	buf.WriteString("    Set-DnsClientServerAddress -InterfaceIndex $idx -ServerAddresses @() -ErrorAction SilentlyContinue\n")
	buf.WriteString("    Set-NetConnectionProfile -InterfaceIndex $idx -NetworkCategory Private -ErrorAction SilentlyContinue\n")
	buf.WriteString("    Clear-DnsClientCache -ErrorAction SilentlyContinue\n\n")

	// PHASE 4: VERIFICATION GATE
	buf.WriteString("    # ------------------------------------------------------------------------------\n")
	buf.WriteString("    # PHASE 4: VERIFICATION GATE\n")
	buf.WriteString("    # ------------------------------------------------------------------------------\n")
	buf.WriteString("    Write-Host \"`n[PHASE 4/4] Verifying applied configuration...\" -ForegroundColor White\n")
	buf.WriteString("    $verifiedIP = Get-NetIPAddress -InterfaceIndex $idx -IPAddress $targetIP -ErrorAction SilentlyContinue\n")
	buf.WriteString("    $verifiedGW = Get-NetRoute -InterfaceIndex $idx -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue\n\n")

	buf.WriteString("    if (-not $verifiedIP) {\n")
	buf.WriteString("        throw \"Target static IP $targetIP/$prefixLength could not be verified on '$($adapter.Name)'.\"\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if ($verifiedGW) {\n")
	buf.WriteString("        throw \"Default gateway violation: isolated network mode requires zero default gateway.\"\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    Write-Host '`n======================================================================' -ForegroundColor Green\n")
	buf.WriteString("    Write-Host \" [PASS] Staging transaction succeeded for '$($adapter.Name)' ($targetIP/$prefixLength).\" -ForegroundColor Green\n")
	buf.WriteString("    Write-Host '======================================================================' -ForegroundColor Green\n")
	buf.WriteString("    exit 0\n")
	buf.WriteString("}\n")
	buf.WriteString("catch {\n")
	buf.WriteString("    Write-Host \"`n[ERROR] Staging failed: $_\" -ForegroundColor Red\n")
	buf.WriteString("    Write-Host \"[ROLLBACK] Invoking rollback script with persistent snapshot...\" -ForegroundColor Yellow\n\n")

	buf.WriteString("    $rollbackScript = Join-Path $PSScriptRoot 'rollback_adapter.ps1'\n")
	buf.WriteString("    if (-not (Test-Path $rollbackScript)) {\n")
	buf.WriteString("        $rollbackScript = 'config\\scripts\\rollback_adapter.ps1'\n")
	buf.WriteString("    }\n")
	buf.WriteString("    if (Test-Path $rollbackScript) {\n")
	buf.WriteString("        & $rollbackScript -SnapshotPath $snapshotPath\n")
	buf.WriteString("        if ($LASTEXITCODE -eq 0) {\n")
	buf.WriteString("            Write-Host \"[ROLLBACK_SUCCESS] Automated rollback successfully restored baseline.\" -ForegroundColor Green\n")
	buf.WriteString("            exit 3\n")
	buf.WriteString("        } else {\n")
	buf.WriteString("            Write-Host \"[ROLLBACK_FAILED] Automated rollback reported failure code $LASTEXITCODE.\" -ForegroundColor Red\n")
	buf.WriteString("            exit 4\n")
	buf.WriteString("        }\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("        Write-Host \"[ROLLBACK_FAILED] Rollback script not found at $rollbackScript!\" -ForegroundColor Red\n")
	buf.WriteString("        exit 4\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n")

	return buf.String()
}
