package network_mgmt

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
)

// AdapterProvider defines the data acquisition interface for network adapters.
type AdapterProvider interface {
	GetRawAdapters() ([]RawAdapterData, error)
}

// RawAdapterData holds unnormalized network configuration directly from OS cmdlets.
type RawAdapterData struct {
	InterfaceAlias                 string   `json:"InterfaceAlias"`
	InterfaceDescription           string   `json:"InterfaceDescription"`
	InterfaceIndex                 int      `json:"InterfaceIndex"`
	InterfaceGuid                  string   `json:"InterfaceGuid"`
	MacAddress                     string   `json:"MacAddress"`
	Status                         string   `json:"Status"`
	LinkSpeed                      string   `json:"LinkSpeed"`
	IPv4Addresses                  []string `json:"IPv4Addresses"`
	IPv6Addresses                  []string `json:"IPv6Addresses"`
	DefaultGateways                []string `json:"DefaultGateways"`
	DNSServers                     []string `json:"DNSServers"`
	DhcpEnabled                    bool     `json:"DhcpEnabled"`
	Dhcpv6Enabled                  bool     `json:"Dhcpv6Enabled"`
	InterfaceMetricIPv4            int      `json:"InterfaceMetricIPv4"`
	InterfaceMetricIPv6            int      `json:"InterfaceMetricIPv6"`
	AutomaticMetricIPv4            bool     `json:"AutomaticMetricIPv4"`
	AutomaticMetricIPv6            bool     `json:"AutomaticMetricIPv6"`
	DNSConfigurationSource         string   `json:"DNSConfigurationSource"`
	RegisterThisConnectionsAddress bool     `json:"RegisterThisConnectionsAddress"`
	NetworkCategory                string   `json:"NetworkCategory"`
}

// MockRawAdapter is an alias for RawAdapterData used in tests.
type MockRawAdapter = RawAdapterData

// WindowsAdapterProvider queries Windows CIM / NetTCPIP modules via PowerShell.
type WindowsAdapterProvider struct{}

func (w *WindowsAdapterProvider) GetRawAdapters() ([]RawAdapterData, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("WindowsAdapterProvider requires Windows OS")
	}

	psScript := `
	$adapters = Get-NetAdapter -ErrorAction SilentlyContinue
	$results = @()
	foreach ($a in $adapters) {
		$idx = $a.InterfaceIndex
		$ipConf = Get-NetIPConfiguration -InterfaceIndex $idx -ErrorAction SilentlyContinue
		$v4 = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue
		$v6 = Get-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv6 -ErrorAction SilentlyContinue
		$conn = Get-NetConnectionProfile -InterfaceIndex $idx -ErrorAction SilentlyContinue
		$dnsClient = Get-DnsClient -InterfaceIndex $idx -ErrorAction SilentlyContinue
		
		$v4Addrs = @()
		if ($ipConf -and $ipConf.IPv4Address) {
			$v4Addrs = @($ipConf.IPv4Address | ForEach-Object { "$($_.IPAddress)/$($_.PrefixLength)" })
		}
		
		$v6Addrs = @()
		if ($ipConf -and $ipConf.IPv6Address) {
			$v6Addrs = @($ipConf.IPv6Address.IPAddress)
		}

		$gws = @()
		if ($ipConf -and $ipConf.IPv4DefaultGateway) {
			$gws = @($ipConf.IPv4DefaultGateway.NextHop)
		}

		$dns = @()
		if ($ipConf -and $ipConf.DNSServer) {
			$dns = @($ipConf.DNSServer.ServerAddresses)
		}

		$dhcp = $false
		$metric4 = 0
		$autoMetric4 = $false
		$regDns = if ($dnsClient) { [bool]$dnsClient.RegisterThisConnectionsAddress } else { $true }
		if ($v4) {
			if ($v4.Dhcp -eq 'Enabled') { $dhcp = $true }
			$metric4 = [int]$v4.InterfaceMetric
			$autoMetric4 = [bool]($v4.AutomaticMetric -eq 'Enabled')
		}

		$dhcp6 = $false
		$metric6 = 0
		$autoMetric6 = $false
		if ($v6) {
			if ($v6.Dhcp -eq 'Enabled') { $dhcp6 = $true }
			$metric6 = [int]$v6.InterfaceMetric
			$autoMetric6 = [bool]($v6.AutomaticMetric -eq 'Enabled')
		}

		$dnsSource = if ($dhcp) { "DHCP" } else { "Manual" }
		$netCat = if ($conn) { $conn.NetworkCategory.ToString() } else { "Private" }

		$results += [PSCustomObject]@{
			InterfaceAlias = $a.Name
			InterfaceDescription = $a.InterfaceDescription
			InterfaceIndex = $idx
			InterfaceGuid = $a.InterfaceGuid
			MacAddress = $a.MacAddress
			Status = $a.Status
			LinkSpeed = $a.LinkSpeed
			IPv4Addresses = $v4Addrs
			IPv6Addresses = $v6Addrs
			DefaultGateways = $gws
			DNSServers = $dns
			DhcpEnabled = $dhcp
			Dhcpv6Enabled = $dhcp6
			InterfaceMetricIPv4 = $metric4
			InterfaceMetricIPv6 = $metric6
			AutomaticMetricIPv4 = $autoMetric4
			AutomaticMetricIPv6 = $autoMetric6
			DNSConfigurationSource = $dnsSource
			RegisterThisConnectionsAddress = $regDns
			NetworkCategory = $netCat
		}
	}
	$results | ConvertTo-Json -Depth 3 -Compress
	`

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("powershell adapter discovery failed: %w", err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []RawAdapterData{}, nil
	}

	var rawList []RawAdapterData
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &rawList); err != nil {
			return nil, fmt.Errorf("failed to parse powershell adapter list: %w", err)
		}
	} else if strings.HasPrefix(trimmed, "{") {
		var single RawAdapterData
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return nil, fmt.Errorf("failed to parse powershell single adapter: %w", err)
		}
		rawList = append(rawList, single)
	}

	return rawList, nil
}

