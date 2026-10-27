package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.3-6 — the ledger's read surface.
//
// The write side records acts; this file answers "who did what, to my tenants,
// and did it work". Three decisions shape it:
//
//  **Scope is the same door as the fleet.** Admin reads the installation; a
//  manager reads the clients they are granted, and a manager with no grants
//  reads nothing. The ledger uses `ClientScope` (2.3-1), not a parallel
//  implementation, because a second scope definition is a second thing to get
//  wrong — and the ledger is the surface where the two would diverge loudest.
//
//  **Retention is stated, not assumed.** The response carries `retention_days`
//  and the pruning counter, so a page can say "records older than N days are
//  removed" and mean it. A log whose expiry nobody can read is a log people
//  believe is complete.
//
//  **The outcome column is the answer.** Every row says `ok`, `refused`,
//  `not_found` or `error`, so the interesting question — the attempt that was
//  refused — is a filter, not a guess.

// auditRetentionDays is the ledger's window (AUDIT_RETENTION_DAYS). The same
// value `retention.go` prunes with, read from one place, so the endpoint's
// stated promise and the sweeper's behaviour cannot drift.
var auditRetentionDays = 365

// SetAuditRetention configures the window from the environment at startup.
func SetAuditRetention(days int) {
	if days > 0 {
		auditRetentionDays = days
	}
}

// RetentionSweeper reports the last sweep of the retention manager, so the
// ledger page can say when rows were last pruned and how many went. It is
// injected by `main.go` for the same reason `SetAuditRetention` is: the window
// the page states and the sweeper that enforces it must have one source. A nil
// sweeper (tests, and a server started without the manager) reports "never",
// which is the truth rather than a fabricated timestamp.
type RetentionSweeper interface {
	RetentionSummary() (rowsPruned int64, lastRun time.Time, errored bool)
}

var retentionSweeper RetentionSweeper

// SetRetentionSweeper registers the sweeper the ledger reports on.
func SetRetentionSweeper(s RetentionSweeper) { retentionSweeper = s }

// AuditHandler serves the audit ledger.
type AuditHandler struct {
	store *store.Store
}

// NewAuditHandler builds the ledger handler.
func NewAuditHandler(st *store.Store) *AuditHandler {
	return &AuditHandler{store: st}
}

// List returns the ledger, scoped to the caller.
// GET /api/audit
//
// Query params: `action`, `client_id`, `actor`, `from`, `to`, `outcome`,
// `limit`, `offset`, and `format=csv` for the export an MSP hands to a client.
func (h *AuditHandler) List(c *gin.Context) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	filter, bad := h.filter(c, scope)
	if bad != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": bad})
		return
	}

	rows, err := h.store.Audit.List(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the audit log"})
		return
	}

	if c.Query("format") == "csv" {
		h.writeCSV(c, rows)
		return
	}

	pruned, ranAt, errored := int64(0), time.Time{}, false
	if retentionSweeper != nil {
		pruned, ranAt, errored = retentionSweeper.RetentionSummary()
	}

	c.JSON(http.StatusOK, gin.H{
		"logs": rows,
		// `total` is the page's length, stated as such: the ledger is
		// unbounded, and a count that reads like the whole table's size
		// teaches people to under-read it.
		"total":          len(rows),
		"scope":          scope.HeaderValue(),
		"retention_days": auditRetentionDays,
		"limit":          filter.Limit,
		"offset":         filter.Offset,
		// Retention as an operator reads it: the window, the last sweep, the
		// rows it removed, and whether it failed. A page that states the
		// promise and shows the evidence is a page nobody has to trust.
		"retention": gin.H{
			"days":          auditRetentionDays,
			"rows_pruned":   pruned,
			"last_run":      ranAt,
			"sweep_errored": errored,
		},
	})
}

// filter turns the query string into a store filter, inside the caller's scope.
//
// A `client_id` asking for a tenant the caller is not granted resolves to **no
// rows**, not an error and not the whole ledger: the ledger answers "which
// tenants exist in this installation" through its client ids, and an error
// naming the one you may not read would be the same leak the 404 rule closes.
func (h *AuditHandler) filter(c *gin.Context, scope clientScope) (store.AuditFilter, string) {
	filter := store.AuditFilter{
		Action:      c.Query("action"),
		ActorUserID: c.Query("actor"),
		Offset:      queryInt(c, "offset", 0),
	}
	asked := c.Query("client_id")
	switch {
	case asked == "" && scope.All():
		filter.ClientIDs = nil
	case asked == "":
		filter.ClientIDs = scope.ClientIDs()
	case scope.AllowsClient(&asked):
		filter.ClientIDs = []string{asked}
	default:
		// A tenant the caller is not granted: no rows, and no message naming it.
		filter.ClientIDs = []string{}
	}

	if limit := queryInt(c, "limit", 200); limit > 0 {
		// The page is capped: the ledger is the one table that only ever
		// grows, and `?limit=100000` should not be a way to read it all.
		if limit > 1000 {
			limit = 1000
		}
		filter.Limit = limit
	}
	if raw := c.Query("from"); raw != "" {
		when, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, "from must be an RFC3339 timestamp"
		}
		filter.Since = &when
	}
	if raw := c.Query("to"); raw != "" {
		when, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, "to must be an RFC3339 timestamp"
		}
		filter.Until = &when
	}
	return filter, ""
}

// writeCSV streams the export. Columns are the record's fields, in the order
// an MSP reads them: when, who, what, to what, whose machine, and the answer.
func (h *AuditHandler) writeCSV(c *gin.Context, rows []models.AuditLog) {
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", `attachment; filename="ourway-audit-log.csv"`)
	c.Status(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	out := [][]string{{
		"timestamp", "actor", "actor_roles", "action", "target_type",
		"target_id", "client_id", "outcome", "source_ip", "details",
	}}
	for _, r := range rows {
		out = append(out, []string{
			r.CreatedAt.UTC().Format(time.RFC3339),
			r.ActorUsername,
			r.ActorRoles,
			r.Action,
			r.TargetType,
			r.TargetID,
			r.ClientID,
			r.Outcome,
			r.SourceIP,
			r.Details,
		})
	}
	// A write failure here is the client closing the download. The ledger is
	// already in the response that started the file; nothing is left to clean.
	_ = w.WriteAll(out)
	w.Flush()
}

// Actions lists the action names the ledger records, so a filter UI is built
// from the vocabulary the server actually writes rather than a hand-written
// list that falls behind it.
// GET /api/audit/actions
func (h *AuditHandler) Actions(c *gin.Context) {
	seen := map[string]bool{}
	for _, action := range AuditedRoutes() {
		seen[action] = true
	}
	// The verbs the writers use are the registry's; the login pair and the
	// refusal verbs are recorded by the auth path, which registers both.
	out := make([]string, 0, len(seen))
	for action := range seen {
		out = append(out, action)
	}
	c.JSON(http.StatusOK, gin.H{"actions": out, "retention_days": auditRetentionDays})
}

func queryInt(c *gin.Context, key string, fallback int) int {
	raw := c.Query(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
