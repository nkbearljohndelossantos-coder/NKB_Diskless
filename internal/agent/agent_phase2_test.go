package agent_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nkb-diskless/internal/agent"
	"nkb-diskless/internal/models"
)

func TestResultBufferPersistenceAndRecovery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_buf_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	bufPath := filepath.Join(tempDir, "pending_results.json")
	buf := agent.NewResultBuffer(bufPath)

	if buf.Count() != 0 {
		t.Fatalf("expected 0 initial items, got %d", buf.Count())
	}

	// 1. Enqueue results
	res1 := &models.CommandResult{
		CommandID:  "cmd-1",
		ClientID:   "client-1",
		ExitCode:   0,
		Output:     "output 1",
		ExecutedAt: time.Now(),
	}
	res2 := &models.CommandResult{
		CommandID:  "cmd-2",
		ClientID:   "client-1",
		ExitCode:   1,
		Output:     "output 2",
		ExecutedAt: time.Now(),
	}

	if err := buf.Enqueue(res1); err != nil {
		t.Fatalf("failed to enqueue res1: %v", err)
	}
	if err := buf.Enqueue(res2); err != nil {
		t.Fatalf("failed to enqueue res2: %v", err)
	}

	if buf.Count() != 2 {
		t.Fatalf("expected 2 items, got %d", buf.Count())
	}

	// 2. Verify persistence across instances
	bufReloaded := agent.NewResultBuffer(bufPath)
	if bufReloaded.Count() != 2 {
		t.Fatalf("expected 2 items after reload, got %d", bufReloaded.Count())
	}

	// 3. Remove res1
	if err := bufReloaded.Remove("cmd-1"); err != nil {
		t.Fatalf("failed to remove cmd-1: %v", err)
	}
	if bufReloaded.Count() != 1 {
		t.Fatalf("expected 1 item after removal, got %d", bufReloaded.Count())
	}

	items := bufReloaded.GetAll()
	if len(items) != 1 || items[0].CommandID != "cmd-2" {
		t.Fatalf("unexpected items: %+v", items)
	}

	// 4. Test Corrupt File Recovery
	if err := os.WriteFile(bufPath, []byte("NOT_VALID_JSON{{{"), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	bufCorrupt := agent.NewResultBuffer(bufPath)
	if bufCorrupt.Count() != 0 {
		t.Fatalf("expected empty buffer after corruption recovery, got %d", bufCorrupt.Count())
	}

	// Check that a corrupt backup file was preserved
	files, _ := filepath.Glob(filepath.Join(tempDir, "pending_results.json.corrupt_*.json"))
	if len(files) == 0 {
		t.Fatalf("expected preserved corrupt backup file, found none")
	}
}

