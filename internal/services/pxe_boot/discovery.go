package pxe_boot

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
)

// NetworkInterfaceInfo represents detailed inventory and safety classification for an adapter.
type NetworkInterfaceInfo struct {
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	MACAddress         string   `json:"mac_address"`
	IPv4Addresses      []string `json:"ipv4_addresses"`
	SubnetMasks        []string `json:"subnet_masks"`
	Gateway            string   `json:"gateway"`
	DHCPEnabled        bool     `json:"dhcp_enabled"`
	OperStatus         string   `json:"oper_status"` // "Up", "Down", "Disconnected"
	IsPhysical         bool     `json:"is_physical"`
	IsLoopback         bool     `json:"is_loopback"`
	IsDedicatedTestNIC bool     `json:"is_dedicated_test_nic"`
	IsProtected        bool     `json:"is_protected"` // Corporate LAN / Wi-Fi that must NOT be bound
	SafetyWarning      string   `json:"safety_warning,omitempty"`
}

// DiscoverNetworkInterfaces inspects the host network configuration in a strictly read-only manner.
func DiscoverNetworkInterfaces() ([]NetworkInterfaceInfo, error) {
	if runtime.GOOS == "windows" {
		return discoverWindowsInterfaces()
	}
	return discoverGenericInterfaces()
}

func discoverGenericInterfaces() ([]NetworkInterfaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate network interfaces: %w", err)
	}

	var results []NetworkInterfaceInfo
	for _, iface := range ifaces {
		info := NetworkInterfaceInfo{
			Name:       iface.Name,
			MACAddress: iface.HardwareAddr.String(),
			OperStatus: "Down",
		}

		if (iface.Flags & net.FlagUp) != 0 {
			info.OperStatus = "Up"
		}
		if (iface.Flags & net.FlagLoopback) != 0 {
			info.IsLoopback = true
		}

		addrs, err := iface.Addrs()
		if err == nil {
			for _, a := range addrs {
				if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					info.IPv4Addresses = append(info.IPv4Addresses, ipNet.IP.String())
					info.SubnetMasks = append(info.SubnetMasks, net.IP(ipNet.Mask).String())
				}
			}
		}

		classifyInterface(&info)
		results = append(results, info)
	}
	return results, nil
}

type psNetConfigItem struct {
	InterfaceAlias       string `json:"InterfaceAlias"`
	InterfaceDescription string `json:"InterfaceDescription"`
	IPv4Address          any    `json:"IPv4Address"`
	IPv4DefaultGateway   any    `json:"IPv4DefaultGateway"`
	NetIPv4Interface     any    `json:"NetIPv4Interface"`
	Status               string `json:"Status"`
	PhysicalMediaType    string `json:"PhysicalMediaType"`
	MacAddress           string `json:"MacAddress"`
}

func discoverWindowsInterfaces() ([]NetworkInterfaceInfo, error) {
	// Query PowerShell Get-NetIPConfiguration & Get-NetAdapter read-only
	psScript := `
	$adapters = @{}
	Get-NetAdapter | ForEach-Object {
		$adapters[$_.InterfaceAlias] = @{
			Status = $_.Status
			PhysicalMediaType = $_.PhysicalMediaType
			MacAddress = $_.MacAddress
		}
	}
	Get-NetIPConfiguration | ForEach-Object {
		$alias = $_.InterfaceAlias
		$meta = $adapters[$alias]
		[PSCustomObject]@{
			InterfaceAlias = $alias
			InterfaceDescription = $_.InterfaceDescription
			IPv4Address = ($_.IPv4Address.IPAddress -join ',')
			IPv4DefaultGateway = ($_.IPv4DefaultGateway.NextHop -join ',')
			Status = if ($meta) { $meta.Status } else { 'Unknown' }
			PhysicalMediaType = if ($meta) { $meta.PhysicalMediaType } else { 'Unknown' }
			MacAddress = if ($meta) { $meta.MacAddress } else { '' }
		}
	} | ConvertTo-Json -Compress
	`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		// Fallback to generic Go net.Interfaces if PowerShell execution fails
		return discoverGenericInterfaces()
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return discoverGenericInterfaces()
	}

	// PowerShell outputs object or array of objects
	var items []psNetConfigItem
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &items)
	} else if strings.HasPrefix(trimmed, "{") {
		var single psNetConfigItem
		if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
			items = append(items, single)
		}
	}

	if len(items) == 0 {
		return discoverGenericInterfaces()
	}

	var results []NetworkInterfaceInfo
	for _, item := range items {
		info := NetworkInterfaceInfo{
			Name:        item.InterfaceAlias,
			Description: item.InterfaceDescription,
			MACAddress:  item.MacAddress,
			OperStatus:  item.Status,
		}

		if ipStr, ok := item.IPv4Address.(string); ok && ipStr != "" {
			for _, ip := range strings.Split(ipStr, ",") {
				cleanIP := strings.TrimSpace(ip)
				if cleanIP != "" {
					info.IPv4Addresses = append(info.IPv4Addresses, cleanIP)
				}
			}
		}

		if gwStr, ok := item.IPv4DefaultGateway.(string); ok {
			info.Gateway = strings.TrimSpace(gwStr)
		}

		// Physical media classification
		medLower := strings.ToLower(item.PhysicalMediaType)
		descLower := strings.ToLower(item.InterfaceDescription)
		nameLower := strings.ToLower(item.InterfaceAlias)

		if strings.Contains(descLower, "virtual") ||
			strings.Contains(descLower, "hyper-v") ||
			strings.Contains(nameLower, "vethernet") ||
			strings.Contains(medLower, "unspecified") && strings.Contains(descLower, "adapter") {
			info.IsPhysical = false
		} else if strings.Contains(medLower, "802.3") || strings.Contains(medLower, "native 802.11") || strings.Contains(medLower, "ethernet") {
			info.IsPhysical = true
		}

		classifyInterface(&info)
		results = append(results, info)
	}

	return results, nil
}

// classifyInterface categorizes the interface to protect live networks and identify the dedicated test NIC.
func classifyInterface(info *NetworkInterfaceInfo) {
	nameLower := strings.ToLower(info.Name)
	descLower := strings.ToLower(info.Description)

	// Check if this matches dedicated test NIC subnet (192.168.50.X)
	for _, ip := range info.IPv4Addresses {
		if strings.HasPrefix(ip, "192.168.50.") {
			info.IsDedicatedTestNIC = true
			if info.Gateway != "" {
				info.SafetyWarning = "CAUTION: Dedicated test NIC has a default gateway configured. Gateway should be empty for isolated operation."
			}
			return
		}
	}

	// Protected Networks: Corporate LAN, Home Router subnets, Wi-Fi
	if strings.Contains(nameLower, "wi-fi") ||
		strings.Contains(descLower, "wireless") ||
		strings.Contains(nameLower, "ethernet") ||
		strings.Contains(descLower, "realtek") ||
		strings.Contains(descLower, "intel") {
		// If it has a default gateway or active IP that is NOT 192.168.50.X, it is a protected production NIC
		if info.Gateway != "" || len(info.IPv4Addresses) > 0 {
			info.IsProtected = true
			info.SafetyWarning = "PROTECTED PRODUCTION/CORPORATE ADAPTER: Network boot binding is strictly prohibited on this interface."
		}
	}
}
