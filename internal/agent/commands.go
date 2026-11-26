package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	"nkb-diskless/internal/models"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/process"
)

type CommandExecutor struct {
	cfg       *AgentConfig
	telemetry *TelemetryCollector
}

func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{
		cfg:       DefaultConfig(),
		telemetry: NewTelemetryCollector(),
	}
}

func NewCommandExecutorWithConfig(cfg *AgentConfig) *CommandExecutor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &CommandExecutor{
		cfg:       cfg,
		telemetry: NewTelemetryCollector(),
	}
}

// ExecuteAuthorizedCommand strictly enforces the command whitelist and safe execution boundaries.
// Zero dynamic shell execution, zero PowerShell execution, and zero OS power operations are permitted.
func (e *CommandExecutor) ExecuteAuthorizedCommand(cmd *models.RemoteCommand) *models.CommandResult {
	start := time.Now()
	res := &models.CommandResult{
		CommandID:  cmd.ID,
		ClientID:   cmd.ClientID,
		ExecutedAt: start,
	}

	commandType := strings.ToLower(strings.TrimSpace(cmd.CommandType))

	switch commandType {
	// 1. Approved Read-Only Diagnostic Commands
	case "query_processes":
		return e.executeProcessQuery(res)

	case "query_status", "query_health":
		return e.executeStatusQuery(res)

	case "query_network":
		return e.executeNetworkQuery(res)

	// 2. Strict Lockdown: Destructive power commands are permanently disabled
	case "restart", "reboot":
		res.ExitCode = 1
		res.ErrorMessage = "DISABLED_BY_SAFETY_POLICY: Remote restart/reboot is disabled by safety policy"
		return res

	case "shutdown", "force_shutdown":
		res.ExitCode = 1
		res.ErrorMessage = "DISABLED_BY_SAFETY_POLICY: Remote shutdown is disabled by safety policy"
		return res

	case "logoff", "force_logoff":
		res.ExitCode = 1
		res.ErrorMessage = "DISABLED_BY_SAFETY_POLICY: Remote logoff is disabled by safety policy"
		return res

	// 3. Deferred Network Commands
	case "wol", "wake_on_lan", "wake-on-lan":
		res.ExitCode = 1
		res.ErrorMessage = "DISABLED_BY_SAFETY_POLICY: Wake-on-LAN is disabled for Phase 2 PoC"
		return res

	// 4. Lock Workstation: Disabled during audit phase to protect development and test host
	case "lock", "lock_workstation":
		res.ExitCode = 1
		res.ErrorMessage = "DISABLED_BY_SAFETY_POLICY: Lock workstation is disabled during audit phase"
		return res

	default:
		// Fail closed for any unapproved or arbitrary execution
		res.ExitCode = 1
		res.ErrorMessage = fmt.Sprintf("unauthorized command rejected: '%s' is not in the safe command whitelist", cmd.CommandType)
		return res
	}
}

type ProcessSummary struct {
	PID      int32  `json:"pid"`
	Name     string `json:"name"`
	MemoryMB uint64 `json:"memory_mb"`
}

func (e *CommandExecutor) executeProcessQuery(res *models.CommandResult) *models.CommandResult {
	procs, err := process.Processes()
	if err != nil {
		res.ExitCode = 1
		res.ErrorMessage = fmt.Sprintf("failed to query processes: %v", err)
		return res
	}

	summaries := make([]ProcessSummary, 0, 30)
	for i, p := range procs {
		if i >= 30 {
			break
		}
		name, _ := p.Name()
		memInfo, _ := p.MemoryInfo()
		var memMB uint64
		if memInfo != nil {
			memMB = memInfo.RSS / (1024 * 1024)
		}

		summaries = append(summaries, ProcessSummary{
			PID:      p.Pid,
			Name:     name,
			MemoryMB: memMB,
		})
	}

	outBytes, _ := json.MarshalIndent(summaries, "", "  ")
	res.ExitCode = 0
	res.Output = string(outBytes)
	return res
}

func (e *CommandExecutor) executeStatusQuery(res *models.CommandResult) *models.CommandResult {
	hInfo, _ := host.Info()
	uptime, _ := host.Uptime()

	status := map[string]any{
		"logged_in_user": e.telemetry.GetLoggedInUser(),
		"os_platform":    runtime.GOOS,
		"uptime_seconds": uptime,
		"primary_mac":    e.telemetry.GetPrimaryMACAddress(),
		"local_ip":       e.telemetry.GetLocalIP(),
	}
	if hInfo != nil {
		status["platform_version"] = hInfo.PlatformVersion
		status["kernel_version"] = hInfo.KernelVersion
	}

	outBytes, _ := json.MarshalIndent(status, "", "  ")
	res.ExitCode = 0
	res.Output = string(outBytes)
	return res
}

func (e *CommandExecutor) executeNetworkQuery(res *models.CommandResult) *models.CommandResult {
	interfaces, err := net.Interfaces()
	if err != nil {
		res.ExitCode = 1
		res.ErrorMessage = err.Error()
		return res
	}

	type netAdapter struct {
		Name        string   `json:"name"`
		MAC         string   `json:"mac"`
		Flags       string   `json:"flags"`
		IPAddresses []string `json:"ip_addresses"`
	}

	adapters := make([]netAdapter, 0, len(interfaces))
	for _, iface := range interfaces {
		addrs, _ := iface.Addrs()
		ipList := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			ipList = append(ipList, addr.String())
		}
		adapters = append(adapters, netAdapter{
			Name:        iface.Name,
			MAC:         strings.ToUpper(iface.HardwareAddr.String()),
			Flags:       iface.Flags.String(),
			IPAddresses: ipList,
		})
	}

	outBytes, _ := json.MarshalIndent(adapters, "", "  ")
	res.ExitCode = 0
	res.Output = string(outBytes)
	return res
}
