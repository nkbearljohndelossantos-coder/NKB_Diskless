package agent_test

import (
	"strings"
	"testing"

	"nkb-diskless/internal/agent"
	"nkb-diskless/internal/models"
)

// TestAgentSafety_ProhibitedCommandsPermanentlyBlocked verifies that the Workstation Agent
// independently enforces safety blocks for all reboot, shutdown, logoff, lock, and WOL commands.
func TestAgentSafety_ProhibitedCommandsPermanentlyBlocked(t *testing.T) {
	executor := agent.NewCommandExecutor()

	prohibitedCommands := []string{
		"shutdown",
		"force_shutdown",
		"reboot",
		"restart",
		"logoff",
		"force_logoff",
		"lock",
		"lock_workstation",
		"wol",
		"wake_on_lan",
		"wake-on-lan",
	}

	for _, cmdType := range prohibitedCommands {
		t.Run("AgentExecute: "+cmdType, func(t *testing.T) {
			cmd := &models.RemoteCommand{
				ID:          "safety-test-cmd-" + cmdType,
				ClientID:    "client-test-id",
				CommandType: cmdType,
			}

			result := executor.ExecuteAuthorizedCommand(cmd)
			if result.ExitCode == 0 {
				t.Fatalf("expected non-zero ExitCode for prohibited command '%s', got: %d", cmdType, result.ExitCode)
			}
			if !strings.Contains(result.ErrorMessage, "DISABLED_BY_SAFETY_POLICY") {
				t.Fatalf("expected ErrorMessage to contain DISABLED_BY_SAFETY_POLICY for '%s', got: %s", cmdType, result.ErrorMessage)
			}
		})
	}
}

// TestAgentSafety_ArbitraryCommandsRejected verifies fail-closed behavior on arbitrary or dynamic shell commands.
func TestAgentSafety_ArbitraryCommandsRejected(t *testing.T) {
	executor := agent.NewCommandExecutor()

	arbitraryCommands := []string{
		"cmd.exe",
		"powershell",
		"format c:",
		"rmdir /s /q c:\\",
		"whoami",
		"del *.*",
	}

	for _, cmdType := range arbitraryCommands {
		t.Run("Arbitrary: "+cmdType, func(t *testing.T) {
			cmd := &models.RemoteCommand{
				ID:          "safety-arbitrary-cmd",
				ClientID:    "client-test-id",
				CommandType: cmdType,
			}

			result := executor.ExecuteAuthorizedCommand(cmd)
			if result.ExitCode == 0 {
				t.Fatalf("expected non-zero ExitCode for arbitrary command '%s', got: %d", cmdType, result.ExitCode)
			}
			if !strings.Contains(result.ErrorMessage, "not in the safe command whitelist") {
				t.Fatalf("expected error indicating command is not in safe whitelist, got: %s", result.ErrorMessage)
			}
		})
	}
}
