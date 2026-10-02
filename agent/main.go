package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ourway/agent/client"
	"ourway/agent/collector"
	"ourway/agent/config"
	"ourway/agent/install"
	"ourway/agent/session"
)

func main() {
	// Per-user session helper: the agent service (Session 0) re-launches
	// this binary with --user-helper to capture the interactive desktop
	// and synthesize input. It must be handled before flag.Parse(),
	// which would reject the unknown flag.
	if addr := userHelperAddr(os.Args[1:]); addr != "" {
		os.Exit(session.RunUserHelper(addr))
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Handle --version
	if cfg.ShowVersion {
		fmt.Printf("ourway-agent %s\n", config.Version)
		return
	}

	// Handle --install
	if cfg.Install {
		if err := cfg.Validate(); err != nil {
			log.Fatalf("invalid config: %v", err)
		}
		fmt.Println("Installing OurWay Agent as a service...")
		if err := install.Install(cfg); err != nil {
			log.Fatalf("install failed: %v", err)
		}
		fmt.Println("Installation complete.")
		return
	}

	// Handle --uninstall
	if cfg.Uninstall {
		fmt.Println("Uninstalling OurWay Agent service...")
		if err := install.Uninstall(); err != nil {
			log.Fatalf("uninstall failed: %v", err)
		}
		fmt.Println("Uninstallation complete.")
		return
	}

	// Validate config for normal run
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	// When running under the Windows SCM, hand off to the service runtime,
	// which performs the SCM handshake and drives the agent loop.
	if isWindowsService() {
		if err := runServiceMain(cfg); err != nil {
			log.Fatalf("service main failed: %v", err)
		}
		return
	}

	// Set up logging
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s, shutting down...", sig)
		cancel()
	}()

	runAgent(ctx, cfg)
}

// userHelperAddr scans args for the --user-helper flag and returns its
// address argument, or "" when the flag is not present.
func userHelperAddr(args []string) string {
	for i, a := range args {
		if a == "--user-helper" || a == "-user-helper" {
			if i+1 < len(args) {
				return args[i+1]
			}
			log.Fatalf("--user-helper requires a 127.0.0.1:port address")
		}
	}
	return ""
}

// runAgent runs the agent's main loop: resolve the device ID, start metric
// collection, connect to the server, and block until the context is
// cancelled. It is shared by the console entry point and the Windows
// service handler.
func runAgent(ctx context.Context, cfg *config.Config) {
	log.Printf("OurWay Agent v%s starting...", config.Version)
	log.Printf("Server: %s", cfg.ServerURL)
	log.Printf("Device key: %s", cfg.DeviceKey)

	// Resolve this agent's device ID so the patch handler can verify that
	// deploy/scan/rollback payloads are addressed to this device. Use the
	// configured value if present, otherwise fetch it from the server.
	deviceID := cfg.DeviceID
	if deviceID == "" {
		var err error
		deviceID, err = client.ResolveDeviceID(config.HTTPBaseURL(cfg.ServerURL), cfg.DeviceKey)
		if err != nil {
			// The WebSocket connection is key-authenticated, so a failed
			// lookup is not fatal — the patch handler proceeds without a
			// verified ID (and logs a warning per payload).
			log.Printf("Warning: could not resolve device ID: %v", err)
		}
	}
	if deviceID != "" {
		log.Printf("Device ID: %s", deviceID)
	}

	// Create collector manager
	collectorMgr := collector.NewCollectorManager()

	// Collect initial metrics
	log.Println("Collecting initial metrics...")
	_, err := collectorMgr.CollectAll()
	if err != nil {
		log.Printf("Warning: initial metrics collection failed: %v", err)
	}

	// Create WebSocket client
	c := client.New(cfg.ServerURL, cfg.DeviceKey,
		client.WithHeartbeatInterval(cfg.Heartbeat),
		client.WithMetricsInterval(cfg.MetricsInterval),
		client.WithStreamInterval(cfg.StreamInterval),
		client.WithDeviceID(deviceID),
		client.WithMetricsFunc(func() (interface{}, error) {
			return collectorMgr.CollectAll()
		}),
	)

	// Start the client (blocks until cancelled)
	if err := c.Run(ctx); err != nil {
		log.Printf("Client error: %v", err)
	}

	log.Println("Agent stopped.")
}
