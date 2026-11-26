package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Config represents all runtime settings for the NKB Diskless System.
type Config struct {
	// Server identity & binding
	ServerID        string `json:"server_id"`
	Hostname        string `json:"hostname"`
	BindAddress     string `json:"bind_address"`
	APIPort         int    `json:"api_port"`
	HTTPBootPort    int    `json:"http_boot_port"`
	TFTPPort        int    `json:"tftp_port"`
	ProxyDHCPPort   int    `json:"proxy_dhcp_port"`
	EnableProxyDHCP bool   `json:"enable_proxy_dhcp"`
	EnableTFTP      bool   `json:"enable_tftp"`
	EnableHTTPBoot  bool   `json:"enable_http_boot"`
	Phase3AMode     bool   `json:"phase3a_mode"`

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
	ISCSIIQNPfx string `json:"iscsi_iqn_prefix"`
	ISCSIPortal string `json:"iscsi_portal"`
	ISCSIPort   int    `json:"iscsi_port"`
	EnableCHAP  bool   `json:"enable_chap"`

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
		EnableProxyDHCP: false,
		EnableTFTP:      false,
		EnableHTTPBoot:  false,
		Phase3AMode:     true, // Default to safe diagnostic Phase 3A mode

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

// Validate checks for configuration inconsistencies and safety requirements.
func (cfg *Config) Validate() error {
	if cfg.APIPort <= 0 || cfg.APIPort > 65535 {
		return fmt.Errorf("invalid api_port: %d", cfg.APIPort)
	}

	// When network boot services are enabled, strict non-wildcard binding is required
	if cfg.EnableProxyDHCP || cfg.EnableTFTP {
		trimmed := strings.TrimSpace(cfg.BindAddress)
		if trimmed == "" || trimmed == "0.0.0.0" || trimmed == "::" {
			return errors.New("network boot services (ProxyDHCP/TFTP) cannot be enabled on a wildcard (0.0.0.0) or empty bind address; an explicit dedicated test NIC IP must be configured")
		}
		ip := net.ParseIP(trimmed)
		if ip == nil || ip.IsUnspecified() || ip.To4() == nil {
			return fmt.Errorf("network boot services require a valid non-wildcard IPv4 bind address, got: %q", cfg.BindAddress)
		}
	}

	return nil
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

	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Also parse nested configuration blocks if present (from first-run setup wizard)
	var nested struct {
		HTTPBoot *struct {
			Enabled *bool `json:"enabled"`
			Port    int   `json:"port"`
		} `json:"http_boot"`
		TFTP *struct {
			Enabled *bool `json:"enabled"`
			Port    int   `json:"port"`
		} `json:"tftp"`
		DHCP *struct {
			Enabled *bool `json:"enabled"`
		} `json:"dhcp"`
	}
	if err := json.Unmarshal(data, &nested); err == nil {
		if nested.HTTPBoot != nil {
			if nested.HTTPBoot.Enabled != nil {
				cfg.EnableHTTPBoot = *nested.HTTPBoot.Enabled
			}
			if nested.HTTPBoot.Port > 0 {
				cfg.HTTPBootPort = nested.HTTPBoot.Port
			}
		}
		if nested.TFTP != nil {
			if nested.TFTP.Enabled != nil {
				cfg.EnableTFTP = *nested.TFTP.Enabled
			}
			if nested.TFTP.Port > 0 {
				cfg.TFTPPort = nested.TFTP.Port
			}
		}
		if nested.DHCP != nil && nested.DHCP.Enabled != nil {
			cfg.EnableProxyDHCP = *nested.DHCP.Enabled
		}
	}

	return cfg, nil
}
