package network_mgmt_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"nkb-diskless/internal/services/network_mgmt"
)

// Helper to construct a mock provider with realistic multi-adapter inventory
func newTestMockProvider() *network_mgmt.MockAdapterProvider {
	return &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias:       "Wi-Fi",
				InterfaceDescription: "Intel(R) Wireless-AC 9560 160MHz",
				InterfaceIndex:       18,
				InterfaceGuid:        "{BF35765D-438D-4C08-B44F-E342D4929050}",
				MacAddress:           "D8-F2-CA-95-4A-75",
				Status:               "Up",
				LinkSpeed:            "866 Mbps",
				IPv4Addresses:        []string{"192.168.100.13/24"},
				DefaultGateways:      []string{"192.168.100.1"},
				DNSServers:           []string{"192.168.100.1"},
				DhcpEnabled:          true,
			},
			{
				InterfaceAlias:       "vEthernet (WSL)",
				InterfaceDescription: "Hyper-V Virtual Ethernet Adapter",
				InterfaceIndex:       38,
				InterfaceGuid:        "{7E7CB6E2-EB0C-4DCB-9112-E87E1EE3EE29}",
				MacAddress:           "00-15-5D-9D-34-70",
				Status:               "Up",
				LinkSpeed:            "10 Gbps",
				IPv4Addresses:        []string{"172.28.128.1/20"},
				DefaultGateways:      []string{},
				DNSServers:           []string{},
				DhcpEnabled:          false,
			},
			{
				InterfaceAlias:       "Ethernet",
				InterfaceDescription: "Realtek Gaming GbE Family Controller",
				InterfaceIndex:       8,
				InterfaceGuid:        "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
				MacAddress:           "B0-0C-D1-E6-53-31",
				Status:               "Up",
				LinkSpeed:            "100 Mbps",
				IPv4Addresses:        []string{"192.168.50.1/24"},
				DefaultGateways:      []string{},
				DNSServers:           []string{},
				DhcpEnabled:          false,
			},
			{
				InterfaceAlias:       "Ethernet 2",
				InterfaceDescription: "Realtek USB 3.0 to GbE Family Controller",
				InterfaceIndex:       22,
				InterfaceGuid:        "{99AABBCC-1122-3344-5566-778899AABBCC}",
				MacAddress:           "00-E0-4C-68-01-23",
				Status:               "Disconnected",
				LinkSpeed:            "0 bps",
				IPv4Addresses:        []string{},
				DefaultGateways:      []string{},
				DNSServers:           []string{},
				DhcpEnabled:          true,
			},
		},
	}
}

// 1. Test Multiple Network Adapters Discovery & Safety Roles
func TestDiscovery_MultipleAdapters_And_SafetyRoles(t *testing.T) {
	mock := newTestMockProvider()
	discovery := network_mgmt.NewDiscoveryService(mock)

	adapters, err := discovery.DiscoverAdapters()
	if err != nil {
		t.Fatalf("expected discovery to succeed, got: %v", err)
	}

	if len(adapters) != 4 {
		t.Fatalf("expected 4 adapters, got: %d", len(adapters))
	}

	// Wi-Fi check
	wifi := adapters[0]
	if !wifi.IsWireless || wifi.SafetyRole != network_mgmt.RoleProtectedInternet {
		t.Errorf("Wi-Fi not marked as PROTECTED_INTERNET: %+v", wifi)
	}

	// Virtual check
	vEthernet := adapters[1]
	if !vEthernet.IsVirtual || vEthernet.SafetyRole != network_mgmt.RoleVirtualBlocked {
		t.Errorf("vEthernet not marked as VIRTUAL_BLOCKED: %+v", vEthernet)
	}

	// Physical GbE check
	eth := adapters[2]
	if !eth.IsPhysical || eth.SafetyRole != network_mgmt.RoleCandidate {
		t.Errorf("Ethernet not marked as CANDIDATE: %+v", eth)
	}
	if eth.ID == "" {
		t.Errorf("expected persistent ID token for Ethernet")
	}
}

// 2. Test Configuration Management: Backup, Migration, and Restore
func TestConfigManager_Backup_And_Restore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_cfg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mgr, err := network_mgmt.NewNetworkConfigManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 1. Initial Load returns safe default without crashing
	profile, err := mgr.LoadProfile()
	if err != nil {
		t.Fatalf("failed to load default profile: %v", err)
	}
	if profile.IPv4Config.IPAddress != "192.168.50.1" {
		t.Fatalf("expected default IP 192.168.50.1, got: %s", profile.IPv4Config.IPAddress)
	}

	// 2. Save profile creates initial file
	profile.ProfileName = "Custom Test Profile 1"
	if err := mgr.SaveProfile(profile); err != nil {
		t.Fatalf("failed to save profile: %v", err)
	}

	// 3. Modify and Save creates backup
	profile.IPv4Config.IPAddress = "192.168.50.2"
	if err := mgr.SaveProfile(profile); err != nil {
		t.Fatalf("failed to save modified profile: %v", err)
	}

	loaded, err := mgr.LoadProfile()
	if err != nil || loaded.IPv4Config.IPAddress != "192.168.50.2" {
		t.Fatalf("expected updated IP 192.168.50.2, got: %v", loaded)
	}

	// 4. Rollback restores previous version
	rolledBack, err := mgr.Rollback()
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if rolledBack.ProfileName != "Custom Test Profile 1" {
		t.Fatalf("expected restored profile name, got: %s", rolledBack.ProfileName)
	}
}

// 3. Test Adapter Reconciliation: Handles Index Changes and Alias Renames
func TestAdapter_Reconciliation_Renumbering(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_reconcile_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: network_mgmt.ComputePersistentKey("B0:0C:D1:E6:53:31", "{620271F0-ABF2-492B-B226-1871CF04E0BB}", "Realtek Gaming GbE Family Controller"),
		MAC:           "B0:0C:D1:E6:53:31",
		Alias:         "Ethernet",
		Index:         8,
	}

	// Simulate Windows adapter renumbering (index changed 8 -> 15, alias changed "Ethernet" -> "Ethernet 3")
	renumberedAdapters := []network_mgmt.NetworkAdapter{
		{
			ID:                   profile.SelectedAdapter.PersistentKey,
			InterfaceAlias:       "Ethernet 3",
			InterfaceIndex:       15,
			InterfaceGUID:        "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
			MACAddress:           "B0:0C:D1:E6:53:31",
			InterfaceDescription: "Realtek Gaming GbE Family Controller",
			Status:               "Up",
		},
	}

	status, matched := mgr.ReconcileAdapter(profile, renumberedAdapters)
	if status != network_mgmt.StatusSelected || matched == nil {
		t.Fatalf("expected adapter to be reconciled, got status %s", status)
	}
	if profile.SelectedAdapter.Index != 15 || profile.SelectedAdapter.Alias != "Ethernet 3" {
		t.Errorf("expected runtime index/alias to update: index=%d, alias=%s", profile.SelectedAdapter.Index, profile.SelectedAdapter.Alias)
	}
}

// 4. Test Missing Adapter Detection: Does NOT Silently Select Another Adapter
func TestAdapter_Missing_Detection(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_missing_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: "nic_nonexistent_12345",
		MAC:           "00:11:22:33:44:99",
		Alias:         "Old NIC",
		Index:         4,
	}

	// Current system has other adapters, but NOT the adopted NIC
	currentAdapters := []network_mgmt.NetworkAdapter{
		{
			ID:             "nic_other_67890",
			InterfaceAlias: "Wi-Fi",
			MACAddress:     "D8:F2:CA:95:4A:75",
		},
		{
			ID:             "nic_different_54321",
			InterfaceAlias: "Ethernet Replacement",
			MACAddress:     "AA:BB:CC:DD:EE:01",
		},
	}

	status, matched := mgr.ReconcileAdapter(profile, currentAdapters)
	if status != network_mgmt.StatusMissing {
		t.Fatalf("expected status MISSING, got: %s", status)
	}
	if matched != nil {
		t.Fatalf("SAFETY VIOLATION: system silently matched an unrelated adapter: %+v", matched)
	}
}

// 5. Test Hardware Replacement Workflow
func TestAdapter_Guided_Replacement(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_replace_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.Status = network_mgmt.StatusMissing

	// Operator selects new adapter
	newReplacement := network_mgmt.NetworkAdapter{
		ID:                   network_mgmt.ComputePersistentKey("00:E0:4C:68:01:23", "{99AABBCC-1122}", "Realtek USB 3.0 to GbE"),
		InterfaceAlias:       "Ethernet 2",
		InterfaceIndex:       22,
		InterfaceGUID:        "{99AABBCC-1122}",
		MACAddress:           "00:E0:4C:68:01:23",
		InterfaceDescription: "Realtek USB 3.0 to GbE",
		SafetyRole:           network_mgmt.RoleCandidate,
	}

	err := mgr.AdoptAdapter(profile, newReplacement)
	if err != nil {
		t.Fatalf("expected adoption to succeed, got: %v", err)
	}

	if profile.SelectedAdapter.Alias != "Ethernet 2" || profile.SelectedAdapter.MAC != "00:E0:4C:68:01:23" {
		t.Errorf("expected adopted adapter details to be saved: %+v", profile.SelectedAdapter)
	}
	if profile.Status != network_mgmt.StatusSelected {
		t.Errorf("expected status SELECTED, got: %s", profile.Status)
	}
}

// 6. Test Wi-Fi & Corporate Protection: Rejects Adoption of Protected Adapters
func TestSafety_RejectsProtectedAdapters(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_safety_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)
	profile := network_mgmt.DefaultProfile(tempDir)

	// Attempt to adopt Wi-Fi
	wifi := network_mgmt.NetworkAdapter{
		InterfaceAlias: "Wi-Fi",
		IsWireless:     true,
		SafetyRole:     network_mgmt.RoleProtectedInternet,
	}
	if err := mgr.AdoptAdapter(profile, wifi); err == nil {
		t.Fatalf("SAFETY VIOLATION: AdoptAdapter allowed adopting corporate Wi-Fi interface")
	}

	// Attempt to adopt Virtual WSL adapter
	wsl := network_mgmt.NetworkAdapter{
		InterfaceAlias: "vEthernet (WSL)",
		IsVirtual:      true,
		SafetyRole:     network_mgmt.RoleVirtualBlocked,
	}
	if err := mgr.AdoptAdapter(profile, wsl); err == nil {
		t.Fatalf("SAFETY VIOLATION: AdoptAdapter allowed adopting virtual adapter")
	}
}

