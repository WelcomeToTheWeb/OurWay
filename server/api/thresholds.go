package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/alerts"
	"ourway/server/models"
	"ourway/server/store"
)

// Threshold rules (2.4-5): the alert levels a metric is measured against, per
// scope. `server/alerts` resolves them device → tag → client → global, most
// specific wins, per metric independently.
type ThresholdHandler struct {
	store *store.Store
	// engine is invalidated through the rule store on write; the handler keeps
	// the reference so the preview can ask the engine the same question the
	// engine answers when a metric arrives.
	engine *alerts.Engine
}

// NewThresholdHandler creates the threshold-rules handler.
func NewThresholdHandler(st *store.Store, engine *alerts.Engine) *ThresholdHandler {
	return &ThresholdHandler{store: st, engine: engine}
}

// thresholdRequest is the rule an operator writes. Scope and metric are named
// explicitly: "the rule applies to this tag" is the product, and a UI that
// inferred it from a filter is a rule nobody can audit later.
type thresholdRequest struct {
	ScopeType          *string  `json:"scope_type"`
	ScopeValue         *string  `json:"scope_value"`
	Metric             *string  `json:"metric"`
	Warning            *float64 `json:"warning"`
	Critical           *float64 `json:"critical"`
	DedupWindowSeconds *int     `json:"dedup_window_seconds"`
	Enabled            *bool    `json:"enabled"`
}

// ListThresholds answers every rule, plus the built-in floor each metric falls
// back to. Admin and manager (managers see rules that touch their tenants, which
// includes the global floor — that is what their units are measured against when
// no tenant rule exists).
func (h *ThresholdHandler) ListThresholds(c *gin.Context) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	rows, err := h.store.Thresholds.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list threshold rules"})
		return
	}

	rules := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, h.ruleView(c, row, scope))
	}

	defaults := store.DefaultThresholds()
	floor := make([]gin.H, 0, 3)
	for _, metric := range []string{models.MetricCPU, models.MetricRAM, models.MetricDisk} {
		floor = append(floor, gin.H{
			"metric":   metric,
			"warning":  defaults[metric].Warning,
			"critical": defaults[metric].Critical,
		})
	}

	c.JSON(http.StatusOK, gin.H{"rules": rules, "defaults": floor})
}

// CreateThreshold stores a new rule.
//
// Managers act inside their tenants only: a rule for a tenant they cannot see is
// refused, and a rule for a tag (which reaches every tenant's units) or for the
// installation floor is admin-only.
func (h *ThresholdHandler) CreateThreshold(c *gin.Context) {
	var req thresholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}

	rule := models.AlertThreshold{
		ID:      uuid.NewString(),
		Enabled: true,
	}
	if problem, ok := h.applyThresholdEdits(c, &rule, &req, scope, false); !ok {
		c.JSON(problem.status, problem.body)
		return
	}

	if existing, err := h.findRule(rule.ScopeType, rule.ScopeValue, rule.Metric); err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "a rule for this scope and metric already exists: edit it instead of adding a second one",
			"reason": "rule_exists",
			// The rule to edit is named: the console's Add is how a duplicate
			// happens, and the answer has to point at the row to change.
			"id": existing.ID,
		})
		return
	}

	if err := h.store.Thresholds.Create(&rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save threshold rule"})
		return
	}
	defer h.audit(c, models.AuditThresholdCreate, rule)
	c.JSON(http.StatusCreated, gin.H{"rule": h.ruleView(c, rule, scope)})
}

// UpdateThreshold edits a rule in place. The scope is immutable: moving a rule
// from one device to another is a different rule, and silently retargeting one
// the operator is looking at is not an edit.
func (h *ThresholdHandler) UpdateThreshold(c *gin.Context) {
	id := c.Param("id")
	rule, err := h.store.Thresholds.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "threshold rule not found"})
		return
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	if problem, ok := h.canTouch(c, rule, scope); !ok {
		c.JSON(problem.status, problem.body)
		return
	}

	var req thresholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if problem, ok := h.applyThresholdEdits(c, rule, &req, scope, true); !ok {
		c.JSON(problem.status, problem.body)
		return
	}

	if err := h.store.Thresholds.Update(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save threshold rule"})
		return
	}
	defer h.audit(c, models.AuditThresholdUpdate, *rule)
	c.JSON(http.StatusOK, gin.H{"rule": h.ruleView(c, *rule, scope)})
}

