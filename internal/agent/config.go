package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type AgentConfig struct {
	ServerURL         string `json:"server_url"`
	DeviceToken       string `json:"device_token"`
	ClientID          string `json:"client_id"`
	HeartbeatSec      int    `json:"heartbeat_interval_seconds"`
	ConfigPath        string `json:"-"`
}

func DefaultConfig() *AgentConfig {
	return &AgentConfig{
		ServerURL:    "http://localhost:8443",
		HeartbeatSec: 5,
	}
}

func ResolveConfigPath() string {
	programData := os.Getenv("ProgramData")
	if programData != "" {
		dir := filepath.Join(programData, "NKB_Agent")
		_ = os.MkdirAll(dir, 0755)
		return filepath.Join(dir, "agent_config.json")
	}
	return "agent_config.json"
}

func LoadConfig(path string) (*AgentConfig, error) {
	if path == "" {
		path = ResolveConfigPath()
	}

	cfg := DefaultConfig()
	cfg.ConfigPath = path

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	return cfg, nil
}

func (c *AgentConfig) Save() error {
	if c.ConfigPath == "" {
		c.ConfigPath = ResolveConfigPath()
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.ConfigPath, data, 0644)
}
