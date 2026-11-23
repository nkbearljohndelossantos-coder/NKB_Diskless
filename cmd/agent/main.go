package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nkb-diskless/internal/agent"

	"golang.org/x/sys/windows/svc"
)

const serviceName = "NKBAgent"

type nkbWindowsService struct {
	agent *agent.Agent
}

func (s *nkbWindowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = s.agent.Run(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for req := range r {
		switch req.Cmd {
		case svc.Interrogate:
			changes <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			cancel()
			return
		default:
			slog.Warn("Unexpected service control request", "cmd", req.Cmd)
		}
	}
	return
}

func main() {
	serverURL := flag.String("server", "http://localhost:8443", "NKB Central Server URL")
	configPath := flag.String("config", "", "Path to agent config file")
	debug := flag.Bool("debug", false, "Run in console debug mode")
	flag.Parse()

	// Setup logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load agent config", "err", err)
		os.Exit(1)
	}

	if *serverURL != "http://localhost:8443" || cfg.ServerURL == "" {
		cfg.ServerURL = *serverURL
	}
	_ = cfg.Save()

	ag := agent.NewAgent(cfg)

	// Check if running as a Windows Service
	isWindowsService, err := svc.IsWindowsService()
	if err == nil && isWindowsService && !*debug {
		slog.Info("Running as native Windows Service: " + serviceName)
		if err := svc.Run(serviceName, &nkbWindowsService{agent: ag}); err != nil {
			slog.Error("Windows service execution failed", "err", err)
		}
		return
	}

	// Interactive Console Mode
	fmt.Println("==================================================")
	fmt.Println("   NKB WORKSTATION AGENT (CONSOLE / DEBUG MODE)   ")
	fmt.Println("==================================================")
	slog.Info("Target Server URL", "url", cfg.ServerURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		slog.Info("Termination signal received. Exiting agent...")
		cancel()
	}()

	if err := ag.Run(ctx); err != nil {
		slog.Error("Agent stopped with error", "err", err)
	}

	time.Sleep(500 * time.Millisecond)
	slog.Info("Agent exited cleanly.")
}