// DeleteThreshold removes a rule. The metric falls back to the next-most specific
// rule, or to the built-in floor.
func (h *ThresholdHandler) DeleteThreshold(c *gin.Context) {
	id := c.Param("id")
	rule, err := h.store.Thresholds.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "threshold rule not found"})
		return
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	if problem, ok := h.canTouch(c, rule, scope); !ok {
		c.JSON(problem.status, problem.body)
		return
	}

	removed := *rule
	if err := h.store.Thresholds.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete threshold rule"})
		return
	}
	defer h.audit(c, models.AuditThresholdDelete, removed)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ResolveThresholds is the precedence preview: for one unit, per metric, what the
// engine will measure it against, and every rule that was in the running.
// "Why did this unit alert at 62?" is answered here without reading the ledger.
func (h *ThresholdHandler) ResolveThresholds(c *gin.Context) {
	// The preview is the one door that asks "what applies to **this unit**", so
	// it walks the same scoped door every by-device route walks: absent and
	// out-of-scope answer the same body, and the scope is stated on the response.
	// The preview and the simulation name the unit in a query parameter (they are
	// reads over a collection, not routes under a unit), so the unit id is placed
	// where the shared scoped door reads it. One door, one set of answers —
	// including "absent" and "not yours" answering the same body.
	c.Params = append(c.Params, gin.Param{Key: "id", Value: c.Query("device")})
	device, ok := ScopedDevice(c, h.store)
	if !ok {
		return
	}

	rules, err := h.store.Thresholds.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read threshold rules"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device":  gin.H{"id": device.ID, "name": device.Name, "tags": device.Tags, "client_id": device.ClientID},
		"metrics": h.chainFor(device, rules),
	})
}

// SimulateThreshold is the console's simulation input (2.4-5): a metric value on
// a unit, and what the resolved rule makes of it — severity, the threshold named
// in the message, and the message itself. A read, so it is a GET: it changes
// nothing and fires nothing.
func (h *ThresholdHandler) SimulateThreshold(c *gin.Context) {
	deviceID := c.Query("device")
	metric := strings.ToLower(strings.TrimSpace(c.Query("metric")))
	value, err := strconv.ParseFloat(c.Query("value"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value must be a number"})
		return
	}
	if !models.ValidMetric(metric) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metric must be cpu, ram or disk"})
		return
	}
	// Same scoped door as the preview (see `ResolveThresholds`).
	c.Params = append(c.Params, gin.Param{Key: "id", Value: deviceID})
	device, ok := ScopedDevice(c, h.store)
	if !ok {
		return
	}

	resolved, err := h.store.Thresholds.Resolve(device.ID, device.Tags, clientIDOf(device), metric)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve thresholds"})
		return
	}

	severity, threshold, message := "", 0.0, ""
	label := metricLabel(metric)
	switch {
	case value > resolved.Critical:
		severity, threshold = "critical", resolved.Critical
		message = metricMessage(label, value, threshold, "high")
	case value > resolved.Warning:
		severity, threshold = "warning", resolved.Warning
		message = metricMessage(label, value, threshold, "elevated")
	}

	c.JSON(http.StatusOK, gin.H{
		"device_id":   device.ID,
		"metric":      metric,
		"value":       value,
		"warning":     resolved.Warning,
		"critical":    resolved.Critical,
		"scope_type":  resolved.ScopeType,
		"scope_value": resolved.ScopeValue,
		"scope_name":  ruleName(resolved.ScopeType, resolved.ScopeValue, device.Name),
		"source":      resolved.Source,
		"would_fire":  severity != "",
		"severity":    severity,
		"threshold":   threshold,
		"message":     message,
	})
}