// 7. Test Subnet Collision Detection with Corporate Wi-Fi
func TestValidation_SubnetCollisionWithCorporateNetwork(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_collision_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	hostAdapters := []network_mgmt.NetworkAdapter{
		{
			InterfaceAlias:  "Wi-Fi",
			IPv4Addresses:   []string{"192.168.100.13/24"},
			DefaultGateways: []string{"192.168.100.1"},
			IsWireless:      true,
			SafetyRole:      network_mgmt.RoleProtectedInternet,
		},
		{
			InterfaceAlias: "Ethernet",
			SafetyRole:     network_mgmt.RoleCandidate,
		},
	}

	profile := network_mgmt.DefaultProfile(tempDir)
	// Propose a test subnet that collides with Wi-Fi (192.168.100.X/24)
	profile.IPv4Config.IPAddress = "192.168.100.50"
	profile.IPv4Config.PrefixLength = 24
	profile.DHCPConfig.PoolStart = "192.168.100.100"
	profile.DHCPConfig.PoolEnd = "192.168.100.200"

	report, err := mgr.ValidateProfile(profile, hostAdapters)
	if err == nil || !report.OverlappingSubnet {
		t.Fatalf("SAFETY VIOLATION: expected subnet collision error with Wi-Fi, got: err=%v, report=%+v", err, report)
	}
}

// 8. Test Invalid DHCP Pools (Pool Start > End, Server IP inside Pool)
func TestValidation_InvalidDHCPPool(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_pool_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	// Case A: Pool start > Pool end
	profileA := network_mgmt.DefaultProfile(tempDir)
	profileA.DHCPConfig.PoolStart = "192.168.50.200"
	profileA.DHCPConfig.PoolEnd = "192.168.50.100"

	_, err := mgr.ValidateProfile(profileA, nil)
	if err == nil {
		t.Fatalf("expected error when PoolStart > PoolEnd")
	}

	// Case B: Server IP falls INSIDE DHCP Pool
	profileB := network_mgmt.DefaultProfile(tempDir)
	profileB.IPv4Config.IPAddress = "192.168.50.150"
	profileB.DHCPConfig.PoolStart = "192.168.50.100"
	profileB.DHCPConfig.PoolEnd = "192.168.50.200"

	reportB, err := mgr.ValidateProfile(profileB, nil)
	if err == nil || !reportB.IPConflict {
		t.Fatalf("expected IP conflict error when Server IP is inside DHCP pool")
	}

	// Case C: DHCP pool is in completely different subnet
	profileC := network_mgmt.DefaultProfile(tempDir)
	profileC.IPv4Config.IPAddress = "192.168.50.1"
	profileC.DHCPConfig.PoolStart = "10.0.0.10"
	profileC.DHCPConfig.PoolEnd = "10.0.0.50"

	_, err = mgr.ValidateProfile(profileC, nil)
	if err == nil {
		t.Fatalf("expected error when DHCP pool is outside server subnet")
	}
}

// 9. Test Staging Live Verification: PASS vs FAIL vs BLOCKED
func TestStagingService_LiveVerification(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_staging_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: "nic_eth_8",
		MAC:           "B0:0C:D1:E6:53:31",
		Alias:         "Ethernet",
		Index:         8,
	}

	// Scenario 1: Adapter configured perfectly (192.168.50.1/24, zero gateway) -> PASS
	mockPass := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias: "Ethernet",
				MacAddress:     "B0:0C:D1:E6:53:31",
				InterfaceGuid:  "{620271F0}",
				InterfaceIndex: 8,
				IPv4Addresses:  []string{"192.168.50.1/24"},
				Status:         "Up",
			},
		},
	}
	stagingPass := network_mgmt.NewStagingService(mgr, network_mgmt.NewDiscoveryService(mockPass))
	resPass, err := stagingPass.VerifyLiveConfiguration(profile)
	if err != nil || !resPass.Verified || resPass.Status != "PASS" {
		t.Fatalf("expected PASS on verified staging, got: %+v (err: %v)", resPass, err)
	}

	// Scenario 2: Adapter has wrong IP -> FAIL
	mockFail := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias: "Ethernet",
				MacAddress:     "B0:0C:D1:E6:53:31",
				InterfaceGuid:  "{620271F0}",
				InterfaceIndex: 8,
				IPv4Addresses:  []string{"192.168.254.20/24"}, // Old stale IP
				Status:         "Up",
			},
		},
	}
	stagingFail := network_mgmt.NewStagingService(mgr, network_mgmt.NewDiscoveryService(mockFail))
	resFail, err := stagingFail.VerifyLiveConfiguration(profile)
	if err != nil || resFail.Verified || resFail.Status != "FAIL" {
		t.Fatalf("expected FAIL on mismatched IP, got: %+v", resFail)
	}

	// Scenario 3: Adapter has gateway in isolated mode -> BLOCKED (Phase 3E.1 Safety Gate)
	mockGWFail := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias:  "Ethernet",
				MacAddress:      "B0:0C:D1:E6:53:31",
				InterfaceGuid:   "{620271F0}",
				InterfaceIndex:  8,
				IPv4Addresses:   []string{"192.168.50.1/24"},
				DefaultGateways: []string{"192.168.50.254"}, // Active gateway causes BLOCKED classification
				Status:          "Up",
			},
		},
	}
	stagingGWFail := network_mgmt.NewStagingService(mgr, network_mgmt.NewDiscoveryService(mockGWFail))
	resGWFail, err := stagingGWFail.VerifyLiveConfiguration(profile)
	if err != nil || resGWFail.Verified || resGWFail.Status != "BLOCKED" {
		t.Fatalf("expected BLOCKED on active gateway in isolated mode, got: %+v", resGWFail)
	}

	// Scenario 3b: Adapter has DNS in isolated mode -> FAIL
	mockDNSFail := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias:  "Ethernet",
				MacAddress:      "B0:0C:D1:E6:53:31",
				InterfaceGuid:   "{620271F0}",
				InterfaceIndex:  8,
				IPv4Addresses:   []string{"192.168.50.1/24"},
				DefaultGateways: []string{},
				DNSServers:      []string{"8.8.8.8"}, // Illegal DNS in isolated airgap
				Status:          "Up",
			},
		},
	}
	stagingDNSFail := network_mgmt.NewStagingService(mgr, network_mgmt.NewDiscoveryService(mockDNSFail))
	resDNSFail, err := stagingDNSFail.VerifyLiveConfiguration(profile)
	if err != nil || resDNSFail.Verified || resDNSFail.Status != "FAIL" {
		t.Fatalf("expected FAIL on DNS violation in isolated mode, got: %+v", resDNSFail)
	}

	// Scenario 4: Selected adapter is completely MISSING -> BLOCKED
	mockMissing := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias: "Other Adapter",
				MacAddress:     "11:22:33:44:55:66",
			},
		},
	}
	stagingBlocked := network_mgmt.NewStagingService(mgr, network_mgmt.NewDiscoveryService(mockMissing))
	resBlocked, err := stagingBlocked.VerifyLiveConfiguration(profile)
	if err != nil || resBlocked.Verified || resBlocked.Status != "BLOCKED" {
		t.Fatalf("expected BLOCKED on missing adapter, got: %+v", resBlocked)
	}
}

// 10. Test Dynamic Elevated Staging Script Generation
func TestScript_DynamicGeneration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_script_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Realtek GbE Test"
	profile.SelectedAdapter.MAC = "B0:0C:D1:E6:53:31"

	script := mgr.GenerateElevatedStagingScript(profile)
	if !strings.Contains(script, "B0:0C:D1:E6:53:31") {
		t.Errorf("expected script to contain target MAC B0:0C:D1:E6:53:31")
	}
	if !strings.Contains(script, "192.168.50.1") {
		t.Errorf("expected script to assign 192.168.50.1")
	}
	if !strings.Contains(script, "-DefaultGateway $null") {
		t.Errorf("expected script to enforce zero gateway ($null)")
	}
	if !strings.Contains(script, "ROLLBACK") {
		t.Errorf("expected script to contain automated transactional rollback logic")
	}
}

// ==============================================================================
// PHASE 3E.1 SPECIFIC AUDIT & TRANSACTIONAL ROLLBACK TEST SUITES
// ==============================================================================

// 11. Test Preflight Rejects Active Internet Adapter when Wi-Fi is Disconnected
func TestPreflight_BlocksWhenTargetIsSoleGateway(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_preflight_sole_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	mock := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias:  "Ethernet",
				MacAddress:      "B0:0C:D1:E6:53:31",
				Status:          "Up",
				IPv4Addresses:   []string{"192.168.254.20/24"},
				DefaultGateways: []string{"192.168.254.254"}, // Sole active default route!
				DhcpEnabled:     true,
			},
			{
				InterfaceAlias:  "Wi-Fi",
				MacAddress:      "D8:F2:CA:95:4A:75",
				Status:          "Disconnected", // Disconnected Wi-Fi cannot provide management route
				IPv4Addresses:   []string{},
				DefaultGateways: []string{},
			},
		},
	}

	disc := network_mgmt.NewDiscoveryService(mock)
	adapters, _ := disc.DiscoverAdapters()

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0:0C:D1:E6:53:31"

	res, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected error during preflight: %v", err)
	}

	if res.Eligible {
		t.Errorf("expected adapter to NOT be eligible when holding sole default route")
	}
	if res.Status != network_mgmt.StatusBlocked {
		t.Errorf("expected status BLOCKED, got: %s", res.Status)
	}
	if res.ManagementRouteSecured {
		t.Errorf("expected ManagementRouteSecured to be false when Wi-Fi is disconnected")
	}
	if !strings.Contains(res.Message, "sole default gateway") {
		t.Errorf("expected explanation of sole default gateway, got: %s", res.Message)
	}
}

