package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"nkb-diskless/internal/agent"
	"nkb-diskless/internal/models"
)

func TestTelemetryCollector(t *testing.T) {
	collector := agent.NewTelemetryCollector()

	mac := collector.GetPrimaryMACAddress()
	if mac == "" {
		t.Fatalf("expected non-empty MAC address")
	}
	t.Logf("Primary MAC Address: %s", mac)

	ip := collector.GetLocalIP()
	if ip == "" {
		t.Fatalf("expected valid IP address")
	}
	t.Logf("Local IP: %s", ip)

	user := collector.GetLoggedInUser()
	t.Logf("Logged-in user: %s", user)

	hwInfo := collector.CollectHardwareInfo()
	if hwInfo == "" {
		t.Fatalf("expected non-empty hardware info")
	}
	var specs map[string]any
	if err := json.Unmarshal([]byte(hwInfo), &specs); err != nil {
		t.Fatalf("invalid hardware info JSON: %v", err)
	}
	if _, ok := specs["cpu"]; !ok {
		t.Fatalf("expected 'cpu' in hardware specs")
	}

	sample, err := collector.SampleHeartbeat("test-token")
	if err != nil {
		t.Fatalf("failed to sample heartbeat: %v", err)
	}
	if sample.AgentStatus != "connected" {
		t.Fatalf("expected agent_status='connected', got '%s'", sample.AgentStatus)
	}
	t.Logf("Sampled Uptime: %d seconds, CPU: %.1f%%, RAM: %.1f%%", sample.UptimeSec, sample.CPUUsagePct, sample.RAMUsagePct)
}

func TestCommandExecutorWhitelist(t *testing.T) {
	executor := agent.NewCommandExecutor()

	// 1. Authorized Safe Command: query_processes
	queryCmd := &models.RemoteCommand{
		ID:          "cmd-01",
		ClientID:    "client-01",
		CommandType: "query_processes",
	}
	res := executor.ExecuteAuthorizedCommand(queryCmd)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for query_processes, got %d (err: %s)", res.ExitCode, res.ErrorMessage)
	}
	if !strings.Contains(res.Output, "pid") {
		t.Fatalf("expected process list JSON in output, got: %s", res.Output)
	}

	// 2. Authorized Safe Command: wol qualification
	wolCmd := &models.RemoteCommand{
		ID:          "cmd-02",
		ClientID:    "client-01",
		CommandType: "wol",
	}
	wolRes := executor.ExecuteAuthorizedCommand(wolCmd)
	if wolRes.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for wol, got %d", wolRes.ExitCode)
	}

	// 3. Unauthorized Dangerous Command must be strictly rejected
	dangerCmd := &models.RemoteCommand{
		ID:          "cmd-03",
		ClientID:    "client-01",
		CommandType: "format_c_drive",
	}
	dangerRes := executor.ExecuteAuthorizedCommand(dangerCmd)
	if dangerRes.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for unauthorized command")
	}
	if !strings.Contains(dangerRes.ErrorMessage, "unauthorized command rejected") {
		t.Fatalf("expected rejection error message, got: %s", dangerRes.ErrorMessage)
	}
}
