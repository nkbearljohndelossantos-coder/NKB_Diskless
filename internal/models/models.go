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

// Client represents a workstation managed by NKB (local or diskless).
type Client struct {
	ID                  string     `json:"id" db:"id"`
	MACAddress          string     `json:"mac_address" db:"mac_address"` // e.g. 00:11:22:33:44:55
	IPAddress           string     `json:"ip_address" db:"ip_address"`
	Hostname            string     `json:"hostname" db:"hostname"`
	GroupID             *string    `json:"group_id,omitempty" db:"group_id"`
	AssignedDiskID      *string    `json:"assigned_disk_id,omitempty" db:"assigned_disk_id"`
	Status              string     `json:"status" db:"status"` // online, offline, booting, maintenance, discovered
	IsSuperClient       bool       `json:"is_super_client" db:"is_super_client"`
	SuperClientExpireAt *time.Time `json:"super_client_expires_at,omitempty" db:"super_client_expires_at"`
	BootMode            string     `json:"boot_mode" db:"boot_mode"` // UEFI, BIOS
	WriteCacheMode      string     `json:"write_cache_mode" db:"write_cache_mode"` // inherited, volatile, persistent
	HardwareInfo        string     `json:"hardware_info" db:"hardware_info"` // JSON: CPU, RAM, GPU, NIC, Disk, OS
	LastBootTime        *time.Time `json:"last_boot_time,omitempty" db:"last_boot_time"`
	LastSeen            *time.Time `json:"last_seen,omitempty" db:"last_seen"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`

	// Enriched Agent Metadata parsed from HardwareInfo / Heartbeats
	LoggedInUser string `json:"logged_in_user,omitempty" db:"-"`
	OSVersion    string `json:"os_version,omitempty" db:"-"`
	UptimeSec    int64  `json:"uptime_seconds,omitempty" db:"-"`
	AgentStatus  string `json:"agent_status,omitempty" db:"-"`
	AgentVersion string `json:"agent_version,omitempty" db:"-"`
	CPUUsagePct  float32 `json:"cpu_usage_pct,omitempty" db:"-"`
	RAMUsagePct  float32 `json:"ram_usage_pct,omitempty" db:"-"`

	// Joined relations for API responses
	GroupName        string `json:"group_name,omitempty" db:"-"`
	AssignedDiskName string `json:"assigned_disk_name,omitempty" db:"-"`
}

// ClientGroup represents a logical collection of workstations (e.g. Factory Line A, Office, CAD Lab).
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

	ClientCount int `json:"client_count,omitempty" db:"-"`
}

