package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/events"
	"ourway/server/files"
	"ourway/server/patching"
	"ourway/server/sessions"
	"ourway/server/store"
	"ourway/server/webhooks"
	"ourway/server/ws"
)

// SetupRouter configures and returns the Gin router with all routes.
func SetupRouter(store *store.Store, jwtAuth *auth.JWTAuth, hub *ws.Hub, engine *alerts.Engine, webURL string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Rate limiter: 100 requests per minute per user
	// Use Redis-backed limiter if Redis is available, otherwise in-memory
	var rateLimitMiddleware gin.HandlerFunc
	if store.Cache != nil {
		rateLimitMiddleware = NewRedisRateLimiter(store.Cache, time.Minute, 100).Middleware()
	} else {
		rateLimitMiddleware = NewRateLimiter(time.Minute, 100).Middleware()
	}

	authHandler := NewAuthHandler(store, jwtAuth)
	deviceHandler := NewDeviceHandler(store, hub, engine, store.MetricHistory)
	alertHandler := NewAlertHandler(store)
	roleHandler := NewRoleHandler(store)
	userHandler := NewUserHandler(store)
	gateway := sessions.NewGateway()
	sessionHandler := NewSessionHandler(store, gateway, hub)
	scanner := patching.NewScanner(store, hub)
	deployer := patching.NewDeployer(store, hub)
	rollbacker := patching.NewRollbackManager(store, hub)
	patchHandler := NewPatchHandler(store, scanner, deployer, rollbacker)
	rebooter := patching.NewRebooter(hub)
	rebootHandler := NewRebootHandler(store, rebooter)
	fileService := files.NewService(store, hub, "")
	fileHandler := NewFileHandler(store, fileService)
	agentFileHandler := NewAgentFileHandler(store, fileService)
	agentPatchHandler := NewAgentPatchHandler(store, deployer)
	ssoHandler := NewSSOHandler(store, jwtAuth, webURL)
	apiKeyHandler := CreateAPIKeyHandler(store, jwtAuth)
	webhookDispatcher := webhooks.NewDispatcher(store)
	webhookHandler := NewWebhookHandler(store, webhookDispatcher)
	events.SetPublisher(webhookDispatcher)

	// Start webhook delivery retry loop
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			webhookDispatcher.RetryPending(context.Background())
		}
	}()

	// Periodic fleet-wide update scan (every 24h); the scanner was
	// created above but ScanAll was never started, so scheduled scans
	// never ran.
	go scanner.ScanAll(context.Background(), 24*time.Hour)

	// Auth routes (no auth required)
	authGroup := r.Group("/api/auth")
	{
		authGroup.GET("/status", authHandler.Status)
		authGroup.POST("/login", authHandler.Login)
		authGroup.POST("/register", authHandler.Register)
		authGroup.POST("/refresh", authHandler.Refresh)

		// SSO routes
		authGroup.GET("/sso/providers", ssoHandler.ListProviders)
		authGroup.GET("/sso/:provider/authorize", ssoHandler.Authorize)
		authGroup.GET("/sso/:provider/callback", ssoHandler.Callback)
	}

	// Agent routes (device key auth via header)
	agentGroup := r.Group("/api/agent")
	{
		agentGroup.POST("/register", deviceHandler.RegisterDevice)
		agentGroup.POST("/heartbeat", deviceHandler.Heartbeat)
		agentGroup.POST("/metrics", deviceHandler.ReportMetrics)
		agentGroup.POST("/updates", agentPatchHandler.ReportUpdate)
		agentGroup.POST("/deployments/result", agentPatchHandler.ReportDeploymentResult)
		agentGroup.POST("/files/status", agentFileHandler.ReportStatus)
		agentGroup.GET("/files/:transfer_id/download", agentFileHandler.DownloadForAgent)
		agentGroup.POST("/files/:transfer_id/upload", agentFileHandler.UploadFromAgent)

		// Agent binary download (no auth)
		agentGroup.GET("/binary", func(c *gin.Context) {
			osName := c.Query("os")
			arch := c.Query("arch")
			if osName == "" || arch == "" {
				c.JSON(400, gin.H{"error": "missing os and/or arch parameter"})
				return
			}
			filename := fmt.Sprintf("ourway-agent-%s-%s", osName, arch)
			if osName == "windows" {
				filename += ".exe"
			}
			path := filepath.Join("dist", "agents", filename)
			if _, err := os.Stat(path); err != nil {
				// Try without dist prefix (running from repo root)
				path = filename
				if _, err := os.Stat(path); err != nil {
					c.JSON(404, gin.H{"error": "binary not found", "filename": filename})
					return
				}
			}
			c.File(path)
		})
	}

	// Protected routes (JWT or API key auth required, rate limited)
	protected := r.Group("/api", APIKeyMiddleware(store, jwtAuth), rateLimitMiddleware)
	{
		// User profile
		protected.GET("/auth/profile", authHandler.GetProfile)
		protected.PUT("/auth/profile", authHandler.UpdateProfile)
		protected.PUT("/auth/password", authHandler.UpdatePassword)
		protected.DELETE("/auth/me", authHandler.DeleteAccount)

		// Monitoring data (Danger Zone: clear all metrics + alerts)
		protected.DELETE("/monitoring/data", RequireRole("admin"), deviceHandler.ClearMonitoringData)

		// Device routes
		protected.GET("/devices", deviceHandler.ListDevices)
		protected.GET("/devices/:id", deviceHandler.GetDevice)
		protected.DELETE("/devices/:id", RequireRole("admin"), deviceHandler.DeleteDevice)
		protected.GET("/devices/:id/metrics", deviceHandler.GetLatestMetrics)
		protected.GET("/devices/:id/metrics/history", deviceHandler.GetMetricsHistory)

		// Alert routes
		protected.GET("/alerts", alertHandler.ListAlerts)
		protected.POST("/alerts/:id/resolve", RequireAnyRole("admin", "manager", "technician"), alertHandler.ResolveAlert)
		protected.POST("/alerts/:id/acknowledge", RequireAnyRole("admin", "manager", "technician"), alertHandler.AcknowledgeAlert)
		protected.POST("/alerts/:id/assign", RequireAnyRole("admin", "manager", "technician"), alertHandler.AssignAlert)

		// Role routes
		roleGroup := protected.Group("/roles")
		{
			roleGroup.GET("", roleHandler.ListRoles)
			roleGroup.GET("/:id", roleHandler.GetRole)
			roleGroup.POST("", RequireRole("admin"), roleHandler.CreateRole)
			roleGroup.PUT("/:id", RequireRole("admin"), roleHandler.UpdateRole)
			roleGroup.DELETE("/:id", RequireRole("admin"), roleHandler.DeleteRole)
		}

		// User routes
		userGroup := protected.Group("/users")
		{
			userGroup.GET("", RequireAnyRole("admin", "manager"), userHandler.ListUsers)
			userGroup.POST("", RequireRole("admin"), userHandler.CreateUser)
			userGroup.PUT("/:id/roles", RequireAnyRole("admin", "manager"), userHandler.UpdateUserRoles)
			userGroup.DELETE("/:id", RequireRole("admin"), userHandler.DeleteUser)
		}

		// Session routes
		protected.POST("/devices/:id/sessions", sessionHandler.StartSession)
		protected.GET("/sessions", sessionHandler.ListSessions)
		protected.POST("/sessions/:id/answer", sessionHandler.SubmitAnswer)
		protected.POST("/sessions/:id/ice", sessionHandler.AddICECandidate)
		protected.POST("/sessions/:id/input", sessionHandler.SendInput)
		protected.POST("/sessions/:id/quality", sessionHandler.SetQuality)
		protected.DELETE("/sessions/:id", sessionHandler.EndSession)

		// Agent-facing session frame upload: the agent authenticates with the
		// X-Device-Key header (no JWT), so this route is registered outside the
		// protected group, following the /api/agent/* auth pattern.
		r.POST("/api/sessions/:id/frame", sessionHandler.ReportFrame)

		// Patch management routes
		protected.GET("/devices/:id/updates", patchHandler.ListUpdates)
		protected.POST("/devices/:id/updates/scan", patchHandler.ScanDevice)
		protected.POST("/updates/:id/approve", RequireAnyRole("admin", "manager"), patchHandler.ApproveUpdate)
		protected.POST("/devices/:id/reboot", RequireAnyRole("admin", "manager", "technician"), rebootHandler.RebootDevice)
		protected.GET("/patch/policies", patchHandler.ListPolicies)
		protected.POST("/patch/policies", RequireRole("admin"), patchHandler.CreatePolicy)
		protected.GET("/patch/deployments", patchHandler.ListDeployments)
		protected.POST("/patch/deploy", RequireAnyRole("admin", "manager"), patchHandler.DeployNow)
		protected.POST("/patch/deployments/:id/rollback", RequireAnyRole("admin", "manager"), patchHandler.RollbackDeployment)

		// File transfer routes
		protected.POST("/files/upload", fileHandler.UploadFile)
		protected.POST("/files/push", fileHandler.PushFile)
		protected.POST("/files/pull", fileHandler.PullFile)
		protected.GET("/files/transfers", fileHandler.ListTransfers)
		protected.GET("/files/transfers/:id", fileHandler.GetTransfer)
		protected.GET("/files/:transfer_id/file", fileHandler.DownloadFile)

		// SSO provider management (admin)
		protected.GET("/sso/providers", RequireRole("admin"), ssoHandler.ListProvidersAdmin)
		protected.POST("/sso/providers", RequireRole("admin"), ssoHandler.CreateProvider)
		protected.DELETE("/sso/providers/:id", RequireRole("admin"), ssoHandler.DeleteProvider)

		// Webhook routes (API v2)
		webhookGroup := protected.Group("/v2/webhooks")
		{
			webhookGroup.POST("", RequireAnyRole("admin", "manager"), webhookHandler.Create)
			webhookGroup.GET("", RequireAnyRole("admin", "manager"), webhookHandler.List)
			webhookGroup.GET("/:id", RequireAnyRole("admin", "manager"), webhookHandler.Get)
			webhookGroup.PUT("/:id", RequireAnyRole("admin", "manager"), webhookHandler.Update)
			webhookGroup.DELETE("/:id", RequireRole("admin"), webhookHandler.Delete)
			webhookGroup.POST("/:id/test", RequireAnyRole("admin", "manager"), webhookHandler.Test)
			webhookGroup.GET("/:id/deliveries", RequireAnyRole("admin", "manager"), webhookHandler.ListDeliveries)
			webhookGroup.POST("/:id/deliveries/:delivery_id/retry", RequireAnyRole("admin", "manager"), webhookHandler.RetryDelivery)
		}

		// API key routes
		apiKeyGroup := protected.Group("/v2/api-keys")
		{
			apiKeyGroup.POST("", apiKeyHandler.CreateKey)
			apiKeyGroup.GET("", apiKeyHandler.ListKeys)
			apiKeyGroup.GET("/:id", apiKeyHandler.GetKey)
			apiKeyGroup.PUT("/:id", apiKeyHandler.UpdateKey)
			apiKeyGroup.DELETE("/:id", apiKeyHandler.DeleteKey)
			apiKeyGroup.POST("/:id/revoke", apiKeyHandler.RevokeKey)
			apiKeyGroup.POST("/:id/rotate", apiKeyHandler.RotateKey)
		}
	}

	// Health check (no auth)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return r
}
