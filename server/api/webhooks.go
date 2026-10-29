package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/webhooks"
)

// WebhookHandler handles notification-channel management endpoints (2.4-6).
//
// The routes kept their v2.3 paths and verbs — a console or script written
// against webhooks keeps working — and the row gained a kind, per-kind settings,
// and routing. Validation is per kind, because a Slack endpoint with no URL and
// an email channel with a URL are both real configurations and both useless.
type WebhookHandler struct {
	store      *store.Store
	dispatcher *webhooks.Dispatcher
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(store *store.Store, dispatcher *webhooks.Dispatcher) *WebhookHandler {
	return &WebhookHandler{store: store, dispatcher: dispatcher}
}

// channelRequest is the shared create/update shape. Pointers mean "touched":
// the console's enable toggle sends one field and cannot wipe a routing rule by
// omission, the same rule 2.4-4 established for providers.
type channelRequest struct {
	Name         *string            `json:"name"`
	Kind         *string            `json:"kind"`
	URL          *string            `json:"url"`
	Events       *[]string          `json:"events"`
	Headers      *map[string]string `json:"headers"`
	Enabled      *bool              `json:"enabled"`
	Config       *map[string]any    `json:"config"`
	ScopeClients *[]string          `json:"scope_clients"`
	MinSeverity  *string            `json:"min_severity"`
	Tags         *[]string          `json:"tags"`
}

// channelProblem is a named refusal carried out of the shared validation.
type channelProblem struct {
	Status int
	Reason string
	Error  string
}

// Create creates a new channel.
func (h *WebhookHandler) Create(c *gin.Context) {
	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}

	row := &models.Webhook{
		ID:           uuid.New().String(),
		Name:         "channel",
		Kind:         models.ChannelKindWebhook,
		Events:       "[]",
		Headers:      "{}",
		Config:       "{}",
		ScopeClients: "[]",
		Tags:         "[]",
		Enabled:      true,
		Secret:       generateWebhookSecret(),
	}
	if problem := h.applyEdits(c, row, req, scope, true); problem != nil {
		c.JSON(problem.Status, gin.H{"error": problem.Error, "reason": problem.Reason})
		return
	}

	// A channel ships fleet data to an outside system, so creating one is a
	// data-egress decision: the record names the kind, the destination and the
	// routing, and never the signing secret.
	defer auditForAnswer(c, h.store, models.AuditWebhookCreate, AuditTarget{Type: "webhook", ID: row.ID}, channelAuditDetails(row))
	if err := h.store.Webhooks.Create(row); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create webhook"})
		return
	}

	// The secret is never returned by list/get/update (json:"-"); show it
	// exactly once, on creation, so the receiver can verify signatures.
	c.JSON(http.StatusCreated, gin.H{"webhook": row, "secret": row.Secret})
}

