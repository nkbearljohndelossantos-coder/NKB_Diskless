package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/auth_rbac"
	"nkb-diskless/internal/services/client_mgmt"
	"nkb-diskless/internal/services/image_mgmt"
	"nkb-diskless/internal/services/monitoring"
	"nkb-diskless/internal/services/network_mgmt"
	"nkb-diskless/internal/services/pxe_boot"
	"nkb-diskless/internal/services/storage_mgmt"
	"nkb-diskless/internal/services/update_manager"
)

// Server holds all dependencies for API handling.
type Server struct {
	cfg       *config.Config
	db        *database.DB
	telemetry *monitoring.TelemetryService
	disks     *storage_mgmt.DiskManager
	iscsi     *storage_mgmt.ISCSITargetController
	images    *image_mgmt.ImageService
	updates   *update_manager.UpdateManager
	auth      *auth_rbac.AuthService
	netMgr    *network_mgmt.NetworkConfigManager
	discovery *network_mgmt.DiscoveryService
	staging   *network_mgmt.StagingService
	wsHub     *WSHub
	mux       *http.ServeMux
	stopChan  chan struct{}
}

// NewServer initializes the API router.
func NewServer(
	cfg *config.Config,
	db *database.DB,
	telemetry *monitoring.TelemetryService,
	disks *storage_mgmt.DiskManager,
	iscsi *storage_mgmt.ISCSITargetController,
	images *image_mgmt.ImageService,
	updates *update_manager.UpdateManager,
	auth *auth_rbac.AuthService,
) *Server {
	hub := NewWSHub()
	go hub.Run()

	netMgr, _ := network_mgmt.NewNetworkConfigManager(cfg.StorageRoot)
	disc := network_mgmt.NewDiscoveryService(nil)
	stag := network_mgmt.NewStagingService(netMgr, disc)

	s := &Server{
		cfg:       cfg,
		db:        db,
		telemetry: telemetry,
		disks:     disks,
		iscsi:     iscsi,
		images:    images,
		updates:   updates,
		auth:      auth,
		netMgr:    netMgr,
		discovery: disc,
		staging:   stag,
		wsHub:     hub,
		mux:       http.NewServeMux(),
		stopChan:  make(chan struct{}),
	}
	s.registerRoutes()
	s.startBackgroundTasks()
	return s
}

func (s *Server) Hub() *WSHub {
	return s.wsHub
}

func (s *Server) Close() {
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}
	if s.wsHub != nil {
		s.wsHub.Stop()
	}
}

func (s *Server) startBackgroundTasks() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopChan:
				return
			case <-ticker.C:
				count, err := s.db.MarkOfflineStaleClients(15 * time.Second)
				if err == nil && count > 0 {
					s.wsHub.Broadcast("workstation.offline", map[string]any{
						"stale_clients_count": count,
						"timestamp":           time.Now(),
					})
				}
			}
		}
	}()
}

// Handler returns the HTTP handler with CORS & logging middleware applied.
func (s *Server) Handler() http.Handler {
	return s.corsMiddleware(s.mux)
}

// BootHandler returns a restricted HTTP handler exposing ONLY the /boot/ipxe endpoint.
// All administrative API routes, management endpoints, and static UI files are strictly omitted
// from this handler to ensure network clients booting over unencrypted HTTP (port 8080) cannot access control endpoints.
func (s *Server) BootHandler() http.Handler {
	bootMux := http.NewServeMux()
	bootMux.HandleFunc("GET /boot/ipxe", s.handleBootIPXE)
	return bootMux
}

