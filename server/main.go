package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/alerts"
	"ourway/server/api"
	"ourway/server/auth"
	"ourway/server/cache"
	"ourway/server/config"
	"ourway/server/files"
	"ourway/server/metrics"
	"ourway/server/store"
	"ourway/server/ws"
)

func main() {
	// Load configuration
	cfg := config.Load()
	log.Printf("Starting OurWay server on %s", cfg.ServerPort)

	// Server lifecycle context: background workers started with this
	// context stop when it is cancelled during shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database store
	st, err := store.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	sqlDB, err := st.DB.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	log.Println("Database connected and migrated")

	// Initialize Redis cache (optional)
	var rc *cache.RedisClient
	var redisPubSub *ws.RedisPubSub
	if cfg.RedisEnabled {
		ctx := context.Background()
		var err error
		rc, err = cache.NewRedisClient(ctx, cfg.RedisURL)
		if err != nil {
			log.Printf("Warning: Redis not available, continuing without cache: %v", err)
		} else {
			log.Println("Redis cache connected")
			st.Cache = rc
			st.Devices = store.NewCachedDeviceStore(st.Devices, rc)
			// Setup Redis pub/sub for distributed WebSocket hub
			ps := cache.NewPubSub(rc)
			redisPubSub = &ws.RedisPubSub{
				Publish: func(topic string, message []byte) error {
					return ps.Publish(topic, message)
				},
				Subscribe: func(topic string, handler func([]byte)) error {
					return ps.Subscribe(topic, handler)
				},
			}
		}
	}

	// Initialize JWT auth
	jwtAuth := auth.NewJWTAuth(cfg.JWTSecret)

	// Initialize WebSocket hub (distributed if Redis is available)
	var hub *ws.Hub
	if redisPubSub != nil {
		hub = ws.NewDistributedHub(redisPubSub)
	} else {
		hub = ws.NewHub()
	}
	go hub.Run()
	log.Println("WebSocket hub started")

	// Initialize alert engine
	alertEngine := alerts.NewEngine(st.Alerts)

	// Wire the alert engine into the hub so the WS metrics path evaluates
	// thresholds (the agent's primary reporting path).
	hub.Alerts = alertEngine

	// Initialize metrics retention manager
	retentionManager := metrics.NewRetentionManager(
		st.MetricHistory,
		metrics.DefaultRetentionPolicy(),
		time.Hour, // Check hourly
	)
	retentionManager.Start()
	defer retentionManager.Stop()

	// Setup API routes
	router := api.SetupRouter(st, jwtAuth, hub, alertEngine, cfg.WebURL)

	// Start periodic cleanup of staged upload files. This service uses the
	// same default staging directory (/tmp/ourway-files) as the one created
	// inside SetupRouter, and the goroutine stops when the server context
	// is cancelled at shutdown.
	fileService := files.NewService(st, hub, "")
	fileHandler := api.NewFileHandler(st, fileService)
	go fileHandler.Cleanup(ctx, time.Hour)
	log.Println("File upload cleanup started")

	// Register WebSocket endpoint
	router.GET(cfg.WSPath, func(c *gin.Context) {
		hub.ServeHTTP(c, st, jwtAuth)
	})

	// Start HTTP server
	srv := &http.Server{
		Addr:    cfg.ServerPort,
		Handler: router,
		// No server-level Read/Write timeouts: WebSocket connections are
		// long-lived and the http.Server timeouts would force-close them
		// after 15s. Liveness is enforced by the agent's 15s heartbeat
		// (dead conns stop reading and are closed) and the hub's 45s
		// stale-device reaper.
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Println("OurWay server is running")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	cancel() // stop background workers (file cleanup)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("OurWay server exited")
}
