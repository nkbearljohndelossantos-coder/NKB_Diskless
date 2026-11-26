package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"nkb-diskless/internal/services/client_mgmt"
)

// App struct
type App struct {
	ctx       context.Context
	serverURL string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		serverURL: "http://127.0.0.1:8080",
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GetServerURL returns configured API endpoint
func (a *App) GetServerURL() string {
	return a.serverURL
}

// SetServerURL updates target API endpoint
func (a *App) SetServerURL(url string) {
	a.serverURL = url
}

// CheckServerHealth verifies connectivity to backend API
func (a *App) CheckServerHealth(url string) map[string]any {
	if url == "" {
		url = a.serverURL
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url + "/api/v1/health")
	if err != nil {
		return map[string]any{
			"online":  false,
			"error":   err.Error(),
			"version": "unknown",
		}
	}
	defer resp.Body.Close()

	return map[string]any{
		"online":      resp.StatusCode == http.StatusOK,
		"status_code": resp.StatusCode,
		"target_url":  url,
	}
}

// LaunchRemoteDesktop creates a transient .rdp connection and opens mstsc.exe
func (a *App) LaunchRemoteDesktop(hostname string, ip string) string {
	rdpContent := client_mgmt.GenerateRDPFile(hostname, ip)
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("nkb_%s.rdp", hostname))
	if err := os.WriteFile(tempPath, []byte(rdpContent), 0600); err != nil {
		return fmt.Sprintf("failed to create rdp file: %v", err)
	}

	if runtime.GOOS == "windows" {
		cmd := exec.Command("mstsc.exe", tempPath)
		if err := cmd.Start(); err != nil {
			return fmt.Sprintf("failed to launch mstsc: %v", err)
		}
	}

	return "RDP session launched"
}

// SendWakeOnLAN is permanently disabled by NKB safety policy
func (a *App) SendWakeOnLAN(mac string) string {
	return "WOL error: DISABLED_BY_SAFETY_POLICY: Wake-on-LAN is permanently disabled by safety policy"
}

// OpenStorageFolder opens Explorer to specified path
func (a *App) OpenStorageFolder(folderPath string) string {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("explorer.exe", folderPath)
		if err := cmd.Start(); err != nil {
			return fmt.Sprintf("failed to open folder: %v", err)
		}
	}
	return "Explorer opened"
}