func (s *Server) registerRoutes() {
	// 1. Authentication Endpoints
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.auth.RequireAuth(s.handleAuthMe))

	// 2. Workstation Background Agent Protocol
	s.mux.HandleFunc("POST /api/v1/agent/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /api/v1/agent/heartbeat", s.auth.RequireAgentToken(s.handleAgentHeartbeat))
	s.mux.HandleFunc("GET /api/v1/agent/commands", s.auth.RequireAgentToken(s.handleAgentGetCommands))
	s.mux.HandleFunc("POST /api/v1/agent/commands/{id}/result", s.auth.RequireAgentToken(s.handleAgentCommandResult))

	// 3. System Telemetry & Health
	s.mux.HandleFunc("GET /api/v1/ws", s.HandleWebSocket)
	s.mux.HandleFunc("GET /api/v1/system/overview", s.auth.RequireAuth(s.handleSystemOverview))
	s.mux.HandleFunc("GET /api/v1/system/health", s.handleHealthCheck)

	// 4. Workstation / Client PC Management
	s.mux.HandleFunc("GET /api/v1/workstations", s.auth.RequireAuth(s.handleListClients))
	s.mux.HandleFunc("GET /api/v1/clients", s.auth.RequireAuth(s.handleListClients))
	s.mux.HandleFunc("POST /api/v1/clients", s.auth.RequireRole("admin", "operator")(s.handleCreateClient))
	s.mux.HandleFunc("GET /api/v1/clients/{id}", s.auth.RequireAuth(s.handleGetClient))
	s.mux.HandleFunc("PUT /api/v1/clients/{id}", s.auth.RequireRole("admin", "operator")(s.handleUpdateClient))
	s.mux.HandleFunc("DELETE /api/v1/clients/{id}", s.auth.RequireRole("admin")(s.handleDeleteClient))
	s.mux.HandleFunc("POST /api/v1/clients/{id}/super-client", s.auth.RequireRole("admin")(s.handleToggleSuperClient))
	s.mux.HandleFunc("POST /api/v1/clients/{id}/reboot", s.auth.RequireRole("admin", "operator")(s.handleRebootClient))
	s.mux.HandleFunc("POST /api/v1/workstations/{id}/command", s.auth.RequireRole("admin", "operator")(s.handleQueueCommand))
	s.mux.HandleFunc("GET /api/v1/workstations/{id}/commands", s.auth.RequireAuth(s.handleListCommands))
	s.mux.HandleFunc("POST /api/v1/workstations/{id}/wol", s.auth.RequireRole("admin")(s.handleWakeOnLAN))
	s.mux.HandleFunc("GET /api/v1/workstations/{id}/rdp", s.auth.RequireAuth(s.handleDownloadRDP))

	// 5. Virtual Disk & Golden Image Management
	s.mux.HandleFunc("GET /api/v1/disks", s.auth.RequireAuth(s.handleListDisks))
	s.mux.HandleFunc("POST /api/v1/disks", s.auth.RequireRole("admin")(s.handleCreateDisk))
	s.mux.HandleFunc("GET /api/v1/disks/{id}", s.auth.RequireAuth(s.handleGetDisk))
	s.mux.HandleFunc("PUT /api/v1/disks/{id}", s.auth.RequireRole("admin")(s.handleUpdateDisk))
	s.mux.HandleFunc("DELETE /api/v1/disks/{id}", s.auth.RequireRole("admin")(s.handleDeleteDisk))
	s.mux.HandleFunc("POST /api/v1/disks/{id}/duplicate", s.auth.RequireRole("admin")(s.handleDuplicateDisk))
	s.mux.HandleFunc("POST /api/v1/disks/{id}/assign", s.auth.RequireRole("admin")(s.handleAssignDisk))
	s.mux.HandleFunc("POST /api/v1/workstations/{id}/unassign-disk", s.auth.RequireRole("admin")(s.handleUnassignDisk))
	s.mux.HandleFunc("GET /api/v1/disks/{id}/versions", s.auth.RequireAuth(s.handleListDiskVersions))
	s.mux.HandleFunc("POST /api/v1/disks/{id}/rollback", s.auth.RequireRole("admin")(s.handleRollbackDisk))
	s.mux.HandleFunc("POST /api/v1/disks/{id}/validate", s.auth.RequireRole("admin")(s.handleValidateDisk))
	s.mux.HandleFunc("GET /api/v1/installers", s.auth.RequireAuth(s.handleListInstallers))
	s.mux.HandleFunc("POST /api/v1/disks/{id}/apply-os", s.auth.RequireRole("admin")(s.handleApplyOSImage))
	s.mux.HandleFunc("GET /api/v1/disks/{id}/build-status", s.auth.RequireAuth(s.handleGetDiskBuildStatus))
	s.mux.HandleFunc("POST /api/v1/super-client/commit", s.auth.RequireRole("admin")(s.handleCommitSuperClient))

	// 6. Client Groups
	s.mux.HandleFunc("GET /api/v1/groups", s.auth.RequireAuth(s.handleListGroups))
	s.mux.HandleFunc("POST /api/v1/groups", s.auth.RequireRole("admin")(s.handleCreateGroup))

	// 7. Audit Logs
	s.mux.HandleFunc("GET /api/v1/audit-logs", s.auth.RequireRole("admin", "auditor")(s.handleListAuditLogs))

	// 8. Update Management
	s.mux.HandleFunc("GET /api/v1/updates/version", s.auth.RequireAuth(s.handleGetVersion))
	s.mux.HandleFunc("POST /api/v1/updates/apply", s.auth.RequireRole("admin")(s.handleApplyUpdate))

	// 9. Hardware & Network Management (Phase 3E & 3E.1)
	s.mux.HandleFunc("GET /api/v1/network/adapters", s.auth.RequireAuth(s.handleListNetworkAdapters))
	s.mux.HandleFunc("GET /api/v1/network/profile", s.auth.RequireAuth(s.handleGetNetworkProfile))
	s.mux.HandleFunc("PUT /api/v1/network/profile", s.auth.RequireRole("admin")(s.handleUpdateNetworkProfile))
	s.mux.HandleFunc("POST /api/v1/network/profile", s.auth.RequireRole("admin")(s.handleUpdateNetworkProfile))
	s.mux.HandleFunc("POST /api/v1/network/adopt", s.auth.RequireRole("admin")(s.handleAdoptAdapter))
	s.mux.HandleFunc("POST /api/v1/network/validate", s.auth.RequireRole("admin")(s.handleValidateNetwork))
	s.mux.HandleFunc("POST /api/v1/network/preflight", s.auth.RequireRole("admin")(s.handlePreflightNetwork))
	s.mux.HandleFunc("POST /api/v1/network/verify", s.auth.RequireRole("admin")(s.handleVerifyNetwork))
	s.mux.HandleFunc("POST /api/v1/network/rollback", s.auth.RequireRole("admin")(s.handleRollbackNetwork))
	s.mux.HandleFunc("POST /api/v1/network/rollback/profile", s.auth.RequireRole("admin")(s.handleRollbackProfile))
	s.mux.HandleFunc("POST /api/v1/network/rollback/windows", s.auth.RequireRole("admin")(s.handleRollbackWindows))
	s.mux.HandleFunc("GET /api/v1/network/snapshots", s.auth.RequireRole("admin")(s.handleListSnapshots))
	s.mux.HandleFunc("GET /api/v1/network/scripts/stage", s.auth.RequireRole("admin")(s.handleGetStagingScript))
	s.mux.HandleFunc("GET /api/v1/network/scripts/rollback", s.auth.RequireRole("admin")(s.handleGetRollbackScript))

	// 10. PXE Dynamic Boot Endpoint
	s.mux.HandleFunc("GET /boot/ipxe", s.handleBootIPXE)

	// 11. Static UI Serving
	frontendCandidates := []string{
		filepath.Join(s.cfg.StorageRoot, "frontend", "dist"),
		filepath.Join(filepath.Dir(os.Args[0]), "web", "dist"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "web", "dist"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "frontend", "dist"),
		filepath.Join("web", "dist"),
		filepath.Join("frontend", "dist"),
	}
	var frontendDist string
	for _, c := range frontendCandidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			frontendDist = c
			break
		}
	}
	if frontendDist != "" {
		fileServer := http.FileServer(http.Dir(frontendDist))
		s.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(frontendDist, r.URL.Path)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				http.ServeFile(w, r, filepath.Join(frontendDist, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		}))
	} else {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"system":  "NKB Diskless System Platform",
					"version": update_manager.CurrentVersion,
					"status":  "online",
					"api_doc": "/api/v1/system/overview",
				})
				return
			}
			http.NotFound(w, r)
		})
	}
}

