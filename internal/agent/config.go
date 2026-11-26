package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AgentConfig struct {
	DeviceUUID      string `json:"device_uuid,omitempty"`
	ServerURL       string `json:"server_url"`
	DeviceToken     string `json:"device_token"`
	ClientID        string `json:"client_id"`
	HeartbeatSec    int    `json:"heartbeat_interval_seconds"`
	AllowSelfReboot bool   `json:"allow_self_reboot"` // Safety guard: defaults to false
	ConfigPath      string `json:"-"`
}

func DefaultConfig() *AgentConfig {
	return &AgentConfig{
		ServerURL:       "http://localhost:8443",
		HeartbeatSec:    5,
		AllowSelfReboot: false, // Strict safety: fail-closed against accidental reboot
	}
}

// ResolveConfigPath locates or establishes the agent configuration file path.
// Priority:
// 1. C:\NKB-PoC\config\nkb_agent.json (if C:\NKB-PoC exists)
// 2. %ProgramData%\NKB_Agent\nkb_agent.json
// 3. ./config/nkb_agent.json
func ResolveConfigPath() string {
	// Check PoC data root
	pocDir := `C:\NKB-PoC\config`
	if fi, err := os.Stat(`C:\NKB-PoC`); err == nil && fi.IsDir() {
		_ = os.MkdirAll(pocDir, 0755)
		return filepath.Join(pocDir, "nkb_agent.json")
	}

	programData := os.Getenv("ProgramData")
	if programData != "" {
		dir := filepath.Join(programData, "NKB_Agent")
		_ = os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "nkb_agent.json")
	}

	_ = os.MkdirAll("config", 0755)
	return filepath.Join("config", "nkb_agent.json")
}

func LoadConfig(path string) (*AgentConfig, error) {
	if path == "" {
		path = ResolveConfigPath()
	}

	cfg := DefaultConfig()
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("corrupt agent config at %s: %w", path, err)
		}
	}

	return cfg, nil
}

// Save atomically writes the configuration to disk to prevent file corruption.
func (c *AgentConfig) Save() error {
	if c.ConfigPath == "" {
		c.ConfigPath = ResolveConfigPath()
	}

	dir := filepath.Dir(c.ConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Write to temporary file first
	tmpPath := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	// On Windows, os.Rename fails if destination exists, so remove destination if present
	_ = os.Remove(c.ConfigPath)
	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		// Fallback: write directly
		return os.WriteFile(c.ConfigPath, data, 0644)
	}

	return nil
}
