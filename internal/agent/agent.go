package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"nkb-diskless/internal/models"

	"github.com/google/uuid"
)

type Agent struct {
	cfg       *AgentConfig
	telemetry *TelemetryCollector
	executor  *CommandExecutor
	buffer    *ResultBuffer
	client    *http.Client
}

func NewAgent(cfg *AgentConfig) *Agent {
	return &Agent{
		cfg:       cfg,
		telemetry: NewTelemetryCollector(),
		executor:  NewCommandExecutorWithConfig(cfg),
		buffer:    NewResultBuffer(""),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (a *Agent) Buffer() *ResultBuffer {
	return a.buffer
}

// Run executes the main agent background loop until context cancellation.
func (a *Agent) Run(ctx context.Context) error {
	slog.Info("Starting NKB Workstation Agent", "server", a.cfg.ServerURL)

	// 1. Initial Device Registration if needed
	if a.cfg.DeviceToken == "" || a.cfg.ClientID == "" {
		if err := a.registerWithServer(); err != nil {
			slog.Warn("Initial registration failed; will retry in background loop", "err", err)
		}
	}

	ticker := time.NewTicker(time.Duration(a.cfg.HeartbeatSec) * time.Second)
	defer ticker.Stop()

	// Initial heartbeat immediately
	a.heartbeatAndPoll()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Agent shutting down...")
			return nil
		case <-ticker.C:
			if a.cfg.DeviceToken == "" {
				_ = a.registerWithServer()
				continue
			}
			a.heartbeatAndPoll()
		}
	}
}

func (a *Agent) registerWithServer() error {
	hostname, _ := os.Hostname()
	mac := a.telemetry.GetPrimaryMACAddress()
	macs := a.telemetry.GetMACInventory()
	ip := a.telemetry.GetLocalIP()

	// Ensure persistent DeviceUUID
	if a.cfg.DeviceUUID == "" {
		a.cfg.DeviceUUID = uuid.New().String()
		_ = a.cfg.Save()
	}

	reqPayload := models.AgentRegisterRequest{
		DeviceUUID:   a.cfg.DeviceUUID,
		DeviceToken:  a.cfg.DeviceToken,
		MACAddress:   mac,
		MACInventory: macs,
		Hostname:     hostname,
		IPAddress:    ip,
		OSVersion:    "Windows Workstation",
		OSBuild:      a.telemetry.GetOSBuild(),
		HardwareInfo: a.telemetry.CollectHardwareInfo(),
		AgentVersion: "1.0.0",
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/agent/register", a.cfg.ServerURL)
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.cfg.DeviceToken != "" {
		httpReq.Header.Set("X-Device-Token", a.cfg.DeviceToken)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("registration returned HTTP %d", resp.StatusCode)
	}

	var regRes models.AgentRegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regRes); err != nil {
		return err
	}

	a.cfg.ClientID = regRes.ClientID
	a.cfg.DeviceToken = regRes.DeviceToken
	_ = a.cfg.Save()

	slog.Info("Workstation agent registered successfully", "client_id", regRes.ClientID, "mac", mac)
	return nil
}

func (a *Agent) flushOfflineResults() {
	items := a.buffer.GetAll()
	if len(items) == 0 {
		return
	}

	for _, res := range items {
		resBody, err := json.Marshal(res)
		if err != nil {
			continue
		}
		urlRes := fmt.Sprintf("%s/api/v1/agent/commands/%s/result", a.cfg.ServerURL, res.CommandID)
		rReq, err := http.NewRequest("POST", urlRes, bytes.NewBuffer(resBody))
		if err != nil {
			continue
		}
		rReq.Header.Set("Content-Type", "application/json")
		rReq.Header.Set("X-Device-Token", a.cfg.DeviceToken)

		rResp, err := a.client.Do(rReq)
		if err == nil {
			if rResp.StatusCode == http.StatusOK || rResp.StatusCode == http.StatusCreated {
				_ = a.buffer.Remove(res.CommandID)
				slog.Info("Successfully flushed offline command result to server", "command_id", res.CommandID)
			}
			_ = rResp.Body.Close()
		}
	}
}

func (a *Agent) heartbeatAndPoll() {
	// 0. Flush any offline results if server is now reachable
	a.flushOfflineResults()

	// 1. Send Telemetry Heartbeat
	hbReq, err := a.telemetry.SampleHeartbeat(a.cfg.DeviceToken)
	if err != nil {
		slog.Warn("Failed to sample telemetry", "err", err)
		return
	}
	hbReq.DeviceUUID = a.cfg.DeviceUUID

	body, err := json.Marshal(hbReq)
	if err == nil {
		url := fmt.Sprintf("%s/api/v1/agent/heartbeat", a.cfg.ServerURL)
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Token", a.cfg.DeviceToken)

		resp, err := a.client.Do(req)
		if err != nil {
			slog.Debug("Server unreachable for heartbeat", "err", err)
			return
		}
		_ = resp.Body.Close()
	}

	// 2. Poll Pending Commands
	urlCmds := fmt.Sprintf("%s/api/v1/agent/commands", a.cfg.ServerURL)
	reqCmds, _ := http.NewRequest("GET", urlCmds, nil)
	reqCmds.Header.Set("X-Device-Token", a.cfg.DeviceToken)

	respCmds, err := a.client.Do(reqCmds)
	if err != nil {
		return
	}
	defer respCmds.Body.Close()

	if respCmds.StatusCode != http.StatusOK {
		return
	}

	var cmds []models.RemoteCommand
	if err := json.NewDecoder(respCmds.Body).Decode(&cmds); err != nil {
		return
	}

	// 3. Execute & Report Results
	for _, cmd := range cmds {
		slog.Info("Executing authorized command", "command_type", cmd.CommandType, "id", cmd.ID)
		res := a.executor.ExecuteAuthorizedCommand(&cmd)

		// Enqueue locally first to guarantee persistence against network failure
		_ = a.buffer.Enqueue(res)

		// Attempt immediate delivery
		resBody, err := json.Marshal(res)
		if err == nil {
			urlRes := fmt.Sprintf("%s/api/v1/agent/commands/%s/result", a.cfg.ServerURL, cmd.ID)
			rReq, _ := http.NewRequest("POST", urlRes, bytes.NewBuffer(resBody))
			rReq.Header.Set("Content-Type", "application/json")
			rReq.Header.Set("X-Device-Token", a.cfg.DeviceToken)
			if rResp, err := a.client.Do(rReq); err == nil {
				if rResp.StatusCode == http.StatusOK || rResp.StatusCode == http.StatusCreated {
					_ = a.buffer.Remove(cmd.ID)
				}
				_ = rResp.Body.Close()
			}
		}
	}
}