// VirtualDisk represents a local or centralized master virtual disk image (VHD/VHDX/RAW).
type VirtualDisk struct {
	ID             string    `json:"id" db:"id"`
	Name           string    `json:"name" db:"name"`
	Description    string    `json:"description" db:"description"`
	FilePath       string    `json:"file_path" db:"file_path"`
	SizeBytes      int64     `json:"size_bytes" db:"size_bytes"`
	DiskFormat     string    `json:"disk_format" db:"disk_format"` // VHD, VHDX, RAW
	IsGolden       bool      `json:"is_golden" db:"is_golden"`
	IsLocked       bool      `json:"is_locked" db:"is_locked"`
	OSType         string    `json:"os_type" db:"os_type"` // Windows 10 x64, Windows 11 x64
	ChecksumSHA256 string    `json:"checksum_sha256" db:"checksum_sha256"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`

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

// DiskAssignment links a workstation to a virtual disk or Golden Image.
type DiskAssignment struct {
	ID             string    `json:"id" db:"id"`
	ClientID       string    `json:"client_id" db:"client_id"`
	DiskID         string    `json:"disk_id" db:"disk_id"`
	DeploymentType string    `json:"deployment_type" db:"deployment_type"` // local_vhdx, network_iscsi
	MountPoint     string    `json:"mount_point" db:"mount_point"` // e.g. D:\NKB_VirtualDisk
	IsActive       bool      `json:"is_active" db:"is_active"`
	AssignedAt     time.Time `json:"assigned_at" db:"assigned_at"`
}

// MaintenanceLock represents a distributed lock for Super-Client operations.
type MaintenanceLock struct {
	ID           string     `json:"id" db:"id"`
	DiskID       string     `json:"disk_id" db:"disk_id"`
	ClientID     string     `json:"client_id" db:"client_id"`
	AcquiredBy   string     `json:"acquired_by" db:"acquired_by"`
	SessionToken string     `json:"session_token" db:"session_token"`
	IsActive     bool       `json:"is_active" db:"is_active"`
	AcquiredAt   time.Time  `json:"acquired_at" db:"acquired_at"`
	ExpiresAt    time.Time  `json:"expires_at" db:"expires_at"`
	ReleasedAt   *time.Time `json:"released_at,omitempty" db:"released_at"`
}

// AgentCredential stores per-device authentication tokens for the Windows background agent.
type AgentCredential struct {
	ID                 string     `json:"id" db:"id"`
	ClientID           string     `json:"client_id" db:"client_id"`
	DeviceToken        string     `json:"device_token" db:"device_token"`
	SecretHash         string     `json:"-" db:"secret_hash"`
	Status             string     `json:"status" db:"status"` // active, revoked
	LastAuthenticatedAt *time.Time `json:"last_authenticated_at,omitempty" db:"last_authenticated_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
}

// RemoteCommand represents an authorized management command issued to a workstation agent.
type RemoteCommand struct {
	ID             string     `json:"id" db:"id"`
	ClientID       string     `json:"client_id" db:"client_id"`
	IssuerUserID   *string    `json:"issuer_user_id,omitempty" db:"issuer_user_id"`
	IssuerUsername string     `json:"issuer_username" db:"issuer_username"`
	CommandType    string     `json:"command_type" db:"command_type"` // shutdown, restart, lock, wol, query_processes
	Payload        string     `json:"payload" db:"payload"` // JSON parameters
	Status         string     `json:"status" db:"status"` // pending, sent, executing, completed, failed
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty" db:"expires_at"`

	Result *CommandResult `json:"result,omitempty" db:"-"`
}

// CommandResult records the execution outcome from the workstation agent.
type CommandResult struct {
	ID           string     `json:"id" db:"id"`
	CommandID    string     `json:"command_id" db:"command_id"`
	ClientID     string     `json:"client_id" db:"client_id"`
	ExitCode     int        `json:"exit_code" db:"exit_code"`
	Output       string     `json:"output" db:"output"`
	ErrorMessage string     `json:"error_message" db:"error_message"`
	ExecutedAt   time.Time  `json:"executed_at" db:"executed_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// SystemSetting represents platform-wide persistent key-value configuration.
type SystemSetting struct {
	Key         string    `json:"key" db:"key"`
	Value       string    `json:"value" db:"value"`
	Description string    `json:"description" db:"description"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// WriteCache manages the per-client differential write layer.
type WriteCache struct {
	ID             string     `json:"id" db:"id"`
	ClientID       string     `json:"client_id" db:"client_id"`
	DiskID         string     `json:"disk_id" db:"disk_id"`
	CachePath      string     `json:"cache_path" db:"cache_path"`
	AllocatedBytes int64      `json:"allocated_bytes" db:"allocated_bytes"`
	UsedBytes      int64      `json:"used_bytes" db:"used_bytes"`
	PurgeMode      string     `json:"purge_mode" db:"purge_mode"`
	LastFlushedAt  *time.Time `json:"last_flushed_at,omitempty" db:"last_flushed_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// User represents an administrator, operator, or auditor in the system.
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
	Status         string     `json:"status" db:"status"`
	MigrationLog   string     `json:"migration_log" db:"migration_log"`
	AppliedBy      *string    `json:"applied_by,omitempty" db:"applied_by"`
	InstalledAt    *time.Time `json:"installed_at,omitempty" db:"installed_at"`
	RollbackAt     *time.Time `json:"rollback_at,omitempty" db:"rollback_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// API DTOs

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	User      User   `json:"user"`
	ExpiresAt string `json:"expires_at"`
}

type AgentRegisterRequest struct {
	MACAddress   string `json:"mac_address"`
	Hostname     string `json:"hostname"`
	IPAddress    string `json:"ip_address"`
	OSVersion    string `json:"os_version"`
	HardwareInfo string `json:"hardware_info"` // JSON string
	AgentVersion string `json:"agent_version"`
}

type AgentRegisterResponse struct {
	ClientID    string `json:"client_id"`
	DeviceToken string `json:"device_token"`
	Status      string `json:"status"`
}

type AgentHeartbeatRequest struct {
	DeviceToken  string  `json:"device_token"`
	LoggedInUser string  `json:"logged_in_user"`
	UptimeSec    int64   `json:"uptime_seconds"`
	CPUUsagePct  float32 `json:"cpu_usage_pct"`
	RAMUsagePct  float32 `json:"ram_usage_pct"`
	DiskUsagePct float32 `json:"disk_usage_pct"`
	NetworkRxMBs float32 `json:"network_rx_mbs"`
	NetworkTxMBs float32 `json:"network_tx_mbs"`
	AgentStatus  string  `json:"agent_status"`
	HardwareInfo string  `json:"hardware_info,omitempty"`
}

type QueueCommandRequest struct {
	CommandType string `json:"command_type"` // shutdown, restart, lock, query_processes
	Payload     string `json:"payload,omitempty"`
}
