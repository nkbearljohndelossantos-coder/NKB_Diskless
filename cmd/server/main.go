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
	"nkb-diskless/internal/services/image_mgmt"
	"nkb-diskless/internal/services/monitoring"
	"nkb-diskless/internal/services/pxe_boot"
	"nkb-diskless/internal/services/storage_mgmt"
	"nkb-diskless/internal/services/update_manager"
)

func main() {
	configPath := flag.String("config", "nkb_config.json", "Path to NKB configuration file")
	apiPort := flag.Int("port", 8443, "Management API & Dashboard HTTP port")
	baseDir := flag.String("data", ".", "Base directory for disks, caches, and database")
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

	// 1. Connect to Database (PostgreSQL or local SQLite fallback)
	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("Database connection failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// 2. Run Database Migrations
	migrationsDir := filepath.Join(absBase, "internal", "database", "migrations")
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

	// 4. Start Network Boot Infrastructure
	// TFTP Service (Optional / Configurable)
	if cfg.EnableTFTP {
		tftpServer := pxe_boot.NewTFTPServer(cfg.TFTPBootDir, cfg.TFTPPort)
		if err := tftpServer.Start(); err != nil {
			slog.Warn("Could not bind TFTP port (may require admin privileges on UDP 69)", "err", err)
		} else {
			defer tftpServer.Stop()
		}
	}

	// ProxyDHCP Service (Optional / Configurable)
	if cfg.EnableProxyDHCP {
		proxyDHCP := pxe_boot.NewProxyDHCPServer(cfg.BindAddress, cfg.ProxyDHCPPort)
		if err := proxyDHCP.Start(); err != nil {
			slog.Warn("Could not bind ProxyDHCP port (UDP 4011)", "err", err)
		} else {
			defer proxyDHCP.Stop()
		}
	}

	// 5. Initialize API Server
	apiServer := api.NewServer(cfg, db, telemetry, diskManager, iscsiController, imageService, updateManager)
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

	// 6. Graceful Shutdown on OS Signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down NKB Diskless System...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Error("Server forced shutdown", "err", err)
	}

	slog.Info("NKB Diskless System cleanly terminated")
}
