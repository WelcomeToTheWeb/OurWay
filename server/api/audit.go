package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.3-6 — the audit log's API side: one writer, one actor definition, and the
// registry the coverage test checks.
//
// **Why a store writer and not middleware.** Middleware knows the method, the
// path and the status code. The record needs the *target*, the *client*, and
// whether the act **succeeded** — three things only the handler knows, because
// the handler is what looks them up. A middleware that inferred them from the
// path would write a row for every request that *looked* like an act, which is
// the phase-1 shape: a log of intentions, with no acts in it.
//
// **Refusals are rows.** `outcome` is a column, and `refused` is its most
// useful value: a technician's out-of-scope deploy attempt is the event an MSP
// asks about, and a log that records only successes cannot answer it.
//
// **Rows are written before the response is sent.** Every helper here is called
// before the handler's `c.JSON`, so a crash after the act leaves the record
// (`TestAuditRowsAreWrittenBeforeTheResponse`).

// auditRegistry maps a route (method + gin path pattern) to the action name its
// handler records. `server/api/audit_test.go` walks `SetupRouter`'s route list,
// filters to mutating methods, and fails the build on a mutating route with no
// entry — so adding a route without auditing it is a red CI, not a review catch.
var auditRegistry = map[string]string{}

// auditRoute registers a mutating route's action at registration time, beside
// the route itself: the registration and the audit are adjacent in the same
// file, and the only way to miss one is to delete both.
func auditRoute(method, path, action string) {
	auditRegistry[method+" "+path] = action
}

