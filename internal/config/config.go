package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents all runtime settings for the NKB Diskless System.
type Config struct {
	// Server identity & binding
	ServerID       string `json:"server_id"`
	Hostname       string `json:"hostname"`
	BindAddress    string `json:"bind_address"`
	APIPort        int    `json:"api_port"`
	HTTPBootPort   int    `json:"http_boot_port"`
	TFTPPort       int    `json:"tftp_port"`
	ProxyDHCPPort  int    `json:"proxy_dhcp_port"`
	EnableProxyDHCP bool   `json:"enable_proxy_dhcp"`
	EnableTFTP     bool   `json:"enable_tftp"`

	// Storage and image repositories (Default: C:\NKB-PoC)
	StorageRoot     string `json:"storage_root"`
	AppDir          string `json:"app_dir"`
	DatabaseDir     string `json:"database_dir"`
	LogsDir         string `json:"logs_dir"`
	BackupsDir      string `json:"backups_dir"`
	DisksDir        string `json:"disks_dir"`
	DisksVirtualDir string `json:"disks_virtual_dir"`
	DisksGoldenDir  string `json:"disks_golden_dir"`
	DisksClientsDir string `json:"disks_clients_dir"`
	DisksStagingDir string `json:"disks_staging_dir"`
	CacheDir        string `json:"cache_dir"`
	SnapshotsDir    string `json:"snapshots_dir"`
	BootDir         string `json:"boot_dir"`
	ConfigDir       string `json:"config_dir"`
	TFTPBootDir     string `json:"tftpboot_dir"`

	// Database
	DBDriver string `json:"db_driver"` // "postgres" or "sqlite3"
	DBDSN    string `json:"db_dsn"`

	// Security
	JWTSecret       string `json:"jwt_secret"`
	TokenExpiration int    `json:"token_expiration_hours"` // in hours

	// iSCSI Configuration
	ISCSIIQNPfx  string `json:"iscsi_iqn_prefix"`
	ISCSIPortal  string `json:"iscsi_portal"`
	ISCSIPort    int    `json:"iscsi_port"`
	EnableCHAP   bool   `json:"enable_chap"`

	// Environment
	Environment string `json:"environment"` // "development", "staging", "production"
}

// DefaultConfig returns safe production-ready defaults.
func DefaultConfig(baseDir string) *Config {
	if baseDir == "" {
		baseDir = `C:\NKB-PoC`
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "nkb-master-server"
	}

	appDir := filepath.Join(baseDir, "app")
	dbDir := filepath.Join(baseDir, "database")
	logsDir := filepath.Join(baseDir, "logs")
	backupsDir := filepath.Join(baseDir, "backups")
	disksVirtualDir := filepath.Join(baseDir, "disks", "virtual")
	disksGoldenDir := filepath.Join(baseDir, "disks", "golden")
	disksClientsDir := filepath.Join(baseDir, "disks", "clients")
	disksStagingDir := filepath.Join(baseDir, "disks", "staging")
	bootDir := filepath.Join(baseDir, "boot")
	configDir := filepath.Join(baseDir, "config")

	return &Config{
		ServerID:        "nkb-srv-primary-01",
		Hostname:        hostname,
		BindAddress:     "0.0.0.0",
		APIPort:         8443,
		HTTPBootPort:    8080,
		TFTPPort:        69,
		ProxyDHCPPort:   4011,
		EnableProxyDHCP: true,
		EnableTFTP:      true,

		StorageRoot:     baseDir,
		AppDir:          appDir,
		DatabaseDir:     dbDir,
		LogsDir:         logsDir,
		BackupsDir:      backupsDir,
		DisksDir:        disksVirtualDir,
		DisksVirtualDir: disksVirtualDir,
		DisksGoldenDir:  disksGoldenDir,
		DisksClientsDir: disksClientsDir,
		DisksStagingDir: disksStagingDir,
		CacheDir:        disksClientsDir,
		SnapshotsDir:    disksStagingDir,
		BootDir:         bootDir,
		ConfigDir:       configDir,
		TFTPBootDir:     bootDir,

		DBDriver: "sqlite",
		DBDSN:    "",

		JWTSecret:       "nkb-enterprise-diskless-secret-key-change-in-production",
		TokenExpiration: 24,

		ISCSIIQNPfx: "iqn.2026-03.com.nkb",
		ISCSIPortal: "0.0.0.0",
		ISCSIPort:   3260,
		EnableCHAP:  false,

		Environment: "development",
	}
}

// EnsureStorageLayout creates the directory hierarchy according to NKB data protection standards.
func (cfg *Config) EnsureStorageLayout() error {
	dirs := []string{
		cfg.StorageRoot,
		cfg.AppDir,
		cfg.DatabaseDir,
		cfg.LogsDir,
		cfg.BackupsDir,
		cfg.DisksVirtualDir,
		cfg.DisksGoldenDir,
		cfg.DisksClientsDir,
		cfg.DisksStagingDir,
		cfg.BootDir,
		cfg.ConfigDir,
	}
	for _, d := range dirs {
		if d != "" {
			if err := os.MkdirAll(d, 0755); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadConfig reads config from a JSON file, or falls back to defaults and saves.
func LoadConfig(configPath string, baseDir string) (*Config, error) {
	cfg := DefaultConfig(baseDir)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Ensure directory exists and write default config
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return nil, err
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