// ----------------- AUTHENTICATION HANDLERS -----------------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid login payload")
		return
	}

	user, err := s.db.VerifyUserPassword(req.Username, req.Password)
	if err != nil {
		s.errorResponse(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, expiresAt, err := s.auth.GenerateToken(user)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "failed to generate authentication token")
		return
	}

	_ = s.db.RecordAudit(&user.ID, user.Username, "AUTH_LOGIN", "user", user.ID, "Administrator logged in", r.RemoteAddr)

	// Set secure HTTP-only session cookie for browser WebSocket & API requests
	http.SetCookie(w, &http.Cookie{
		Name:     "nkb_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})

	s.jsonResponse(w, http.StatusOK, models.LoginResponse{
		Token:     token,
		User:      *user,
		ExpiresAt: expiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "nkb_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(auth_rbac.UserContextKey).(*models.User)
	s.jsonResponse(w, http.StatusOK, user)
}

// ----------------- AGENT PROTOCOL HANDLERS -----------------

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req models.AgentRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid registration payload")
		return
	}

	if req.MACAddress == "" || req.Hostname == "" {
		s.errorResponse(w, http.StatusBadRequest, "mac_address and hostname are required")
		return
	}

	if req.IPAddress == "" {
		ip := r.RemoteAddr
		if colon := strings.LastIndex(ip, ":"); colon != -1 {
			ip = ip[:colon]
		}
		req.IPAddress = ip
	}

	if req.DeviceToken == "" {
		req.DeviceToken = r.Header.Get("X-Device-Token")
	}

	res, err := s.db.RegisterAgent(&req)
	if err != nil {
		if strings.Contains(err.Error(), "unauthorized") {
			s.errorResponse(w, http.StatusUnauthorized, err.Error())
		} else {
			s.errorResponse(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	_ = s.db.RecordAudit(nil, "agent", "AGENT_REGISTER", "client", res.ClientID, fmt.Sprintf("Workstation agent registered: %s (%s)", req.Hostname, req.MACAddress), r.RemoteAddr)
	s.wsHub.Broadcast("workstation.registered", map[string]any{
		"client_id":    res.ClientID,
		"hostname":     req.Hostname,
		"mac_address":  req.MACAddress,
		"ip_address":   req.IPAddress,
		"status":       "online",
		"device_uuid":  req.DeviceUUID,
	})
	s.jsonResponse(w, http.StatusOK, res)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	client := r.Context().Value(auth_rbac.ClientContextKey).(*models.Client)
	var req models.AgentHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid heartbeat payload")
		return
	}

	if err := s.db.ProcessAgentHeartbeat(client.ID, &req); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.wsHub.Broadcast("telemetry.updated", map[string]any{
		"client_id":       client.ID,
		"status":          "online",
		"logged_in_user":  req.LoggedInUser,
		"uptime_seconds":  req.UptimeSec,
		"cpu_usage_pct":   req.CPUUsagePct,
		"ram_usage_pct":   req.RAMUsagePct,
		"ram_total_bytes": req.RAMTotalBytes,
		"ram_avail_bytes": req.RAMAvailBytes,
		"disk_usage_pct":  req.DiskUsagePct,
		"network_rx_mbs":  req.NetworkRxMBs,
		"network_tx_mbs":  req.NetworkTxMBs,
		"agent_status":    req.AgentStatus,
		"last_seen":       time.Now().Format(time.RFC3339),
	})
	s.wsHub.Broadcast("workstation.updated", map[string]any{
		"client_id": client.ID,
		"status":    "online",
		"last_seen": time.Now().Format(time.RFC3339),
	})

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (s *Server) handleAgentGetCommands(w http.ResponseWriter, r *http.Request) {
	client := r.Context().Value(auth_rbac.ClientContextKey).(*models.Client)
	cmds, err := s.db.GetPendingCommands(client.ID)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cmds == nil {
		cmds = []models.RemoteCommand{}
	}
	s.jsonResponse(w, http.StatusOK, cmds)
}

func (s *Server) handleAgentCommandResult(w http.ResponseWriter, r *http.Request) {
	cmdID := r.PathValue("id")
	client := r.Context().Value(auth_rbac.ClientContextKey).(*models.Client)

	var res models.CommandResult
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid result payload")
		return
	}
	res.CommandID = cmdID
	res.ClientID = client.ID

	if err := s.db.RecordCommandResult(&res); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "agent", "COMMAND_RESULT", "command", cmdID, fmt.Sprintf("Command finished with exit code %d", res.ExitCode), r.RemoteAddr)
	s.wsHub.Broadcast("command.updated", map[string]any{
		"command_id": cmdID,
		"client_id":  client.ID,
		"status":     "completed",
		"result":     res,
		"timestamp":  time.Now(),
	})
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// ----------------- WORKSTATION REMOTE COMMANDS -----------------

func (s *Server) handleQueueCommand(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	var req models.QueueCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid command request")
		return
	}

	disabledCommands := map[string]bool{
		"shutdown":         true,
		"force_shutdown":   true,
		"restart":          true,
		"reboot":           true,
		"logoff":           true,
		"force_logoff":     true,
		"lock":             true,
		"lock_workstation": true,
		"wol":              true,
		"wake_on_lan":      true,
		"wake-on-lan":      true,
	}
	cmdLower := strings.ToLower(strings.TrimSpace(req.CommandType))
	if disabledCommands[cmdLower] {
		_ = s.db.RecordAudit(nil, "admin", "COMMAND_BLOCKED", "client", clientID, fmt.Sprintf("Command %s rejected: DISABLED_BY_SAFETY_POLICY", req.CommandType), r.RemoteAddr)
		s.errorResponse(w, http.StatusForbidden, fmt.Sprintf("command '%s' is DISABLED_BY_SAFETY_POLICY", req.CommandType))
		return
	}

	allowedCommands := map[string]bool{
		"query_processes": true,
		"query_status":    true,
		"query_health":    true,
		"query_network":   true,
	}
	if !allowedCommands[cmdLower] {
		s.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("unsupported or unauthorized command type: %s", req.CommandType))
		return
	}

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		s.errorResponse(w, http.StatusNotFound, "workstation not found")
		return
	}

	var issuerID *string
	issuerUsername := "admin"
	if u, ok := r.Context().Value(auth_rbac.UserContextKey).(*models.User); ok && u != nil {
		issuerID = &u.ID
		issuerUsername = u.Username
	}

	cmd := &models.RemoteCommand{
		ClientID:       clientID,
		IssuerUserID:   issuerID,
		IssuerUsername: issuerUsername,
		CommandType:    strings.ToLower(req.CommandType),
		Payload:        req.Payload,
	}

	if err := s.db.QueueCommand(cmd); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(issuerID, issuerUsername, "COMMAND_QUEUE", "client", clientID, fmt.Sprintf("Queued %s for %s", cmd.CommandType, client.Hostname), r.RemoteAddr)
	s.wsHub.Broadcast("command.updated", map[string]any{
		"command_id":   cmd.ID,
		"client_id":    clientID,
		"command_type": cmd.CommandType,
		"status":       "pending",
		"command":      cmd,
		"timestamp":    time.Now(),
	})
	s.jsonResponse(w, http.StatusAccepted, cmd)
}