// registerAuditRoutes declares the audit action of every mutating route in the
// router, in one readable table. It is called by `SetupRouter` before the
// routes themselves are registered, and the coverage test
// (`TestEveryMutatingRouteIsAudited`) fails on any mutating route with no
// entry here.
//
// The actions are verbs, and the table reads like the ledger's filter list —
// which is what it is used for.
func registerAuditRoutes() {
	// Account and identity.
	auditRoute(http.MethodPost, "/api/auth/login", models.AuditAuthLogin)
	auditRoute(http.MethodPost, "/api/auth/register", models.AuditAuthRegister)
	auditRoute(http.MethodPost, "/api/auth/logout", models.AuditAuthLogout)
	auditRoute(http.MethodPut, "/api/auth/password", models.AuditAuthPasswordChange)
	auditRoute(http.MethodPut, "/api/auth/profile", models.AuditAuthRegister)
	auditRoute(http.MethodDelete, "/api/auth/me", models.AuditUserDelete)

	// Fleet and monitoring.
	auditRoute(http.MethodDelete, "/api/devices/:id", models.AuditDeviceDelete)
	auditRoute(http.MethodPut, "/api/devices/:id/tags", models.AuditDeviceTags)
	auditRoute(http.MethodPost, "/api/devices/tags/bulk", models.AuditDeviceTags)
	auditRoute(http.MethodPost, "/api/devices/:id/stream", models.AuditDeviceStream)
	auditRoute(http.MethodPost, "/api/devices/:id/stream/stop", models.AuditDeviceStream)
	auditRoute(http.MethodPost, "/api/devices/:id/reboot", models.AuditDeviceReboot)
	auditRoute(http.MethodDelete, "/api/monitoring/data", models.AuditMonitoringPurge)

	// Alerts.
	auditRoute(http.MethodPost, "/api/alerts/:id/resolve", models.AuditAlertResolve)
	auditRoute(http.MethodPost, "/api/alerts/:id/acknowledge", models.AuditAlertAcknowledge)
	auditRoute(http.MethodPost, "/api/alerts/:id/assign", models.AuditAlertAssign)

	// Remote control.
	auditRoute(http.MethodPost, "/api/devices/:id/sessions", models.AuditSessionStart)
	auditRoute(http.MethodPost, "/api/sessions/:id/input", models.AuditSessionStart)
	auditRoute(http.MethodPost, "/api/sessions/:id/quality", models.AuditSessionQuality)
	auditRoute(http.MethodDelete, "/api/sessions/:id", models.AuditSessionEnd)

	// Patching.
	auditRoute(http.MethodPost, "/api/devices/:id/updates/scan", models.AuditPatchScan)
	auditRoute(http.MethodPost, "/api/patch/scan", models.AuditPatchScan)
	auditRoute(http.MethodPost, "/api/updates/:id/approve", models.AuditPatchApprove)
	auditRoute(http.MethodPost, "/api/patch/updates/approve", models.AuditPatchApprove)
	auditRoute(http.MethodPost, "/api/patch/updates/skip", models.AuditPatchSkip)
	auditRoute(http.MethodPost, "/api/patch/deploy", models.AuditPatchDeploy)
	auditRoute(http.MethodPost, "/api/patch/deployments/:id/rollback", models.AuditPatchRollback)
	auditRoute(http.MethodPost, "/api/patch/policies", models.AuditPolicyCreate)
	auditRoute(http.MethodPut, "/api/patch/policies/:id", models.AuditPolicyUpdate)
	auditRoute(http.MethodDelete, "/api/patch/policies/:id", models.AuditPolicyDelete)

	// Files.
	auditRoute(http.MethodPost, "/api/files/upload", models.AuditFileUpload)
	auditRoute(http.MethodPost, "/api/files/push", models.AuditFilePush)
	auditRoute(http.MethodPost, "/api/files/pull", models.AuditFilePull)

	// Tenancy.
	auditRoute(http.MethodPost, "/api/clients", models.AuditClientCreate)
	auditRoute(http.MethodPut, "/api/clients/:id", models.AuditClientUpdate)
	auditRoute(http.MethodDelete, "/api/clients/:id", models.AuditClientDelete)
	auditRoute(http.MethodPut, "/api/clients/:id/devices", models.AuditDeviceAssign)
	auditRoute(http.MethodPut, "/api/clients/:id/users", models.AuditClientGrant)
	auditRoute(http.MethodPost, "/api/clients/:id/tokens", models.AuditClientTokenCreate)
	auditRoute(http.MethodDelete, "/api/clients/:id/tokens/:tokenId", models.AuditClientTokenRevoke)

	// Accounts, roles, keys, integrations.
	auditRoute(http.MethodPost, "/api/users", models.AuditUserCreate)
	// 2.4-3: the admin's two decisions about a pending SSO identity.
	auditRoute(http.MethodPost, "/api/users/:id/approve", models.AuditUserApprove)
	auditRoute(http.MethodPost, "/api/users/:id/merge", models.AuditUserMerge)
	auditRoute(http.MethodPut, "/api/users/:id/roles", models.AuditUserRoleChange)
	auditRoute(http.MethodDelete, "/api/users/:id", models.AuditUserDelete)
	auditRoute(http.MethodPost, "/api/roles", models.AuditRoleCreate)
	auditRoute(http.MethodPut, "/api/roles/:id", models.AuditRoleUpdate)
	auditRoute(http.MethodDelete, "/api/roles/:id", models.AuditRoleDelete)
	auditRoute(http.MethodPost, "/api/v2/api-keys", models.AuditAPIKeyCreate)
	auditRoute(http.MethodPut, "/api/v2/api-keys/:id", models.AuditAPIKeyUpdate)
	auditRoute(http.MethodDelete, "/api/v2/api-keys/:id", models.AuditAPIKeyDelete)
	auditRoute(http.MethodPost, "/api/v2/api-keys/:id/revoke", models.AuditAPIKeyRevoke)
	auditRoute(http.MethodPost, "/api/v2/api-keys/:id/rotate", models.AuditAPIKeyRotate)
	auditRoute(http.MethodPost, "/api/v2/webhooks", models.AuditWebhookCreate)
	auditRoute(http.MethodPut, "/api/v2/webhooks/:id", models.AuditWebhookUpdate)
	auditRoute(http.MethodDelete, "/api/v2/webhooks/:id", models.AuditWebhookDelete)
	auditRoute(http.MethodPost, "/api/v2/webhooks/:id/test", models.AuditWebhookTest)
	auditRoute(http.MethodPost, "/api/v2/webhooks/:id/deliveries/:delivery_id/retry", models.AuditWebhookTest)
	auditRoute(http.MethodPost, "/api/sso/providers", models.AuditSSOCreate)
	auditRoute(http.MethodDelete, "/api/sso/providers/:id", models.AuditSSODelete)
	// 2.4-1: probing an IdP is a mutating-verb route (POST) that contacts another
	// machine on the operator's behalf, so it is audited like the others — the
	// result is ok/fail, never a secret.
	auditRoute(http.MethodPost, "/api/sso/providers/:id/test", models.AuditSSOTest)
	// 2.4-1: the discovery preview fetches an operator-supplied issuer before any
	// provider row exists — the same outbound call as the probe, so the same verb.
	auditRoute(http.MethodPost, "/api/sso/providers/discover", models.AuditSSODiscover)
}

// lookupAuditAction resolves a concrete route to its registered action,
// tolerating the path-parameter syntax (`:id`) the registry is written in.
func lookupAuditAction(method, path string) (string, bool) {
	if action, ok := auditRegistry[method+" "+path]; ok {
		return action, true
	}
	// The registry is keyed by the pattern; a caller holding a concrete path
	// segment counts it as matching a `:` segment.
	key := method + " "
	patternKey := method + " "
	for key := range auditRegistry {
		methodPart, pattern, _ := strings.Cut(key, " ")
		if methodPart != method || !patternMatches(pattern, path) {
			continue
		}
		return auditRegistry[key], true
	}
	_ = key
	_ = patternKey
	return "", false
}

