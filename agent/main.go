package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ourway/agent/client"
	"ourway/agent/collector"
	"ourway/agent/config"
	"ourway/agent/install"
	"ourway/agent/selfupdate"
)

func main() {
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
		runUninstall(cfg)
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

	// Fully automatic self-update: check now and hourly; the client
	// also triggers an immediate check when the server pushes its
	// version on the heartbeat path.
	updater := selfupdate.NewChecker(config.HTTPBaseURL(cfg.ServerURL))
	defer updater.Stop()
	go updater.Run()

	// Create WebSocket client
	c := client.New(cfg.ServerURL, cfg.DeviceKey,
		client.WithHeartbeatInterval(cfg.Heartbeat),
		client.WithMetricsInterval(cfg.MetricsInterval),
		client.WithStreamInterval(cfg.StreamInterval),
		client.WithDeviceID(deviceID),
		client.OnServerVersion(func() { updater.CheckNow() }),
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

// runUninstall removes the agent after a confirmation (skipped with
// --yes), optionally waiting for Enter so a double-clicked console
// window stays readable.
func runUninstall(cfg *config.Config) {
	wait := func() {
		if cfg.Pause {
			fmt.Print("\nPress Enter to exit...")
			fmt.Scanln()
		}
	}
	if !cfg.Yes {
		fmt.Print("Remove the OurWay Agent from this computer? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if ans = strings.ToLower(strings.TrimSpace(ans)); ans != "y" && ans != "yes" {
			fmt.Println("Cancelled.")
			wait()
			return
		}
	}
	fmt.Println("Uninstalling OurWay Agent...")
	switch err := install.Uninstall(); {
	case errors.Is(err, install.ErrElevating):
		fmt.Println("Administrator approval requested; the uninstall continues in a new window.")
	case err != nil:
		fmt.Printf("Uninstall failed: %v\n", err)
		wait()
		os.Exit(1)
	default:
		fmt.Println("Uninstallation complete.")
		wait()
	}
}