func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	cmds, err := s.db.ListCommands(clientID, 20)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cmds == nil {
		cmds = []models.RemoteCommand{}
	}
	s.jsonResponse(w, http.StatusOK, cmds)
}

func (s *Server) handleWakeOnLAN(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	_ = s.db.RecordAudit(nil, "admin", "WOL_BLOCKED", "client", clientID, "Wake-on-LAN request blocked: DISABLED_BY_SAFETY_POLICY for Phase 2 PoC", r.RemoteAddr)
	s.errorResponse(w, http.StatusForbidden, "Wake-on-LAN is DISABLED_BY_SAFETY_POLICY")
}

func (s *Server) handleDownloadRDP(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		s.errorResponse(w, http.StatusNotFound, "workstation not found")
		return
	}

	rdpContent := client_mgmt.GenerateRDPFile(client.Hostname, client.IPAddress)
	w.Header().Set("Content-Type", "application/x-rdp")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.rdp", client.Hostname))
	_, _ = w.Write([]byte(rdpContent))
}

// ----------------- CLIENT / SYSTEM ROUTE HANDLERS -----------------

var validMACRegex = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$|^([0-9A-Fa-f]{12})$`)

func (s *Server) handleBootIPXE(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	rawMAC := strings.TrimSpace(r.URL.Query().Get("mac"))
	if rawMAC == "" {
		// Default to trial client MAC if omitted
		rawMAC = "28:00:AF:A8:44:CC"
	}

	if rawMAC == "" || !validMACRegex.MatchString(rawMAC) {
		s.errorResponse(w, http.StatusBadRequest, "invalid or missing 'mac' query parameter: must be a valid 6-byte hexadecimal MAC address")
		return
	}

	// Normalize MAC address format to AA:BB:CC:DD:EE:FF
	normalizedMAC := strings.ToUpper(strings.ReplaceAll(rawMAC, "-", ":"))
	if len(normalizedMAC) == 12 && !strings.Contains(normalizedMAC, ":") {
		normalizedMAC = fmt.Sprintf("%s:%s:%s:%s:%s:%s",
			normalizedMAC[0:2], normalizedMAC[2:4], normalizedMAC[4:6],
			normalizedMAC[6:8], normalizedMAC[8:10], normalizedMAC[10:12])
	}

	httpPort := s.cfg.HTTPBootPort
	if httpPort <= 0 {
		httpPort = s.cfg.APIPort
	}

	phase3a := s.cfg.Phase3AMode || r.URL.Query().Get("phase") == "3a"

	req := pxe_boot.BootRequest{
		MAC:         normalizedMAC,
		IP:          ip,
		Platform:    r.URL.Query().Get("platform"),
		Arch:        r.URL.Query().Get("arch"),
		ServerIP:    s.cfg.BindAddress,
		HTTPPort:    httpPort,
		ISCSIPort:   s.cfg.ISCSIPort,
		ISCSIPrefix: s.cfg.ISCSIIQNPfx,
		Phase3AMode: phase3a,
	}
	if req.ServerIP == "0.0.0.0" {
		req.ServerIP = r.Host
		if strings.Contains(req.ServerIP, ":") {
			parts := strings.Split(req.ServerIP, ":")
			req.ServerIP = parts[0]
		}
	}

	slog.Info("iPXE dynamic boot script requested",
		"mac", normalizedMAC,
		"client_ip", ip,
		"platform", req.Platform,
		"arch", req.Arch,
		"phase3a", phase3a,
	)

	script, err := pxe_boot.GenerateScript(req, s.db)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (s *Server) handleSystemOverview(w http.ResponseWriter, r *http.Request) {
	overview := s.telemetry.GetOverview()
	s.jsonResponse(w, http.StatusOK, overview)
}

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"version": update_manager.CurrentVersion,
	})
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.db.ListClients()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if clients == nil {
		clients = []models.Client{}
	}

	// Filter by search query if provided
	search := strings.ToLower(r.URL.Query().Get("search"))
	groupFilter := r.URL.Query().Get("group_id")
	statusFilter := r.URL.Query().Get("status")

	if search != "" || groupFilter != "" || statusFilter != "" {
		filtered := make([]models.Client, 0)
		for _, c := range clients {
			if search != "" && !strings.Contains(strings.ToLower(c.Hostname), search) && !strings.Contains(strings.ToLower(c.MACAddress), search) {
				continue
			}
			if groupFilter != "" && (c.GroupID == nil || *c.GroupID != groupFilter) {
				continue
			}
			if statusFilter != "" && c.Status != statusFilter {
				continue
			}
			filtered = append(filtered, c)
		}
		s.jsonResponse(w, http.StatusOK, filtered)
		return
	}

	s.jsonResponse(w, http.StatusOK, clients)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	client, err := s.db.GetClientByID(id)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if client == nil {
		s.errorResponse(w, http.StatusNotFound, "client not found")
		return
	}
	s.jsonResponse(w, http.StatusOK, client)
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var c models.Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	if c.MACAddress == "" || c.Hostname == "" {
		s.errorResponse(w, http.StatusBadRequest, "mac_address and hostname are required")
		return
	}

	if err := s.db.CreateClient(&c); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "CLIENT_CREATE", "client", c.ID, fmt.Sprintf("Registered workstation %s (%s)", c.Hostname, c.MACAddress), r.RemoteAddr)
	s.jsonResponse(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var c models.Client
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	c.ID = id

	if err := s.db.UpdateClient(&c); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "CLIENT_UPDATE", "client", id, fmt.Sprintf("Updated workstation %s", c.Hostname), r.RemoteAddr)
	s.jsonResponse(w, http.StatusOK, c)
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteClient(id); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "CLIENT_DELETE", "client", id, "Workstation deregistered", r.RemoteAddr)
	s.jsonResponse(w, http.StatusOK, map[string]string{"result": "deleted"})
}

func (s *Server) handleToggleSuperClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var payload struct {
		Enable        bool `json:"enable"`
		ExpireMinutes int  `json:"expire_minutes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	var err error
	if payload.Enable {
		err = s.images.StartSuperClient(id, payload.ExpireMinutes, "admin")
	} else {
		err = s.images.DiscardSuperClient(id, "admin")
	}

	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]any{"success": true, "super_client": payload.Enable})
}

