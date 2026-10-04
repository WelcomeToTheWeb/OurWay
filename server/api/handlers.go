package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/config"
	"ourway/server/events"
	"ourway/server/files"
	"ourway/server/patching"
	"ourway/server/sessions"
	"ourway/server/store"
	"ourway/server/webhooks"
	"ourway/server/ws"
)

// SetupRouter configures and returns the Gin router with all routes.
// ctx scopes the router's background jobs (webhook retry loop, scheduled
// update scan); cancelling it stops them (M9).
func SetupRouter(ctx context.Context, store *store.Store, jwtAuth *auth.JWTAuth, hub *ws.Hub, engine *alerts.Engine, webURL, enrollSecret string) *gin.Engine {
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
	deviceHandler := NewDeviceHandler(store, hub, engine, store.MetricHistory, enrollSecret)
	alertHandler := NewAlertHandler(store)
	roleHandler := NewRoleHandler(store)
	userHandler := NewUserHandler(store)
	sessions.NewGateway(ctx, store)
	sessionHandler := NewSessionHandler(store, hub)
	scanner := patching.NewScanner(store, hub)
	deployer := patching.NewDeployer(store, hub)
	rollbacker := patching.NewRollbackManager(store, hub)
	patchHandler := NewPatchHandler(store, scanner, deployer, rollbacker)
	rebooter := patching.NewRebooter(hub).WithStore(store)
	rebootHandler := NewRebootHandler(store, rebooter)
	fileService := files.NewService(store, hub, "")
	fileHandler := NewFileHandler(store, fileService)
	agentFileHandler := NewAgentFileHandler(store, fileService)
	agentPatchHandler := NewAgentPatchHandler(store, deployer)
	ssoHandler := NewSSOHandler(store, jwtAuth, webURL)
	installerHandler := NewInstallerHandler(installersDir)
	apiKeyHandler := CreateAPIKeyHandler(store, jwtAuth)
	webhookDispatcher := webhooks.NewDispatcher(store)
	webhookHandler := NewWebhookHandler(store, webhookDispatcher)
	events.SetPublisher(webhookDispatcher)

	// Start webhook delivery retry loop (stops with the server context)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				webhookDispatcher.RetryPending(ctx)
			}
		}
	}()

	// Periodic fleet-wide update scan (every 24h)
	go scanner.ScanAll(ctx, 24*time.Hour)
	// Patch policy engine: drives scan/approve/deploy/reboot for each
	// policy on its schedule (daily/weekly/monthly, hourly check).
	policyEngine := patching.NewPolicyEngine(store, hub, deployer, rebooter, scanner)
	go policyEngine.Run(ctx)

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
		authGroup.POST("/sso/exchange", ssoHandler.Exchange)
	}

	// Agent routes (device key auth via header)
	agentGroup := r.Group("/api/agent")
	{
		agentGroup.POST("/register", deviceHandler.RegisterDevice)
		agentGroup.GET("/me", deviceHandler.GetSelf)
		agentGroup.POST("/heartbeat", deviceHandler.Heartbeat)
		agentGroup.POST("/metrics", deviceHandler.ReportMetrics)
		agentGroup.POST("/updates", agentPatchHandler.ReportUpdate)
		agentGroup.POST("/deployments/result", agentPatchHandler.ReportDeploymentResult)
		agentGroup.POST("/files/status", agentFileHandler.ReportStatus)
		agentGroup.GET("/files/:transfer_id/download", agentFileHandler.DownloadForAgent)
		agentGroup.POST("/files/:transfer_id/upload", agentFileHandler.UploadFromAgent)

		// Agent binary download (no auth: the installer needs the binary
		// before the device is registered). os and arch are whitelisted so
		// the query parameters can never be used for path traversal.
		agentGroup.GET("/binary", func(c *gin.Context) {
			osName := c.Query("os")
			arch := c.Query("arch")
			if osName == "" || arch == "" {
				c.JSON(400, gin.H{"error": "missing os and/or arch parameter"})
				return
			}
			switch osName {
			case "linux", "darwin", "windows":
			default:
				c.JSON(400, gin.H{"error": "unsupported os", "supported": []string{"linux", "darwin", "windows"}})
				return
			}
			switch arch {
			case "amd64", "arm64", "arm":
			default:
				c.JSON(400, gin.H{"error": "unsupported arch", "supported": []string{"amd64", "arm64", "arm"}})
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

	// Installer download routes (public: onboarding happens before a tech
	// has an account, so the artifacts are downloadable without auth).
	installerGroup := r.Group("/api/v2/installers")
	{
		installerGroup.GET("", installerHandler.List)
		installerGroup.GET("/:name", installerHandler.Download)
	}

	// Protected routes (JWT or API key auth required, rate limited)
	protected := r.Group("/api", APIKeyMiddleware(store, jwtAuth), rateLimitMiddleware)
	{
		// User profile
		protected.GET("/auth/profile", authHandler.GetProfile)
		protected.PUT("/auth/profile", authHandler.UpdateProfile)
		protected.PUT("/auth/password", authHandler.UpdatePassword)
		protected.DELETE("/auth/me", authHandler.DeleteAccount)
		protected.POST("/auth/logout", authHandler.Logout)

		// Monitoring data (Danger Zone: clear all metrics + alerts)
		protected.DELETE("/monitoring/data", RequireRole("admin"), deviceHandler.ClearMonitoringData)

		// Device routes
		tagHandler := NewTagHandler(store)
		protected.GET("/tags", tagHandler.ListTags)
		protected.PUT("/devices/:id/tags", RequireAnyRole("admin", "manager", "technician"), tagHandler.SetDeviceTags)
		protected.POST("/devices/tags/bulk", RequireAnyRole("admin", "manager", "technician"), tagHandler.BulkTags)
		protected.GET("/devices", deviceHandler.ListDevices)
		protected.GET("/devices/:id", deviceHandler.GetDevice)
		protected.DELETE("/devices/:id", RequireRole("admin"), deviceHandler.DeleteDevice)
		protected.GET("/devices/:id/metrics", deviceHandler.GetLatestMetrics)
		protected.GET("/devices/:id/metrics/history", deviceHandler.GetMetricsHistory)
		protected.POST("/devices/:id/stream", deviceHandler.StartStream)
		protected.POST("/devices/:id/stream/stop", deviceHandler.StopStream)

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

		// Session routes: starting a session and sending input are
		// remote-control operations, so plain viewers cannot drive them (C1).
		// Ending someone else's session needs the same control role.
		protected.POST("/devices/:id/sessions", RequireAnyRole("admin", "manager", "technician"), sessionHandler.StartSession)
		protected.GET("/sessions", sessionHandler.ListSessions)
		protected.POST("/sessions/:id/input", RequireAnyRole("admin", "manager", "technician"), sessionHandler.SendInput)
		protected.POST("/sessions/:id/quality", RequireAnyRole("admin", "manager", "technician"), sessionHandler.SetQuality)
		protected.DELETE("/sessions/:id", RequireAnyRole("admin", "manager", "technician"), sessionHandler.EndSession)

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
		protected.GET("/patch/deployments/:id/results", patchHandler.ListDeploymentResults)
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

	// Liveness (no auth): the process is up. Always 200.
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	// Server version (no auth): agents poll it to detect that a newer
	// build is available and self-update (fully automatic updates).
	r.GET("/api/agent/version", func(c *gin.Context) {
		c.JSON(200, gin.H{"version": config.Version, "git_commit": config.GitCommit})
	})

	// Readiness (no auth): dependencies are reachable. Intended for
	// Kubernetes readiness probes and load balancer health checks.
	r.GET("/ready", func(c *gin.Context) {
		deps := gin.H{"db": "ok", "redis": "not_configured"}
		status := http.StatusOK

		if sqlDB, err := store.DB.DB(); err == nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			err = sqlDB.PingContext(ctx)
			cancel()
			if err != nil {
				deps["db"] = "error: " + err.Error()
				status = http.StatusServiceUnavailable
			}
		} else {
			deps["db"] = "error: " + err.Error()
			status = http.StatusServiceUnavailable
		}

		if store.Cache != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			if err := store.Cache.GetClient().Ping(ctx).Err(); err != nil {
				deps["redis"] = "error: " + err.Error()
				status = http.StatusServiceUnavailable
			} else {
				deps["redis"] = "ok"
			}
			cancel()
		}

		if status != http.StatusOK {
			deps["status"] = "degraded"
		} else {
			deps["status"] = "ok"
		}
		c.JSON(status, deps)
	})

	return r
}
