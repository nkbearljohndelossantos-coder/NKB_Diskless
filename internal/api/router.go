package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/auth_rbac"
	"nkb-diskless/internal/services/image_mgmt"
	"nkb-diskless/internal/services/monitoring"
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
	mux       *http.ServeMux
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
	s := &Server{
		cfg:       cfg,
		db:        db,
		telemetry: telemetry,
		disks:     disks,
		iscsi:     iscsi,
		images:    images,
		updates:   updates,
		auth:      auth,
		mux:       http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// Handler returns the HTTP handler with CORS & logging middleware applied.
func (s *Server) Handler() http.Handler {
	return s.corsMiddleware(s.mux)
}

func (s *Server) registerRoutes() {
	// 1. Authentication Endpoints
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.auth.RequireAuth(s.handleAuthMe))

	// 2. Workstation Background Agent Protocol
	s.mux.HandleFunc("POST /api/v1/agent/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /api/v1/agent/heartbeat", s.auth.RequireAgentToken(s.handleAgentHeartbeat))
	s.mux.HandleFunc("GET /api/v1/agent/commands", s.auth.RequireAgentToken(s.handleAgentGetCommands))
	s.mux.HandleFunc("POST /api/v1/agent/commands/{id}/result", s.auth.RequireAgentToken(s.handleAgentCommandResult))

	// 3. System Telemetry & Health
	s.mux.HandleFunc("GET /api/v1/system/overview", s.handleSystemOverview)
	s.mux.HandleFunc("GET /api/v1/system/health", s.handleHealthCheck)

	// 4. Workstation / Client PC Management
	s.mux.HandleFunc("GET /api/v1/workstations", s.handleListClients)
	s.mux.HandleFunc("GET /api/v1/clients", s.handleListClients)
	s.mux.HandleFunc("POST /api/v1/clients", s.handleCreateClient)
	s.mux.HandleFunc("GET /api/v1/clients/{id}", s.handleGetClient)
	s.mux.HandleFunc("PUT /api/v1/clients/{id}", s.handleUpdateClient)
	s.mux.HandleFunc("DELETE /api/v1/clients/{id}", s.handleDeleteClient)
	s.mux.HandleFunc("POST /api/v1/clients/{id}/super-client", s.handleToggleSuperClient)
	s.mux.HandleFunc("POST /api/v1/clients/{id}/reboot", s.handleRebootClient)
	s.mux.HandleFunc("POST /api/v1/workstations/{id}/command", s.handleQueueCommand)
	s.mux.HandleFunc("GET /api/v1/workstations/{id}/commands", s.handleListCommands)

	// 5. Virtual Disk Management
	s.mux.HandleFunc("GET /api/v1/disks", s.handleListDisks)
	s.mux.HandleFunc("POST /api/v1/disks", s.handleCreateDisk)
	s.mux.HandleFunc("GET /api/v1/disks/{id}", s.handleGetDisk)
	s.mux.HandleFunc("PUT /api/v1/disks/{id}", s.handleUpdateDisk)
	s.mux.HandleFunc("DELETE /api/v1/disks/{id}", s.handleDeleteDisk)
	s.mux.HandleFunc("POST /api/v1/disks/{id}/duplicate", s.handleDuplicateDisk)

	// 6. Client Groups
	s.mux.HandleFunc("GET /api/v1/groups", s.handleListGroups)
	s.mux.HandleFunc("POST /api/v1/groups", s.handleCreateGroup)

	// 7. Audit Logs
	s.mux.HandleFunc("GET /api/v1/audit-logs", s.handleListAuditLogs)

	// 8. Update Management
	s.mux.HandleFunc("GET /api/v1/updates/version", s.handleGetVersion)
	s.mux.HandleFunc("POST /api/v1/updates/apply", s.handleApplyUpdate)

	// 9. PXE Dynamic Boot Endpoint
	s.mux.HandleFunc("GET /boot/ipxe", s.handleBootIPXE)

	// 10. Static UI Serving
	frontendDist := filepath.Join(s.cfg.StorageRoot, "frontend", "dist")
	if fi, err := os.Stat(frontendDist); err == nil && fi.IsDir() {
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

	s.jsonResponse(w, http.StatusOK, models.LoginResponse{
		Token:     token,
		User:      *user,
		ExpiresAt: expiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
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

	res, err := s.db.RegisterAgent(&req)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "agent", "AGENT_REGISTER", "client", res.ClientID, fmt.Sprintf("Workstation agent registered: %s (%s)", req.Hostname, req.MACAddress), r.RemoteAddr)
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

	validCommands := map[string]bool{
		"shutdown":        true,
		"restart":         true,
		"lock":            true,
		"wol":             true,
		"query_processes": true,
	}
	if !validCommands[strings.ToLower(req.CommandType)] {
		s.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("unsupported command type: %s", req.CommandType))
		return
	}

	client, err := s.db.GetClientByID(clientID)
	if err != nil || client == nil {
		s.errorResponse(w, http.StatusNotFound, "workstation not found")
		return
	}

	cmd := &models.RemoteCommand{
		ClientID:       clientID,
		IssuerUsername: "admin",
		CommandType:    strings.ToLower(req.CommandType),
		Payload:        req.Payload,
	}

	if err := s.db.QueueCommand(cmd); err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.db.RecordAudit(nil, "admin", "COMMAND_QUEUE", "client", clientID, fmt.Sprintf("Queued %s for %s", cmd.CommandType, client.Hostname), r.RemoteAddr)
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

// ----------------- CLIENT / SYSTEM ROUTE HANDLERS -----------------

func (s *Server) handleBootIPXE(w http.ResponseWriter, r *http.Request) {
	mac := r.URL.Query().Get("mac")
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	req := pxe_boot.BootRequest{
		MAC:         mac,
		IP:          ip,
		Platform:    r.URL.Query().Get("platform"),
		Arch:        r.URL.Query().Get("arch"),
		ServerIP:    s.cfg.BindAddress,
		HTTPPort:    s.cfg.APIPort,
		ISCSIPort:   s.cfg.ISCSIPort,
		ISCSIPrefix: s.cfg.ISCSIIQNPfx,
	}
	if req.ServerIP == "0.0.0.0" {
		req.ServerIP = r.Host
		if strings.Contains(req.ServerIP, ":") {
			parts := strings.Split(req.ServerIP, ":")
			req.ServerIP = parts[0]
		}
	}

	script, err := pxe_boot.GenerateScript(req, s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		err = s.images.StartSuperClient(id, payload.ExpireMinutes)
	} else {
		err = s.images.DiscardSuperClient(id)
	}

	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]any{"success": true, "super_client": payload.Enable})
}

func (s *Server) handleRebootClient(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = s.disks.PurgeClientWriteCache(id)
	_ = s.db.RecordAudit(nil, "admin", "CLIENT_REBOOT", "client", id, "Triggered remote reboot and volatile cache purge", r.RemoteAddr)
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "reboot_signaled", "cache": "purged"})
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