func (s *Server) handleCommitSuperClient(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ClientID     string `json:"client_id"`
		VersionTag   string `json:"version_tag"`
		ReleaseNotes string `json:"release_notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid payload")
		return
	}

	version, err := s.images.CommitSuperClient(payload.ClientID, payload.VersionTag, payload.ReleaseNotes, "admin")
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, version)
}

func (s *Server) handleAssignDisk(w http.ResponseWriter, r *http.Request) {
	diskID := r.PathValue("id")
	var payload struct {
		ClientID       string `json:"client_id"`
		DeploymentType string `json:"deployment_type"`
		MountPoint     string `json:"mount_point"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid payload")
		return
	}

	assignment, err := s.images.AssignDisk(payload.ClientID, diskID, payload.DeploymentType, payload.MountPoint, "admin")
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, assignment)
}

func (s *Server) handleUnassignDisk(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	if err := s.images.UnassignDisk(clientID, "admin"); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (s *Server) handleListDiskVersions(w http.ResponseWriter, r *http.Request) {
	diskID := r.PathValue("id")
	versions, err := s.db.ListDiskVersions(diskID)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if versions == nil {
		versions = []models.DiskVersion{}
	}
	s.jsonResponse(w, http.StatusOK, versions)
}

func (s *Server) handleRollbackDisk(w http.ResponseWriter, r *http.Request) {
	diskID := r.PathValue("id")
	var payload struct {
		VersionID string `json:"version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid payload")
		return
	}

	ver, err := s.images.RollbackToVersion(diskID, payload.VersionID, "admin")
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, ver)
}

func (s *Server) handleValidateDisk(w http.ResponseWriter, r *http.Request) {
	diskID := r.PathValue("id")
	ok, checksum, err := s.images.ValidateImage(diskID)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]any{
		"valid":    ok,
		"checksum": checksum,
	})
}

func (s *Server) handleRebootClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = s.db.RecordAudit(nil, "admin", "REBOOT_BLOCKED", "client", id, "Remote reboot blocked: DISABLED_BY_SAFETY_POLICY for Phase 2 PoC", r.RemoteAddr)
	s.errorResponse(w, http.StatusForbidden, "reboot is DISABLED_BY_SAFETY_POLICY")
}

func (s *Server) handleListDisks(w http.ResponseWriter, r *http.Request) {
	disks, err := s.db.ListDisks()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if disks == nil {
		disks = []models.VirtualDisk{}
	}
	s.jsonResponse(w, http.StatusOK, disks)
}

func (s *Server) handleGetDisk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	disk, err := s.db.GetDiskByID(id)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if disk == nil {
		s.errorResponse(w, http.StatusNotFound, "disk not found")
		return
	}
	s.jsonResponse(w, http.StatusOK, disk)
}

func (s *Server) handleCreateDisk(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name        string `json:"name"`
		SizeBytes   int64  `json:"size_bytes"`
		Format      string `json:"format"`
		OSType      string `json:"os_type"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	disk, err := s.disks.CreateVirtualDisk(payload.Name, payload.SizeBytes, payload.Format, payload.OSType, payload.Description)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "DISK_CREATE", "disk", disk.ID, fmt.Sprintf("Created virtual disk %s (%s)", disk.Name, disk.DiskFormat), r.RemoteAddr)
	s.jsonResponse(w, http.StatusCreated, disk)
}

func (s *Server) handleUpdateDisk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var d models.VirtualDisk
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid payload")
		return
	}
	d.ID = id

	if err := s.db.UpdateDisk(&d); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "DISK_UPDATE", "disk", id, fmt.Sprintf("Updated disk %s", d.Name), r.RemoteAddr)
	s.jsonResponse(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDisk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteDisk(id); err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "DISK_DELETE", "disk", id, "Virtual disk deleted", r.RemoteAddr)
	s.jsonResponse(w, http.StatusOK, map[string]string{"result": "deleted"})
}

