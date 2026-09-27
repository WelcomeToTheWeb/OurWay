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
	"ourway/server/metrics"
	"ourway/server/store"
	"ourway/server/ws"
)

func main() {
	// Load configuration
	cfg := config.Load()
	log.Printf("Starting OurWay server on %s", cfg.ServerPort)

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

	// Register WebSocket endpoint
	router.GET(cfg.WSPath, func(c *gin.Context) {
		hub.ServeHTTP(c, st, jwtAuth)
	})

	// Start HTTP server
	srv := &http.Server{
		Addr:         cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("OurWay server exited")
}
