package network_mgmt

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Safety roles for network interfaces
const (
	RoleCandidate         = "CANDIDATE"          // Dedicated Physical GbE Adapter
	RoleProtectedInternet = "PROTECTED_INTERNET" // Corporate LAN or Wi-Fi with active default gateway
	RoleVirtualBlocked    = "VIRTUAL_BLOCKED"    // Hyper-V, WSL, VPN, Loopback, Tunnel
	RoleUnknown           = "UNKNOWN"
)

// Network profile status states (Phase 3E.1 Granular Lifecycle)
const (
	StatusDiscovered     = "DISCOVERED"
	StatusProtected      = "PROTECTED"
	StatusCandidate      = "CANDIDATE"
	StatusSelected       = "SELECTED"
	StatusValidated      = "VALIDATED"
	StatusReadyToStage   = "READY_TO_STAGE"
	StatusStaging        = "STAGING"
	StatusStaged         = "STAGED"
	StatusActive         = "ACTIVE"
	StatusMissing        = "MISSING"
	StatusRollback       = "ROLLBACK"
	StatusRollbackFailed = "ROLLBACK_FAILED"
	StatusBlocked        = "BLOCKED"
	StatusError          = "ERROR"
)

// Network operating modes
const (
	ModeIsolatedStandalone = "isolated_standalone" // Isolated switch, NKB provides Port 67 DHCP & PXE
	ModeProxyDHCPShared    = "proxydhcp_shared"    // Existing router provides DHCP, NKB provides Port 4011 ProxyDHCP
	ModeDiagnostic         = "diagnostic"         // Read-only diagnostic banner, zero disks
)

// NetworkAdapter represents detailed inventory and safety classification for an adapter.
type NetworkAdapter struct {
	ID                   string   `json:"id"`                    // Stable persistent hash
	InterfaceAlias       string   `json:"interface_alias"`
	InterfaceDescription string   `json:"interface_description"`
	InterfaceIndex       int      `json:"interface_index"`
	InterfaceGUID        string   `json:"interface_guid"`
	MACAddress           string   `json:"mac_address"`
	Status               string   `json:"status"`                // "Up", "Down", "Disconnected"
	LinkSpeed            string   `json:"link_speed"`
	IPv4Addresses        []string `json:"ipv4_addresses"`
	IPv6Addresses        []string `json:"ipv6_addresses"`
	DefaultGateways      []string `json:"default_gateways"`
	DNSServers                     []string `json:"dns_servers"`
	DHCPEnabled                    bool     `json:"dhcp_enabled"`
	DHCPv6Enabled                  bool     `json:"dhcpv6_enabled"`
	InterfaceMetricIPv4            int      `json:"interface_metric_ipv4"`
	InterfaceMetricIPv6            int      `json:"interface_metric_ipv6"`
	AutomaticMetricIPv4            bool     `json:"automatic_metric_ipv4"`
	AutomaticMetricIPv6            bool     `json:"automatic_metric_ipv6"`
	DNSConfigurationSource         string   `json:"dns_configuration_source,omitempty"`
	RegisterThisConnectionsAddress bool     `json:"register_this_connections_address"`
	NetworkCategory                string   `json:"network_category,omitempty"`
	IsPhysical                     bool     `json:"is_physical"`
	IsVirtual            bool     `json:"is_virtual"`
	IsWireless           bool     `json:"is_wireless"`
	IsPrimaryInternet    bool     `json:"is_primary_internet"`
	SafetyRole           string   `json:"safety_role"`           // CANDIDATE, PROTECTED_INTERNET, VIRTUAL_BLOCKED
	SafetyWarning        string   `json:"safety_warning,omitempty"`
}

// ComputePersistentKey generates a stable identity token based on MAC, GUID, and Hardware Description.
// This survives interface index changes and alias renames in Windows.
func ComputePersistentKey(mac, guid, desc string) string {
	normMAC := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(mac, "-", ":"), " ", ""))
	normGUID := strings.ToUpper(strings.TrimSpace(guid))
	normDesc := strings.ToLower(strings.TrimSpace(desc))
	raw := fmt.Sprintf("%s|%s|%s", normMAC, normGUID, normDesc)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("nic_%x", h[:8])
}

// AdapterMatch encapsulates the criteria for re-identifying an adopted adapter.
type AdapterMatch struct {
	PersistentKey string `json:"persistent_key"`
	Alias         string `json:"alias"`
	Description   string `json:"description"`
	Index         int    `json:"index"`
	GUID          string `json:"guid"`
	MAC           string `json:"mac"`
}