func (s *Server) handleDuplicateDisk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var payload struct {
		NewName string `json:"new_name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.NewName == "" {
		s.errorResponse(w, http.StatusBadRequest, "new_name is required")
		return
	}

	newDisk, err := s.images.DuplicateDisk(id, payload.NewName)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "DISK_CLONE", "disk", newDisk.ID, fmt.Sprintf("Cloned disk to %s", newDisk.Name), r.RemoteAddr)
	s.jsonResponse(w, http.StatusCreated, newDisk)
}

type WimEdition struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func parseWimEditions(wimPath string) []WimEdition {
	f, err := os.Open(wimPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil
	}

	offset := int64(0)
	readSize := int64(4 * 1024 * 1024)
	if fi.Size() > readSize {
		offset = fi.Size() - readSize
	} else {
		readSize = fi.Size()
	}

	buf := make([]byte, readSize)
	_, err = f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil
	}

	var sb strings.Builder
	for i := 0; i < len(buf)-1; i += 2 {
		if buf[i+1] == 0 && buf[i] >= 32 && buf[i] <= 126 {
			sb.WriteByte(buf[i])
		} else if buf[i] == '\n' || buf[i] == '\r' || buf[i] == '\t' {
			sb.WriteByte(buf[i])
		}
	}
	ascii := sb.String()

	reImage := regexp.MustCompile(`(?s)<IMAGE INDEX="(\d+)">(.*?)</IMAGE>`)
	reName := regexp.MustCompile(`<NAME>(.*?)</NAME>`)
	reDesc := regexp.MustCompile(`<DESCRIPTION>(.*?)</DESCRIPTION>`)

	matches := reImage.FindAllStringSubmatch(ascii, -1)
	var editions []WimEdition
	for _, m := range matches {
		idx, _ := strconv.Atoi(m[1])
		name := fmt.Sprintf("Edition %d", idx)
		desc := ""
		if nm := reName.FindStringSubmatch(m[2]); len(nm) > 1 {
			name = nm[1]
		}
		if dm := reDesc.FindStringSubmatch(m[2]); len(dm) > 1 {
			desc = dm[1]
		}
		editions = append(editions, WimEdition{
			Index:       idx,
			Name:        name,
			Description: desc,
		})
	}
	return editions
}

func inspectInstallerEditions(installerPath string) []WimEdition {
	ext := strings.ToLower(filepath.Ext(installerPath))
	if ext == ".wim" || ext == ".esd" {
		if ed := parseWimEditions(installerPath); len(ed) > 0 {
			return ed
		}
	}

	if ext == ".iso" {
		// 1. Check if ISO is already mounted on a Windows drive letter
		for _, drive := range []string{"D", "E", "F", "G", "H", "I"} {
			testWim := fmt.Sprintf("%s:\\sources\\install.wim", drive)
			if _, err := os.Stat(testWim); err == nil {
				if ed := parseWimEditions(testWim); len(ed) > 0 {
					return ed
				}
			}
			testEsd := fmt.Sprintf("%s:\\sources\\install.esd", drive)
			if _, err := os.Stat(testEsd); err == nil {
				if ed := parseWimEditions(testEsd); len(ed) > 0 {
					return ed
				}
			}
		}

		// 2. Try mounting via PowerShell
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Mount-DiskImage -ImagePath '%s' -PassThru | Out-Null; (Get-DiskImage -ImagePath '%s' | Get-Volume).DriveLetter", installerPath, installerPath)).Output()
		if err == nil {
			dl := strings.TrimSpace(string(out))
			if dl != "" {
				testWim := fmt.Sprintf("%s:\\sources\\install.wim", dl)
				if ed := parseWimEditions(testWim); len(ed) > 0 {
					return ed
				}
			}
		}
	}

	return []WimEdition{
		{Index: 1, Name: "Windows 11 Pro / Master Image (Auto-Detected)", Description: "Default Installation Image"},
	}
}

func (s *Server) handleListInstallers(w http.ResponseWriter, r *http.Request) {
	candidateDirs := []string{
		`C:\NKB-PoC\installers`,
		filepath.Join(s.cfg.StorageRoot, "installers"),
		"/var/nkb-diskless/installers",
		"installers",
	}

	type InstallerItem struct {
		Name      string       `json:"name"`
		Path      string       `json:"path"`
		SizeBytes int64        `json:"size_bytes"`
		Type      string       `json:"type"`
		Editions  []WimEdition `json:"editions"`
	}

	seenPaths := make(map[string]bool)
	var items []InstallerItem

	for _, baseDir := range candidateDirs {
		if _, err := os.Stat(baseDir); err != nil {
			continue
		}
		_ = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".iso" || ext == ".wim" || ext == ".esd" {
				normPath := filepath.Clean(path)
				if !seenPaths[normPath] {
					seenPaths[normPath] = true
					editions := inspectInstallerEditions(normPath)
					items = append(items, InstallerItem{
						Name:      info.Name(),
						Path:      normPath,
						SizeBytes: info.Size(),
						Type:      strings.TrimPrefix(ext, "."),
						Editions:  editions,
					})
				}
			}
			return nil
		})
	}

	if items == nil {
		items = []InstallerItem{}
	}
	s.jsonResponse(w, http.StatusOK, items)
}

func (s *Server) handleApplyOSImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	disk, err := s.db.GetDiskByID(id)
	if err != nil || disk == nil {
		s.errorResponse(w, http.StatusNotFound, "virtual disk not found")
		return
	}

	var payload struct {
		InstallerPath string `json:"installer_path"`
		WimIndex      int    `json:"wim_index"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.InstallerPath == "" {
		payload.InstallerPath = `C:\NKB-PoC\installers\iso\Windows 11 Pro.ISO`
	}
	if payload.WimIndex <= 0 {
		payload.WimIndex = 1
	}

	scriptPath := filepath.Join(s.cfg.StorageRoot, "bin", "deploy-os-vhd.ps1")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		scriptPath = `C:\NKB-PoC\bin\deploy-os-vhd.ps1`
	}

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath,
		"-VhdPath", disk.FilePath,
		"-IsoPath", payload.InstallerPath,
		"-WimIndex", fmt.Sprintf("%d", payload.WimIndex),
	)

	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error("OS Image deployment failed", "disk", disk.Name, "err", err, "out", string(out))
			return
		}
		slog.Info("OS Image deployment completed successfully", "disk", disk.Name)
		iscsiExe := filepath.Join(s.cfg.StorageRoot, "bin", "nkb-iscsi.exe")
		if _, err := os.Stat(iscsiExe); os.IsNotExist(err) {
			iscsiExe = `C:\NKB-PoC\bin\nkb-iscsi.exe`
		}
		_ = exec.Command(iscsiExe, "-vhd", disk.FilePath, "-port", "3260", "-size", "100").Start()
	}()

	_ = s.db.RecordAudit(nil, "admin", "DISK_APPLY_OS", "disk", disk.ID, fmt.Sprintf("Started OS deployment from %s to %s", payload.InstallerPath, disk.Name), r.RemoteAddr)
	s.jsonResponse(w, http.StatusAccepted, map[string]string{
		"status":  "deploying",
		"message": fmt.Sprintf("OS image deployment from %s started. This will take 2-4 minutes in the background.", filepath.Base(payload.InstallerPath)),
	})
}