// 12. Test Preflight Passes when Alternate Management Interface (Wi-Fi) is Active
func TestPreflight_PassesWhenManagementRouteSecured(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_preflight_alt_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	mock := &network_mgmt.MockAdapterProvider{
		Adapters: []network_mgmt.MockRawAdapter{
			{
				InterfaceAlias:  "Ethernet",
				MacAddress:      "B0:0C:D1:E6:53:31",
				Status:          "Up",
				IPv4Addresses:   []string{"192.168.254.20/24"},
				DefaultGateways: []string{"192.168.254.254"},
				DhcpEnabled:     true,
			},
			{
				InterfaceAlias:  "Wi-Fi",
				MacAddress:      "D8:F2:CA:95:4A:75",
				Status:          "Up", // Active Wi-Fi provides independent gateway
				IPv4Addresses:   []string{"192.168.100.15/24"},
				DefaultGateways: []string{"192.168.100.1"},
			},
		},
	}

	disc := network_mgmt.NewDiscoveryService(mock)
	adapters, _ := disc.DiscoverAdapters()

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0:0C:D1:E6:53:31"

	res, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected error during preflight: %v", err)
	}

	if !res.Eligible {
		t.Errorf("expected adapter to be ELIGIBLE when management connectivity is secured by Wi-Fi, got: %+v", res)
	}
	if res.Status != network_mgmt.StatusReadyToStage {
		t.Errorf("expected status READY_TO_STAGE, got: %s", res.Status)
	}
	if !res.ManagementRouteSecured || res.ManagementInterfaceAlias != "Wi-Fi" {
		t.Errorf("expected management interface alias Wi-Fi, got: %s (secured: %v)", res.ManagementInterfaceAlias, res.ManagementRouteSecured)
	}
}

// 13. Test Windows Snapshot Creation and Atomic Persistence
func TestWindowsSnapshot_CaptureAndRestore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_test_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	adapter := network_mgmt.NetworkAdapter{
		ID:                   "nic_test_123",
		InterfaceAlias:       "Ethernet Test",
		InterfaceIndex:       8,
		InterfaceGUID:        "{11223344-5566}",
		MACAddress:           "B0:0C:D1:E6:53:31",
		InterfaceDescription: "Realtek GbE Test Controller",
		DHCPEnabled:          false,
		IPv4Addresses:        []string{"192.168.1.100/24", "192.168.1.101/24"}, // Multiple IPs
		DefaultGateways:      []string{"192.168.1.1", "192.168.1.254"},        // Multiple Gateways
		DNSServers:           []string{"1.1.1.1", "8.8.8.8"},
		Status:               "Up",
	}

	snap := mgr.BuildSnapshotFromAdapter(&adapter)
	if snap.SnapshotID == "" || len(snap.IPv4Addresses) != 2 || len(snap.DefaultGateways) != 2 {
		t.Fatalf("snapshot built incorrectly: %+v", snap)
	}

	if err := mgr.SaveWindowsSnapshot(snap); err != nil {
		t.Fatalf("failed to save snapshot: %v", err)
	}

	// Verify snapshot file exists
	list, err := mgr.ListWindowsSnapshots()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 snapshot in list, got %d (err: %v)", len(list), err)
	}

	retrieved, err := mgr.GetLatestWindowsSnapshot("nic_test_123")
	if err != nil || retrieved.InterfaceAlias != "Ethernet Test" {
		t.Fatalf("failed to retrieve snapshot: %+v", retrieved)
	}

	// Verify rollback script generation contains original static configuration
	rollbackScript := mgr.GenerateRollbackScript(retrieved)
	if !strings.Contains(rollbackScript, "192.168.1.100") || !strings.Contains(rollbackScript, "192.168.1.101") {
		t.Errorf("rollback script missing original static IPs")
	}
	if !strings.Contains(rollbackScript, "192.168.1.1") {
		t.Errorf("rollback script missing original default gateway")
	}
	if !strings.Contains(rollbackScript, "1.1.1.1") {
		t.Errorf("rollback script missing original DNS server")
	}
}

// 14. Test DHCP Adapter Snapshot and Rollback Script Generation
func TestWindowsSnapshot_DhcpAdapterRollbackScript(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_dhcp_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	dhcpAdapter := network_mgmt.NetworkAdapter{
		ID:             "nic_dhcp_999",
		InterfaceAlias: "LAN-Dynamic",
		MACAddress:     "AA:BB:CC:DD:EE:FF",
		DHCPEnabled:    true,
		Status:         "Up",
	}

	snap := mgr.BuildSnapshotFromAdapter(&dhcpAdapter)
	script := mgr.GenerateRollbackScript(snap)

	if !strings.Contains(script, "Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Enabled") {
		t.Errorf("expected rollback script to re-enable DHCP via Set-NetIPInterface")
	}
	if !strings.Contains(script, "ResetServerAddresses") {
		t.Errorf("expected rollback script to reset DNS server addresses to DHCP")
	}
}

// 15. Test Unrelated Adapters Remain Untouched by Staging Script
func TestScript_StrictInterfaceIndexScoping(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_script_scope_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Dedicated-PXE"
	profile.SelectedAdapter.MAC = "B0:0C:D1:E6:53:31"

	script := mgr.GenerateElevatedStagingScript(profile)

	// Ensure all mutations target $adapter.InterfaceIndex strictly
	if !strings.Contains(script, "Remove-NetRoute") || !strings.Contains(script, "-InterfaceIndex $adapter.InterfaceIndex") {
		t.Errorf("Remove-NetRoute must be scoped strictly to $adapter.InterfaceIndex")
	}
	if !strings.Contains(script, "Remove-NetIPAddress") || !strings.Contains(script, "-InterfaceIndex $adapter.InterfaceIndex") {
		t.Errorf("Remove-NetIPAddress must be scoped strictly to $adapter.InterfaceIndex")
	}
	if !strings.Contains(script, "New-NetIPAddress") || !strings.Contains(script, "-InterfaceIndex $adapter.InterfaceIndex") {
		t.Errorf("New-NetIPAddress must be scoped strictly to $adapter.InterfaceIndex")
	}
}

// ==============================================================================
// PHASE 3E.2 COMPREHENSIVE AUTOMATED TEST SUITE (17 SCENARIOS)
// ==============================================================================

// 1. Complete Snapshot Capture (All 19 Mandatory Fields)
func TestPhase3E2_Complete19FieldsSnapshotCapture(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_19fields_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	adapter := network_mgmt.NetworkAdapter{
		ID:                             "nic_all19_complete",
		InterfaceAlias:                 "Ethernet 19F",
		InterfaceDescription:           "Intel Gigabit Network Adapter",
		InterfaceIndex:                 8,
		InterfaceGUID:                  "{ABCDEF01-2345-6789-ABCD-EF0123456789}",
		MACAddress:                     "00:11:22:33:44:55",
		Status:                         "Up",
		LinkSpeed:                      "1 Gbps",
		IPv4Addresses:                  []string{"192.168.1.50/24"},
		IPv6Addresses:                  []string{"fe80::1/64"},
		DefaultGateways:                []string{"192.168.1.1"},
		DNSServers:                     []string{"1.1.1.1", "8.8.8.8"},
		DHCPEnabled:                    false,
		DHCPv6Enabled:                  true,
		InterfaceMetricIPv4:            15,
		InterfaceMetricIPv6:            25,
		RegisterThisConnectionsAddress: true,
	}

	snap := mgr.BuildSnapshotFromAdapter(&adapter)
	if err := mgr.SaveWindowsSnapshot(snap); err != nil {
		t.Fatalf("failed to save snapshot: %v", err)
	}

	// Verify all 19 fields
	if snap.SnapshotID == "" {
		t.Errorf("field 1 (SnapshotID) missing")
	}
	if snap.Timestamp.IsZero() {
		t.Errorf("field 2 (Timestamp) missing")
	}
	if snap.PersistentKey != "nic_all19_complete" {
		t.Errorf("field 3 (PersistentKey) missing")
	}
	if snap.InterfaceGUID != "{ABCDEF01-2345-6789-ABCD-EF0123456789}" {
		t.Errorf("field 4 (InterfaceGUID) missing")
	}
	if snap.InterfaceIndex != 8 {
		t.Errorf("field 5 (InterfaceIndex) missing")
	}
	if snap.InterfaceAlias != "Ethernet 19F" {
		t.Errorf("field 6 (InterfaceAlias) missing")
	}
	if snap.MACAddress != "00:11:22:33:44:55" {
		t.Errorf("field 7 (MACAddress) missing")
	}
	if snap.HardwareDescription != "Intel Gigabit Network Adapter" {
		t.Errorf("field 8 (HardwareDescription) missing")
	}
	if snap.Status != "Up" {
		t.Errorf("field 9 (Status) missing")
	}
	if snap.Dhcpv4Enabled != false {
		t.Errorf("field 10 (Dhcpv4Enabled) incorrect")
	}
	if snap.Dhcpv6Enabled != true {
		t.Errorf("field 11 (Dhcpv6Enabled) incorrect")
	}
	if len(snap.IPv4Addresses) != 1 || snap.IPv4Addresses[0].IPAddress != "192.168.1.50" || snap.IPv4Addresses[0].PrefixLength != 24 {
		t.Errorf("field 12 (IPv4Addresses) incorrect: %+v", snap.IPv4Addresses)
	}
	if len(snap.IPv6AddressesDetailed) != 1 || snap.IPv6AddressesDetailed[0].IPAddress != "fe80::1" {
		t.Errorf("field 13 (IPv6AddressesDetailed) incorrect: %+v", snap.IPv6AddressesDetailed)
	}
	if len(snap.IPv4DefaultGateways) != 1 || snap.IPv4DefaultGateways[0] != "192.168.1.1" {
		t.Errorf("field 14 (IPv4DefaultGateways) incorrect: %+v", snap.IPv4DefaultGateways)
	}
	if len(snap.Routes) != 1 || snap.Routes[0].NextHop != "192.168.1.1" {
		t.Errorf("field 15 (Routes) incorrect: %+v", snap.Routes)
	}
	if snap.InterfaceMetricIPv4 != 15 || snap.InterfaceMetricIPv6 != 25 {
		t.Errorf("field 16 (InterfaceMetrics) incorrect: v4=%d, v6=%d", snap.InterfaceMetricIPv4, snap.InterfaceMetricIPv6)
	}
	if len(snap.IPv4DNSServers) != 2 || snap.IPv4DNSServers[0] != "1.1.1.1" {
		t.Errorf("field 17 (IPv4DNSServers) incorrect: %+v", snap.IPv4DNSServers)
	}
	if !snap.RegisterThisConnectionsAddress {
		t.Errorf("field 18 (RegisterThisConnectionsAddress) incorrect")
	}
	if snap.NetworkCategory != "Private" {
		t.Errorf("field 19 (NetworkCategory) incorrect")
	}
	if snap.IntegrityHash == "" || !strings.HasPrefix(snap.IntegrityHash, "sha256:") {
		t.Errorf("checksum integrity hash missing or invalid: %s", snap.IntegrityHash)
	}
}