// applyEdits validates and applies a request to a row. Create and update share
// it, so no door skips a rule the other enforces (2.4-4's lesson).
func (h *WebhookHandler) applyEdits(c *gin.Context, row *models.Webhook, req channelRequest, scope clientScope, creating bool) *channelProblem {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return &channelProblem{http.StatusBadRequest, "name_required", "name is required"}
		}
		row.Name = name
	}

	if req.Kind != nil {
		kind := strings.TrimSpace(*req.Kind)
		if !creating {
			// The kind decides the payload shape and what the channel's delivery
			// history means. Retargeting a channel the operator is looking at is not
			// an edit; create the other kind as its own channel.
			if kind != row.Kind {
				return &channelProblem{http.StatusConflict, "kind_immutable",
					"kind cannot be changed: a channel that delivers a different payload is a different channel"}
			}
		}
		if !models.ValidChannelKind(kind) {
			return &channelProblem{http.StatusBadRequest, "kind_invalid",
				"kind must be webhook, email, slack or teams"}
		}
		row.Kind = kind
	}

	if req.Events != nil {
		events := cleanStrings(*req.Events)
		if len(events) == 0 {
			return &channelProblem{http.StatusBadRequest, "events_required",
				"events must name at least one event"}
		}
		row.Events = string(mustJSON(events))
	}

	if req.Headers != nil {
		row.Headers = string(mustJSON(*req.Headers))
	}

	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}

	if req.MinSeverity != nil {
		floor := strings.TrimSpace(*req.MinSeverity)
		if floor != "" && models.SeverityRank(floor) == 0 {
			return &channelProblem{http.StatusBadRequest, "min_severity_invalid",
				"min_severity must be empty, info, warning or critical"}
		}
		row.MinSeverity = floor
	}

	if req.Tags != nil {
		tags := cleanStrings(*req.Tags)
		for _, tag := range tags {
			if tag == "" {
				return &channelProblem{http.StatusBadRequest, "tag_empty", "tags must not be empty strings"}
			}
		}
		row.Tags = string(mustJSON(tags))
	}

	if req.ScopeClients != nil {
		if problem := h.applyScopeClients(c, row, *req.ScopeClients, scope); problem != nil {
			return problem
		}
	}

	// A channel that names no tenant receives every tenant's events. That is an
	// installation-wide act, not a tenant one.
	if row.ScopeClients == "[]" && !scope.All() && !creating {
		return &channelProblem{http.StatusForbidden, "admin_only_scope",
			"only an admin may define a channel that reaches every tenant"}
	}
	if row.ScopeClients == "[]" && !scope.All() && creating {
		return &channelProblem{http.StatusForbidden, "admin_only_scope",
			"only an admin may define a channel that reaches every tenant; name the tenants in scope_clients"}
	}

	if req.URL != nil {
		target := strings.TrimSpace(*req.URL)
		if models.ChannelNeedsURL(row.Kind) {
			if problem := validEndpointURL(target); problem != nil {
				return problem
			}
			row.URL = target
		} else {
			// An email channel has no endpoint. An address typed here is the
			// recipient in the wrong column, so say where it belongs.
			if target != "" {
				return &channelProblem{http.StatusBadRequest, "url_not_used",
					"an email channel has no url: name the recipients in config.to"}
			}
			row.URL = ""
		}
	} else if creating {
		if models.ChannelNeedsURL(row.Kind) {
			return &channelProblem{http.StatusBadRequest, "url_required",
				"url is required for a webhook, slack and teams channel"}
		}
		row.URL = ""
	}

	if req.Config != nil {
		encoded, err := json.Marshal(*req.Config)
		if err != nil {
			return &channelProblem{http.StatusBadRequest, "config_invalid", "config must be a JSON object"}
		}
		row.Config = string(encoded)
	}
	if problem := h.validateConfig(row); problem != nil {
		return problem
	}
	return nil
}

// applyScopeClients validates a channel's tenant routing. A tenant the caller
// cannot see answers the answer a tenant that does not exist gets: 2.3.0's rule,
// applied to a write.
func (h *WebhookHandler) applyScopeClients(c *gin.Context, row *models.Webhook, ids []string, scope clientScope) *channelProblem {
	cleaned := cleanStrings(ids)
	seen := map[string]bool{}
	for _, id := range cleaned {
		if id == "" {
			return &channelProblem{http.StatusBadRequest, "client_id_empty", "scope_clients must name client ids"}
		}
		if _, err := h.store.Clients.GetByID(id); err != nil {
			return &channelProblem{http.StatusNotFound, "client_not_found", "client not found"}
		}
		if !scope.AllowsClient(&id) {
			return &channelProblem{http.StatusNotFound, "client_not_found", "client not found"}
		}
		seen[id] = true
	}
	ordered := make([]string, 0, len(seen))
	for _, id := range cleaned {
		if seen[id] {
			ordered = append(ordered, id)
			delete(seen, id)
		}
	}
	row.ScopeClients = string(mustJSON(ordered))
	return nil
}