func (s *Server) handleGetDiskBuildStatus(w http.ResponseWriter, r *http.Request) {
	statusFile := filepath.Join(s.cfg.StorageRoot, "build_status.json")
	if _, err := os.Stat(statusFile); os.IsNotExist(err) {
		statusFile = `C:\NKB-PoC\build_status.json`
	}
	data, err := os.ReadFile(statusFile)
	if err != nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"status":  "idle",
			"percent": 0,
			"message": "No active build in progress",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.db.ListGroups()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []models.ClientGroup{}
	}
	s.jsonResponse(w, http.StatusOK, groups)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var g models.ClientGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if err := s.db.CreateGroup(&g); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "GROUP_CREATE", "group", g.ID, fmt.Sprintf("Created client group %s", g.Name), r.RemoteAddr)
	s.jsonResponse(w, http.StatusCreated, g)
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.db.ListAuditLogs(50)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []models.AuditLog{}
	}
	s.jsonResponse(w, http.StatusOK, logs)
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{
		"current_version": update_manager.CurrentVersion,
		"platform":        "NKB Diskless System",
	})
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.Version == "" {
		payload.Version = "1.1.0"
	}

	err := s.updates.ApplyUpdate("manual-update-01", payload.Version)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]string{
		"result":  "success",
		"version": payload.Version,
	})
}

// ----------------- HARDWARE & NETWORK MANAGEMENT (PHASE 3E) -----------------

func (s *Server) handleListNetworkAdapters(w http.ResponseWriter, r *http.Request) {
	adapters, err := s.discovery.DiscoverAdapters()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile, _ := s.netMgr.LoadProfile()
	var reconciledStatus string
	if profile != nil {
		reconciledStatus, _ = s.netMgr.ReconcileAdapter(profile, adapters)
	}
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"adapters":        adapters,
		"adopted_adapter": profile.SelectedAdapter,
		"status":          reconciledStatus,
		"network_mode":    profile.NetworkMode,
	})
}

func (s *Server) handleGetNetworkProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	adapters, _ := s.discovery.DiscoverAdapters()
	if len(adapters) > 0 {
		s.netMgr.ReconcileAdapter(profile, adapters)
	}
	s.jsonResponse(w, http.StatusOK, profile)
}

func (s *Server) handleUpdateNetworkProfile(w http.ResponseWriter, r *http.Request) {
	var proposed network_mgmt.NetworkProfile
	if err := json.NewDecoder(r.Body).Decode(&proposed); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid network profile payload: "+err.Error())
		return
	}
	adapters, _ := s.discovery.DiscoverAdapters()
	report, err := s.netMgr.ValidateProfile(&proposed, adapters)
	if err != nil {
		s.jsonResponse(w, http.StatusBadRequest, map[string]any{
			"error":     err.Error(),
			"conflicts": report,
		})
		return
	}
	if err := s.netMgr.SaveProfile(&proposed); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, proposed)
}