// 2. Static IP Restore
func TestPhase3E2_Rollback_StaticIPRestore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_static_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_static_ip",
		PersistentKey:  "nic_static",
		InterfaceAlias: "Ethernet Static",
		InterfaceGUID:  "{1111-2222}",
		MACAddress:     "AA:BB:CC:11:22:33",
		Dhcpv4Enabled:  false,
		IPv4Addresses: []network_mgmt.IPv4AddressEntry{
			{IPAddress: "192.168.50.1", PrefixLength: 24},
		},
		NetworkCategory: "Private",
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "192.168.50.1") {
		t.Errorf("expected script to contain static IP 192.168.50.1")
	}
	if !strings.Contains(script, "Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Disabled") {
		t.Errorf("expected script to disable DHCP for static IP restoration")
	}
}

// 3. DHCP v4 Restore
func TestPhase3E2_Rollback_DHCPv4Restore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_dhcpv4_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_dhcpv4",
		PersistentKey:  "nic_dhcp",
		InterfaceAlias: "Ethernet DHCP",
		InterfaceGUID:  "{1111-3333}",
		MACAddress:     "AA:BB:CC:44:55:66",
		Dhcpv4Enabled:  true,
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -Dhcp Enabled") {
		t.Errorf("expected script to enable DHCP")
	}
	if !strings.Contains(script, "ResetServerAddresses") {
		t.Errorf("expected script to reset DNS addresses for DHCP")
	}
}

// 4. DHCP v6 Restore
func TestPhase3E2_Rollback_DHCPv6Restore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_dhcpv6_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_dhcpv6",
		PersistentKey:  "nic_v6",
		InterfaceAlias: "Ethernet DHCPv6",
		InterfaceGUID:  "{1111-4444}",
		MACAddress:     "AA:BB:CC:77:88:99",
		Dhcpv6Enabled:  true,
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Enabled") {
		t.Errorf("expected script to restore IPv6 DHCP")
	}
}

// 5. Static IPv6 Restore
func TestPhase3E2_Rollback_StaticIPv6Restore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_staticv6_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_staticv6",
		PersistentKey:  "nic_static_v6",
		InterfaceAlias: "Ethernet Staticv6",
		InterfaceGUID:  "{1111-5555}",
		MACAddress:     "AA:BB:CC:AA:BB:CC",
		Dhcpv6Enabled:  false,
		IPv6AddressesDetailed: []network_mgmt.IPv6AddressEntry{
			{IPAddress: "2001:db8::10", PrefixLength: 64},
		},
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "2001:db8::10") {
		t.Errorf("expected script to contain static IPv6 2001:db8::10")
	}
	if !strings.Contains(script, "Set-NetIPInterface -InterfaceIndex $adapter.InterfaceIndex -AddressFamily IPv6 -Dhcp Disabled") {
		t.Errorf("expected script to disable IPv6 DHCP")
	}
}

// 6. Multi-IP Restore
func TestPhase3E2_Rollback_MultiIPRestore(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_multirip_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_multi_ip",
		PersistentKey:  "nic_multi",
		InterfaceAlias: "Ethernet Multi",
		InterfaceGUID:  "{1111-6666}",
		MACAddress:     "AA:BB:CC:DD:EE:01",
		Dhcpv4Enabled:  false,
		IPv4Addresses: []network_mgmt.IPv4AddressEntry{
			{IPAddress: "10.0.0.10", PrefixLength: 24},
			{IPAddress: "10.0.0.11", PrefixLength: 24},
			{IPAddress: "10.0.0.12", PrefixLength: 24},
		},
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "10.0.0.10") || !strings.Contains(script, "10.0.0.11") || !strings.Contains(script, "10.0.0.12") {
		t.Errorf("expected script to contain all 3 static IP addresses")
	}
}

// 7. Gateway Restoration
func TestPhase3E2_Rollback_GatewayRestoration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_gw_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_gw",
		PersistentKey:       "nic_gw",
		InterfaceAlias:      "Ethernet Gateway",
		InterfaceGUID:       "{1111-7777}",
		MACAddress:          "AA:BB:CC:DD:EE:02",
		Dhcpv4Enabled:       false,
		IPv4Addresses:       []network_mgmt.IPv4AddressEntry{{IPAddress: "192.168.1.100", PrefixLength: 24}},
		IPv4DefaultGateways: []string{"192.168.1.254"},
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "192.168.1.254") {
		t.Errorf("expected script to contain default gateway 192.168.1.254")
	}
}

// 8. Route Metric Restoration
func TestPhase3E2_Rollback_RouteAndInterfaceMetricRestoration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_metric_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_metric",
		PersistentKey:       "nic_metric",
		InterfaceAlias:      "Ethernet Metric",
		InterfaceGUID:       "{1111-8888}",
		MACAddress:          "AA:BB:CC:DD:EE:03",
		InterfaceMetricIPv4: 45,
		InterfaceMetricIPv6: 55,
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "InterfaceMetric $snap.interface_metric_ipv4") && !strings.Contains(script, "45") {
		t.Errorf("expected script to restore IPv4 interface metric")
	}
	if !strings.Contains(script, "InterfaceMetric $snap.interface_metric_ipv6") && !strings.Contains(script, "55") {
		t.Errorf("expected script to restore IPv6 interface metric")
	}
}

// 9. Custom DNS Restoration
func TestPhase3E2_Rollback_CustomDNSRestoration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_dns_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_dns",
		PersistentKey:  "nic_dns",
		InterfaceAlias: "Ethernet DNS",
		InterfaceGUID:  "{1111-9999}",
		MACAddress:     "AA:BB:CC:DD:EE:04",
		Dhcpv4Enabled:  false,
		IPv4Addresses:  []network_mgmt.IPv4AddressEntry{{IPAddress: "192.168.1.100", PrefixLength: 24}},
		IPv4DNSServers: []string{"9.9.9.9", "149.112.112.112"},
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "9.9.9.9") || !strings.Contains(script, "149.112.112.112") {
		t.Errorf("expected script to restore custom DNS servers")
	}
}

// 10. Dynamic DNS Registration Restoration
func TestPhase3E2_Rollback_DynamicDNSRegistration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_regdns_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:                     "snap_regdns",
		PersistentKey:                  "nic_regdns",
		InterfaceAlias:                 "Ethernet RegDNS",
		InterfaceGUID:                  "{1111-AAAA}",
		MACAddress:                     "AA:BB:CC:DD:EE:05",
		RegisterThisConnectionsAddress: true,
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "RegisterThisConnectionsAddress") {
		t.Errorf("expected script to restore RegisterThisConnectionsAddress")
	}
}

// 11. Network Category Restoration
func TestPhase3E2_Rollback_NetworkCategoryRestoration(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_cat_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:      "snap_cat",
		PersistentKey:   "nic_cat",
		InterfaceAlias:  "Ethernet Category",
		InterfaceGUID:   "{1111-BBBB}",
		MACAddress:      "AA:BB:CC:DD:EE:06",
		NetworkCategory: "DomainAuthenticated",
	}

	script := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(script, "Set-NetConnectionProfile") || !strings.Contains(script, "DomainAuthenticated") {
		t.Errorf("expected script to restore NetworkCategory DomainAuthenticated")
	}
}

// 12. Interface Renumbering Handling During Rollback
func TestPhase3E2_Rollback_InterfaceRenumbering(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_renum_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_renum",
		PersistentKey:  "nic_renum",
		InterfaceAlias: "Ethernet Renumbered",
		InterfaceIndex: 8, // Index was 8 at snapshot time
		InterfaceGUID:  "{1111-CCCC}",
		MACAddress:     "AA:BB:CC:DD:EE:07",
		Dhcpv4Enabled:  true,
	}

	script := mgr.GenerateRollbackScript(snap)
	// Must match by MAC or GUID dynamically, not just hardcoded index
	if !strings.Contains(script, "$targetMacClean") && !strings.Contains(script, "InterfaceGuid") {
		t.Errorf("expected rollback script to match adapter dynamically by MAC and GUID")
	}
}

// 13. Missing Snapshot File Handling
func TestPhase3E2_Rollback_MissingSnapshotFile(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_missing_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	_, err := mgr.GetLatestWindowsSnapshot("non_existent_key")
	if err == nil {
		t.Errorf("expected error when querying non-existent snapshot")
	}
}

// 14. Corrupted Snapshot File Handling
func TestPhase3E2_Rollback_CorruptedSnapshotFile(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_corrupt_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snapDir := tempDir + "/config/snapshots"
	_ = os.MkdirAll(snapDir, 0755)
	corruptFile := snapDir + "/snap_corrupt.json"
	_ = os.WriteFile(corruptFile, []byte("{ malformed json content ..."), 0644)

	snapshots, err := mgr.ListWindowsSnapshots()
	if err != nil {
		t.Fatalf("ListWindowsSnapshots failed: %v", err)
	}
	// Malformed file should be safely skipped, not crash
	for _, s := range snapshots {
		if s.SnapshotID == "snap_corrupt" {
			t.Errorf("corrupted snapshot should not have been loaded")
		}
	}
}

// 15. Snapshot Integrity Hash and Tamper Detection
func TestPhase3E2_Snapshot_IntegrityHashAndTamperDetection(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_tamper_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_tamper_test",
		PersistentKey:  "nic_tamper",
		InterfaceAlias: "Ethernet Tamper",
		InterfaceGUID:  "{1111-DDDD}",
		MACAddress:     "AA:BB:CC:DD:EE:08",
		Dhcpv4Enabled:  false,
		IPv4Addresses:  []network_mgmt.IPv4AddressEntry{{IPAddress: "192.168.50.1", PrefixLength: 24}},
	}
	snap.IntegrityHash = network_mgmt.ComputeSnapshotHash(snap)

	// Valid snapshot passes
	if err := mgr.ValidateSnapshotIntegrity(snap); err != nil {
		t.Fatalf("expected valid snapshot to pass integrity check: %v", err)
	}

	// Tamper with IP address
	snap.IPv4Addresses[0].IPAddress = "192.168.99.99"
	err := mgr.ValidateSnapshotIntegrity(snap)
	if err == nil {
		t.Errorf("expected error when validating tampered snapshot")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected checksum mismatch error, got: %v", err)
	}
}