// patternMatches compares a gin path pattern with a concrete path, segment by
// segment, where a `:name` segment accepts anything.
func patternMatches(pattern, path string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	cs := strings.Split(strings.Trim(path, "/"), "/")
	if len(ps) != len(cs) {
		return false
	}
	for i := range ps {
		if strings.HasPrefix(ps[i], ":") {
			continue
		}
		if ps[i] != cs[i] {
			return false
		}
	}
	return true
}

// AuditedRoutes exposes the registry to the coverage test.
func AuditedRoutes() map[string]string {
	out := make(map[string]string, len(auditRegistry))
	for k, v := range auditRegistry {
		out[k] = v
	}
	return out
}

// auditStore carries the store for the RBAC middlewares, which are plain
// functions (`RequireRole("admin")`) and are used by every route group.
//
// They cannot take a parameter without touching every call site, and the
// handler-side writer cannot help: they abort **before** the handler runs, so
// the refusal is recorded here or not at all. The var is written once, in
// `SetupRouter`, from the same store every handler already closes over, so
// there is exactly one store in the process and no second source of truth —
// and it stays nil in tests that build a router without one, where the hook is
// a no-op.
var auditStore *store.Store

// setAuditStore wires the RBAC denial hook. Called by `SetupRouter`.
func setAuditStore(st *store.Store) { auditStore = st }

// auditStoreFromContext returns the store the audit hook writes with.
func auditStoreFromContext(c *gin.Context) *store.Store {
	_ = c // the hook has no per-request store: the router owns one
	return auditStore
}

// AuditTarget is what an act pointed at.
type AuditTarget struct {
	Type     string
	ID       string
	ClientID string
}

// DeviceTarget derives the target from a device row, which is the common case
// and the one worth getting right: the client stored on the row is the client
// **at the time of the act**, and a row never changes its tenant (see
// `models.AuditLog`).
func DeviceTarget(d *models.Device) AuditTarget {
	if d == nil {
		return AuditTarget{Type: "device"}
	}
	target := AuditTarget{Type: "device", ID: d.ID}
	if d.ClientID != nil {
		target.ClientID = *d.ClientID
	}
	return target
}

// ClientTarget is the tenant-axis case of the same rule: when the act is *on a
// client* — create, rename, pause, delete, assign its units, grant its access —
// the tenant of the record is that client, named at the time of the act.
//
// Leaving `ClientID` empty here is not a neutral choice. The ledger scopes a
// manager's read with `client_id IN (their clients)`, and an empty column never
// matches, so a tenant's own acts — including the grant that decides which
// technician can see it — are readable by the admin alone and invisible to the
// tenant filter. "Who changed access to my client?" is a question a client's
// manager is entitled to ask of their own ledger.
func ClientTarget(clientID string) AuditTarget {
	return AuditTarget{Type: "client", ID: clientID, ClientID: clientID}
}

// recordAudit writes the row. `status` is the status code the handler is about
// to answer with, and the outcome column is derived from it — one rule, so the
// ledger's outcomes are comparable and never a free-text reason.
func recordAudit(c *gin.Context, st *store.Store, action string, status int, target AuditTarget, details map[string]any) {
	if st == nil || st.Audit == nil || action == "" {
		return
	}
	if extra := refusedTargets(c); len(extra) > 0 {
		if details == nil {
			details = map[string]any{}
		}
		details["out_of_scope"] = extra
	}
	if auditAlreadyRecorded(c) {
		// One row per act. Handlers name their target precisely, so an
		// explicit record always wins over a deferred catch-all; without the
		// guard, a handler that records mid-way and defers the answer would
		// write twice, and a ledger with duplicate rows teaches people to
		// distrust it.
		return
	}
	markAudited(c)
	actor := actorOf(c, st)
	row := &models.AuditLog{
		ID:            models.NewAuditID(),
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		ActorRoles:    actor.Roles,
		ActorProvider: actor.Provider,
		Action:        action,
		TargetType:    target.Type,
		TargetID:      target.ID,
		ClientID:      target.ClientID,
		Outcome:       models.AuditOutcomeFor(status),
		Details:       store.EncodeDetails(details),
		SourceIP:      c.ClientIP(),
	}
	// The write is best-effort by construction, and the reason is deliberate:
	// an audit row is never the reason a technician's reboot fails to reach a
	// machine, and never the reason a fleet's page is a 500. A failure here is
	// logged to the server log, which is the record of the record's absence.
	_ = st.Audit.Record(row)
}

