package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ourway/server/auth"
)

func TestRequireRoleMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		roles interface{}
		want  int
		mw    gin.HandlerFunc
	}{
		{"admin allowed on RequireRole(admin)", []string{"admin"}, 200, RequireRole("admin")},
		{"manager denied on RequireRole(admin)", []string{"manager"}, 403, RequireRole("admin")},
		{"viewer denied on RequireRole(admin)", []string{"viewer"}, 403, RequireRole("admin")},
		{"technician denied on RequireRole(admin)", []string{"technician"}, 403, RequireRole("admin")},
		{"admin allowed on RequireAnyRole(admin,manager)", []string{"admin"}, 200, RequireAnyRole("admin", "manager")},
		{"manager allowed on RequireAnyRole(admin,manager)", []string{"manager"}, 200, RequireAnyRole("admin", "manager")},
		{"technician denied on RequireAnyRole(admin,manager)", []string{"technician"}, 403, RequireAnyRole("admin", "manager")},
		{"viewer denied on RequireAnyRole(admin,manager,technician)", []string{"viewer"}, 403, RequireAnyRole("admin", "manager", "technician")},
		{"technician allowed on RequireAnyRole(admin,manager,technician)", []string{"technician"}, 200, RequireAnyRole("admin", "manager", "technician")},
		{"empty roles denied", []string{}, 403, RequireAnyRole("admin")},
		{"multiple roles honored", []string{"viewer", "manager"}, 200, RequireAnyRole("admin", "manager")},
		{"no roles in context", nil, 401, RequireRole("admin")},
		{"roles wrong type", "admin", 401, RequireRole("admin")},
		{"roles wrong type on RequireAnyRole", "admin", 401, RequireAnyRole("admin", "manager")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/x", func(c *gin.Context) {
				if tt.roles != nil {
					c.Set("roles", tt.roles)
				}
				c.Next()
			}, tt.mw, func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
			req, _ := http.NewRequest("GET", "/x", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d (body: %s)", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareRejectsMissingAndInvalidTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	valid, err := jwtAuth.GenerateToken("u1", "user", []string{"admin"})
	if err != nil {
		t.Fatalf("token generation failed: %v", err)
	}

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"missing header", "", 401},
		{"malformed header", "justakey", 401},
		{"invalid token", "Bearer not-a-token", 401},
		{"valid token", "Bearer " + valid, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/x", AuthMiddleware(jwtAuth), func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
			req, _ := http.NewRequest("GET", "/x", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

// TestRouteRoleMatrix exercises the real full router from SetupRouter so a
// role check dropped from any mutating route fails here (C1 regression
// guard). Expected non-403 statuses are the handler's own responses to
// missing fixtures: 404 (not found), 400 (validation), 409 (device not
// connected) — anything that proves the request passed the middleware.
// Each case uses a distinct user ID so the per-user rate limiter (100/min,
// burst 10) never interferes.
func TestRouteRoleMatrix(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)

	// Each case mints a token with a unique user ID: the router's
	// per-user rate limiter (100/min, burst 10) would otherwise trip
	// mid-matrix and mask the role checks with 429s.
	tests := []struct {
		name   string
		method string
		path   string
		role   string // "" means anonymous (no token)
		want   int
	}{
		// Remote control (C1): viewers must be 403, control roles must
		// pass the middleware (then hit handler-level fixture errors).
		{"viewer cannot start session", "POST", "/api/devices/dev-id/sessions", "viewer", 403},
		{"viewer cannot send input", "POST", "/api/sessions/sess-id/input", "viewer", 403},
		{"viewer cannot set quality", "POST", "/api/sessions/sess-id/quality", "viewer", 403},
		{"viewer cannot end session", "DELETE", "/api/sessions/sess-id", "viewer", 403},
		{"viewer cannot reboot", "POST", "/api/devices/dev-id/reboot", "viewer", 403},
		{"technician passes session start middleware", "POST", "/api/devices/dev-id/sessions", "technician", 404},
		{"technician passes input middleware", "POST", "/api/sessions/sess-id/input", "technician", 400},
		{"manager passes session start middleware", "POST", "/api/devices/dev-id/sessions", "manager", 404},
		{"admin passes session start middleware", "POST", "/api/devices/dev-id/sessions", "admin", 404},
		// Alerts.
		{"viewer cannot resolve alert", "POST", "/api/alerts/alert-id/resolve", "viewer", 403},
		{"technician passes resolve middleware", "POST", "/api/alerts/alert-id/resolve", "technician", 404},
		{"viewer cannot acknowledge alert", "POST", "/api/alerts/alert-id/acknowledge", "viewer", 403},
		{"viewer cannot assign alert", "POST", "/api/alerts/alert-id/assign", "viewer", 403},
		// User management.
		{"viewer cannot list users", "GET", "/api/users", "viewer", 403},
		{"manager can list users", "GET", "/api/users", "manager", 200},
		{"viewer cannot create user", "POST", "/api/users", "viewer", 403},
		{"manager cannot create user", "POST", "/api/users", "manager", 403},
		{"admin passes create-user middleware", "POST", "/api/users", "admin", 400},
		{"viewer cannot delete user", "DELETE", "/api/users/some-id", "viewer", 403},
		{"manager cannot delete user", "DELETE", "/api/users/some-id", "manager", 403},
		{"viewer cannot update user roles", "PUT", "/api/users/some-id/roles", "viewer", 403},
		// Devices.
		{"viewer cannot delete device", "DELETE", "/api/devices/dev-id", "viewer", 403},
		{"manager cannot delete device", "DELETE", "/api/devices/dev-id", "manager", 403},
		{"viewer cannot clear monitoring data", "DELETE", "/api/monitoring/data", "viewer", 403},
		{"manager cannot clear monitoring data", "DELETE", "/api/monitoring/data", "manager", 403},
		// Patching.
		{"viewer cannot approve update", "POST", "/api/updates/up-id/approve", "viewer", 403},
		{"technician cannot approve update", "POST", "/api/updates/up-id/approve", "technician", 403},
		{"manager passes approve middleware", "POST", "/api/updates/up-id/approve", "manager", 404},
		{"viewer cannot create patch policy", "POST", "/api/patch/policies", "viewer", 403},
		{"manager cannot create patch policy", "POST", "/api/patch/policies", "manager", 403},
		{"admin passes create-policy middleware", "POST", "/api/patch/policies", "admin", 400},
		{"viewer cannot deploy patches", "POST", "/api/patch/deploy", "viewer", 403},
		{"technician cannot deploy patches", "POST", "/api/patch/deploy", "technician", 403},
		{"manager passes deploy middleware", "POST", "/api/patch/deploy", "manager", 400},
		{"viewer cannot rollback deployment", "POST", "/api/patch/deployments/d-id/rollback", "viewer", 403},
		{"technician cannot rollback deployment", "POST", "/api/patch/deployments/d-id/rollback", "technician", 403},
		// Webhooks.
		{"viewer cannot create webhook", "POST", "/api/v2/webhooks", "viewer", 403},
		{"technician cannot create webhook", "POST", "/api/v2/webhooks", "technician", 403},
		{"manager passes create-webhook middleware", "POST", "/api/v2/webhooks", "manager", 400},
		{"manager cannot delete webhook", "DELETE", "/api/v2/webhooks/wh-id", "manager", 403},
		{"admin passes delete-webhook middleware", "DELETE", "/api/v2/webhooks/wh-id", "admin", 404},
		// SSO provider management.
		{"viewer cannot list sso providers", "GET", "/api/sso/providers", "viewer", 403},
		{"admin can list sso providers", "GET", "/api/sso/providers", "admin", 200},
		{"viewer cannot create sso provider", "POST", "/api/sso/providers", "viewer", 403},
		{"viewer cannot delete sso provider", "DELETE", "/api/sso/providers/id", "viewer", 403},
		// Roles.
		{"viewer cannot create role", "POST", "/api/roles", "viewer", 403},
		{"admin passes create-role middleware", "POST", "/api/roles", "admin", 400},
		{"viewer cannot delete role", "DELETE", "/api/roles/role-id", "viewer", 403},
		// Read routes are open to any authenticated user.
		{"viewer can list devices", "GET", "/api/devices", "viewer", 200},
		{"viewer can list alerts", "GET", "/api/alerts", "viewer", 200},
		{"viewer can list sessions", "GET", "/api/sessions", "viewer", 200},
		{"viewer can list patch policies", "GET", "/api/patch/policies", "viewer", 200},
		// Unauthenticated requests never reach handlers.
		{"anonymous cannot list users", "GET", "/api/users", "", 401},
		{"anonymous cannot start session", "POST", "/api/devices/dev-id/sessions", "", 401},
		{"anonymous cannot delete device", "DELETE", "/api/devices/dev-id", "", 401},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request build failed: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if tt.role != "" {
				req.Header.Set("Authorization", "Bearer "+mintToken(t, jwtAuth, fmt.Sprintf("%s-%d", tt.role, i), []string{tt.role}))
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("%s %s as %s: got %d, want %d", tt.method, tt.path, tt.role, resp.StatusCode, tt.want)
			}
		})
	}
}

func mintToken(t *testing.T, jwtAuth *auth.JWTAuth, userID string, roles []string) string {
	t.Helper()
	tok, err := jwtAuth.GenerateToken(userID, "user-"+userID, roles)
	if err != nil {
		t.Fatalf("token generation failed: %v", err)
	}
	return tok
}