// 16. Staging Catch Block Auto-Rollback Invocation
func TestPhase3E2_StagingScript_CatchBlockAutoRollback(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_catch_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.PersistentKey = "nic_catch_key"
	profile.SelectedAdapter.Alias = "Dedicated-PXE"
	profile.SelectedAdapter.MAC = "B0:0C:D1:E6:53:31"

	script := mgr.GenerateElevatedStagingScript(profile)

	// Verify snapshot is captured BEFORE changes
	if !strings.Contains(script, "PHASE 2/4] Capturing persistent Windows adapter configuration snapshot") {
		t.Errorf("staging script missing pre-apply snapshot capture phase")
	}
	if !strings.Contains(script, "$snapshotPath") {
		t.Errorf("staging script missing $snapshotPath variable")
	}

	// Verify catch block invokes rollback_adapter.ps1 with -SnapshotPath
	if !strings.Contains(script, "catch {") {
		t.Errorf("staging script missing catch block")
	}
	if !strings.Contains(script, "& $rollbackScript -SnapshotPath $snapshotPath") {
		t.Errorf("staging catch block must invoke rollback_adapter.ps1 with -SnapshotPath")
	}
}

// 17. Exact Restoration Verification Engine
func TestPhase3E2_Verification_ExactRestorationEngine(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_verify_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	referenceSnap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_ref",
		PersistentKey:       "nic_ref",
		InterfaceAlias:      "Ethernet Ref",
		InterfaceGUID:       "{1111-EEEE}",
		MACAddress:          "AA:BB:CC:DD:EE:09",
		Dhcpv4Enabled:       false,
		IPv4Addresses:       []network_mgmt.IPv4AddressEntry{{IPAddress: "192.168.1.100", PrefixLength: 24}},
		IPv4DefaultGateways: []string{"192.168.1.1"},
		IPv4DNSServers:      []string{"1.1.1.1"},
	}

	// Case 1: Exact match
	currentExact := &network_mgmt.NetworkAdapter{
		InterfaceAlias:  "Ethernet Ref",
		MACAddress:      "AA:BB:CC:DD:EE:09",
		DHCPEnabled:     false,
		IPv4Addresses:   []string{"192.168.1.100/24"},
		DefaultGateways: []string{"192.168.1.1"},
		DNSServers:      []string{"1.1.1.1"},
	}

	matched, mismatches := mgr.VerifyRestoration(referenceSnap, currentExact)
	if !matched || len(mismatches) > 0 {
		t.Errorf("expected exact restoration to match, got mismatches: %v", mismatches)
	}

	// Case 2: DHCP state mismatch
	currentDhcpMismatch := &network_mgmt.NetworkAdapter{
		DHCPEnabled: true,
	}
	matched, mismatches = mgr.VerifyRestoration(referenceSnap, currentDhcpMismatch)
	if matched || len(mismatches) == 0 {
		t.Errorf("expected DHCP state mismatch to be detected")
	}

	// Case 3: Gateway missing
	currentGwMissing := &network_mgmt.NetworkAdapter{
		DHCPEnabled:   false,
		IPv4Addresses: []string{"192.168.1.100/24"},
		DNSServers:    []string{"1.1.1.1"},
	}
	matched, mismatches = mgr.VerifyRestoration(referenceSnap, currentGwMissing)
	if matched || len(mismatches) == 0 {
		t.Errorf("expected missing gateway to be detected")
	}
}

// ==============================================================================
// PHASE 3E.4 RECOVERY HARDENING & AST VALIDATION TEST SUITE
// ==============================================================================

// 1. PowerShell AST Native Parser Validation (CRIT-01 remediation)
func TestPhase3E4_PowerShellAST_ParserValidation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skipping powershell AST validation on non-windows platform")
	}

	tempDir, err := os.MkdirTemp("", "nkb_ast_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mgr, err := network_mgmt.NewNetworkConfigManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: "nic_ast_test",
		Alias:         "Ethernet",
		Index:         8,
		GUID:          "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MAC:           "B0:0C:D1:E6:53:31",
	}

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_ast_test",
		PersistentKey:       "nic_ast_test",
		InterfaceAlias:      "Ethernet",
		InterfaceIndex:      8,
		InterfaceGUID:       "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:          "B0:0C:D1:E6:53:31",
		HardwareDescription: "Realtek Gaming GbE Family Controller",
		Description:         "Realtek Gaming GbE Family Controller",
		Status:              "Up",
		Dhcpv4Enabled:       true,
		NetworkCategory:     "Public",
		IsSynthetic:         false,
		Provenance:          "LIVE_WINDOWS_CIM",
	}

	stageScript := mgr.GenerateElevatedStagingScript(profile)
	rollbackScript := mgr.GenerateRollbackScript(snap)

	stageFile := filepath.Join(tempDir, "test_stage.ps1")
	rollbackFile := filepath.Join(tempDir, "test_rollback.ps1")

	if err := os.WriteFile(stageFile, []byte(stageScript), 0644); err != nil {
		t.Fatalf("failed to write staging script: %v", err)
	}
	if err := os.WriteFile(rollbackFile, []byte(rollbackScript), 0644); err != nil {
		t.Fatalf("failed to write rollback script: %v", err)
	}

	checkAST := func(filePath, scriptName string) {
		cmdStr := "$errs = @(); $tokens = $null; $null = [System.Management.Automation.Language.Parser]::ParseFile('" + filePath + "', [ref]$tokens, [ref]$errs); $errs.Count"
		out, err := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", cmdStr).CombinedOutput()
		if err != nil {
			t.Fatalf("[%s] failed to execute AST parser: %v, output: %s", scriptName, err, string(out))
		}
		trimmed := strings.TrimSpace(string(out))
		count, convErr := strconv.Atoi(trimmed)
		if convErr != nil {
			t.Fatalf("[%s] failed to parse error count '%s': %v", scriptName, trimmed, convErr)
		}
		if count != 0 {
			t.Errorf("[%s] PowerShell AST syntax errors found: %d (expected 0)", scriptName, count)
		}
	}

	checkAST(stageFile, "Dynamic stage_adapter.ps1")
	checkAST(rollbackFile, "Dynamic rollback_adapter.ps1")

	// Also verify repo scripts if they exist
	repoStage := filepath.Join("..", "..", "config", "scripts", "stage_adapter.ps1")
	if abs, err := filepath.Abs(repoStage); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			checkAST(abs, "Repo stage_adapter.ps1")
		}
	}
	repoRollback := filepath.Join("..", "..", "config", "scripts", "rollback_adapter.ps1")
	if abs, err := filepath.Abs(repoRollback); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			checkAST(abs, "Repo rollback_adapter.ps1")
		}
	}
}

// 2. Disconnected Alternate NIC With Stale Route Must Block Staging (CRIT-02)
func TestPhase3E4_ManagementConnectivity_DisconnectedWithStaleRoute(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_stale_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	adapters := []network_mgmt.NetworkAdapter{
		{
			ID:                   "nic_target",
			InterfaceAlias:       "Ethernet",
			InterfaceIndex:       8,
			MACAddress:           "B0-0C-D1-E6-53-31",
			Status:               "Up",
			IsPhysical:           true,
			SafetyRole:           network_mgmt.RoleProtectedInternet,
			DefaultGateways:      []string{"192.168.254.254"},
			IPv4Addresses:        []string{"192.168.254.20/24"},
		},
		{
			// Disconnected Wi-Fi with stale cached gateway route
			ID:                   "nic_wifi_stale",
			InterfaceAlias:       "Wi-Fi",
			InterfaceIndex:       18,
			MACAddress:           "D8-F2-CA-95-4A-75",
			Status:               "Disconnected", // Physical link is down!
			IsWireless:           true,
			SafetyRole:           network_mgmt.RoleProtectedInternet,
			DefaultGateways:      []string{"192.168.100.1"},
			IPv4Addresses:        []string{"192.168.100.13/24"},
		},
	}

	result, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}

	if result.Eligible {
		t.Errorf("preflight must be BLOCKED when alternate management NIC is Disconnected (stale route)")
	}
	if result.Status != network_mgmt.StatusBlocked {
		t.Errorf("expected Status BLOCKED, got: %s", result.Status)
	}
	if result.ManagementRouteSecured {
		t.Errorf("expected ManagementRouteSecured false when alternate is Disconnected")
	}
}

// 3. Alternate NIC Up But Lacking Valid IPv4 Address Must Block Staging (CRIT-02)
func TestPhase3E4_ManagementConnectivity_UpButNoIP(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_noip_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	adapters := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			SafetyRole:      network_mgmt.RoleProtectedInternet,
			DefaultGateways: []string{"192.168.254.254"},
			IPv4Addresses:   []string{"192.168.254.20/24"},
		},
		{
			// Up but APIPA only / unconfigured
			ID:              "nic_alt_apipa",
			InterfaceAlias:  "Ethernet 2",
			InterfaceIndex:  22,
			MACAddress:      "00-E0-4C-68-01-23",
			Status:          "Up",
			IsPhysical:      true,
			DefaultGateways: []string{"192.168.1.1"},
			IPv4Addresses:   []string{"169.254.12.34/16"}, // APIPA auto-assign
		},
	}

	result, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}

	if result.Eligible {
		t.Errorf("preflight must be BLOCKED when alternate management NIC has only APIPA address")
	}
}

// 4. Target Adapter Holds Sole Default Gateway -> Staging Blocked
func TestPhase3E4_ManagementConnectivity_TargetIsSoleGateway(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_sole_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	adapters := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			SafetyRole:      network_mgmt.RoleProtectedInternet,
			DefaultGateways: []string{"192.168.254.254"},
			IPv4Addresses:   []string{"192.168.254.20/24"},
		},
	}

	result, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}

	if result.Eligible {
		t.Errorf("preflight must be BLOCKED when target adapter holds the host sole default gateway")
	}
	if result.Status != network_mgmt.StatusBlocked {
		t.Errorf("expected Status BLOCKED, got: %s", result.Status)
	}
}