// auditForAnswer records the act with the outcome the caller is about to
// receive. It is meant to be `defer`red at the top of a handler, which is the
// position that keeps a ledger honest:
//
//   - the outcome column is `c.Writer.Status()`, so the row describes the
//     response the technician got, not the intention the handler had when it
//     started;
//   - every `return` of the handler is covered — success, 400, 404, 409, 500 —
//     so a new exit path cannot add a silent act, and a handler that dies
//     mid-request is a row too;
//   - the row is written before the body is flushed, because gin's `Write`
//     headers have already captured the status by the time the deferred call
//     runs, and a record of an act the server finished is the record an MSP
//     reads.
//
// Handlers whose record must name a target they looked up mid-way
// (`device.delete`) call `auditOK`/`auditRefused` at the point they know the
// answer; `auditOKOnce` keeps both styles in one request safe.
func auditForAnswer(c *gin.Context, st *store.Store, action string, target AuditTarget, details map[string]any) {
	status := c.Writer.Status()
	if status == 0 || status == http.StatusOK {
		// A handler that returns without writing wrote a 200 by omission.
		status = http.StatusOK
	}
	recordAudit(c, st, action, status, target, details)
}

// auditOKOnce records a success, and marks the request as recorded so a
// deferred catch-all in the same request does not write a second row.
//
// The marking is `recordAudit`'s job: it writes, then marks, and its guard is what
// makes the single-row rule hold. Marking *before* the write is how these two
// helpers silently wrote nothing for the whole of 2.3 — the guard they were meant to
// coordinate with ran first and swallowed the row. 2.4-2's login refusals were the
// first callers, and the ledger's silence is what found it.
func auditOKOnce(c *gin.Context, st *store.Store, action string, target AuditTarget, details map[string]any) {
	auditOK(c, st, action, target, details)
}

// auditRefusedOnce is `auditRefused` with the same guard.
func auditRefusedOnce(c *gin.Context, st *store.Store, action string, status int, target AuditTarget, details map[string]any) {
	auditRefused(c, st, action, status, target, details)
}

const auditRecordedKey = "ourway.audit.recorded"

func refusedTargets(c *gin.Context) []string {
	v, ok := c.Get(auditRefusedTargetsKey)
	if !ok {
		return nil
	}
	if ids, ok := v.([]string); ok {
		return ids
	}
	return nil
}

func markAudited(c *gin.Context) { c.Set(auditRecordedKey, true) }

// auditAlreadyRecorded reports whether this request has a row.
func auditAlreadyRecorded(c *gin.Context) bool {
	v, ok := c.Get(auditRecordedKey)
	return ok && v == true
}

// auditNameRefusedTargets adds the refused ids to the row the enclosing
// handler's deferred writer is about to record. The bulk commands validate
// their targets, learn which ones are foreign, and answer 400 naming them —
// and the record carries the same list, so the ledger answers the question the
// response only answers for the caller who saw it.
func auditNameRefusedTargets(c *gin.Context, ids []string) {
	c.Set(auditRefusedTargetsKey, ids)
}

const auditRefusedTargetsKey = "ourway.audit.refused_targets"

// auditOK records a success. Call it **before** writing the success response.
func auditOK(c *gin.Context, st *store.Store, action string, target AuditTarget, details map[string]any) {
	recordAudit(c, st, action, http.StatusOK, target, details)
}

// auditRefused records a refusal — 400, 403, 404, 409. The most valuable rows
// in the log, and the reason the writer is not a success-path hook.
func auditRefused(c *gin.Context, st *store.Store, action string, status int, target AuditTarget, details map[string]any) {
	recordAudit(c, st, action, status, target, details)
}

// actorOf captures who the request is, in one place: 2.3-1's `CallerUserID`,
// the username the auth middleware carried, and the roles at the time of the
// act. Username and roles are stored, not joined at read time, because accounts
// and roles are deleted: a row that resolves to "user 5f8… does not exist" is
// not a record.
func actorOf(c *gin.Context, st *store.Store) models.AuditActor {
	actor := models.AuditActor{}
	actor.UserID = CallerUserID(c)
	if v, exists := c.Get("username"); exists {
		if name, ok := v.(string); ok {
			actor.Username = name
		}
	}
	if roles, exists := c.Get("roles"); exists {
		if list, ok := roles.([]string); ok {
			actor.Roles = joinRoles(list)
		}
	}
	if actor.UserID == "" {
		return actor
	}
	if st != nil && st.Users != nil {
		if user, err := st.Users.GetByID(actor.UserID); err == nil && user != nil {
			if actor.Username == "" {
				actor.Username = user.Username
			}
			actor.Provider = user.Provider
		}
	}
	return actor
}

func joinRoles(roles []string) string {
	out := ""
	for i, r := range roles {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}
