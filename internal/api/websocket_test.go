package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nkb-diskless/internal/api"
	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/services/auth_rbac"
	"nkb-diskless/internal/services/image_mgmt"
	"nkb-diskless/internal/services/monitoring"
	"nkb-diskless/internal/services/storage_mgmt"
	"nkb-diskless/internal/services/update_manager"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

func setupTestServer(t *testing.T) (*api.Server, *httptest.Server, *database.DB, *auth_rbac.AuthService, func()) {
	tempDir, err := os.MkdirTemp("", "nkb_ws_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfg := config.DefaultConfig(tempDir)
	cfg.DBDriver = "sqlite"

	db, err := database.Connect(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("failed to connect to db: %v", err)
	}

	migrationsDir := filepath.Join("..", "database", "migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("migrations")
	}
	if err := db.Migrate(migrationsDir); err != nil {
		db.Close()
		os.RemoveAll(tempDir)
		t.Fatalf("failed to migrate db: %v", err)
	}

	telemetry := monitoring.NewTelemetryService(db)
	disks, _ := storage_mgmt.NewDiskManager(cfg, db)
	iscsi := storage_mgmt.NewISCSITargetController(cfg, db)
	images := image_mgmt.NewImageService(cfg, db)
	updates, _ := update_manager.NewUpdateManager(cfg, db)
	auth := auth_rbac.NewAuthService("test-secret-key-32-characters-ok!!", db, 24)

	server := api.NewServer(cfg, db, telemetry, disks, iscsi, images, updates, auth)
	ts := httptest.NewServer(server.Handler())

	cleanup := func() {
		ts.Close()
		server.Close()
		db.Close()
		os.RemoveAll(tempDir)
	}

	return server, ts, db, auth, cleanup
}

func getAdminToken(t *testing.T, db *database.DB, auth *auth_rbac.AuthService) string {
	admin, err := db.VerifyUserPassword("admin", "nkbadmin2026")
	if err != nil {
		t.Fatalf("admin auth failed: %v", err)
	}
	token, _, err := auth.GenerateToken(admin)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	return token
}

// Test 1: Unauthenticated connection is rejected (HTTP 401)
func TestWebSocket_UnauthenticatedRejected(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected unauthenticated websocket dial to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got: %v", resp)
	}
}

// Test 2: Invalid/garbage token is rejected (HTTP 401)
func TestWebSocket_InvalidTokenRejected(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	header := http.Header{}
	header.Set("Cookie", "nkb_session=totally.invalid.token-garbage")

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected invalid token websocket dial to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got: %v", resp)
	}
}

// Test 3: Expired token is rejected (HTTP 401)
func TestWebSocket_ExpiredTokenRejected(t *testing.T) {
	_, ts, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	claims := &auth_rbac.Claims{
		UserID:   "admin-id",
		Username: "admin",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, err := tokenObj.SignedString([]byte("test-secret-key-32-characters-ok!!"))
	if err != nil {
		t.Fatalf("failed to create expired token: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	header := http.Header{}
	header.Set("Cookie", "nkb_session="+expiredToken)

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected expired token websocket dial to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got: %v", resp)
	}
}

// Test 4: Disallowed origin is rejected (HTTP 403 Forbidden)
func TestWebSocket_DisallowedOriginRejected(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	header := http.Header{}
	header.Set("Cookie", "nkb_session="+token)
	header.Set("Origin", "http://malicious-cross-origin.com")

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected cross-origin websocket dial to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 Forbidden, got: %v", resp)
	}
}

// Test 5: Query-string token (?token=...) is rejected (HTTP 400 Bad Request)
func TestWebSocket_QueryStringTokenRejected(t *testing.T) {
	_, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws?token=" + token

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected query-string token websocket dial to fail, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for query-string token, got: %v", resp)
	}
}

// Test 6: Valid authenticated session via Cookie: nkb_session connects successfully & receives broadcasts
func TestWebSocket_ValidCookieAuthentication(t *testing.T) {
	server, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	header := http.Header{}
	header.Set("Cookie", "nkb_session="+token)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to dial websocket with valid cookie: %v (resp: %v)", err, resp)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	if server.Hub().ClientCount() != 1 {
		t.Fatalf("expected 1 active ws client, got %d", server.Hub().ClientCount())
	}

	// Verify broadcast reception
	server.Hub().Broadcast("telemetry.updated", map[string]any{
		"client_id": "test-client-123",
		"status":    "online",
		"cpu":       15.2,
	})

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read websocket message: %v", err)
	}

	var evt api.WSEvent
	if err := json.Unmarshal(msg, &evt); err != nil {
		t.Fatalf("failed to parse event json: %v, raw: %s", err, string(msg))
	}

	if evt.Type != "telemetry.updated" {
		t.Fatalf("expected event type 'telemetry.updated', got '%s'", evt.Type)
	}
}

// Test 7: Client disconnect cleanly unregisters and decrements count
func TestWebSocket_CleanDisconnect(t *testing.T) {
	server, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	header := http.Header{}
	header.Set("Cookie", "nkb_session="+token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if server.Hub().ClientCount() != 1 {
		t.Fatalf("expected 1 active client, got %d", server.Hub().ClientCount())
	}

	// Close connection
	_ = conn.Close()

	// Wait for readPump to detect EOF and unregister
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.Hub().ClientCount() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if server.Hub().ClientCount() != 0 {
		t.Fatalf("expected 0 active clients after disconnect, got %d", server.Hub().ClientCount())
	}
}

// Test 8: Multiple concurrent clients connect and receive broadcasts with no panics or deadlocks
func TestWebSocket_ConcurrentBroadcast(t *testing.T) {
	server, ts, db, auth, cleanup := setupTestServer(t)
	defer cleanup()

	token := getAdminToken(t, db, auth)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"

	const clientCount = 10
	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < clientCount; i++ {
		header := http.Header{}
		header.Set("Cookie", "nkb_session="+token)
		c, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("failed to dial client %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	time.Sleep(100 * time.Millisecond)
	if server.Hub().ClientCount() != clientCount {
		t.Fatalf("expected %d active clients, got %d", clientCount, server.Hub().ClientCount())
	}

	// Broadcast an event to all 10 clients
	server.Hub().Broadcast("system.alert", map[string]any{"msg": "concurrent test"})

	var wg sync.WaitGroup
	wg.Add(clientCount)
	for i, c := range conns {
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("client %d failed to read: %v", idx, err)
				return
			}
			var evt api.WSEvent
			if err := json.Unmarshal(msg, &evt); err != nil {
				t.Errorf("client %d bad json: %v", idx, err)
			}
			if evt.Type != "system.alert" {
				t.Errorf("client %d got type %s", idx, evt.Type)
			}
		}(i, c)
	}
	wg.Wait()
}
