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

	// Storage and image repositories
	StorageRoot    string `json:"storage_root"`
	DisksDir       string `json:"disks_dir"`
	CacheDir       string `json:"cache_dir"`
	SnapshotsDir   string `json:"snapshots_dir"`
	TFTPBootDir    string `json:"tftpboot_dir"`

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
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "nkb-master-server"
	}

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

		StorageRoot:  baseDir,
		DisksDir:     filepath.Join(baseDir, "disks"),
		CacheDir:     filepath.Join(baseDir, "cache"),
		SnapshotsDir: filepath.Join(baseDir, "snapshots"),
		TFTPBootDir:  filepath.Join(baseDir, "tftpboot"),

		DBDriver: "postgres",
		DBDSN:    "postgres://postgres:nkbdiskless@localhost:5432/nkb_diskless?sslmode=disable",

		JWTSecret:       "nkb-enterprise-diskless-secret-key-change-in-production",
		TokenExpiration: 24,

		ISCSIIQNPfx: "iqn.2026-11.com.nkb",
		ISCSIPortal: "0.0.0.0",
		ISCSIPort:   3260,
		EnableCHAP:  false,

		Environment: "development",
	}
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