// chainFor lists, per metric, the rules that apply to this unit in precedence
// order, marking the one that wins.
//
// The order comes from the store's own precedence function (`RuleWins`), not a
// copy of it: a preview that sorts by its own idea of specificity is a chain the
// operator can read and the engine contradicts.
func (h *ThresholdHandler) chainFor(device *models.Device, rules []models.AlertThreshold) []gin.H {
	tagSet := map[string]bool{}
	for _, tag := range device.Tags {
		tagSet[tag] = true
	}
	defaults := store.DefaultThresholds()

	out := []gin.H{}
	for _, metric := range []string{models.MetricCPU, models.MetricRAM, models.MetricDisk} {
		matching := []models.AlertThreshold{}
		for _, rule := range rules {
			if rule.Metric != metric || !rule.Enabled {
				continue
			}
			if !ruleAppliesTo(rule, device, tagSet) {
				continue
			}
			matching = append(matching, rule)
		}
		sort.SliceStable(matching, func(i, j int) bool {
			return store.RuleWins(&matching[i], &matching[j])
		})

		candidates := []gin.H{}
		for _, rule := range matching {
			candidates = append(candidates, gin.H{
				"id":          rule.ID,
				"scope_type":  rule.ScopeType,
				"scope_value": rule.ScopeValue,
				"scope_name":  ruleName(rule.ScopeType, rule.ScopeValue, device.Name),
				"warning":     rule.Warning,
				"critical":    rule.Critical,
				"rank":        models.ScopeRank(rule.ScopeType),
				"source":      "rule",
			})
		}
		// The floor is listed last, always: "no rule applies" is an answer, and a
		// chain that ends at the built-in default states it.
		candidates = append(candidates, gin.H{
			"id":          "",
			"scope_type":  models.ScopeGlobal,
			"scope_value": "",
			"scope_name":  globalScopeName,
			"warning":     defaults[metric].Warning,
			"critical":    defaults[metric].Critical,
			"rank":        0,
			"source":      "built-in default",
		})
		winner := 0
		if len(matching) == 0 {
			winner = len(candidates) - 1
		}
		out = append(out, gin.H{
			"metric":     metric,
			"candidates": candidates,
			"applies":    candidates[winner],
		})
	}
	return out
}

func warningOf(view gin.H) float64 {
	v, _ := view["warning"].(float64)
	return v
}

func ruleAppliesTo(rule models.AlertThreshold, device *models.Device, tagSet map[string]bool) bool {
	switch rule.ScopeType {
	case models.ScopeDevice:
		return rule.ScopeValue == device.ID
	case models.ScopeTag:
		return tagSet[rule.ScopeValue]
	case models.ScopeClient:
		return device.ClientID != nil && rule.ScopeValue == *device.ClientID
	case models.ScopeGlobal:
		return true
	default:
		return false
	}
}

// globalScopeName is the phrase the ledger shows for a rule that names no
// scope. `Thresholds.test.tsx` requires the console's English bundle to carry the
// same phrase, and the page displays its own translation of it.
const globalScopeName = "every unit"

// ruleView decorates a stored rule with the name of what it points at: a rule
// naming a device id and a rule naming a tenant id are unreadable in a ledger
// without their names.
func (h *ThresholdHandler) ruleView(c *gin.Context, rule models.AlertThreshold, scope clientScope) gin.H {
	view := gin.H{
		"id":                   rule.ID,
		"scope_type":           rule.ScopeType,
		"scope_value":          rule.ScopeValue,
		"metric":               rule.Metric,
		"warning":              rule.Warning,
		"critical":             rule.Critical,
		"dedup_window_seconds": rule.DedupWindowSeconds,
		"enabled":              rule.Enabled,
		"created_at":           rule.CreatedAt,
		"updated_at":           rule.UpdatedAt,
		"editable":             true,
	}
	switch rule.ScopeType {
	case models.ScopeDevice:
		if device, err := h.store.Devices.GetByID(rule.ScopeValue); err == nil {
			view["scope_name"] = device.Name
			view["editable"] = scope.AllowsDevice(device)
		}
	case models.ScopeClient:
		if client, err := h.store.Clients.GetByID(rule.ScopeValue); err == nil {
			view["scope_name"] = client.Name
			view["editable"] = scope.AllowsClient(&rule.ScopeValue)
		}
	case models.ScopeTag:
		view["scope_name"] = rule.ScopeValue
	case models.ScopeGlobal:
		view["scope_name"] = globalScopeName
		view["editable"] = hasRole(c, RoleAdmin)
	}
	return view
}

// ruleName names what a resolved rule points at, for a read that has no rule
// row to look up. A device rule on a unit read names that unit.
func ruleName(scopeType, scopeValue, deviceName string) string {
	switch scopeType {
	case models.ScopeDevice:
		return deviceName
	case models.ScopeGlobal:
		return globalScopeName
	default:
		return scopeValue
	}
}

