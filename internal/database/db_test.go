package database_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
)

func TestDatabaseMigrationAndCRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_test_*")
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

	// Find migrations folder relative to workspace
	migrationsDir := filepath.Join("..", "database", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("migrations")
	}

	err = db.Migrate(migrationsDir)
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Test Group Creation
	group := &models.ClientGroup{
		Name:               "Manufacturing Floor Line A",
		Description:        "Assembly computers for Line A",
		BootTimeoutSeconds: 3,
		WriteCachePolicy:   "volatile",
		AllowSuperClient:   true,
	}
	err = db.CreateGroup(group)
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// Test Virtual Disk Creation
	disk := &models.VirtualDisk{
		Name:        "Windows 11 Enterprise LTSC 2024",
		Description: "Golden base image for Line A workstations",
		FilePath:    filepath.Join(tempDir, "disks", "win11_ltsc.vhdx"),
		SizeBytes:   64424509440, // 60 GB
		DiskFormat:  "VHDX",
		IsGolden:    true,
		IsLocked:    false,
		OSType:      "Windows 11 x64",
	}
	err = db.CreateDisk(disk)
	if err != nil {
		t.Fatalf("failed to create disk: %v", err)
	}

	// Test Client Creation
	client := &models.Client{
		MACAddress:     "00:1A:2B:3C:4D:5E",
		IPAddress:      "192.168.10.101",
		Hostname:       "NKB-PC-01",
		GroupID:        &group.ID,
		AssignedDiskID: &disk.ID,
		Status:         "offline",
		BootMode:       "UEFI",
		WriteCacheMode: "volatile",
		HardwareInfo:   `{"cpu":"Intel Core i5-12400","ram_mb":16384,"nic":"Intel I219-V"}`,
	}
	err = db.CreateClient(client)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test Client Retrieval by MAC
	retrieved, err := db.GetClientByMAC("00-1A-2B-3C-4D-5E")
	if err != nil {
		t.Fatalf("failed to get client by MAC: %v", err)
	}
	if retrieved == nil {
		t.Fatalf("expected client, got nil")
	}
	if retrieved.Hostname != "NKB-PC-01" {
		t.Fatalf("expected hostname 'NKB-PC-01', got '%s'", retrieved.Hostname)
	}

	// Test Audit Logging
	err = db.RecordAudit(nil, "admin", "CLIENT_CREATE", "client", client.ID, "Created client NKB-PC-01", "127.0.0.1")
	if err != nil {
		t.Fatalf("failed to record audit log: %v", err)
	}

	logs, err := db.ListAuditLogs(10)
	if err != nil {
		t.Fatalf("failed to list audit logs: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected at least 1 audit log, got 0")
	}

	// Test Phase 2: Agent Registration Idempotency with DeviceUUID
	regReq := &models.AgentRegisterRequest{
		DeviceUUID:   "test-uuid-11223344",
		MACAddress:   "AA:BB:CC:DD:EE:FF",
		MACInventory: []string{"AA:BB:CC:DD:EE:FF", "11:22:33:44:55:66"},
		Hostname:     "NKB-TEST-WORKSTATION",
		IPAddress:    "192.168.1.150",
		OSVersion:    "Windows 11 Pro",
		OSBuild:      "26100.2161",
		TotalRAMBytes: 17179869184, // 16 GB
	}

	res1, err := db.RegisterAgent(regReq)
	if err != nil {
		t.Fatalf("failed to register agent (first time): %v", err)
	}
	if res1.ClientID == "" || res1.DeviceToken == "" {
		t.Fatalf("invalid register response: %+v", res1)
	}

	// 1. Unauthenticated re-registration without device token must be rejected
	_, err = db.RegisterAgent(regReq)
	if err == nil {
		t.Fatal("expected unauthenticated re-registration to fail, but it succeeded")
	}

	// 2. Authenticated re-registration with existing token must succeed and be idempotent
	regReq.DeviceToken = res1.DeviceToken
	res2, err := db.RegisterAgent(regReq)
	if err != nil {
		t.Fatalf("failed to register agent (with valid token): %v", err)
	}
	if res2.ClientID != res1.ClientID {
		t.Fatalf("expected identical ClientID %s, got %s", res1.ClientID, res2.ClientID)
	}
	if res2.DeviceToken != res1.DeviceToken {
		t.Fatalf("expected identical DeviceToken %s, got %s", res1.DeviceToken, res2.DeviceToken)
	}

	// Verify retrieval by DeviceUUID
	byUUID, err := db.GetClientByDeviceUUID("test-uuid-11223344")
	if err != nil || byUUID == nil {
		t.Fatalf("expected client by UUID, err=%v", err)
	}
	if byUUID.RAMTotalBytes != 17179869184 {
		t.Fatalf("expected RAMTotalBytes 17179869184, got %d", byUUID.RAMTotalBytes)
	}

	// Verify AuthenticateAgent
	authClient, err := db.AuthenticateAgent(res1.DeviceToken)
	if err != nil || authClient == nil {
		t.Fatalf("failed to authenticate agent with device token: %v", err)
	}
	if authClient.ID != res1.ClientID {
		t.Fatalf("authenticated client ID mismatch: expected %s, got %s", res1.ClientID, authClient.ID)
	}

	// Test Heartbeat & Telemetry Ingestion
	hbReq := &models.AgentHeartbeatRequest{
		DeviceToken:   res1.DeviceToken,
		LoggedInUser:  "operator_line1",
		UptimeSec:     3600,
		CPUUsagePct:   12.5,
		RAMUsagePct:   45.0,
		RAMTotalBytes: 17179869184,
		RAMAvailBytes: 9448928051,
		AgentStatus:   "connected",
	}
	err = db.ProcessAgentHeartbeat(res1.ClientID, hbReq)
	if err != nil {
		t.Fatalf("failed to process agent heartbeat: %v", err)
	}

	// Verify telemetry updated in DB
	updatedClient, err := db.GetClientByID(res1.ClientID)
	if err != nil || updatedClient == nil {
		t.Fatalf("failed to get updated client: %v", err)
	}
	if updatedClient.Status != "online" {
		t.Fatalf("expected status 'online', got '%s'", updatedClient.Status)
	}
	if updatedClient.RAMAvailBytes != 9448928051 {
		t.Fatalf("expected RAMAvailBytes 9448928051, got %d", updatedClient.RAMAvailBytes)
	}
	if updatedClient.LoggedInUser != "operator_line1" {
		t.Fatalf("expected LoggedInUser 'operator_line1', got '%s'", updatedClient.LoggedInUser)
	}

	// Test Offline Stale Clients marking
	// Client last_seen was just now, so timeout 1 hour shouldn't mark it offline
	staleCount, err := db.MarkOfflineStaleClients(1 * time.Hour)
	if err != nil {
		t.Fatalf("failed to mark stale clients: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("expected 0 stale clients with 1h timeout, got %d", staleCount)
	}

	// Test Command Queue and Idempotent Result Recording
	cmd := &models.RemoteCommand{
		ClientID:       res1.ClientID,
		IssuerUsername: "admin",
		CommandType:    "query_status",
	}
	if err := db.QueueCommand(cmd); err != nil {
		t.Fatalf("failed to queue command: %v", err)
	}

	now := time.Now()
	resObj := &models.CommandResult{
		CommandID:    cmd.ID,
		ClientID:     res1.ClientID,
		ExitCode:     0,
		Output:       `{"status":"ok"}`,
		ErrorMessage: "",
		ExecutedAt:   now,
		CompletedAt:  &now,
	}

	// First record attempt
	if err := db.RecordCommandResult(resObj); err != nil {
		t.Fatalf("failed to record command result: %v", err)
	}

	// Second record attempt (duplicate delivery from offline buffer retry) must succeed idempotently
	resObj.Output = `{"status":"ok_updated"}`
	if err := db.RecordCommandResult(resObj); err != nil {
		t.Fatalf("idempotent command result recording failed: %v", err)
	}

	// Test Token Revocation
	if err := db.RevokeAgentToken(res1.ClientID); err != nil {
		t.Fatalf("failed to revoke agent token: %v", err)
	}
	_, err = db.AuthenticateAgent(res1.DeviceToken)
	if err == nil {
		t.Fatal("expected AuthenticateAgent to fail after token revocation, but it succeeded")
	}
}

