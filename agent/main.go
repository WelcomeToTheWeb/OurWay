package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ourway/agent/client"
	"ourway/agent/collector"
	"ourway/agent/config"
	"ourway/agent/install"
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

	// Set up logging
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	log.Printf("OurWay Agent v%s starting...", config.Version)
	log.Printf("Server: %s", cfg.ServerURL)
	log.Printf("Device key: %s", cfg.DeviceKey)

	// Create collector manager
	collectorMgr := collector.NewCollectorManager()

	// Collect initial metrics
	log.Println("Collecting initial metrics...")
	_, err = collectorMgr.CollectAll()
	if err != nil {
		log.Printf("Warning: initial metrics collection failed: %v", err)
	}

	// Create WebSocket client
	c := client.New(cfg.ServerURL, cfg.DeviceKey,
		client.WithHeartbeatInterval(cfg.Heartbeat),
		client.WithMetricsInterval(cfg.MetricsInterval),
		client.WithStreamInterval(cfg.StreamInterval),
		client.WithMetricsFunc(func() (interface{}, error) {
			return collectorMgr.CollectAll()
		}),
	)

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

	// Start the client (blocks until cancelled)
	if err := c.Run(ctx); err != nil {
		log.Printf("Client error: %v", err)
	}

	log.Println("Agent stopped.")
}

var _ = time.Second // ensure time package is used