// MockAdapterProvider provides simulated adapters for testing without host interaction.
type MockAdapterProvider struct {
	Adapters []RawAdapterData
}

func (m *MockAdapterProvider) GetRawAdapters() ([]RawAdapterData, error) {
	return m.Adapters, nil
}

// DiscoveryService manages the discovery and classification of network adapters.
type DiscoveryService struct {
	provider AdapterProvider
}

// NewDiscoveryService constructs a discovery service with the appropriate provider.
func NewDiscoveryService(provider AdapterProvider) *DiscoveryService {
	if provider == nil {
		if runtime.GOOS == "windows" {
			provider = &WindowsAdapterProvider{}
		} else {
			provider = &MockAdapterProvider{}
		}
	}
	return &DiscoveryService{provider: provider}
}

// DiscoverAdapters enumerates and classifies all network adapters on the system.
func (s *DiscoveryService) DiscoverAdapters() ([]NetworkAdapter, error) {
	rawList, err := s.provider.GetRawAdapters()
	if err != nil {
		return nil, err
	}

	var results []NetworkAdapter
	for _, raw := range rawList {
		adapter := NetworkAdapter{
			InterfaceAlias:       raw.InterfaceAlias,
			InterfaceDescription: raw.InterfaceDescription,
			InterfaceIndex:       raw.InterfaceIndex,
			InterfaceGUID:        raw.InterfaceGuid,
			MACAddress:           normalizeMAC(raw.MacAddress),
			Status:               raw.Status,
			LinkSpeed:            raw.LinkSpeed,
			IPv4Addresses:        raw.IPv4Addresses,
			IPv6Addresses:        raw.IPv6Addresses,
			DefaultGateways:      raw.DefaultGateways,
			DNSServers:                     raw.DNSServers,
			DHCPEnabled:                    raw.DhcpEnabled,
			DHCPv6Enabled:                  raw.Dhcpv6Enabled,
			InterfaceMetricIPv4:            raw.InterfaceMetricIPv4,
			InterfaceMetricIPv6:            raw.InterfaceMetricIPv6,
			AutomaticMetricIPv4:            raw.AutomaticMetricIPv4,
			AutomaticMetricIPv6:            raw.AutomaticMetricIPv6,
			DNSConfigurationSource:         raw.DNSConfigurationSource,
			RegisterThisConnectionsAddress: raw.RegisterThisConnectionsAddress,
			NetworkCategory:                raw.NetworkCategory,
		}

		adapter.ID = ComputePersistentKey(adapter.MACAddress, adapter.InterfaceGUID, adapter.InterfaceDescription)
		s.classifyAdapter(&adapter)
		results = append(results, adapter)
	}

	return results, nil
}