// 5. Air-Gap Isolation: Target Has No Default Gateway -> Staging Eligible
func TestPhase3E4_ManagementConnectivity_AirgapIsolation_TargetHasNoGateway(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_airgap_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	adapters := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			SafetyRole:      network_mgmt.RoleCandidate,
			DefaultGateways: []string{}, // Zero default gateway
			IPv4Addresses:   []string{"192.168.50.1/24"},
		},
	}

	result, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}

	if !result.Eligible {
		t.Errorf("preflight must be ELIGIBLE for candidate adapter with zero default gateway: %s", result.Message)
	}
	if result.Status != network_mgmt.StatusReadyToStage {
		t.Errorf("expected Status READY_TO_STAGE, got: %s", result.Status)
	}
}

// 6. Valid Alternate Management Interface Confirmed -> Staging Eligible
func TestPhase3E4_ManagementConnectivity_ValidAlternateSecured(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_validalt_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	adapters := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			SafetyRole:      network_mgmt.RoleProtectedInternet,
			DefaultGateways: []string{"192.168.254.254"},
			IPv4Addresses:   []string{"192.168.254.20/24"},
		},
		{
			// Valid active Wi-Fi management adapter
			ID:              "nic_wifi_valid",
			InterfaceAlias:  "Wi-Fi",
			InterfaceIndex:  18,
			MACAddress:      "D8-F2-CA-95-4A-75",
			Status:          "Up",
			IsWireless:      true,
			SafetyRole:      network_mgmt.RoleProtectedInternet,
			DefaultGateways: []string{"192.168.100.1"},
			IPv4Addresses:   []string{"192.168.100.15/24"},
		},
	}

	result, err := mgr.PreflightStaging(profile, adapters)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}

	if !result.Eligible {
		t.Errorf("preflight should be ELIGIBLE when verified alternate management route exists: %s", result.Message)
	}
	if !result.ManagementRouteSecured {
		t.Errorf("expected ManagementRouteSecured true")
	}
	if result.ManagementInterfaceAlias != "Wi-Fi" {
		t.Errorf("expected management interface alias 'Wi-Fi', got: %s", result.ManagementInterfaceAlias)
	}
}

// 7. Synthetic Snapshot Rejection For Production Rollback (CRIT-03)
func TestPhase3E4_Snapshot_SyntheticRejectionForRollback(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_synth_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	syntheticSnap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_sample_synthetic_template",
		PersistentKey:  "nic_template",
		InterfaceAlias: "Ethernet",
		InterfaceGUID:  "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:     "B0:0C:D1:E6:53:31",
		Dhcpv4Enabled:  true,
		IsSynthetic:    true,
		Provenance:     "SYNTHETIC_TEST_FIXTURE",
	}

	err := mgr.ValidateSnapshotForRollback(syntheticSnap)
	if err == nil {
		t.Errorf("expected ValidateSnapshotForRollback to reject synthetic fixture")
	}
	if !strings.Contains(err.Error(), "synthetic snapshot fixture cannot be used for live host rollback") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Generated script contains check
	script := mgr.GenerateRollbackScript(syntheticSnap)
	if !strings.Contains(script, "$snap.is_synthetic -or $snap.provenance -eq 'SYNTHETIC_TEST_FIXTURE'") {
		t.Errorf("rollback script missing synthetic fixture rejection check")
	}
	if !strings.Contains(script, "-AllowSyntheticFixture") {
		t.Errorf("rollback script missing -AllowSyntheticFixture parameter")
	}
}

// 8. Foreign Host Identity Mismatch Rejection
func TestPhase3E4_Rollback_CrossHostMismatchRejection(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_host_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	foreignSnap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_foreign_host",
		PersistentKey:  "nic_foreign",
		InterfaceAlias: "Ethernet",
		InterfaceGUID:  "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:     "B0:0C:D1:E6:53:31",
		Dhcpv4Enabled:  true,
		IsSynthetic:    false,
		Provenance:     "LIVE_WINDOWS_CIM",
		HostIdentity:   "NON_EXISTENT_FOREIGN_SERVER_HOSTNAME_9999",
	}
	foreignSnap.IntegrityHash = network_mgmt.ComputeSnapshotHash(foreignSnap)

	err := mgr.ValidateSnapshotForRollback(foreignSnap)
	if err == nil {
		t.Errorf("expected ValidateSnapshotForRollback to reject foreign host snapshot")
	}
	if !strings.Contains(err.Error(), "snapshot host identity mismatch") {
		t.Errorf("unexpected error message: %v", err)
	}

	script := mgr.GenerateRollbackScript(foreignSnap)
	if !strings.Contains(script, "$snap.host_identity -and $snap.host_identity -ne $env:COMPUTERNAME") {
		t.Errorf("rollback script missing host identity mismatch check")
	}
}

// 9. Fail-Closed On Ambiguous Adapter Match
func TestPhase3E4_Rollback_FailClosed_AmbiguousAdapter(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_ambig_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_ambig",
		PersistentKey:  "nic_ambig",
		InterfaceAlias: "Ethernet",
		InterfaceGUID:  "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:     "B0:0C:D1:E6:53:31",
		Dhcpv4Enabled:  true,
	}

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	rollbackScript := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(rollbackScript, "$matchedAdapters.Count -gt 1") {
		t.Errorf("rollback script missing ambiguous adapter fail-closed check")
	}

	stagingScript := mgr.GenerateElevatedStagingScript(profile)
	if !strings.Contains(stagingScript, "$matchedAdapters.Count -gt 1") {
		t.Errorf("staging script missing ambiguous adapter fail-closed check")
	}
}

// 10. Fail-Closed On Missing Snapshot File (No Silent Fallback)
func TestPhase3E4_Rollback_FailClosed_MissingSnapshot(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_miss_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_miss",
		PersistentKey:  "nic_miss",
		InterfaceAlias: "Ethernet",
		InterfaceGUID:  "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:     "B0:0C:D1:E6:53:31",
		Dhcpv4Enabled:  true,
	}

	rollbackScript := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(rollbackScript, "Snapshot file not found at '$SnapshotPath' and no matching snapshot found in candidate directories") {
		t.Errorf("rollback script missing explicit fail-closed error for missing snapshot")
	}
	if !strings.Contains(rollbackScript, "Rollback is fail-closed. Staging baseline cannot be restored without a valid snapshot.") {
		t.Errorf("rollback script missing fail-closed message")
	}
}

// 11. Dynamic DNS Registration Uses Set-DnsClient and Get-DnsClient
func TestPhase3E4_DynamicDNS_SetDnsClientCmdlet(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_dnsclient_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:                     "snap_dnsclient",
		PersistentKey:                  "nic_dnsclient",
		InterfaceAlias:                 "Ethernet",
		InterfaceGUID:                  "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:                     "B0:0C:D1:E6:53:31",
		Dhcpv4Enabled:                  true,
		RegisterThisConnectionsAddress: true,
	}

	rollbackScript := mgr.GenerateRollbackScript(snap)
	if !strings.Contains(rollbackScript, "Set-DnsClient -InterfaceIndex $adapter.InterfaceIndex -RegisterThisConnectionsAddress:$snap.register_this_connections_address") {
		t.Errorf("rollback script must use Set-DnsClient for RegisterThisConnectionsAddress, not Set-NetIPInterface")
	}

	profile := network_mgmt.DefaultProfile(tempDir)
	stagingScript := mgr.GenerateElevatedStagingScript(profile)
	if !strings.Contains(stagingScript, "$dnsClient = Get-DnsClient -InterfaceIndex $idx") {
		t.Errorf("staging script must query Get-DnsClient for dynamic DNS registration status")
	}
}

// ==============================================================================
// PHASE 3E.5 INDEPENDENT RECOVERY VALIDATION & HARDWARE ADOPTION TEST SUITE
// ==============================================================================

// 1. Transaction Journal - Full Happy Path Lifecycle
func TestPhase3E5_TransactionJournal_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_tx_life_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	journal, err := network_mgmt.NewTransactionJournal(tempDir)
	if err != nil {
		t.Fatalf("failed to create transaction journal: %v", err)
	}

	profile := network_mgmt.DefaultProfile(tempDir)
	target := &network_mgmt.NetworkAdapter{
		ID:             "nic_test_target",
		InterfaceAlias: "Ethernet",
		MACAddress:     "B0:0C:D1:E6:53:31",
	}
	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:    "snap_tx_test",
		IntegrityHash: "sha256:11223344",
	}

	// 1. PREFLIGHT_PASSED
	tx, err := journal.BeginTransaction(profile, target, snap)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	if tx.CurrentState != network_mgmt.TxPreflightPassed {
		t.Errorf("expected state PREFLIGHT_PASSED, got: %s", tx.CurrentState)
	}

	// 2. SNAPSHOT_PERSISTED
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotPersisted, "Snapshot file saved to disk", nil); err != nil {
		t.Fatalf("failed transition to SNAPSHOT_PERSISTED: %v", err)
	}

	// 3. SNAPSHOT_VALIDATED
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotValidated, "Snapshot checksum validated", nil); err != nil {
		t.Fatalf("failed transition to SNAPSHOT_VALIDATED: %v", err)
	}

	// 4. OPERATOR_APPROVED
	if err := journal.Approve(tx.TransactionID, "admin@domain.local", "Authorized single-client test staging"); err != nil {
		t.Fatalf("failed operator approval: %v", err)
	}

	// 5. APPLY_STARTED
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxApplyStarted, "Applying static IP to Ethernet", nil); err != nil {
		t.Fatalf("failed transition to APPLY_STARTED: %v", err)
	}

	// 6. APPLY_VERIFIED
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxApplyVerified, "Static IP and zero gateway verified", nil); err != nil {
		t.Fatalf("failed transition to APPLY_VERIFIED: %v", err)
	}

	// 7. COMMITTED
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxCommitted, "Transaction committed successfully", nil); err != nil {
		t.Fatalf("failed transition to COMMITTED: %v", err)
	}

	loaded, err := journal.LoadTransaction(tx.TransactionID)
	if err != nil {
		t.Fatalf("failed to load transaction: %v", err)
	}
	if loaded.CurrentState != network_mgmt.TxCommitted {
		t.Errorf("expected final state COMMITTED, got: %s", loaded.CurrentState)
	}
	if len(loaded.History) != 7 {
		t.Errorf("expected 7 history transitions, got %d", len(loaded.History))
	}
}