func TestCommandExecutorSafetyPolicies(t *testing.T) {
	cfg := agent.DefaultConfig()
	cfg.ServerURL = "http://localhost:8443"
	cfg.AllowSelfReboot = false

	executor := agent.NewCommandExecutorWithConfig(cfg)

	// 1. Whitelist Rejection
	arbitraryCmd := &models.RemoteCommand{
		ID:          "cmd-evil",
		ClientID:    "c1",
		CommandType: "cmd.exe /c del C:\\",
	}
	res := executor.ExecuteAuthorizedCommand(arbitraryCmd)
	if res.ExitCode != 1 || !strings.Contains(res.ErrorMessage, "not in the safe command whitelist") {
		t.Fatalf("expected rejection of arbitrary command, got exit=%d err=%s", res.ExitCode, res.ErrorMessage)
	}

	// 2. Destructive Reboot Command (Permanently Disabled by Safety Policy)
	rebootCmd := &models.RemoteCommand{
		ID:          "cmd-reboot",
		ClientID:    "c1",
		CommandType: "reboot",
	}
	resReboot := executor.ExecuteAuthorizedCommand(rebootCmd)
	if resReboot.ExitCode != 1 || !strings.Contains(resReboot.ErrorMessage, "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected safety policy reject for reboot, got exit=%d err=%s out=%s",
			resReboot.ExitCode, resReboot.ErrorMessage, resReboot.Output)
	}

	// 3. Destructive Shutdown Command (Permanently Disabled by Safety Policy)
	shutdownCmd := &models.RemoteCommand{
		ID:          "cmd-shutdown",
		ClientID:    "c1",
		CommandType: "shutdown",
	}
	resShutdown := executor.ExecuteAuthorizedCommand(shutdownCmd)
	if resShutdown.ExitCode != 1 || !strings.Contains(resShutdown.ErrorMessage, "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected safety policy reject for shutdown, got exit=%d err=%s out=%s",
			resShutdown.ExitCode, resShutdown.ErrorMessage, resShutdown.Output)
	}

	// 4. Lock Workstation Command (Disabled during Audit Phase)
	lockCmd := &models.RemoteCommand{
		ID:          "cmd-lock",
		ClientID:    "c1",
		CommandType: "lock",
	}
	resLock := executor.ExecuteAuthorizedCommand(lockCmd)
	if resLock.ExitCode != 1 || !strings.Contains(resLock.ErrorMessage, "DISABLED_BY_SAFETY_POLICY") {
		t.Fatalf("expected safety policy reject for lock, got exit=%d err=%s out=%s",
			resLock.ExitCode, resLock.ErrorMessage, resLock.Output)
	}

	// 5. Query Status
	statusCmd := &models.RemoteCommand{
		ID:          "cmd-status",
		ClientID:    "c1",
		CommandType: "query_status",
	}
	resStatus := executor.ExecuteAuthorizedCommand(statusCmd)
	if resStatus.ExitCode != 0 {
		t.Fatalf("expected query_status exit code 0, got %d, err=%s", resStatus.ExitCode, resStatus.ErrorMessage)
	}
	var statusMap map[string]any
	if err := json.Unmarshal([]byte(resStatus.Output), &statusMap); err != nil {
		t.Fatalf("expected valid JSON output from query_status, got: %s", resStatus.Output)
	}

	// 6. Query Processes
	procCmd := &models.RemoteCommand{
		ID:          "cmd-proc",
		ClientID:    "c1",
		CommandType: "query_processes",
	}
	resProc := executor.ExecuteAuthorizedCommand(procCmd)
	if resProc.ExitCode != 0 {
		t.Fatalf("expected query_processes exit code 0, got %d, err=%s", resProc.ExitCode, resProc.ErrorMessage)
	}
	var procList []agent.ProcessSummary
	if err := json.Unmarshal([]byte(resProc.Output), &procList); err != nil {
		t.Fatalf("expected valid JSON output from query_processes, got: %s", resProc.Output)
	}
	if len(procList) == 0 {
		t.Fatalf("expected at least 1 running process")
	}

	// 7. Query Network
	netCmd := &models.RemoteCommand{
		ID:          "cmd-net",
		ClientID:    "c1",
		CommandType: "query_network",
	}
	resNet := executor.ExecuteAuthorizedCommand(netCmd)
	if resNet.ExitCode != 0 {
		t.Fatalf("expected query_network exit code 0, got %d, err=%s", resNet.ExitCode, resNet.ErrorMessage)
	}
}

func TestResultBufferCapacityLimit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_buf_cap_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	bufPath := filepath.Join(tempDir, "pending_results.json")
	buffer := agent.NewResultBuffer(bufPath)

	now := time.Now()
	// Enqueue 550 items (exceeding maxBufferCapacity = 500)
	for i := 0; i < 550; i++ {
		res := &models.CommandResult{
			CommandID:   fmt.Sprintf("cmd-%04d", i),
			ClientID:    "client-1",
			ExitCode:    0,
			Output:      "result",
			ExecutedAt:  now,
			CompletedAt: &now,
		}
		if err := buffer.Enqueue(res); err != nil {
			t.Fatalf("failed to enqueue result %d: %v", i, err)
		}
	}

	// Length must be strictly capped at 500
	if buffer.Count() != 500 {
		t.Fatalf("expected buffer count capped at 500, got %d", buffer.Count())
	}

	// GetAll and verify count
	items := buffer.GetAll()
	if len(items) != 500 {
		t.Fatalf("expected 500 items in buffer, got %d", len(items))
	}

	// Remove all and verify empty
	for _, item := range items {
		if err := buffer.Remove(item.CommandID); err != nil {
			t.Fatalf("failed to remove item: %v", err)
		}
	}
	if buffer.Count() != 0 {
		t.Fatalf("expected buffer empty after removing all, got %d", buffer.Count())
	}
}