// validateConfig checks the per-kind settings, including the one fact the
// installation controls: an email channel needs a mail server.
func (h *WebhookHandler) validateConfig(row *models.Webhook) *channelProblem {
	config, err := webhooks.ParseChannelConfig(row.Config)
	if err != nil {
		return &channelProblem{http.StatusBadRequest, "config_invalid", err.Error()}
	}
	switch row.Kind {
	case models.ChannelKindEmail:
		if len(config.To) == 0 {
			return &channelProblem{http.StatusBadRequest, "recipients_required",
				"an email channel needs recipients: config.to"}
		}
		for _, address := range config.To {
			if !looksLikeAddress(address) {
				return &channelProblem{http.StatusBadRequest, "recipient_invalid",
					"config.to must hold mail addresses"}
			}
		}
		if !h.dispatcher.SMTPEnabled() {
			return &channelProblem{http.StatusBadRequest, "mail_not_configured",
				"this installation has no mail server: set SMTP_HOST and SMTP_PORT before adding an email channel"}
		}
	case models.ChannelKindWebhook, models.ChannelKindSlack, models.ChannelKindTeams:
		if len(config.To) > 0 {
			return &channelProblem{http.StatusBadRequest, "recipients_not_used",
				"config.to belongs to an email channel"}
		}
	}
	return nil
}

// generateWebhookSecret creates a random 32-byte hex HMAC signing secret.
func generateWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String() + uuid.New().String()
	}
	return hex.EncodeToString(b)
}

// channelAuditDetails states what a channel send ships, without the secret.
// channelAuditDetails is the routing record an audit entry carries: who is
// notified, how, and under what floor. The signing secret never appears, and a
// mail channel records the mailbox it was told to reach — the address is routing
// the admin set, not a credential.
func channelAuditDetails(row *models.Webhook) map[string]any {
	details := map[string]any{
		"name":          row.Name,
		"kind":          row.Kind,
		"url":           row.URL,
		"events":        row.Events,
		"min_severity":  row.MinSeverity,
		"scope_clients": row.ScopeClients,
		"tags":          row.Tags,
		"enabled":       row.Enabled,
	}
	if recipients := channelRecipients(row); len(recipients) > 0 {
		details["recipients"] = recipients
	}
	return details
}

// channelRecipients reads a mail channel's addresses out of its config.
func channelRecipients(row *models.Webhook) []string {
	config, err := webhooks.ParseChannelConfig(row.Config)
	if err != nil {
		return nil
	}
	return config.To
}

func validEndpointURL(target string) *channelProblem {
	if target == "" {
		return &channelProblem{http.StatusBadRequest, "url_required",
			"url is required for a webhook, slack and teams channel"}
	}
	parsed, err := url.Parse(target)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return &channelProblem{http.StatusBadRequest, "url_invalid",
			"url must be an absolute http(s) endpoint"}
	}
	return nil
}

func looksLikeAddress(address string) bool {
	address = strings.TrimSpace(address)
	at := strings.Index(address, "@")
	return at > 0 && at < len(address)-1 && !strings.ContainsAny(address, " \t,")
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

// List returns all channels. A manager sees the channels that can deliver their
// tenants' events: their own scoped channels, and no one else's.
func (h *WebhookHandler) List(c *gin.Context) {
	rows, err := h.store.Webhooks.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list webhooks"})
		return
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	visible := []models.Webhook{}
	for _, row := range rows {
		if h.visibleTo(&row, scope) {
			visible = append(visible, row)
		}
	}
	scope.ApplyScopeHeader(c)
	// The mail transport is a fact of the installation, not of any channel: the
	// console needs it to explain an email channel that cannot deliver, and it is
	// the API's own answer to "why is this channel refused?". Credentials are not
	// part of the summary.
	c.JSON(http.StatusOK, gin.H{"webhooks": visible, "mail": h.dispatcher.SMTPSummary()})
}

// visibleTo is the channel's tenancy rule: a channel that names tenants is
// readable by a caller whose tenants it covers; a channel that names none is
// installation-wide and belongs to the admin.
func (h *WebhookHandler) visibleTo(row *models.Webhook, scope clientScope) bool {
	if scope.All() {
		return true
	}
	var ids []string
	if json.Unmarshal([]byte(row.ScopeClients), &ids) != nil {
		return false
	}
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		id := id
		if !scope.AllowsClient(&id) {
			return false
		}
	}
	return true
}

