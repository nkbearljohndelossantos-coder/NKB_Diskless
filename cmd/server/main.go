package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nkb-diskless/internal/api"
	"nkb-diskless/internal/config"
	"nkb-diskless/internal/database"
	"nkb-diskless/internal/models"
	"nkb-diskless/internal/services/auth_rbac"
	"nkb-diskless/internal/services/image_mgmt"
	"nkb-diskless/internal/services/monitoring"
	"nkb-diskless/internal/services/storage_mgmt"
	"nkb-diskless/internal/services/update_manager"
)

func main() {
	configPath := flag.String("config", "nkb_config.json", "Path to NKB configuration file")
	apiPort := flag.Int("port", 8443, "Management API & Dashboard HTTP port")
	baseDir := flag.String("data", `C:\NKB-PoC`, "Base directory for NKB POC storage (Default: C:\\NKB-PoC)")
	flag.Parse()

	// Configure structured JSON logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("Starting NKB Diskless System Platform", "version", update_manager.CurrentVersion)

	absBase, err := filepath.Abs(*baseDir)
	if err != nil {
		slog.Error("Failed to resolve base directory", "err", err)
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(*configPath, absBase)
	if err != nil {
		slog.Error("Failed to load configuration", "err", err)
		os.Exit(1)
	}
	if *apiPort != 8443 {
		cfg.APIPort = *apiPort
	}

	// Ensure structured storage hierarchy exists safely
	if err := cfg.EnsureStorageLayout(); err != nil {
		slog.Error("Failed to initialize storage layout", "root", cfg.StorageRoot, "err", err)
		os.Exit(1)
	}
	slog.Info("Initialized NKB Storage Layout", "root", cfg.StorageRoot)

	// 1. Connect to Database (PostgreSQL or local SQLite fallback)
	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("Database connection failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// 2. Run Database Migrations
	candidateMigrationDirs := []string{
		filepath.Join(absBase, "internal", "database", "migrations"),
		filepath.Join(filepath.Dir(os.Args[0]), "migrations"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "migrations"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "internal", "database", "migrations"),
		filepath.Join("internal", "database", "migrations"),
		filepath.Join("migrations"),
	}
	var migrationsDir string
	for _, dir := range candidateMigrationDirs {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			migrationsDir = dir
			break
		}
	}
	if err := db.Migrate(migrationsDir); err != nil {
		slog.Error("Failed to run schema migrations", "err", err)
		os.Exit(1)
	}

	// Seed default workstation group if empty
	groups, _ := db.ListGroups()
	if len(groups) == 0 {
		defaultGroup := &models.ClientGroup{
			Name:               "Standard Workstations",
			Description:        "Production factory floor workstations",
			BootTimeoutSeconds: 5,
			WriteCachePolicy:   "volatile",
			AllowSuperClient:   true,
		}
		_ = db.CreateGroup(defaultGroup)
		slog.Info("Initialized default client group: Standard Workstations")
	}

	// 3. Initialize Modular Services
	telemetry := monitoring.NewTelemetryService(db)
	telemetry.Start()
	defer telemetry.Stop()

	diskManager, err := storage_mgmt.NewDiskManager(cfg, db)
	if err != nil {
		slog.Error("Failed to initialize Disk Manager", "err", err)
		os.Exit(1)
	}

	imageService := image_mgmt.NewImageService(cfg, db)
	iscsiController := storage_mgmt.NewISCSITargetController(cfg, db)

	updateManager, err := update_manager.NewUpdateManager(cfg, db)
	if err != nil {
		slog.Error("Failed to initialize Update Manager", "err", err)
		os.Exit(1)
	}

	authService := auth_rbac.NewAuthService(cfg.JWTSecret, db, cfg.TokenExpiration)

	// 4. Start Network Boot Infrastructure
	// Strict Phase 2 Safety Policy: Network boot daemon listeners (TFTP/ProxyDHCP) remain permanently disabled.
	if cfg.EnableTFTP {
		slog.Warn("TFTP daemon listener is DISABLED_BY_SAFETY_POLICY for Phase 2 PoC; deferred to Phase 3 isolated test network")
	}
	if cfg.EnableProxyDHCP {
		slog.Warn("ProxyDHCP daemon listener is DISABLED_BY_SAFETY_POLICY for Phase 2 PoC; deferred to Phase 3 isolated test network")
	}

	// 5. Initialize API Server
	apiServer := api.NewServer(cfg, db, telemetry, diskManager, iscsiController, imageService, updateManager, authService)
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.APIPort),
		Handler:      apiServer.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Launch HTTP Server in background
	go func() {
		slog.Info("NKB Management API & Dashboard live", "address", fmt.Sprintf("http://localhost:%d", cfg.APIPort))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP Server error", "err", err)
		}
	}()

	// Launch dedicated HTTP Boot Server if explicitly enabled and configured on a separate port (e.g. port 8080)
	var bootServer *http.Server
	if cfg.EnableHTTPBoot && cfg.HTTPBootPort > 0 && cfg.HTTPBootPort != cfg.APIPort {
		bootServer = &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.HTTPBootPort),
			Handler:      apiServer.BootHandler(),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		go func() {
			slog.Info("NKB HTTP Boot Service live", "address", fmt.Sprintf("http://%s:%d", cfg.BindAddress, cfg.HTTPBootPort))
			if err := bootServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTP Boot Server error", "err", err)
			}
		}()
	}

	// 6. Graceful Shutdown on OS Signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down NKB Diskless System...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if bootServer != nil {
		_ = bootServer.Shutdown(ctx)
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("Server forced shutdown", "err", err)
	}

	slog.Info("NKB Diskless System cleanly terminated")
}