func TestConstantTimeTokenComparisonAndAuthentication(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_token_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(filepath.Join("migrations")); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 1. Empty and whitespace token rejection
	if _, err := db.AuthenticateAgent(""); err == nil {
		t.Fatal("expected empty token to be rejected, but it succeeded")
	}
	if _, err := db.AuthenticateAgent("   \t\n  "); err == nil {
		t.Fatal("expected whitespace token to be rejected, but it succeeded")
	}

	// 2. Register valid agent
	regReq := &models.AgentRegisterRequest{
		DeviceUUID: "token-test-uuid-001",
		MACAddress: "AA:BB:CC:DD:EE:01",
		Hostname:   "TOKEN-STATION-01",
		IPAddress:  "192.168.1.101",
	}
	res, err := db.RegisterAgent(regReq)
	if err != nil {
		t.Fatalf("failed to register agent: %v", err)
	}

	// 3. Valid token authentication succeeds
	authClient, err := db.AuthenticateAgent(res.DeviceToken)
	if err != nil || authClient == nil {
		t.Fatalf("expected valid token authentication to succeed, got: %v", err)
	}
	if authClient.ID != res.ClientID {
		t.Fatalf("expected client ID %s, got %s", res.ClientID, authClient.ID)
	}

	// 4. Invalid token rejection (different length, same length wrong chars)
	if _, err := db.AuthenticateAgent("nkb_agt_short"); err == nil {
		t.Fatal("expected short invalid token to be rejected, but it succeeded")
	}
	wrongToken := "nkb_agt_0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := db.AuthenticateAgent(wrongToken); err == nil {
		t.Fatal("expected non-existent token to be rejected, but it succeeded")
	}

	// 5. Revocation by device token
	if err := db.RevokeDeviceToken(res.DeviceToken); err != nil {
		t.Fatalf("failed to revoke token: %v", err)
	}
	if _, err := db.AuthenticateAgent(res.DeviceToken); err == nil {
		t.Fatal("expected revoked token to be rejected, but it succeeded")
	}
}