// 2. Transaction Journal - Rollback Lifecycle & Terminal Recovery
func TestPhase3E5_TransactionJournal_RollbackLifecycle(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_tx_rb_*")
	defer os.RemoveAll(tempDir)

	journal, _ := network_mgmt.NewTransactionJournal(tempDir)
	profile := network_mgmt.DefaultProfile(tempDir)
	target := &network_mgmt.NetworkAdapter{ID: "nic_target", MACAddress: "B0:0C:D1:E6:53:31"}

	tx, _ := journal.BeginTransaction(profile, target, nil)
	_ = journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotPersisted, "snapshot persisted", nil)
	_ = journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotValidated, "snapshot validated", nil)
	_ = journal.Approve(tx.TransactionID, "admin", "approved")
	_ = journal.Transition(tx.TransactionID, network_mgmt.TxApplyStarted, "apply started", nil)

	// Simulate apply failure triggering rollback
	applyErr := fmt.Errorf("ip conflict detected")
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxRollbackRequired, "Apply failed: IP conflict", applyErr); err != nil {
		t.Fatalf("failed transition to ROLLBACK_REQUIRED: %v", err)
	}

	if err := journal.Transition(tx.TransactionID, network_mgmt.TxRollbackStarted, "Invoking rollback script", nil); err != nil {
		t.Fatalf("failed transition to ROLLBACK_STARTED: %v", err)
	}

	if err := journal.Transition(tx.TransactionID, network_mgmt.TxRollbackVerified, "Restoration verified from snapshot", nil); err != nil {
		t.Fatalf("failed transition to ROLLBACK_VERIFIED: %v", err)
	}

	loaded, _ := journal.LoadTransaction(tx.TransactionID)
	if loaded.CurrentState != network_mgmt.TxRollbackVerified {
		t.Errorf("expected state ROLLBACK_VERIFIED, got: %s", loaded.CurrentState)
	}
	if loaded.ErrorMessage != "ip conflict detected" {
		t.Errorf("expected recorded error message, got: %s", loaded.ErrorMessage)
	}
}

// 3. Transaction Journal - Illegal State Transitions Blocked
func TestPhase3E5_TransactionJournal_IllegalTransitions(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_tx_illegal_*")
	defer os.RemoveAll(tempDir)

	journal, _ := network_mgmt.NewTransactionJournal(tempDir)
	profile := network_mgmt.DefaultProfile(tempDir)
	target := &network_mgmt.NetworkAdapter{ID: "nic_target", MACAddress: "B0:0C:D1:E6:53:31"}

	tx, _ := journal.BeginTransaction(profile, target, nil)

	// Attempting to jump directly from PREFLIGHT_PASSED to COMMITTED must fail
	err := journal.Transition(tx.TransactionID, network_mgmt.TxCommitted, "illegal skip", nil)
	if err == nil {
		t.Errorf("expected illegal state transition to be rejected")
	}
	if !strings.Contains(err.Error(), "illegal state transition") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// 4. Transaction Journal - Operator Approval Gate Enforcement
func TestPhase3E5_TransactionJournal_OperatorApprovalEnforcement(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_tx_gate_*")
	defer os.RemoveAll(tempDir)

	journal, _ := network_mgmt.NewTransactionJournal(tempDir)
	profile := network_mgmt.DefaultProfile(tempDir)
	target := &network_mgmt.NetworkAdapter{ID: "nic_target", MACAddress: "B0:0C:D1:E6:53:31"}

	tx, _ := journal.BeginTransaction(profile, target, nil)
	_ = journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotPersisted, "snapshot persisted", nil)
	_ = journal.Transition(tx.TransactionID, network_mgmt.TxSnapshotValidated, "snapshot validated", nil)

	// Attempt to start apply without operator approval
	err := journal.Transition(tx.TransactionID, network_mgmt.TxApplyStarted, "unapproved apply", nil)
	if err == nil {
		t.Errorf("expected transition to APPLY_STARTED without OPERATOR_APPROVED to be rejected")
	}

	// Now approve and verify transition succeeds
	if err := journal.Approve(tx.TransactionID, "admin_user", "explicit consent"); err != nil {
		t.Fatalf("approval failed: %v", err)
	}
	if err := journal.Transition(tx.TransactionID, network_mgmt.TxApplyStarted, "approved apply", nil); err != nil {
		t.Fatalf("expected approved apply to succeed: %v", err)
	}
}

// 5. Transaction Journal - Simulated Crash / Interrupted Transaction Detection
func TestPhase3E5_TransactionJournal_InterruptedDetectionOnRestart(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_tx_crash_*")
	defer os.RemoveAll(tempDir)

	// Session 1: Process begins staging and crashes mid-flight in APPLY_STARTED
	journal1, _ := network_mgmt.NewTransactionJournal(tempDir)
	profile := network_mgmt.DefaultProfile(tempDir)
	target := &network_mgmt.NetworkAdapter{ID: "nic_target", MACAddress: "B0:0C:D1:E6:53:31"}

	tx, _ := journal1.BeginTransaction(profile, target, nil)
	_ = journal1.Transition(tx.TransactionID, network_mgmt.TxSnapshotPersisted, "snapshot persisted", nil)
	_ = journal1.Transition(tx.TransactionID, network_mgmt.TxSnapshotValidated, "snapshot validated", nil)
	_ = journal1.Approve(tx.TransactionID, "admin", "approved")
	_ = journal1.Transition(tx.TransactionID, network_mgmt.TxApplyStarted, "apply in progress", nil)

	// Simulate unexpected process termination (power loss / crash)
	// Session 2: System reboots and instantiates a new journal instance
	journal2, err := network_mgmt.NewTransactionJournal(tempDir)
	if err != nil {
		t.Fatalf("failed to instantiate journal on restart: %v", err)
	}

	interrupted, err := journal2.DetectInterruptedTransactions()
	if err != nil {
		t.Fatalf("failed to detect interrupted transactions: %v", err)
	}
	if len(interrupted) != 1 {
		t.Fatalf("expected 1 interrupted transaction, got %d", len(interrupted))
	}

	intTx := interrupted[0]
	if !intTx.Interrupted || !intTx.RequiresReview {
		t.Errorf("transaction must be marked Interrupted: true and RequiresReview: true")
	}
	if intTx.CurrentState != network_mgmt.TxApplyStarted {
		t.Errorf("expected interrupted state APPLY_STARTED, got: %s", intTx.CurrentState)
	}

	// Attempting to transition interrupted transaction without operator review MUST be blocked
	err = journal2.Transition(intTx.TransactionID, network_mgmt.TxApplyVerified, "silent resume attempt", nil)
	if err == nil {
		t.Errorf("silent resumption of interrupted transaction must be BLOCKED")
	}

	// Operator review resolves the interrupted transaction
	if err := journal2.AcknowledgeInterruption(intTx.TransactionID, "Operator verified state; closing as failed", true); err != nil {
		t.Fatalf("failed to acknowledge interruption: %v", err)
	}

	resolved, _ := journal2.LoadTransaction(intTx.TransactionID)
	if resolved.CurrentState != network_mgmt.TxRecoveryFailed {
		t.Errorf("expected resolved state RECOVERY_FAILED, got: %s", resolved.CurrentState)
	}
}

// 6. Management Path Independence - Strict Rejection of Virtual and Inconclusive Adapters
func TestPhase3E5_ManagementPath_RoutingIndependence(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_mgmt_indep_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	profile.SelectedAdapter.Alias = "Ethernet"
	profile.SelectedAdapter.MAC = "B0-0C-D1-E6-53-31"

	// Case A: Alternate adapter is a Virtual / Hyper-V adapter
	adaptersWithVirtual := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			DefaultGateways: []string{"192.168.254.254"},
			IPv4Addresses:   []string{"192.168.254.20/24"},
		},
		{
			ID:                   "nic_wsl",
			InterfaceAlias:       "vEthernet (WSL)",
			InterfaceDescription: "Hyper-V Virtual Ethernet Adapter",
			InterfaceIndex:       38,
			MACAddress:           "00-15-5D-9D-34-70",
			Status:               "Up",
			IsVirtual:            true, // Virtual!
			SafetyRole:           network_mgmt.RoleVirtualBlocked,
			DefaultGateways:      []string{"172.28.128.254"},
			IPv4Addresses:        []string{"172.28.128.1/20"},
		},
	}

	res, err := mgr.PreflightStaging(profile, adaptersWithVirtual)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}
	if res.Eligible {
		t.Errorf("preflight must be BLOCKED when only alternate is a virtual adapter")
	}

	// Case B: Alternate adapter has deprecated IPv4 address
	adaptersWithDeprecated := []network_mgmt.NetworkAdapter{
		{
			ID:              "nic_target",
			InterfaceAlias:  "Ethernet",
			InterfaceIndex:  8,
			MACAddress:      "B0-0C-D1-E6-53-31",
			Status:          "Up",
			IsPhysical:      true,
			DefaultGateways: []string{"192.168.254.254"},
			IPv4Addresses:   []string{"192.168.254.20/24"},
		},
		{
			ID:                   "nic_alt_deprecated",
			InterfaceAlias:       "Ethernet 2",
			InterfaceDescription: "Intel Gigabit Adapter",
			InterfaceIndex:       14,
			MACAddress:           "00-1B-21-AA-BB-CC",
			Status:               "Up",
			IsPhysical:           true,
			DefaultGateways:      []string{"192.168.1.1"},
			IPv4Addresses:        []string{"0.0.0.0/0"}, // Unbound / deprecated
		},
	}

	res2, _ := mgr.PreflightStaging(profile, adaptersWithDeprecated)
	if res2.Eligible {
		t.Errorf("preflight must be BLOCKED when alternate adapter IPv4 address is invalid/unbound")
	}
}