// IPv4Config defines the target IP parameters for the dedicated server interface.
type IPv4Config struct {
	IPAddress      string   `json:"ip_address"`      // e.g. "192.168.50.1"
	PrefixLength   int      `json:"prefix_length"`   // e.g. 24
	DefaultGateway string   `json:"default_gateway"` // optional (empty in airgap)
	DNSServers     []string `json:"dns_servers"`     // optional (empty in airgap)
}

// DHCPConfig defines the IP leasing parameters for standalone DHCP mode.
// Note: Enabled / PoolConfigured indicates the DHCP dynamic pool is configured in the profile.
// Runtime activation of the UDP 67 daemon is separately controlled by PXEConfig.EnableStandaloneDHCP.
type DHCPConfig struct {
	Enabled              bool   `json:"enabled"`
	PoolConfigured       bool   `json:"pool_configured"`        // Explicit flag: leasing pool definition is active
	PoolStart            string `json:"pool_start"`             // e.g. "192.168.50.100"
	PoolEnd              string `json:"pool_end"`               // e.g. "192.168.50.200"
	LeaseDurationSeconds int    `json:"lease_duration_seconds"` // e.g. 120 or 7200
	SubnetMask           string `json:"subnet_mask"`            // e.g. "255.255.255.0"
}

// PXEConfig defines bootstrap protocol listener settings.
type PXEConfig struct {
	EnableStandaloneDHCP bool   `json:"enable_standalone_dhcp"`
	EnableProxyDHCP     bool   `json:"enable_proxy_dhcp"`
	EnableTFTP          bool   `json:"enable_tftp"`
	EnableHTTPBoot      bool   `json:"enable_http_boot"`
	TFTPPort            int    `json:"tftp_port"`
	ProxyDHCPPort       int    `json:"proxy_dhcp_port"`
	HTTPBootPort        int    `json:"http_boot_port"`
	BIOSBootloader      string `json:"bios_bootloader"` // e.g. "bios/undionly.kpxe"
	UEFIBootloader      string `json:"uefi_bootloader"` // e.g. "uefi/ipxe.efi"
}

// NetworkProfile is the persistent, hardware-independent network configuration document.
type NetworkProfile struct {
	Version                  int          `json:"version"`          // Schema version (1)
	ProfileID                string       `json:"profile_id"`       // Unique ID
	ProfileName              string       `json:"profile_name"`     // Friendly name
	NetworkMode              string       `json:"network_mode"`     // isolated_standalone, proxydhcp_shared, diagnostic
	SelectedAdapter          AdapterMatch `json:"selected_adapter"` // Current adopted adapter criteria
	IPv4Config               IPv4Config   `json:"ipv4_config"`      // Dedicated server IP
	DHCPConfig               DHCPConfig   `json:"dhcp_config"`      // Client leasing pool
	PXEConfig                PXEConfig    `json:"pxe_config"`       // PXE bootstrap daemons
	StorageRoot              string       `json:"storage_root"`     // e.g. "C:\\NKB-PoC\\"
	Status                   string       `json:"status"`           // DISCOVERED, PROTECTED, CANDIDATE, VALIDATED, READY_TO_STAGE, STAGING, STAGED, ACTIVE, MISSING, ROLLBACK, ROLLBACK_FAILED, BLOCKED, ERROR
	LastVerification         string       `json:"last_verification"`// "PASS", "FAIL", "BLOCKED", "NOT_TESTED"
	LastVerifiedAt           *time.Time   `json:"last_verified_at,omitempty"`
	LastUpdatedAt            time.Time    `json:"last_updated_at"`
	ActiveSnapshotID         string       `json:"active_snapshot_id,omitempty"`
	StagingTransactionStatus string                 `json:"staging_transaction_status,omitempty"`
	ManagementRouteSecured   bool                   `json:"management_route_secured"`
	AlternateManagementNIC   string                 `json:"alternate_management_nic,omitempty"`
	ManagementPolicy         ManagementSafetyPolicy `json:"management_policy"`
}

// ManagementSafetyPolicy defines restrictions to preserve host connectivity.
type ManagementSafetyPolicy struct {
	RequireSeparateManagementNIC bool     `json:"require_separate_management_nic"`
	CheckGatewayReachability     bool     `json:"check_gateway_reachability"`
	ManagementTestEndpoint       string   `json:"management_test_endpoint,omitempty"`
	AllowedManagementTypes       []string `json:"allowed_management_types,omitempty"`
}

