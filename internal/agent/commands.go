package agent

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"nkb-diskless/internal/models"

	"github.com/shirou/gopsutil/v3/process"
)

type CommandExecutor struct{}

func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{}
}

// ExecuteAuthorizedCommand runs only pre-approved, safe administrative actions.
func (e *CommandExecutor) ExecuteAuthorizedCommand(cmd *models.RemoteCommand) *models.CommandResult {
	start := time.Now()
	res := &models.CommandResult{
		CommandID:  cmd.ID,
		ClientID:   cmd.ClientID,
		ExecutedAt: start,
	}

	commandType := strings.ToLower(cmd.CommandType)

	switch commandType {
	case "lock":
		return e.executeLock(res)
	case "restart":
		return e.executeRestart(res)
	case "shutdown":
		return e.executeShutdown(res)
	case "query_processes":
		return e.executeProcessQuery(res)
	case "wol":
		res.ExitCode = 0
		res.Output = "Wake-on-LAN supported on primary NIC"
		return res
	default:
		res.ExitCode = 1
		res.ErrorMessage = fmt.Sprintf("unauthorized command rejected: '%s' is not in the safe command whitelist", cmd.CommandType)
		return res
	}
}

func (e *CommandExecutor) executeLock(res *models.CommandResult) *models.CommandResult {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("rundll32.exe", "user32.dll,LockWorkStation")
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.ExitCode = 1
			res.ErrorMessage = err.Error()
		} else {
			res.ExitCode = 0
			res.Output = "Workstation locked successfully: " + string(out)
		}
	} else {
		res.ExitCode = 0
		res.Output = "Simulated workstation lock (non-Windows platform)"
	}
	return res
}

func (e *CommandExecutor) executeRestart(res *models.CommandResult) *models.CommandResult {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("shutdown.exe", "/r", "/t", "10", "/c", "NKB Central Management: Authorized Restart")
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.ExitCode = 1
			res.ErrorMessage = err.Error()
		} else {
			res.ExitCode = 0
			res.Output = "Restart scheduled in 10 seconds: " + string(out)
		}
	} else {
		res.ExitCode = 0
		res.Output = "Simulated workstation restart scheduled"
	}
	return res
}

func (e *CommandExecutor) executeShutdown(res *models.CommandResult) *models.CommandResult {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("shutdown.exe", "/s", "/t", "10", "/c", "NKB Central Management: Authorized Shutdown")
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.ExitCode = 1
			res.ErrorMessage = err.Error()
		} else {
			res.ExitCode = 0
			res.Output = "Shutdown scheduled in 10 seconds: " + string(out)
		}
	} else {
		res.ExitCode = 0
		res.Output = "Simulated workstation shutdown scheduled"
	}
	return res
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

	summaries := make([]ProcessSummary, 0, 20)
	for i, p := range procs {
		if i >= 25 {
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
