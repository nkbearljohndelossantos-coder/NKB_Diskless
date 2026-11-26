package agent

import (
	"encoding/json"
	"net"
	"os"
	"runtime"
	"strings"

	"nkb-diskless/internal/models"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	netutil "github.com/shirou/gopsutil/v3/net"
)

type TelemetryCollector struct{}

func NewTelemetryCollector() *TelemetryCollector {
	return &TelemetryCollector{}
}

// GetPrimaryMACAddress identifies the physical network interface hardware address.
func (t *TelemetryCollector) GetPrimaryMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "00:00:00:00:00:00"
	}

	for _, iface := range interfaces {
		// Filter out loopback, virtual/hyper-v adapters, and inactive links
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "vethernet") || strings.Contains(name, "pseudo") || strings.Contains(name, "virtual") {
			continue
		}

		mac := strings.ToUpper(iface.HardwareAddr.String())
		if len(mac) == 17 {
			return mac
		}
	}

	// Fallback to first non-empty MAC
	for _, iface := range interfaces {
		mac := strings.ToUpper(iface.HardwareAddr.String())
		if len(mac) == 17 && mac != "00:00:00:00:00:00" {
			return mac
		}
	}

	return "00:00:00:00:00:00"
}

// GetMACInventory enumerates all physical and virtual network interfaces.
func (t *TelemetryCollector) GetMACInventory() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}

	seen := make(map[string]bool)
	var macs []string

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := strings.ToUpper(strings.ReplaceAll(iface.HardwareAddr.String(), "-", ":"))
		if len(mac) == 17 && mac != "00:00:00:00:00:00" && !seen[mac] {
			seen[mac] = true
			macs = append(macs, mac)
		}
	}

	return macs
}

// GetLocalIP returns the primary IPv4 address of this workstation.
func (t *TelemetryCollector) GetLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}
	return "127.0.0.1"
}

// GetLoggedInUser returns the username of the active user session.
func (t *TelemetryCollector) GetLoggedInUser() string {
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	return user
}

// GetOSBuild returns the OS kernel / platform build version.
func (t *TelemetryCollector) GetOSBuild() string {
	hInfo, err := host.Info()
	if err == nil && hInfo != nil {
		if hInfo.KernelVersion != "" {
			return hInfo.KernelVersion
		}
		return hInfo.PlatformVersion
	}
	return runtime.GOOS
}

// CollectHardwareInfo produces a JSON payload detailing the workstation hardware specs.
func (t *TelemetryCollector) CollectHardwareInfo() string {
	hInfo, _ := host.Info()
	vMem, _ := mem.VirtualMemory()
	cStats, _ := cpu.Info()

	cpuModel := "Unknown CPU"
	if len(cStats) > 0 {
		cpuModel = cStats[0].ModelName
	}

	var ramMB int64
	var ramBytes int64
	if vMem != nil {
		ramMB = int64(vMem.Total / (1024 * 1024))
		ramBytes = int64(vMem.Total)
	}

	platform := runtime.GOOS
	var osBuild string
	if hInfo != nil {
		platform = hInfo.Platform + " " + hInfo.PlatformVersion
		osBuild = hInfo.KernelVersion
	}

	specs := map[string]any{
		"cpu":            cpuModel,
		"ram_mb":         ramMB,
		"ram_total_bytes": ramBytes,
		"os_platform":    platform,
		"os_build":       osBuild,
		"arch":           runtime.GOARCH,
		"mac_inventory":  t.GetMACInventory(),
	}

	bytes, _ := json.Marshal(specs)
	return string(bytes)
}

// SampleHeartbeat gathers instantaneous performance metrics.
func (t *TelemetryCollector) SampleHeartbeat(deviceToken string) (*models.AgentHeartbeatRequest, error) {
	req := &models.AgentHeartbeatRequest{
		DeviceToken:  deviceToken,
		LoggedInUser: t.GetLoggedInUser(),
		AgentStatus:  "connected",
	}

	// Uptime
	uptime, err := host.Uptime()
	if err == nil {
		req.UptimeSec = int64(uptime)
	}

	// CPU
	cpuPcts, err := cpu.Percent(0, false)
	if err == nil && len(cpuPcts) > 0 {
		req.CPUUsagePct = float32(cpuPcts[0])
	}

	// RAM
	vMem, err := mem.VirtualMemory()
	if err == nil {
		req.RAMUsagePct = float32(vMem.UsedPercent)
		req.RAMTotalBytes = int64(vMem.Total)
		req.RAMAvailBytes = int64(vMem.Available)
	}

	// Disk (System Drive)
	dUsage, err := disk.Usage("C:")
	if err != nil {
		dUsage, _ = disk.Usage("/")
	}
	if dUsage != nil {
		req.DiskUsagePct = float32(dUsage.UsedPercent)
	}

	// Network
	netIO, err := netutil.IOCounters(false)
	if err == nil && len(netIO) > 0 {
		req.NetworkRxMBs = float32(netIO[0].BytesRecv) / (1024 * 1024 * 10)
		req.NetworkTxMBs = float32(netIO[0].BytesSent) / (1024 * 1024 * 10)
	}

	return req, nil
}