// ConflictReport summarizes pre-staging conflict analysis.
type ConflictReport struct {
	HasConflict       bool     `json:"has_conflict"`
	OverlappingSubnet bool     `json:"overlapping_subnet"`
	IPConflict        bool     `json:"ip_conflict"`
	GatewayConflict   bool     `json:"gateway_conflict"`
	ProtectedAdapter  bool     `json:"protected_adapter"`
	Warnings          []string `json:"warnings"`
	Errors            []string `json:"errors"`
}

// StagingPreview contains before-and-after values for administrator review.
type StagingPreview struct {
	AdapterName     string         `json:"adapter_name"`
	AdapterMAC      string         `json:"adapter_mac"`
	AdapterDesc     string         `json:"adapter_desc"`
	CurrentIPv4     []string       `json:"current_ipv4"`
	CurrentGateway  []string       `json:"current_gateway"`
	CurrentDNS      []string       `json:"current_dns"`
	ProposedIPv4    string         `json:"proposed_ipv4"`
	ProposedGateway string         `json:"proposed_gateway"`
	ProposedDNS     []string       `json:"proposed_dns"`
	ProposedPool    string         `json:"proposed_pool"`
	NetworkMode     string         `json:"network_mode"`
	Conflicts       ConflictReport `json:"conflicts"`
}

// WindowsAdapterSnapshot captures the complete, real OS networking configuration
// of an adapter prior to any staging actions across all mandatory fields with provenance tracking.
type WindowsAdapterSnapshot struct {
	SnapshotID                     string             `json:"snapshot_id"`
	Timestamp                      time.Time          `json:"timestamp"`
	PersistentKey                  string             `json:"persistent_key"`
	InterfaceGUID                  string             `json:"interface_guid"`
	InterfaceIndex                 int                `json:"interface_index"`
	InterfaceAlias                 string             `json:"interface_alias"`
	MACAddress                     string             `json:"mac_address"`
	HardwareDescription            string             `json:"hardware_description"`
	Description                    string             `json:"description"` // Alias for backward compatibility
	Status                         string             `json:"status"`
	DhcpEnabled                    bool               `json:"dhcp_enabled"` // Backward compatibility alias
	Dhcpv4Enabled                  bool               `json:"dhcpv4_enabled"`
	Dhcpv6Enabled                  bool               `json:"dhcpv6_enabled"`
	IPv4Addresses                  []IPv4AddressEntry `json:"ipv4_addresses"`
	IPv6Addresses                  []string           `json:"ipv6_addresses"`
	IPv6AddressesDetailed          []IPv6AddressEntry `json:"ipv6_addresses_detailed,omitempty"`
	DefaultGateways                []string           `json:"default_gateways"` // Backward compatibility alias
	IPv4DefaultGateways            []string           `json:"ipv4_default_gateways"`
	IPv6DefaultGateways            []string           `json:"ipv6_default_gateways"`
	Routes                         []RouteEntry       `json:"routes"`
	RouteMetrics                   []RouteMetricEntry `json:"route_metrics,omitempty"`
	InterfaceMetricIPv4            int                `json:"interface_metric_ipv4"`
	InterfaceMetricIPv6            int                `json:"interface_metric_ipv6"`
	AutomaticMetricIPv4            bool               `json:"automatic_metric_ipv4"`
	AutomaticMetricIPv6            bool               `json:"automatic_metric_ipv6"`
	DNSServers                     []string           `json:"dns_servers"` // Backward compatibility alias
	IPv4DNSServers                 []string           `json:"ipv4_dns_servers"`
	IPv6DNSServers                 []string           `json:"ipv6_dns_servers"`
	DNSFromDHCP                    bool               `json:"dns_from_dhcp"`
	DNSConfigurationSource         string             `json:"dns_configuration_source"` // "DHCP" or "Manual"
	RegisterThisConnectionsAddress bool               `json:"register_this_connections_address"`
	NetworkCategory                string             `json:"network_category"` // "Private", "Public", "DomainAuthenticated"
	IsSynthetic                    bool               `json:"is_synthetic"`
	Provenance                     string             `json:"provenance"` // "LIVE_WINDOWS_CIM" or "SYNTHETIC_TEST_FIXTURE"
	HostIdentity                   string             `json:"host_identity"` // Computer name / hostname
	IntegrityHash                  string             `json:"integrity_hash"`
}

