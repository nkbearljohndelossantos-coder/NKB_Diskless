package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"nkb-diskless/internal/models"
)

// Test 1: Unauthenticated administrative requests return HTTP 401
func TestRESTAPI_UnauthenticatedRequestsRejected(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	protectedEndpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/system/overview"},
		{"GET", "/api/v1/workstations"},
		{"GET", "/api/v1/clients"},
		{"POST", "/api/v1/clients"},
		{"GET", "/api/v1/disks"},
		{"POST", "/api/v1/disks"},
		{"GET", "/api/v1/groups"},
		{"POST", "/api/v1/groups"},
		{"GET", "/api/v1/audit-logs"},
		{"GET", "/api/v1/updates/version"},
	}

	for _, ep := range protectedEndpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req, err := http.NewRequest(ep.method, ts.URL+ep.path, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected HTTP 401 Unauthorized for unauthenticated request to %s %s, got: %d",
					ep.method, ep.path, resp.StatusCode)
			}
		})
	}
}

// Test 2: Public endpoints remain accessible without credentials
func TestRESTAPI_PublicEndpointsAccessible(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Health check endpoint
	resp, err := http.Get(ts.URL + "/api/v1/system/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for health check, got: %d", resp.StatusCode)
	}

	// 2. Login endpoint (accessible publicly)
	loginPayload := `{"username":"admin","password":"wrongpassword"}`
	loginResp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBufferString(loginPayload))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer loginResp.Body.Close()
	// Wrong password returns 401, NOT 404 or routing failure
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for bad password, got: %d", loginResp.StatusCode)
	}
}

// Test 3: Valid administrative access succeeds with Bearer token & session cookie
func TestRESTAPI_AuthenticatedAdminAccessSucceeds(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)

	// 1. Access with Authorization: Bearer <token>
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/system/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorized request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for overview with Bearer token, got: %d", resp.StatusCode)
	}

	// 2. Access with Cookie: nkb_session=<token>
	reqCookie, _ := http.NewRequest("GET", ts.URL+"/api/v1/workstations", nil)
	reqCookie.Header.Set("Cookie", "nkb_session="+token)
	respCookie, err := http.DefaultClient.Do(reqCookie)
	if err != nil {
		t.Fatalf("cookie authorized request failed: %v", err)
	}
	defer respCookie.Body.Close()
	if respCookie.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for workstations with session cookie, got: %d", respCookie.StatusCode)
	}
}

// Test 4: Insufficient RBAC returns HTTP 403 Forbidden
func TestRESTAPI_InsufficientRBACRejected(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	// Create an operator user (cannot create disks or commit images, admin only)
	now := time.Now()
	operatorUser := &models.User{
		ID:           "test-operator-id",
		Username:     "operator_bob",
		PasswordHash: "fakehash",
		Role:         "operator",
		FullName:     "Bob Operator",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_ = db.CreateUser(operatorUser)

	token, _, err := auth.GenerateToken(operatorUser)
	if err != nil {
		t.Fatalf("failed to generate operator token: %v", err)
	}

	// Operator CAN view workstations (role requirement: any authenticated user)
	reqView, _ := http.NewRequest("GET", ts.URL+"/api/v1/workstations", nil)
	reqView.Header.Set("Authorization", "Bearer "+token)
	respView, err := http.DefaultClient.Do(reqView)
	if err != nil {
		t.Fatalf("operator view request failed: %v", err)
	}
	defer respView.Body.Close()
	if respView.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for operator viewing workstations, got: %d", respView.StatusCode)
	}

	// Operator CANNOT create virtual disks (role requirement: admin)
	diskPayload := `{"name":"Illegal Disk","size_gb":20}`
	reqCreateDisk, _ := http.NewRequest("POST", ts.URL+"/api/v1/disks", bytes.NewBufferString(diskPayload))
	reqCreateDisk.Header.Set("Authorization", "Bearer "+token)
	reqCreateDisk.Header.Set("Content-Type", "application/json")
	respCreateDisk, err := http.DefaultClient.Do(reqCreateDisk)
	if err != nil {
		t.Fatalf("operator disk creation request failed: %v", err)
	}
	defer respCreateDisk.Body.Close()

	if respCreateDisk.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden for operator creating disk, got: %d", respCreateDisk.StatusCode)
	}
}

// Test 5: CSRF protection blocks cross-origin cookie-authenticated state-changing requests
func TestRESTAPI_CSRFProtectionBlocksCrossOriginStateChange(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)

	// State-changing POST authenticated via cookie with cross-origin Origin header
	clientPayload := `{"mac_address":"00:11:22:33:44:99","hostname":"CSRF-CLIENT"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/clients", bytes.NewBufferString(clientPayload))
	req.Header.Set("Cookie", "nkb_session="+token)
	req.Header.Set("Origin", "http://malicious-attacker-site.com")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden for cross-origin cookie POST, got: %d", resp.StatusCode)
	}
}

// Test 6: Agent endpoints enforce X-Device-Token authentication
func TestRESTAPI_AgentEndpointsEnforceDeviceToken(t *testing.T) {
	_, ts, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Unauthenticated heartbeat rejected with HTTP 401
	hbReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/agent/heartbeat", bytes.NewBufferString(`{}`))
	hbReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(hbReq)
	if err != nil {
		t.Fatalf("heartbeat request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for unauthenticated agent heartbeat, got: %d", resp.StatusCode)
	}

	// 2. Register legitimate agent
	regBody, _ := json.Marshal(&models.AgentRegisterRequest{
		DeviceUUID: "agent-api-test-01",
		MACAddress: "00:AA:11:22:33:44",
		Hostname:   "AGENT-TEST-PC",
		IPAddress:  "192.168.1.50",
	})
	regResp, err := http.Post(ts.URL+"/api/v1/agent/register", "application/json", bytes.NewBuffer(regBody))
	if err != nil {
		t.Fatalf("agent register request failed: %v", err)
	}
	defer regResp.Body.Close()
	if regResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for agent register, got: %d", regResp.StatusCode)
	}

	var regResult models.AgentRegisterResponse
	_ = json.NewDecoder(regResp.Body).Decode(&regResult)
	if regResult.DeviceToken == "" {
		t.Fatal("expected non-empty device token from registration")
	}

	// 3. Heartbeat with valid X-Device-Token succeeds
	authHbReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/agent/heartbeat", bytes.NewBufferString(`{"uptime_seconds":120}`))
	authHbReq.Header.Set("Content-Type", "application/json")
	authHbReq.Header.Set("X-Device-Token", regResult.DeviceToken)

	authResp, err := http.DefaultClient.Do(authHbReq)
	if err != nil {
		t.Fatalf("authenticated heartbeat failed: %v", err)
	}
	defer authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for authenticated heartbeat, got: %d", authResp.StatusCode)
	}

	// 4. Heartbeat with revoked token is rejected
	_ = db.RevokeDeviceToken(regResult.DeviceToken)
	revokedHbReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/agent/heartbeat", bytes.NewBufferString(`{"uptime_seconds":120}`))
	revokedHbReq.Header.Set("Content-Type", "application/json")
	revokedHbReq.Header.Set("X-Device-Token", regResult.DeviceToken)

	revokedResp, err := http.DefaultClient.Do(revokedHbReq)
	if err != nil {
		t.Fatalf("revoked heartbeat request failed: %v", err)
	}
	defer revokedResp.Body.Close()
	if revokedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for revoked device token heartbeat, got: %d", revokedResp.StatusCode)
	}
}
