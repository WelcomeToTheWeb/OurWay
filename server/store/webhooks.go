package store

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// WebhookStore provides CRUD operations for webhooks.
type WebhookStore struct {
	db *gorm.DB
}

// NewWebhookStore creates a new webhook store.
func NewWebhookStore(db *gorm.DB) *WebhookStore {
	return &WebhookStore{db: db}
}

// Create stores a new webhook.
func (s *WebhookStore) Create(webhook *models.Webhook) error {
	return s.db.Create(webhook).Error
}

// GetByID retrieves a webhook by ID.
func (s *WebhookStore) GetByID(id string) (*models.Webhook, error) {
	var webhook models.Webhook
	err := s.db.First(&webhook, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}

// ListAll returns all webhooks.
func (s *WebhookStore) ListAll() ([]models.Webhook, error) {
	var webhooks []models.Webhook
	err := s.db.Order("created_at DESC").Find(&webhooks).Error
	return webhooks, err
}

// Update updates an existing webhook.
func (s *WebhookStore) Update(webhook *models.Webhook) error {
	return s.db.Save(webhook).Error
}

// Delete removes a webhook.
func (s *WebhookStore) Delete(id string) error {
	return s.db.Delete(&models.Webhook{}, "id = ?", id).Error
}

// ListByEvent returns webhooks subscribed to a specific event. The event
// list is stored as a JSON array string; membership is checked exactly in
// Go rather than with a SQL LIKE on the serialized array, so
// "device_online" never substring-matches a "device_online_v2"
// subscription (M12).
func (s *WebhookStore) ListByEvent(event string) ([]models.Webhook, error) {
	var all []models.Webhook
	if err := s.db.Where("enabled = ?", true).Find(&all).Error; err != nil {
		return nil, err
	}

	var out []models.Webhook
	for _, w := range all {
		var events []string
		if json.Unmarshal([]byte(w.Events), &events) != nil {
			continue
		}
		for _, e := range events {
			if e == event {
				out = append(out, w)
				break
			}
		}
	}
	return out, nil
}

// WebhookDeliveryStore provides operations for webhook deliveries.
type WebhookDeliveryStore struct {
	db *gorm.DB
}

// NewWebhookDeliveryStore creates a new webhook delivery store.
func NewWebhookDeliveryStore(db *gorm.DB) *WebhookDeliveryStore {
	return &WebhookDeliveryStore{db: db}
}

// Create stores a new webhook delivery.
func (s *WebhookDeliveryStore) Create(delivery *models.WebhookDelivery) error {
	return s.db.Create(delivery).Error
}

// GetByID retrieves a webhook delivery by ID.
func (s *WebhookDeliveryStore) GetByID(id string) (*models.WebhookDelivery, error) {
	var delivery models.WebhookDelivery
	err := s.db.First(&delivery, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &delivery, nil
}

// ListByWebhook returns deliveries for a webhook.
func (s *WebhookDeliveryStore) ListByWebhook(webhookID string) ([]models.WebhookDelivery, error) {
	var deliveries []models.WebhookDelivery
	err := s.db.Where("webhook_id = ?", webhookID).Order("created_at DESC").Limit(50).Find(&deliveries).Error
	return deliveries, err
}

// Update updates an existing delivery.
func (s *WebhookDeliveryStore) Update(delivery *models.WebhookDelivery) error {
	return s.db.Save(delivery).Error
}

// FindPending returns pending deliveries that are due for retry. Rows whose
// next_retry_at is NULL get a 60s grace period based on created_at so a
// delivery that was just persisted (e.g. right after its first attempt
// failed) is not immediately re-picked up by the retry loop in the same
// second.
func (s *WebhookDeliveryStore) FindPending() ([]models.WebhookDelivery, error) {
	var deliveries []models.WebhookDelivery
	now := time.Now()
	graceCutoff := now.Add(-60 * time.Second)
	err := s.db.Where(
		"status = ? AND ((next_retry_at IS NULL AND created_at <= ?) OR next_retry_at <= ?)",
		"pending", graceCutoff, now,
	).Find(&deliveries).Error
	return deliveries, err
}

// ClaimPending atomically marks a pending delivery as in-flight so that the
// first-delivery goroutine and the retry loop can never deliver the same
// row concurrently. Returns true if this caller won the claim.
func (s *WebhookDeliveryStore) ClaimPending(id string) (bool, error) {
	res := s.db.Model(&models.WebhookDelivery{}).
		Where("id = ? AND status = ?", id, "pending").
		Update("status", "in_flight")
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