// scopeProblem is a named refusal, carried out of the validation helpers.
type scopeProblem struct {
	status int
	body   gin.H
}

// applyThresholdEdits validates and applies a request onto a rule row. It is the
// door for every value the resolver can act on: a rule stored with a warning
// above its critical is a rule that can never fire critical, and a rule naming a
// metric the engine never evaluates is a rule that never fires at all.
func (h *ThresholdHandler) applyThresholdEdits(c *gin.Context, rule *models.AlertThreshold, req *thresholdRequest, scope clientScope, editing bool) (scopeProblem, bool) {
	problem := scopeProblem{status: http.StatusBadRequest, body: gin.H{}}
	fail := func(status int, format string, args ...any) (scopeProblem, bool) {
		return scopeProblem{status: status, body: gin.H{"error": fmt.Sprintf(format, args...)}}, false
	}

	if req.ScopeType != nil {
		scopeType := strings.ToLower(strings.TrimSpace(*req.ScopeType))
		if !models.ValidScopeType(scopeType) {
			return fail(http.StatusBadRequest, "scope_type must be device, tag, client or global")
		}
		if scopeType != rule.ScopeType && rule.ID != "" && rule.ScopeType != "" && rule.ScopeValue != "" && editing {
			return fail(http.StatusConflict, "scope cannot be changed: a rule that points at another unit is a different rule")
		}
		if (scopeType == models.ScopeGlobal || scopeType == models.ScopeTag) && !scope.All() {
			return fail(http.StatusForbidden, "a %s rule reaches every tenant: only an admin can define it", scopeType)
		}
		rule.ScopeType = scopeType
	}
	if rule.ScopeType == "" {
		return fail(http.StatusBadRequest, "scope_type is required")
	}
	if req.ScopeValue != nil {
		value := strings.TrimSpace(*req.ScopeValue)
		if rule.ScopeType == models.ScopeGlobal {
			value = ""
		}
		if value == "" && rule.ScopeType != models.ScopeGlobal {
			return fail(http.StatusBadRequest, "scope_value is required for a %s rule", rule.ScopeType)
		}
		if problem, ok := h.scopeValueExists(rule.ScopeType, value, scope); !ok {
			return problem, false
		}
		rule.ScopeValue = value
	}
	if rule.ScopeType != models.ScopeGlobal && rule.ScopeValue == "" {
		return fail(http.StatusBadRequest, "scope_value is required for a %s rule", rule.ScopeType)
	}

	if req.Metric != nil {
		metric := strings.ToLower(strings.TrimSpace(*req.Metric))
		if !models.ValidMetric(metric) {
			return fail(http.StatusBadRequest, "metric must be cpu, ram or disk")
		}
		rule.Metric = metric
	}
	if rule.Metric == "" {
		return fail(http.StatusBadRequest, "metric is required")
	}

	if req.Warning != nil {
		rule.Warning = *req.Warning
	}
	if req.Critical != nil {
		rule.Critical = *req.Critical
	}
	if rule.Warning <= 0 || rule.Critical <= 0 {
		return fail(http.StatusBadRequest, "warning and critical are required percentages")
	}
	if rule.Warning >= rule.Critical {
		return fail(http.StatusBadRequest, "warning must be below critical: a rule that warns at 90 and alerts at 85 can never alert")
	}
	if rule.Warning > 100 || rule.Critical > 100 {
		return fail(http.StatusBadRequest, "warning and critical are percentages of a metric (0-100)")
	}

	if req.DedupWindowSeconds != nil {
		window := *req.DedupWindowSeconds
		if window < 0 || window > 86400 {
			return fail(http.StatusBadRequest, "dedup_window_seconds must be between 0 (the engine default) and 86400")
		}
		rule.DedupWindowSeconds = window
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	return problem, true
}

// scopeValueExists refuses a rule that points at nothing: a rule naming a device
// id that is not in the installation is a rule that never fires, and a manager
// naming a tenant they cannot see is the 2.3.0 boundary, not a detail.
func (h *ThresholdHandler) scopeValueExists(scopeType, value string, scope clientScope) (scopeProblem, bool) {
	problem := scopeProblem{status: http.StatusBadRequest, body: gin.H{}}
	switch scopeType {
	case models.ScopeDevice:
		device, err := h.store.Devices.GetByID(value)
		if err != nil {
			return scopeProblem{status: http.StatusNotFound, body: gin.H{"error": "device not found"}}, false
		}
		if !scope.AllowsDevice(device) {
			return scopeProblem{status: http.StatusNotFound, body: gin.H{"error": "device not found"}}, false
		}
	case models.ScopeClient:
		client, err := h.store.Clients.GetByID(value)
		if err != nil {
			return scopeProblem{status: http.StatusNotFound, body: gin.H{"error": "client not found"}}, false
		}
		if !scope.AllowsClient(&client.ID) {
			// The same body a client that does not exist gets: a manager cannot
			// define a threshold for a tenant they cannot see (2.4-5).
			return scopeProblem{status: http.StatusNotFound, body: gin.H{"error": "client not found"}}, false
		}
	case models.ScopeTag:
		if strings.TrimSpace(value) == "" {
			return scopeProblem{status: http.StatusBadRequest, body: gin.H{"error": "a tag rule names a tag"}}, false
		}
	}
	return problem, true
}

// canTouch is the write boundary on an existing rule.
func (h *ThresholdHandler) canTouch(c *gin.Context, rule *models.AlertThreshold, scope clientScope) (scopeProblem, bool) {
	problem := scopeProblem{status: http.StatusOK, body: gin.H{}}
	if scope.All() {
		return problem, true
	}
	switch rule.ScopeType {
	case models.ScopeDevice:
		device, err := h.store.Devices.GetByID(rule.ScopeValue)
		if err != nil || !scope.AllowsDevice(device) {
			return scopeProblem{status: http.StatusForbidden, body: gin.H{
				"error":  "this rule points at a unit outside your tenants",
				"reason": "out_of_scope",
			}}, false
		}
	case models.ScopeClient:
		if !scope.AllowsClient(&rule.ScopeValue) {
			return scopeProblem{status: http.StatusForbidden, body: gin.H{
				"error":  "this rule points at a tenant outside your tenants",
				"reason": "out_of_scope",
			}}, false
		}
	default:
		// A tag or global rule reaches every tenant.
		return scopeProblem{status: http.StatusForbidden, body: gin.H{
			"error":  "only an admin can edit a rule of this scope",
			"reason": "admin_only_scope",
		}}, false
	}
	return problem, true
}

// findRule looks for a rule on the same (scope, metric) pair.
func (h *ThresholdHandler) findRule(scopeType, scopeValue, metric string) (*models.AlertThreshold, error) {
	rows, err := h.store.Thresholds.List()
	if err != nil {
		return nil, err
	}
	for i := range rows {
		row := &rows[i]
		if row.ScopeType == scopeType && row.ScopeValue == scopeValue && row.Metric == metric {
			return row, nil
		}
	}
	return nil, nil
}

// audit records a rule change. Values, scope and metric are the facts an operator
// needs later; there is nothing secret in a threshold, so the row states them.
func (h *ThresholdHandler) audit(c *gin.Context, action string, rule models.AlertThreshold) {
	target := AuditTarget{Type: "alert-threshold", ID: rule.ID}
	auditForAnswer(c, h.store, action, target, map[string]any{
		"scope_type":           rule.ScopeType,
		"scope_value":          rule.ScopeValue,
		"metric":               rule.Metric,
		"warning":              rule.Warning,
		"critical":             rule.Critical,
		"dedup_window_seconds": rule.DedupWindowSeconds,
		"enabled":              rule.Enabled,
	})
}

func clientIDOf(device *models.Device) string {
	if device.ClientID != nil {
		return *device.ClientID
	}
	return ""
}

func metricLabel(metric string) string {
	switch metric {
	case models.MetricCPU:
		return "CPU usage"
	case models.MetricRAM:
		return "RAM usage"
	default:
		return "Disk usage"
	}
}

// metricMessage is the engine's own message shape, so a simulated alert reads
// exactly like the alert that would be written.
func metricMessage(label string, value, threshold float64, word string) string {
	return label + " " + word + ": " + formatPercent(value) + "% (threshold: " + formatWhole(threshold) + "%)"
}

func formatPercent(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
func formatWhole(v float64) string   { return strconv.FormatFloat(v, 'f', 0, 64) }