func (s *DiscoveryService) classifyAdapter(a *NetworkAdapter) {
	nameLower := strings.ToLower(a.InterfaceAlias)
	descLower := strings.ToLower(a.InterfaceDescription)

	// 1. Detect Virtual Adapters
	virtualKeywords := []string{
		"hyper-v", "wsl", "virtual", "vmware", "virtualbox",
		"loopback", "tap", "vpn", "bluetooth", "ndis", "pseudo",
	}
	for _, kw := range virtualKeywords {
		if strings.Contains(descLower, kw) || strings.Contains(nameLower, kw) {
			a.IsVirtual = true
			a.IsPhysical = false
			a.SafetyRole = RoleVirtualBlocked
			a.SafetyWarning = fmt.Sprintf("Virtual or tunnel adapter (%s); cannot be used for physical PXE boot.", a.InterfaceDescription)
			return
		}
	}

	a.IsPhysical = true

	// 2. Detect Wireless / Wi-Fi Adapters
	if strings.Contains(nameLower, "wi-fi") || strings.Contains(descLower, "wireless") || strings.Contains(descLower, "802.11") || strings.Contains(nameLower, "wlan") {
		a.IsWireless = true
		a.SafetyRole = RoleProtectedInternet
		a.SafetyWarning = "Corporate / wireless network adapter. Modification and PXE listener binding are strictly prohibited."
		if len(a.DefaultGateways) > 0 {
			a.IsPrimaryInternet = true
		}
		return
	}

	// 3. Detect Active Internet / Default Gateway Connection
	// Any physical adapter with an active default gateway is protected from automatic staging
	hasGateway := false
	for _, gw := range a.DefaultGateways {
		cleanGW := strings.TrimSpace(gw)
		if cleanGW != "" && cleanGW != "0.0.0.0" {
			hasGateway = true
			break
		}
	}

	if hasGateway {
		a.IsPrimaryInternet = true
		a.SafetyRole = RoleProtectedInternet
		a.SafetyWarning = fmt.Sprintf("Active host gateway detected (%s). Automatic staging prohibited to protect host connectivity.", strings.Join(a.DefaultGateways, ", "))
		return
	}

	// 4. Candidate Dedicated Test Adapter (Physical, non-wireless, zero default gateway)
	a.SafetyRole = RoleCandidate
}

// FindActiveManagementAdapter identifies another active network interface that secures host management connectivity.
// It strictly verifies that the candidate is Up, has at least one valid non-deprecated/non-APIPA IPv4 address,
// and possesses an active non-zero default gateway.
func FindActiveManagementAdapter(adapters []NetworkAdapter, excludeKey, excludeMAC, excludeAlias string) *NetworkAdapter {
	for i, a := range adapters {
		// Skip the target adapter
		if (excludeKey != "" && a.ID == excludeKey) ||
			(excludeMAC != "" && strings.EqualFold(a.MACAddress, excludeMAC)) ||
			(excludeAlias != "" && strings.EqualFold(a.InterfaceAlias, excludeAlias)) {
			continue
		}

		// Must be Up
		if !strings.EqualFold(a.Status, "Up") {
			continue
		}

		// Reject virtual adapters
		if a.IsVirtual || a.SafetyRole == RoleVirtualBlocked {
			continue
		}

		// Must have an active default gateway
		hasGW := false
		for _, gw := range a.DefaultGateways {
			cleanGW := strings.TrimSpace(gw)
			if cleanGW != "" && cleanGW != "0.0.0.0" {
				hasGW = true
				break
			}
		}
		if !hasGW {
			continue
		}

		// Must have a valid non-APIPA IPv4 address
		hasValidIP := false
		for _, ip := range a.IPv4Addresses {
			cleanIP := strings.Split(strings.TrimSpace(ip), "/")[0]
			if cleanIP != "" && !strings.HasPrefix(cleanIP, "169.254.") && cleanIP != "0.0.0.0" {
				hasValidIP = true
				break
			}
		}
		if hasValidIP {
			return &adapters[i]
		}
	}
	return nil
}

func normalizeMAC(mac string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(mac))
	cleaned = strings.ReplaceAll(cleaned, "-", ":")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	return cleaned
}

// IPInSubnet checks whether candidateIP falls within the subnet defined by cidrStr (e.g. "192.168.50.1/24").
func IPInSubnet(candidateIP, cidrStr string) bool {
	ip := net.ParseIP(candidateIP)
	if ip == nil {
		return false
	}
	_, ipNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	return ipNet.Contains(ip)
}

// SubnetsOverlap checks if two CIDR network blocks overlap.
func SubnetsOverlap(cidrA, cidrB string) bool {
	_, netA, errA := net.ParseCIDR(cidrA)
	_, netB, errB := net.ParseCIDR(cidrB)
	if errA != nil || errB != nil {
		return false
	}
	return netA.Contains(netB.IP) || netB.Contains(netA.IP)
}
