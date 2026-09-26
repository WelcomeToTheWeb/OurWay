package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/webhooks"
)

// WebhookHandler handles webhook management endpoints.
type WebhookHandler struct {
	store      *store.Store
	dispatcher *webhooks.Dispatcher
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(store *store.Store, dispatcher *webhooks.Dispatcher) *WebhookHandler {
	return &WebhookHandler{store: store, dispatcher: dispatcher}
}

// Create creates a new webhook.
func (h *WebhookHandler) Create(c *gin.Context) {
	var req struct {
		Name    string   `json:"name" binding:"required"`
		URL     string   `json:"url" binding:"required"`
		Events  []string `json:"events" binding:"required"`
		Headers map[string]string `json:"headers"`
		Enabled *bool    `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	webhook := &models.Webhook{
		ID:        uuid.New().String(),
		Name:      req.Name,
		URL:       req.URL,
		Events:    string(mustJSON(req.Events)),
		Headers:   "{}",
		Enabled:   true,
	}

	if req.Headers != nil {
		webhook.Headers = string(mustJSON(req.Headers))
	}

	if req.Enabled != nil {
		webhook.Enabled = *req.Enabled
	}

	if err := h.store.Webhooks.Create(webhook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create webhook"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"webhook": webhook})
}

// List returns all webhooks.
func (h *WebhookHandler) List(c *gin.Context) {
	webhooks, err := h.store.Webhooks.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list webhooks"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhooks": webhooks})
}

// Get returns a single webhook.
func (h *WebhookHandler) Get(c *gin.Context) {
	id := c.Param("id")
	webhook, err := h.store.Webhooks.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhook": webhook})
}

// Update updates an existing webhook.
func (h *WebhookHandler) Update(c *gin.Context) {
	id := c.Param("id")
	webhook, err := h.store.Webhooks.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	var req struct {
		Name    *string `json:"name"`
		URL     *string `json:"url"`
		Events  []string `json:"events"`
		Headers map[string]string `json:"headers"`
		Enabled *bool   `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		webhook.Name = *req.Name
	}
	if req.URL != nil {
		webhook.URL = *req.URL
	}
	if req.Events != nil {
		webhook.Events = string(mustJSON(req.Events))
	}
	if req.Headers != nil {
		webhook.Headers = string(mustJSON(req.Headers))
	}
	if req.Enabled != nil {
		webhook.Enabled = *req.Enabled
	}

	if err := h.store.Webhooks.Update(webhook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update webhook"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"webhook": webhook})
}

// Delete removes a webhook.
func (h *WebhookHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.Webhooks.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "webhook deleted"})
}

// Test sends a test event to the webhook.
func (h *WebhookHandler) Test(c *gin.Context) {
	id := c.Param("id")
	webhook, err := h.store.Webhooks.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "webhook not found"})
		return
	}

	if err := h.dispatcher.SendTestEvent(c.Request.Context(), webhook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "test event sent"})
}

// ListDeliveries returns delivery history for a webhook.
func (h *WebhookHandler) ListDeliveries(c *gin.Context) {
	id := c.Param("id")
	deliveries, err := h.store.WebhookDeliveries.ListByWebhook(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deliveries"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": deliveries})
}

// RetryDelivery retries a failed delivery.
func (h *WebhookHandler) RetryDelivery(c *gin.Context) {
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

	// Trigger retry
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
