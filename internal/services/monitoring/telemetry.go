package monitoring

import (
	"log/slog"
	"sync"
	"time"

	"nkb-diskless/internal/database"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// TelemetryService samples server hardware utilization and client fleet status.
type TelemetryService struct {
	db     *database.DB
	mu     sync.RWMutex
	latest *SystemOverview
	stopCh chan struct{}
}

// SystemOverview aggregates all live telemetry for the admin dashboard widgets.
type SystemOverview struct {
	ServerHostname      string    `json:"server_hostname"`
	ServerTime          time.Time `json:"server_time"`
	TotalClients        int       `json:"total_clients"`
	OnlineClients       int       `json:"online_clients"`
	OfflineClients      int       `json:"offline_clients"`
	BootingClients      int       `json:"booting_clients"`
	SuperClients        int       `json:"super_clients"`
	CPUUsagePct         float32   `json:"cpu_usage_pct"`
	RAMUsagePct         float32   `json:"ram_usage_pct"`
	RAMUsedGB           float32   `json:"ram_used_gb"`
	RAMTotalGB          float32   `json:"ram_total_gb"`
	StorageUsagePct     float32   `json:"storage_usage_pct"`
	StorageUsedGB       float32   `json:"storage_used_gb"`
	StorageTotalGB      float32   `json:"storage_total_gb"`
	NetworkRxMBs        float32   `json:"network_rx_mbs"`
	NetworkTxMBs        float32   `json:"network_tx_mbs"`
	ActiveISCSISessions int       `json:"active_iscsi_sessions"`
	SystemAlerts        []string  `json:"system_alerts"`
}

// NewTelemetryService creates the collector.
func NewTelemetryService(db *database.DB) *TelemetryService {
	return &TelemetryService{
		db:     db,
		stopCh: make(chan struct{}),
	}
}

// Start begins periodic metric sampling every 3 seconds.
func (t *TelemetryService) Start() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		// Sample immediately
		t.sample()

		for {
			select {
			case <-t.stopCh:
				return
			case <-ticker.C:
				t.sample()
			}
		}
	}()
	slog.Info("Telemetry Service started")
}

// Stop cleanly terminates metric collection.
func (t *TelemetryService) Stop() {
	close(t.stopCh)
}

// GetOverview returns the latest cached system metrics.
func (t *TelemetryService) GetOverview() *SystemOverview {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.latest == nil {
		t.mu.RUnlock()
		t.sample()
		t.mu.RLock()
	}
	return t.latest
}

func (t *TelemetryService) sample() {
	ov := &SystemOverview{
		ServerTime:   time.Now(),
		SystemAlerts: make([]string, 0),
	}

	// 1. Client fleet metrics from DB
	clients, err := t.db.ListClients()
	if err == nil {
		ov.TotalClients = len(clients)
		for _, c := range clients {
			if c.IsSuperClient {
				ov.SuperClients++
			}
			switch c.Status {
			case "online":
				ov.OnlineClients++
			case "booting":
				ov.BootingClients++
			default:
				ov.OfflineClients++
			}
		}
	}

	// 2. Host CPU
	cpuPcts, err := cpu.Percent(0, false)
	if err == nil && len(cpuPcts) > 0 {
		ov.CPUUsagePct = float32(cpuPcts[0])
	}

	// 3. Host Memory
	vMem, err := mem.VirtualMemory()
	if err == nil {
		ov.RAMUsagePct = float32(vMem.UsedPercent)
		ov.RAMUsedGB = float32(vMem.Used) / (1024 * 1024 * 1024)
		ov.RAMTotalGB = float32(vMem.Total) / (1024 * 1024 * 1024)
	}

	// 4. Host Storage
	diskUsage, err := disk.Usage(".")
	if err == nil {
		ov.StorageUsagePct = float32(diskUsage.UsedPercent)
		ov.StorageUsedGB = float32(diskUsage.Used) / (1024 * 1024 * 1024)
		ov.StorageTotalGB = float32(diskUsage.Total) / (1024 * 1024 * 1024)
	}

	// 5. Host Network I/O
	netIO, err := net.IOCounters(false)
	if err == nil && len(netIO) > 0 {
		// Convert bytes to MB/s estimate
		ov.NetworkRxMBs = float32(netIO[0].BytesRecv) / (1024 * 1024 * 10)
		ov.NetworkTxMBs = float32(netIO[0].BytesSent) / (1024 * 1024 * 10)
	}

	// Check for operational warnings
	if ov.RAMUsagePct > 90.0 {
		ov.SystemAlerts = append(ov.SystemAlerts, "Server RAM utilization exceeds 90%. Consider allocating more write-cache RAM.")
	}
	if ov.StorageUsagePct > 85.0 {
		ov.SystemAlerts = append(ov.SystemAlerts, "Primary storage SSD utilization exceeds 85%. Purge unneeded cache deltas.")
	}
	if ov.SuperClients > 0 {
		ov.SystemAlerts = append(ov.SystemAlerts, "A workstation is running in Super-Client Maintenance Mode. Disk changes will persist.")
	}

	t.mu.Lock()
	t.latest = ov
	t.mu.Unlock()
}
