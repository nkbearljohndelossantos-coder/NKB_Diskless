package auth_rbac_test

import (
	"os"
	"path/filepath"
	"testing"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/auth_rbac"
)

func TestAuthAndAgentWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_auth_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	migrationsDir := filepath.Join("..", "..", "database", "migrations")
	if err := db.Migrate(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	authService := auth_rbac.NewAuthService("test-secret-key-12345", db, 2)

	// 1. Test Admin Login & JWT Generation
	adminUser, err := db.VerifyUserPassword("admin", "nkbadmin2026")
	if err != nil {
		t.Fatalf("failed to verify default admin password: %v", err)
	}
	if adminUser.Role != "superadmin" {
		t.Fatalf("expected role 'superadmin', got '%s'", adminUser.Role)
	}

	token, expiresAt, err := authService.GenerateToken(adminUser)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if token == "" || expiresAt.IsZero() {
		t.Fatalf("invalid token or expiry")
	}

	claims, err := authService.ValidateToken(token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}
	if claims.Username != "admin" || claims.Role != "superadmin" {
		t.Fatalf("claims mismatch: %+v", claims)
	}

	// 2. Test Agent Registration
	regReq := &models.AgentRegisterRequest{
		MACAddress:   "11:22:33:44:55:66",
		Hostname:     "NKB-LINE1-TESTPC",
		IPAddress:    "192.168.99.50",
		OSVersion:    "Windows 11 Pro 23H2",
		HardwareInfo: `{"cpu":"Intel Core i5-10400","ram_mb":16384}`,
		AgentVersion: "1.0.0",
	}

	regRes, err := db.RegisterAgent(regReq)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}
	if regRes.DeviceToken == "" || regRes.ClientID == "" {
		t.Fatalf("invalid agent registration response: %+v", regRes)
	}

	// 3. Test Agent Device Token Authentication
	authedClient, err := db.AuthenticateAgent(regRes.DeviceToken)
	if err != nil {
		t.Fatalf("failed to authenticate agent token: %v", err)
	}
	if authedClient.ID != regRes.ClientID || authedClient.Hostname != "NKB-LINE1-TESTPC" {
		t.Fatalf("agent client mismatch: %+v", authedClient)
	}

	// 4. Test Agent Telemetry Heartbeat
	hbReq := &models.AgentHeartbeatRequest{
		DeviceToken:  regRes.DeviceToken,
		LoggedInUser: "operator01",
		UptimeSec:    3600,
		CPUUsagePct:  24.5,
		RAMUsagePct:  48.2,
		DiskUsagePct: 35.0,
		NetworkRxMBs: 1.2,
		NetworkTxMBs: 0.8,
		AgentStatus:  "connected",
	}
	err = db.ProcessAgentHeartbeat(regRes.ClientID, hbReq)
	if err != nil {
		t.Fatalf("failed to process agent heartbeat: %v", err)
	}

	// Verify enriched client listing
	clients, err := db.ListClients()
	if err != nil {
		t.Fatalf("failed to list clients: %v", err)
	}
	var found *models.Client
	for _, c := range clients {
		if c.ID == regRes.ClientID {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatalf("client not found in list")
	}
	if found.LoggedInUser != "operator01" || found.UptimeSec != 3600 {
		t.Fatalf("metadata not populated: LoggedInUser=%s, Uptime=%d", found.LoggedInUser, found.UptimeSec)
	}

	// 5. Test Remote Command Queueing & Retrieval
	cmd := &models.RemoteCommand{
		ClientID:       regRes.ClientID,
		IssuerUsername: "admin",
		CommandType:    "lock",
		Payload:        `{"message":"Maintenance cycle"}`,
	}
	err = db.QueueCommand(cmd)
	if err != nil {
		t.Fatalf("failed to queue command: %v", err)
	}

	pendingCmds, err := db.GetPendingCommands(regRes.ClientID)
	if err != nil {
		t.Fatalf("failed to get pending commands: %v", err)
	}
	if len(pendingCmds) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(pendingCmds))
	}
	if pendingCmds[0].CommandType != "lock" {
		t.Fatalf("expected 'lock', got %s", pendingCmds[0].CommandType)
	}

	// 6. Test Command Result Recording
	res := &models.CommandResult{
		CommandID:    pendingCmds[0].ID,
		ClientID:     regRes.ClientID,
		ExitCode:     0,
		Output:       "Workstation locked successfully",
		ErrorMessage: "",
	}
	err = db.RecordCommandResult(res)
	if err != nil {
		t.Fatalf("failed to record command result: %v", err)
	}

	history, err := db.ListCommands(regRes.ClientID, 5)
	if err != nil {
		t.Fatalf("failed to list command history: %v", err)
	}
	if len(history) != 1 || history[0].Status != "completed" {
		t.Fatalf("expected completed status, got %+v", history)
	}
}