func TestTransactionalDeviceRegistrationIntegrity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nkb_tx_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(filepath.Join("migrations")); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 1. Initial valid registration
	regReq := &models.AgentRegisterRequest{
		DeviceUUID: "tx-test-uuid-002",
		MACAddress: "AA:BB:CC:DD:EE:02",
		Hostname:   "TX-STATION-ORIGINAL",
		IPAddress:  "192.168.1.102",
	}
	res, err := db.RegisterAgent(regReq)
	if err != nil {
		t.Fatalf("initial registration failed: %v", err)
	}

	// 2. Attempt unauthorized registration update with wrong token
	spoofReq := &models.AgentRegisterRequest{
		DeviceUUID:  "tx-test-uuid-002",
		MACAddress:  "AA:BB:CC:DD:EE:02",
		Hostname:    "TX-STATION-HIJACKED",
		IPAddress:   "192.168.1.199",
		DeviceToken: "nkb_agt_wrong_attacker_token_12345678",
	}
	_, err = db.RegisterAgent(spoofReq)
	if err == nil {
		t.Fatal("expected spoofed registration update to fail, but it succeeded")
	}

	// 3. Verify atomic rollback: client record was NOT mutated
	client, err := db.GetClientByID(res.ClientID)
	if err != nil || client == nil {
		t.Fatalf("failed to retrieve client after rollback check: %v", err)
	}
	if client.Hostname != "TX-STATION-ORIGINAL" {
		t.Fatalf("transaction rollback failure: hostname mutated to %s", client.Hostname)
	}
	if client.IPAddress != "192.168.1.102" {
		t.Fatalf("transaction rollback failure: IPAddress mutated to %s", client.IPAddress)
	}
}