func (s *Server) handleAdoptAdapter(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		PersistentKey  string `json:"persistent_key"`
		MACAddress     string `json:"mac_address"`
		InterfaceAlias string `json:"interface_alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid adoption payload")
		return
	}

	adapters, err := s.discovery.DiscoverAdapters()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	var target *network_mgmt.NetworkAdapter
	for _, a := range adapters {
		if (payload.PersistentKey != "" && a.ID == payload.PersistentKey) ||
			(payload.MACAddress != "" && strings.EqualFold(a.MACAddress, payload.MACAddress)) ||
			(payload.InterfaceAlias != "" && a.InterfaceAlias == payload.InterfaceAlias) {
			target = &a
			break
		}
	}

	if target == nil {
		s.errorResponse(w, http.StatusNotFound, "specified adapter not found on system")
		return
	}

	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.netMgr.AdoptAdapter(profile, *target); err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	report, _ := s.netMgr.ValidateProfile(profile, adapters)
	preview := s.netMgr.GenerateStagingPreview(profile, target, *report)
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"profile": profile,
		"preview": preview,
		"status":  "ADOPTED",
	})
}

func (s *Server) handleValidateNetwork(w http.ResponseWriter, r *http.Request) {
	var proposed network_mgmt.NetworkProfile
	if err := json.NewDecoder(r.Body).Decode(&proposed); err != nil {
		s.errorResponse(w, http.StatusBadRequest, "invalid profile payload")
		return
	}

	adapters, err := s.discovery.DiscoverAdapters()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	report, _ := s.netMgr.ValidateProfile(&proposed, adapters)
	var target *network_mgmt.NetworkAdapter
	for _, a := range adapters {
		if (proposed.SelectedAdapter.PersistentKey != "" && a.ID == proposed.SelectedAdapter.PersistentKey) ||
			(proposed.SelectedAdapter.MAC != "" && strings.EqualFold(a.MACAddress, proposed.SelectedAdapter.MAC)) ||
			(proposed.SelectedAdapter.Alias != "" && a.InterfaceAlias == proposed.SelectedAdapter.Alias) {
			target = &a
			break
		}
	}
	preview := s.netMgr.GenerateStagingPreview(&proposed, target, *report)
	s.jsonResponse(w, http.StatusOK, preview)
}

func (s *Server) handleVerifyNetwork(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	res, err := s.staging.VerifyLiveConfiguration(profile)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, res)
}

func (s *Server) handleRollbackNetwork(w http.ResponseWriter, r *http.Request) {
	restored, err := s.netMgr.Rollback()
	if err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"status":  "ROLLBACK_SUCCESS",
		"profile": restored,
	})
}

func (s *Server) handleGetStagingScript(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	script := s.netMgr.GenerateElevatedStagingScript(profile)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func (s *Server) handlePreflightNetwork(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	adapters, err := s.discovery.DiscoverAdapters()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "Failed to discover adapters: "+err.Error())
		return
	}
	res, err := s.netMgr.PreflightStaging(profile, adapters)
	if err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, res)
}

func (s *Server) handleRollbackProfile(w http.ResponseWriter, r *http.Request) {
	restored, err := s.netMgr.RollbackProfile()
	if err != nil {
		s.errorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"status":  "PROFILE_ROLLBACK_SUCCESS",
		"profile": restored,
	})
}

func (s *Server) handleRollbackWindows(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	snap, err := s.netMgr.GetLatestWindowsSnapshot(profile.SelectedAdapter.PersistentKey)
	if err != nil {
		s.errorResponse(w, http.StatusNotFound, "No saved Windows network snapshot found: "+err.Error())
		return
	}
	script := s.netMgr.GenerateRollbackScript(snap)
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"status":      "ROLLBACK_READY",
		"snapshot_id": snap.SnapshotID,
		"adapter":     snap.InterfaceAlias,
		"script":      script,
		"command":     "powershell -ExecutionPolicy Bypass -File C:\\NKB-PoC\\config\\scripts\\rollback_adapter.ps1",
	})
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.netMgr.ListWindowsSnapshots()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.jsonResponse(w, http.StatusOK, map[string]any{
		"snapshots": snapshots,
		"total":     len(snapshots),
	})
}

func (s *Server) handleGetRollbackScript(w http.ResponseWriter, r *http.Request) {
	profile, err := s.netMgr.LoadProfile()
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	snap, err := s.netMgr.GetLatestWindowsSnapshot(profile.SelectedAdapter.PersistentKey)
	if err != nil {
		// If no snapshot exists yet, construct a safe template from current profile
		adapters, _ := s.discovery.DiscoverAdapters()
		for _, a := range adapters {
			if a.ID == profile.SelectedAdapter.PersistentKey || strings.EqualFold(a.MACAddress, profile.SelectedAdapter.MAC) {
				snap = s.netMgr.BuildSnapshotFromAdapter(&a)
				break
			}
		}
	}
	if snap == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("# No adapter rollback snapshot recorded yet.\n# Rollback script will be dynamically generated once an adapter is staged.\n"))
		return
	}
	script := s.netMgr.GenerateRollbackScript(snap)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

// ----------------- HELPERS -----------------

func (s *Server) jsonResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) errorResponse(w http.ResponseWriter, statusCode int, message string) {
	s.jsonResponse(w, statusCode, map[string]string{"error": message})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Device-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
