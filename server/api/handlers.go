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
func SetupRouter(ctx context.Context, store *store.Store, jwtAuth *auth.JWTAuth, hub *ws.Hub, engine *alerts.Engine, webURL, enrollSecret string, ratePerMinute, rateBurst int) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Rate limiter: the per-user budget the console is designed to live
	// inside (RATE_LIMIT_PER_MINUTE, default 100; burst defaults to a tenth
	// of it). Redis-backed when Redis is available, in-memory otherwise.
	if ratePerMinute < 1 {
		ratePerMinute = 100
	}
	effectiveBurst := rateBurst
	if effectiveBurst < 1 {
		effectiveBurst = ratePerMinute / 10
	}
	if effectiveBurst < 1 {
		effectiveBurst = 1
	}
	var rateLimitMiddleware gin.HandlerFunc
	if store.Cache != nil {
		rateLimitMiddleware = NewRedisRateLimiterWithBurst(store.Cache, time.Minute, ratePerMinute, effectiveBurst).Middleware()
	} else {
		rateLimitMiddleware = NewRateLimiterWithBurst(time.Minute, ratePerMinute, effectiveBurst).Middleware()
	}

	authHandler := NewAuthHandler(store, jwtAuth)
	deviceHandler := NewDeviceHandler(store, hub, engine, store.MetricHistory, enrollSecret)
	alertHandler := NewAlertHandler(store)
	thresholdHandler := NewThresholdHandler(store, engine)
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

	// 2.3-6: the audit registry is declared before the routes, so the coverage
	// test compares one table against one router, and the RBAC middlewares get
	// the store they record refusals with.
	registerAuditRoutes()
	setAuditStore(store)

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

		// Threshold rules (2.4-5): the levels a metric is measured against, per
		// scope. Admin and manager; a manager acts inside their tenants, and a
		// tag or global rule — which reaches every tenant's units — is admin-only.
		protected.GET("/alerts/thresholds", RequireAnyRole("admin", "manager"), thresholdHandler.ListThresholds)
		protected.GET("/alerts/thresholds/resolve", RequireAnyRole("admin", "manager"), thresholdHandler.ResolveThresholds)
		protected.GET("/alerts/thresholds/simulate", RequireAnyRole("admin", "manager"), thresholdHandler.SimulateThreshold)
		protected.POST("/alerts/thresholds", RequireAnyRole("admin", "manager"), thresholdHandler.CreateThreshold)
		protected.PUT("/alerts/thresholds/:id", RequireAnyRole("admin", "manager"), thresholdHandler.UpdateThreshold)
		protected.DELETE("/alerts/thresholds/:id", RequireAnyRole("admin", "manager"), thresholdHandler.DeleteThreshold)

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
			// 2.4-3: the identities an identity provider asserted that OurWay will not
			// link by itself. Reads are admin-only: a pending identity names somebody
			// else's account, and a tenant's manager has no business seeing it.
			userGroup.GET("/pending", RequireRole("admin"), userHandler.ListPendingIdentities)
			userGroup.POST("/:id/approve", RequireRole("admin"), userHandler.ApproveIdentity)
			userGroup.POST("/:id/merge", RequireRole("admin"), userHandler.MergeIdentity)
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
		protected.GET("/patch/overview", patchHandler.PatchOverview)
		protected.GET("/patch/updates", patchHandler.ListFleetUpdates)
		protected.POST("/patch/updates/approve", RequireAnyRole("admin", "manager"), patchHandler.BulkApprove)
		protected.POST("/patch/updates/skip", RequireAnyRole("admin", "manager"), patchHandler.BulkSkip)
		protected.POST("/patch/scan", RequireAnyRole("admin", "manager", "technician"), patchHandler.ScanMany)
		protected.POST("/devices/:id/reboot", RequireAnyRole("admin", "manager", "technician"), rebootHandler.RebootDevice)
		protected.GET("/patch/policies", patchHandler.ListPolicies)
		protected.POST("/patch/policies", RequireRole("admin"), patchHandler.CreatePolicy)
		protected.PUT("/patch/policies/:id", RequireRole("admin"), patchHandler.UpdatePolicy)
		protected.DELETE("/patch/policies/:id", RequireRole("admin"), patchHandler.DeletePolicy)
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
		// 2.4-1: the discovery preview — an issuer the operator has not saved yet.
		protected.POST("/sso/providers/discover", RequireRole("admin"), ssoHandler.DiscoverIssuer)
		protected.POST("/sso/providers/:id/test", RequireRole("admin"), ssoHandler.TestProvider)
		protected.DELETE("/sso/providers/:id", RequireRole("admin"), ssoHandler.DeleteProvider)

		// Client (tenant) routes — 2.3-4. Reads respect the caller's client
		// scope; every write is admin-only, because assigning a unit or granting
		// a client is what moves fleets between people.
		clientHandler := NewClientHandler(store)
		clientGroup := protected.Group("/clients")
		{
			clientGroup.GET("", clientHandler.ListClients)
			// The whole per-client ledger in one read. Registered before `/:id`
			// so the literal wins the match, and `rollups` is not a client id.
			clientGroup.GET("/rollups", clientHandler.ClientRollups)
			clientGroup.POST("", RequireRole("admin"), clientHandler.CreateClient)
			clientGroup.GET("/:id", clientHandler.GetClient)
			clientGroup.PUT("/:id", RequireRole("admin"), clientHandler.UpdateClient)
			clientGroup.DELETE("/:id", RequireRole("admin"), clientHandler.DeleteClient)
			clientGroup.PUT("/:id/devices", RequireRole("admin"), clientHandler.AssignDevices)
			clientGroup.GET("/:id/rollup", clientHandler.ClientRollup)
			clientGroup.GET("/:id/users", RequireRole("admin"), clientHandler.ListClientUsers)
			clientGroup.PUT("/:id/users", RequireRole("admin"), clientHandler.SetClientUsers)
			// 2.3-4c: a tenant's enrollment credentials. Admin-only, **reads
			// included**: a token is the ability to place a unit inside a customer's
			// tenant, and the grant model says nothing about who may hand that out.
			clientGroup.GET("/:id/tokens", RequireRole("admin"), clientHandler.ListClientTokens)
			clientGroup.POST("/:id/tokens", RequireRole("admin"), clientHandler.CreateClientToken)
			clientGroup.DELETE("/:id/tokens/:tokenId", RequireRole("admin"), clientHandler.RevokeClientToken)
		}

		// Audit ledger routes — 2.3-6. An operator surface: admins read the
		// installation, managers read their tenants, and a viewer reads nothing,
		// because the ledger answers "who did this to my fleet" and a viewer
		// performs no acts worth reading about.
		auditHandler := NewAuditHandler(store)
		protected.GET("/audit", RequireAnyRole("admin", "manager"), auditHandler.List)
		protected.GET("/audit/actions", RequireAnyRole("admin", "manager"), auditHandler.Actions)

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
	// build is available and self-update (fully automatic updates). The
	// request budget travels with it so the console can state the ceiling the
	// operator is working under, not just the code behind a 429.
	r.GET("/api/agent/version", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"version":               config.Version,
			"git_commit":            config.GitCommit,
			"rate_limit_per_minute": ratePerMinute,
			"rate_limit_burst":      effectiveBurst,
		})
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
