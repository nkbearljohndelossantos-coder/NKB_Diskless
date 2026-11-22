package models

import (
	"time"
)

// Server represents a physical host or cluster storage node running NKB services.
type Server struct {
	ID            string    `json:"id" db:"id"`
	Hostname      string    `json:"hostname" db:"hostname"`
	IPAddress     string    `json:"ip_address" db:"ip_address"`
	Role          string    `json:"role" db:"role"` // primary, secondary, storage_node
	SystemSpecs   string    `json:"system_specs" db:"system_specs"` // JSON string
	Status        string    `json:"status" db:"status"` // online, offline, degraded, maintenance
	LastHeartbeat time.Time `json:"last_heartbeat" db:"last_heartbeat"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// Client represents a diskless workstation managed by NKB.
type Client struct {
	ID                  string     `json:"id" db:"id"`
	MACAddress          string     `json:"mac_address" db:"mac_address"` // e.g. 00:11:22:33:44:55
	IPAddress           string     `json:"ip_address" db:"ip_address"`
	Hostname            string     `json:"hostname" db:"hostname"`
	GroupID             *string    `json:"group_id,omitempty" db:"group_id"`
	AssignedDiskID      *string    `json:"assigned_disk_id,omitempty" db:"assigned_disk_id"`
	Status              string     `json:"status" db:"status"` // online, offline, booting, maintenance
	IsSuperClient       bool       `json:"is_super_client" db:"is_super_client"`
	SuperClientExpireAt *time.Time `json:"super_client_expires_at,omitempty" db:"super_client_expires_at"`
	BootMode            string     `json:"boot_mode" db:"boot_mode"` // UEFI, BIOS
	WriteCacheMode      string     `json:"write_cache_mode" db:"write_cache_mode"` // inherited, volatile, persistent
	HardwareInfo        string     `json:"hardware_info" db:"hardware_info"` // JSON: CPU, RAM, GPU, NIC
	LastBootTime        *time.Time `json:"last_boot_time,omitempty" db:"last_boot_time"`
	LastSeen            *time.Time `json:"last_seen,omitempty" db:"last_seen"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`

	// Joined relations for API responses
	GroupName        string `json:"group_name,omitempty" db:"-"`
	AssignedDiskName string `json:"assigned_disk_name,omitempty" db:"-"`
}

// ClientGroup represents a logical collection of diskless workstations (e.g. Factory Floor, Lab, Office).
type ClientGroup struct {
	ID                 string    `json:"id" db:"id"`
	Name               string    `json:"name" db:"name"`
	Description        string    `json:"description" db:"description"`
	DefaultDiskID      *string   `json:"default_disk_id,omitempty" db:"default_disk_id"`
	BootTimeoutSeconds int       `json:"boot_timeout_seconds" db:"boot_timeout_seconds"`
	WriteCachePolicy   string    `json:"write_cache_policy" db:"write_cache_policy"` // volatile, persistent
	AllowSuperClient   bool      `json:"allow_super_client" db:"allow_super_client"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`

	// Aggregated metadata
	ClientCount int `json:"client_count,omitempty" db:"-"`
}

// VirtualDisk represents a master or standard virtual disk image (VHD/VHDX/RAW).
type VirtualDisk struct {
	ID             string    `json:"id" db:"id"`
	Name           string    `json:"name" db:"name"`
	Description    string    `json:"description" db:"description"`
	FilePath       string    `json:"file_path" db:"file_path"`
	SizeBytes      int64     `json:"size_bytes" db:"size_bytes"`
	DiskFormat     string    `json:"disk_format" db:"disk_format"` // VHD, VHDX, RAW, IMG
	IsGolden       bool      `json:"is_golden" db:"is_golden"`
	IsLocked       bool      `json:"is_locked" db:"is_locked"`
	OSType         string    `json:"os_type" db:"os_type"` // Windows 10 x64, Windows 11 x64
	ChecksumSHA256 string    `json:"checksum_sha256" db:"checksum_sha256"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`

	// Joined version info
	ActiveVersion string `json:"active_version,omitempty" db:"-"`
}

// DiskVersion tracks immutable snapshots/versions of a virtual disk image.
type DiskVersion struct {
	ID             string    `json:"id" db:"id"`
	DiskID         string    `json:"disk_id" db:"disk_id"`
	VersionNumber  int       `json:"version_number" db:"version_number"`
	VersionTag     string    `json:"version_tag" db:"version_tag"` // e.g. v1.0.0, v1.1.0
	SnapshotPath   string    `json:"snapshot_path" db:"snapshot_path"`
	Description    string    `json:"description" db:"description"`
	ChecksumSHA256 string    `json:"checksum_sha256" db:"checksum_sha256"`
	CreatedBy      *string   `json:"created_by,omitempty" db:"created_by"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// WriteCache manages the per-client differential write layer.
type WriteCache struct {
	ID             string     `json:"id" db:"id"`
	ClientID       string     `json:"client_id" db:"client_id"`
	DiskID         string     `json:"disk_id" db:"disk_id"`
	CachePath      string     `json:"cache_path" db:"cache_path"`
	AllocatedBytes int64      `json:"allocated_bytes" db:"allocated_bytes"`
	UsedBytes      int64      `json:"used_bytes" db:"used_bytes"`
	PurgeMode      string     `json:"purge_mode" db:"purge_mode"` // on_boot, manual, scheduled
	LastFlushedAt  *time.Time `json:"last_flushed_at,omitempty" db:"last_flushed_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// User represents an administrator or operator in the system.
type User struct {
	ID           string     `json:"id" db:"id"`
	Username     string     `json:"username" db:"username"`
	PasswordHash string     `json:"-" db:"password_hash"`
	FullName     string     `json:"full_name" db:"full_name"`
	Role         string     `json:"role" db:"role"` // superadmin, admin, operator, auditor
	IsActive     bool       `json:"is_active" db:"is_active"`
	LastLogin    *time.Time `json:"last_login,omitempty" db:"last_login"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// AuditLog records every administrative operation for non-repudiation.
type AuditLog struct {
	ID         string    `json:"id" db:"id"`
	UserID     *string   `json:"user_id,omitempty" db:"user_id"`
	Username   string    `json:"username" db:"username"`
	Action     string    `json:"action" db:"action"`
	TargetType string    `json:"target_type" db:"target_type"`
	TargetID   string    `json:"target_id" db:"target_id"`
	Details    string    `json:"details" db:"details"`
	IPAddress  string    `json:"ip_address" db:"ip_address"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// SystemUpdate manages NKB platform versions and rollback packages.
type SystemUpdate struct {
	ID             string     `json:"id" db:"id"`
	VersionString  string     `json:"version_string" db:"version_string"`
	PackagePath    string     `json:"package_path" db:"package_path"`
	ChecksumSHA256 string     `json:"checksum_sha256" db:"checksum_sha256"`
	Status         string     `json:"status" db:"status"` // staged, applying, applied, failed, rolled_back
	MigrationLog   string     `json:"migration_log" db:"migration_log"`
	AppliedBy      *string    `json:"applied_by,omitempty" db:"applied_by"`
	InstalledAt    *time.Time `json:"installed_at,omitempty" db:"installed_at"`
	RollbackAt     *time.Time `json:"rollback_at,omitempty" db:"rollback_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// ServerMetrics stores historical telemetry for the NKB server.
type ServerMetrics struct {
	ID                   string    `json:"id" db:"id"`
	ServerID             string    `json:"server_id" db:"server_id"`
	CPUUsagePct          float32   `json:"cpu_usage_pct" db:"cpu_usage_pct"`
	RAMUsagePct          float32   `json:"ram_usage_pct" db:"ram_usage_pct"`
	RAMUsedBytes         int64     `json:"ram_used_bytes" db:"ram_used_bytes"`
	RAMTotalBytes        int64     `json:"ram_total_bytes" db:"ram_total_bytes"`
	StorageReadIOPS      float32   `json:"storage_read_iops" db:"storage_read_iops"`
	StorageWriteIOPS     float32   `json:"storage_write_iops" db:"storage_write_iops"`
	StorageReadBytesSec  int64     `json:"storage_read_bytes_sec" db:"storage_read_bytes_sec"`
	StorageWriteBytesSec int64     `json:"storage_write_bytes_sec" db:"storage_write_bytes_sec"`
	NetworkRxBytesSec    int64     `json:"network_rx_bytes_sec" db:"network_rx_bytes_sec"`
	NetworkTxBytesSec    int64     `json:"network_tx_bytes_sec" db:"network_tx_bytes_sec"`
	ActiveISCSISessions  int       `json:"active_iscsi_sessions" db:"active_iscsi_sessions"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

// ClientMetrics stores client-side performance telemetry.
type ClientMetrics struct {
	ID                  string    `json:"id" db:"id"`
	ClientID            string    `json:"client_id" db:"client_id"`
	CPUUsagePct         float32   `json:"cpu_usage_pct" db:"cpu_usage_pct"`
	RAMUsagePct         float32   `json:"ram_usage_pct" db:"ram_usage_pct"`
	DiskReadBytesSec    int64     `json:"disk_read_bytes_sec" db:"disk_read_bytes_sec"`
	DiskWriteBytesSec   int64     `json:"disk_write_bytes_sec" db:"disk_write_bytes_sec"`
	NetworkRxBytesSec   int64     `json:"network_rx_bytes_sec" db:"network_rx_bytes_sec"`
	NetworkTxBytesSec   int64     `json:"network_tx_bytes_sec" db:"network_tx_bytes_sec"`
	BootDurationSeconds float32   `json:"boot_duration_seconds" db:"boot_duration_seconds"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
}