// 7. Snapshot Integrity - Deterministic Canonical SHA-256 Checksum
func TestPhase3E5_SnapshotIntegrity_DeterministicHash(t *testing.T) {
	snap1 := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_det_1",
		PersistentKey:       "nic_det_key",
		InterfaceGUID:       "{AAAA-BBBB-CCCC}",
		MACAddress:          "B0:0C:D1:E6:53:31",
		InterfaceIndex:      8,
		Dhcpv4Enabled:       false,
		Dhcpv6Enabled:       false,
		IPv4Addresses: []network_mgmt.IPv4AddressEntry{
			{IPAddress: "192.168.50.1", PrefixLength: 24},
			{IPAddress: "192.168.50.2", PrefixLength: 24},
		},
		IPv6Addresses:       []string{"fe80::1", "2001:db8::1"},
		IPv4DefaultGateways: []string{"192.168.50.254"},
		IPv4DNSServers:      []string{"1.1.1.1", "8.8.8.8"},
		InterfaceMetricIPv4: 10,
		InterfaceMetricIPv6: 20,
		NetworkCategory:     "Private",
		IsSynthetic:         false,
		Provenance:          "LIVE_WINDOWS_CIM",
		HostIdentity:        "TESTHOST|guid123",
	}

	// snap2 has elements in different slice order
	snap2 := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:          "snap_det_1",
		PersistentKey:       "nic_det_key",
		InterfaceGUID:       "{aaaa-bbbb-cccc}", // different casing
		MACAddress:          "B0-0C-D1-E6-53-31", // hyphenated instead of colon
		InterfaceIndex:      8,
		Dhcpv4Enabled:       false,
		Dhcpv6Enabled:       false,
		IPv4Addresses: []network_mgmt.IPv4AddressEntry{
			{IPAddress: "192.168.50.2", PrefixLength: 24}, // reversed
			{IPAddress: "192.168.50.1", PrefixLength: 24},
		},
		IPv6Addresses:       []string{"2001:db8::1", "fe80::1"}, // reversed
		IPv4DefaultGateways: []string{"192.168.50.254"},
		IPv4DNSServers:      []string{"8.8.8.8", "1.1.1.1"}, // reversed
		InterfaceMetricIPv4: 10,
		InterfaceMetricIPv6: 20,
		NetworkCategory:     "Private",
		IsSynthetic:         false,
		Provenance:          "LIVE_WINDOWS_CIM",
		HostIdentity:        "TESTHOST|guid123",
	}

	hash1 := network_mgmt.ComputeSnapshotHash(snap1)
	hash2 := network_mgmt.ComputeSnapshotHash(snap2)

	if hash1 != hash2 {
		t.Errorf("expected deterministic hash calculation regardless of slice ordering or MAC formatting:\nhash1: %s\nhash2: %s", hash1, hash2)
	}

	// Mutating any field must change the checksum
	snap2.IPv4Addresses[0].IPAddress = "192.168.50.99"
	hash3 := network_mgmt.ComputeSnapshotHash(snap2)
	if hash1 == hash3 {
		t.Errorf("mutated snapshot must produce a different hash")
	}
}

// 8. Snapshot Integrity - Missing Hash Strict Rejection
func TestPhase3E5_SnapshotIntegrity_MissingHashRejection(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_snap_nohash_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	snap := &network_mgmt.WindowsAdapterSnapshot{
		SnapshotID:     "snap_nohash",
		PersistentKey:  "nic_nohash",
		InterfaceAlias: "Ethernet",
		InterfaceGUID:  "{1111-2222}",
		MACAddress:     "AA:BB:CC:DD:EE:FF",
		Dhcpv4Enabled:  true,
		IsSynthetic:    false,
		Provenance:     "LIVE_WINDOWS_CIM",
		IntegrityHash:  "", // Missing!
	}

	err := mgr.ValidateSnapshotForRollback(snap)
	if err == nil {
		t.Errorf("expected ValidateSnapshotForRollback to reject snapshot with empty integrity_hash")
	}
	if !strings.Contains(err.Error(), "missing mandatory integrity checksum") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// 9. Hardware Adoption - Realtek to Intel Physical NIC Replacement
func TestPhase3E5_HardwareAdoption_RealtekToIntelReplacement(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_adopt_intel_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	oldRealtekKey := network_mgmt.ComputePersistentKey("B0-0C-D1-E6-53-31", "{620271F0-ABF2-492B-B226-1871CF04E0BB}", "Realtek Gaming GbE Family Controller")
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: oldRealtekKey,
		Alias:         "Ethernet",
		Index:         8,
		GUID:          "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MAC:           "B0-0C-D1-E6-53-31",
	}
	profile.Status = network_mgmt.StatusSelected

	// Hardware replacement event: Realtek card is removed, Intel I219-V is installed
	newIntelAdapter := network_mgmt.NetworkAdapter{
		ID:                   network_mgmt.ComputePersistentKey("00-1B-21-AA-BB-CC", "{AABBCCDD-1122-3344-5566-778899AABBCC}", "Intel(R) Ethernet Connection I219-V"),
		InterfaceAlias:       "Ethernet",
		InterfaceDescription: "Intel(R) Ethernet Connection I219-V",
		InterfaceIndex:       14,
		InterfaceGUID:        "{AABBCCDD-1122-3344-5566-778899AABBCC}",
		MACAddress:           "00-1B-21-AA-BB-CC",
		Status:               "Up",
		IsPhysical:           true,
		SafetyRole:           network_mgmt.RoleCandidate,
	}

	adapters := []network_mgmt.NetworkAdapter{newIntelAdapter}

	// Reconcile detects old Realtek is missing
	status, matched := mgr.ReconcileAdapter(profile, adapters)
	if status != network_mgmt.StatusMissing || matched != nil {
		t.Errorf("expected old Realtek to be detected as MISSING, got: %s", status)
	}

	// Operator adopts the new Intel hardware
	if err := mgr.AdoptAdapter(profile, newIntelAdapter); err != nil {
		t.Fatalf("failed to adopt replacement Intel adapter: %v", err)
	}

	if profile.SelectedAdapter.PersistentKey != newIntelAdapter.ID {
		t.Errorf("expected adopted key %s, got: %s", newIntelAdapter.ID, profile.SelectedAdapter.PersistentKey)
	}
	if profile.SelectedAdapter.Index != 14 {
		t.Errorf("expected adopted index 14, got: %d", profile.SelectedAdapter.Index)
	}
	if profile.SelectedAdapter.MAC != "00-1B-21-AA-BB-CC" {
		t.Errorf("expected adopted MAC 00-1B-21-AA-BB-CC, got: %s", profile.SelectedAdapter.MAC)
	}
	if profile.Status != network_mgmt.StatusSelected {
		t.Errorf("expected profile status SELECTED after adoption, got: %s", profile.Status)
	}
}

// 10. Hardware Adoption - Replacement with USB Ethernet Adapter
func TestPhase3E5_HardwareAdoption_USBToPCIeReplacement(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_adopt_usb_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	usbAdapter := network_mgmt.NetworkAdapter{
		ID:                   network_mgmt.ComputePersistentKey("00-E0-4C-68-01-23", "{11223344-5566-7788-99AA-BBCCDDEEFF00}", "Realtek USB GbE Controller"),
		InterfaceAlias:       "Ethernet 2",
		InterfaceDescription: "Realtek USB GbE Controller",
		InterfaceIndex:       22,
		InterfaceGUID:        "{11223344-5566-7788-99AA-BBCCDDEEFF00}",
		MACAddress:           "00-E0-4C-68-01-23",
		Status:               "Up",
		IsPhysical:           true,
		SafetyRole:           network_mgmt.RoleCandidate,
	}

	if err := mgr.AdoptAdapter(profile, usbAdapter); err != nil {
		t.Fatalf("failed to adopt USB Ethernet adapter: %v", err)
	}
	if profile.SelectedAdapter.Alias != "Ethernet 2" {
		t.Errorf("expected alias 'Ethernet 2', got: %s", profile.SelectedAdapter.Alias)
	}
}

// 11. Hardware Adoption - Protected Management Adapter Cannot Be Adopted
func TestPhase3E5_HardwareAdoption_ProtectedManagementCannotBeAdopted(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_adopt_prot_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)

	// Attempting to adopt a corporate Wi-Fi adapter
	wifiAdapter := network_mgmt.NetworkAdapter{
		InterfaceAlias:       "Wi-Fi",
		InterfaceDescription: "Intel Wireless-AC 9560",
		IsWireless:           true,
		SafetyRole:           network_mgmt.RoleProtectedInternet,
	}

	err := mgr.AdoptAdapter(profile, wifiAdapter)
	if err == nil {
		t.Errorf("expected adoption of wireless adapter to fail")
	}
	if !strings.Contains(err.Error(), "cannot adopt protected corporate/wireless adapter") {
		t.Errorf("unexpected error: %v", err)
	}

	// Attempting to adopt a virtual adapter
	virtualAdapter := network_mgmt.NetworkAdapter{
		InterfaceAlias:       "vEthernet (WSL)",
		InterfaceDescription: "Hyper-V Virtual Adapter",
		IsVirtual:            true,
		SafetyRole:           network_mgmt.RoleVirtualBlocked,
	}

	err2 := mgr.AdoptAdapter(profile, virtualAdapter)
	if err2 == nil {
		t.Errorf("expected adoption of virtual adapter to fail")
	}
}

// 12. Hardware Adoption - Survives Windows InterfaceIndex Renumbering
func TestPhase3E5_HardwareAdoption_SurvivesInterfaceIndexChange(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "nkb_adopt_renum_*")
	defer os.RemoveAll(tempDir)
	mgr, _ := network_mgmt.NewNetworkConfigManager(tempDir)

	profile := network_mgmt.DefaultProfile(tempDir)
	key := network_mgmt.ComputePersistentKey("B0-0C-D1-E6-53-31", "{620271F0-ABF2-492B-B226-1871CF04E0BB}", "Realtek Gaming GbE Family Controller")
	profile.SelectedAdapter = network_mgmt.AdapterMatch{
		PersistentKey: key,
		Alias:         "Ethernet",
		Index:         8, // Original index
		GUID:          "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MAC:           "B0-0C-D1-E6-53-31",
	}
	profile.Status = network_mgmt.StatusSelected

	// Windows driver update renumbers interface to index 27
	renumberedAdapter := network_mgmt.NetworkAdapter{
		ID:                   key, // Stable persistent key survives
		InterfaceAlias:       "Ethernet",
		InterfaceDescription: "Realtek Gaming GbE Family Controller",
		InterfaceIndex:       27, // Changed!
		InterfaceGUID:        "{620271F0-ABF2-492B-B226-1871CF04E0BB}",
		MACAddress:           "B0-0C-D1-E6-53-31",
		Status:               "Up",
		IsPhysical:           true,
		SafetyRole:           network_mgmt.RoleCandidate,
	}

	status, matched := mgr.ReconcileAdapter(profile, []network_mgmt.NetworkAdapter{renumberedAdapter})
	if status != network_mgmt.StatusSelected || matched == nil {
		t.Errorf("expected adapter to reconcile successfully, got status: %s", status)
	}
	if profile.SelectedAdapter.Index != 27 {
		t.Errorf("expected profile index to update to 27, got: %d", profile.SelectedAdapter.Index)
	}
}