// CanonicalSnapshotPayload builds an unambiguous, sorted string representation of snapshot fields
// that produces identical SHA-256 checksums in both Go and PowerShell.
func CanonicalSnapshotPayload(snap *WindowsAdapterSnapshot) string {
	var ipStrs []string
	for _, e := range snap.IPv4Addresses {
		ipStrs = append(ipStrs, fmt.Sprintf("%s/%d", strings.TrimSpace(e.IPAddress), e.PrefixLength))
	}
	sort.Strings(ipStrs)

	v6Strs := make([]string, 0, len(snap.IPv6Addresses))
	for _, v6 := range snap.IPv6Addresses {
		clean := strings.TrimSpace(v6)
		if clean != "" {
			v6Strs = append(v6Strs, strings.ToLower(clean))
		}
	}
	sort.Strings(v6Strs)

	gwStrs := make([]string, 0, len(snap.IPv4DefaultGateways))
	for _, gw := range snap.IPv4DefaultGateways {
		clean := strings.TrimSpace(gw)
		if clean != "" && clean != "0.0.0.0" {
			gwStrs = append(gwStrs, clean)
		}
	}
	sort.Strings(gwStrs)

	dnsStrs := make([]string, 0, len(snap.IPv4DNSServers))
	for _, d := range snap.IPv4DNSServers {
		clean := strings.TrimSpace(d)
		if clean != "" {
			dnsStrs = append(dnsStrs, clean)
		}
	}
	sort.Strings(dnsStrs)

	normMAC := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(snap.MACAddress, "-", ":"), " ", ""))
	normGUID := strings.ToUpper(strings.TrimSpace(snap.InterfaceGUID))

	return fmt.Sprintf("ID:%s|KEY:%s|GUID:%s|MAC:%s|INDEX:%d|DHCP4:%t|DHCP6:%t|IPS:%s|IPV6:%s|GWS:%s|DNS:%s|METRIC4:%d|METRIC6:%d|REGDNS:%t|CAT:%s|SYNTH:%t|PROV:%s|HOST:%s",
		snap.SnapshotID,
		snap.PersistentKey,
		normGUID,
		normMAC,
		snap.InterfaceIndex,
		snap.Dhcpv4Enabled,
		snap.Dhcpv6Enabled,
		strings.Join(ipStrs, ","),
		strings.Join(v6Strs, ","),
		strings.Join(gwStrs, ","),
		strings.Join(dnsStrs, ","),
		snap.InterfaceMetricIPv4,
		snap.InterfaceMetricIPv6,
		snap.RegisterThisConnectionsAddress,
		snap.NetworkCategory,
		snap.IsSynthetic,
		snap.Provenance,
		snap.HostIdentity,
	)
}

// ComputeSnapshotHash calculates the canonical SHA-256 integrity checksum for a snapshot.
func ComputeSnapshotHash(snap *WindowsAdapterSnapshot) string {
	raw := CanonicalSnapshotPayload(snap)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("sha256:%x", h)
}

type IPv4AddressEntry struct {
	IPAddress    string `json:"ip_address"`
	PrefixLength int    `json:"prefix_length"`
}

type IPv6AddressEntry struct {
	IPAddress    string `json:"ip_address"`
	PrefixLength int    `json:"prefix_length"`
}

type RouteEntry struct {
	DestinationPrefix string `json:"destination_prefix"`
	NextHop           string `json:"next_hop"`
	RouteMetric       int    `json:"route_metric"`
	AddressFamily     string `json:"address_family,omitempty"`
	InterfaceIndex    int    `json:"interface_index,omitempty"`
}

type RouteMetricEntry struct {
	DestinationPrefix string `json:"destination_prefix"`
	Metric            int    `json:"metric"`
}

// PreflightResult evaluates whether the target adapter can safely transition to staging.
type PreflightResult struct {
	Eligible                 bool             `json:"eligible"`
	Status                   string           `json:"status"` // "READY_TO_STAGE", "BLOCKED", "CONFLICT"
	AdapterAlias             string           `json:"adapter_alias"`
	AdapterMAC               string           `json:"adapter_mac"`
	AdapterGUID              string           `json:"adapter_guid"`
	CurrentRole              string           `json:"current_role"`
	HasActiveDefaultRoute    bool             `json:"has_active_default_route"`
	ActiveGateway            string           `json:"active_gateway,omitempty"`
	ManagementRouteSecured   bool             `json:"management_route_secured"`
	ManagementInterfaceAlias string           `json:"management_interface_alias,omitempty"`
	SubnetOverlapDetected    bool             `json:"subnet_overlap_detected"`
	SubnetOverlapDetails     string           `json:"subnet_overlap_details,omitempty"`
	Message                  string           `json:"message"`
	Checks                   []PreflightCheck `json:"checks"`
}

type PreflightCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Blocked bool   `json:"blocked"`
	Detail  string `json:"detail"`
}
