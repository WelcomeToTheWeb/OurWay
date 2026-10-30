package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // policy time zones must resolve in minimal containers

	"github.com/gin-gonic/gin"

	"ourway/server/alerts"
	"ourway/server/api"
	"ourway/server/auth"
	"ourway/server/cache"
	"ourway/server/config"
	"ourway/server/files"
	"ourway/server/metrics"
	"ourway/server/recordings"
	"ourway/server/store"
	"ourway/server/webhooks"
	"ourway/server/ws"
)

func main() {
	// Load configuration
	cfg := config.Load()
	log.Printf("Starting OurWay server on %s", cfg.ServerPort)

	// Single source of truth for the installer download directory.
	api.SetInstallersDir(cfg.InstallersDir)
	api.SetMailTransport(webhooks.SMTP{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword, From: cfg.SMTPFrom, StartTLS: cfg.SMTPStartTLS,
	})

	// 2.4-8: the recording policy is the installation's, read from the environment,
	// and the writer's open files belong to this process's life — they close on
	// shutdown, so a restart resumes a recording instead of orphaning it. Off is
	// the default, and the boot line states it: "are you recording me" deserves an
	// answer an operator can read in the log.
	recordingPolicy := recordings.Policy{
		Mode:      cfg.RecordSessions,
		Dir:       cfg.RecordingsDir,
		Retention: time.Duration(cfg.RecordingRetentionDays) * 24 * time.Hour,
	}
	recordingPolicy.Normalize()
	recordingWriter := recordings.NewWriter(recordingPolicy)
	defer recordingWriter.Close()
	api.SetRecording(recordingPolicy, recordingWriter)

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
				SetEX: func(key string, value string, ttl time.Duration) error {
					return rc.Set(key, value, ttl)
				},
				Get: func(key string, out *string) error {
					return rc.Get(key, out)
				},
				Del: func(key string) error {
					return rc.Delete(key)
				},
			}
		}
	}

	// Initialize JWT auth
	jwtAuth := auth.NewJWTAuth(cfg.JWTSecret)

	// Initialize WebSocket hub (distributed if Redis is available)
	var hub *ws.Hub
	if redisPubSub != nil {
		hub = ws.NewDistributedHub(redisPubSub, cfg.WebURL, cfg.WSOrigins)
	} else {
		hub = ws.NewHub(cfg.WebURL, cfg.WSOrigins)
	}
	go hub.Run()
	log.Println("WebSocket hub started")

	// Initialize alert engine
	alertEngine := alerts.NewEngine(st.Alerts)
	// 2.4-5: the engine measures a metric against the most specific rule that
	// applies to that unit and metric (device → tag → client → global), with the
	// built-in numbers as the floor. The scope keys come from the device row.
	alerts.WireDeviceScopes(alertEngine, st)

	// Wire the alert engine into the hub so the WS metrics path evaluates
	// thresholds (the agent's primary reporting path).
	hub.Alerts = alertEngine

	// Initialize metrics retention manager. The cold cutoff is
	// configurable (METRIC_RETENTION_DAYS); hot/warm tiers stay fixed.
	retentionPolicy := metrics.DefaultRetentionPolicy()
	if days := cfg.MetricRetentionDays; days > 0 {
		retentionPolicy.Cold = time.Duration(days) * 24 * time.Hour
	}
	// 2.3-6: the audit ledger is pruned by the same sweeper, on the same
	// schedule, with its own window. A second goroutine nobody supervises is
	// how a retention policy becomes a rumour (M11), so the ledger shares the
	// manager that already owns this job — and the counters say what it did.
	api.SetAuditRetention(cfg.AuditRetentionDays)
	retentionManager := metrics.NewRetentionManager(
		st.MetricHistory,
		retentionPolicy,
		time.Hour, // Check hourly
	).WithAudit(metrics.AuditRetentionPolicy{
		Store:  st.Audit,
		Window: time.Duration(cfg.AuditRetentionDays) * 24 * time.Hour,
	}).WithRecordings(metrics.RecordingRetentionPolicy{
		// 2.4-8: recordings are pruned by the same supervised sweeper, on the same
		// schedule, with their own counters — M11's lesson applied to a second
		// table, and the reason this line is here rather than a goroutine in main.
		Sweep: recordings.NewSweep(st, recordingWriter, time.Duration(cfg.RecordingRetentionDays)*24*time.Hour),
	})
	retentionManager.Start()
	api.SetRetentionSweeper(retentionManager)
	defer retentionManager.Stop()

	// Setup API routes (background jobs scope to ctx, cancelled at shutdown)
	router := api.SetupRouter(ctx, st, jwtAuth, hub, alertEngine, cfg.WebURL, cfg.EnrollSecret, cfg.RateLimitPerMinute, cfg.RateLimitBurst)

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