// Get returns a single channel.
func (h *WebhookHandler) Get(c *gin.Context) {
	row, ok := h.channel(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook": row})
}

// channel resolves the caller and loads the channel, answering 404 for absent
// and out-of-scope identically — the 2.3.0 rule, on the channel routes too.
func (h *WebhookHandler) channel(c *gin.Context) (*models.Webhook, bool) {
	row, err := h.store.Webhooks.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return nil, false
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return nil, false
	}
	if !h.visibleTo(row, scope) {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return nil, false
	}
	scope.ApplyScopeHeader(c)
	return row, true
}

// Update updates an existing channel.
func (h *WebhookHandler) Update(c *gin.Context) {
	row, ok := h.channel(c)
	if !ok {
		return
	}

	var req channelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	if problem := h.applyEdits(c, row, req, scope, false); problem != nil {
		c.JSON(problem.Status, gin.H{"error": problem.Error, "reason": problem.Reason})
		return
	}

	defer auditForAnswer(c, h.store, models.AuditWebhookUpdate, AuditTarget{Type: "webhook", ID: row.ID}, channelAuditDetails(row))
	if err := h.store.Webhooks.Update(row); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update webhook"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook": row})
}

// Delete removes a channel.
func (h *WebhookHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	defer auditForAnswer(c, h.store, models.AuditWebhookDelete, AuditTarget{Type: "webhook", ID: id}, nil)
	if err := h.store.Webhooks.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "webhook deleted"})
}

// Test sends a test event through a channel.
func (h *WebhookHandler) Test(c *gin.Context) {
	row, ok := h.channel(c)
	if !ok {
		return
	}
	// A test event ships real fleet data to an outside destination, so it is an
	// act, not a page interaction.
	defer auditForAnswer(c, h.store, models.AuditWebhookTest, AuditTarget{Type: "webhook", ID: row.ID}, map[string]any{
		"kind": row.Kind,
	})
	if err := h.dispatcher.SendTestEvent(c.Request.Context(), row); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "reason": "test_refused"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "test event sent"})
}

// ListDeliveries returns delivery history for a channel.
func (h *WebhookHandler) ListDeliveries(c *gin.Context) {
	row, ok := h.channel(c)
	if !ok {
		return
	}
	deliveries, err := h.store.WebhookDeliveries.ListByWebhook(row.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deliveries"})
		return
	}
	// The ledger rows themselves do not record a kind — the channel does. Stamping
	// them here is what lets the console answer "this mailbox refused" and "this
	// endpoint refused" from one list, with nothing for the operator to infer.
	rows := make([]map[string]any, 0, len(deliveries))
	for _, delivery := range deliveries {
		rows = append(rows, map[string]any{
			"id":            delivery.ID,
			"webhook_id":    delivery.WebhookID,
			"kind":          row.Kind,
			"channel":       row.Name,
			"event":         delivery.Event,
			"payload":       delivery.Payload,
			"status":        delivery.Status,
			"status_code":   delivery.StatusCode,
			"response_body": delivery.ResponseBody,
			"attempts":      delivery.Attempts,
			"next_retry_at": delivery.NextRetryAt,
			"delivered_at":  delivery.DeliveredAt,
			"created_at":    delivery.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": rows})
}

// RetryDelivery retries a failed delivery.
func (h *WebhookHandler) RetryDelivery(c *gin.Context) {
	row, ok := h.channel(c)
	if !ok {
		return
	}
	// Re-delivering an event re-sends fleet data to an outside system, so it
	// shares the test verb: the ledger's filter list stays one verb per act.
	defer auditForAnswer(c, h.store, models.AuditWebhookTest, AuditTarget{Type: "webhook", ID: row.ID}, map[string]any{
		"kind": row.Kind,
	})
	deliveryID := c.Param("delivery_id")

	delivery, err := h.store.WebhookDeliveries.GetByID(deliveryID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "delivery not found"})
		return
	}

	// Reset for retry
	delivery.Status = "pending"
	delivery.Attempts = 0
	delivery.NextRetryAt = nil
	h.store.WebhookDeliveries.Update(delivery)

	h.dispatcher.RetryPending(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"message": "retry scheduled"})
}

func toJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
