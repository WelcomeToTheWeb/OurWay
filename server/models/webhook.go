package models

import (
	"time"
)

// Webhook represents a configured webhook endpoint.
type Webhook struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	URL         string    `gorm:"not null" json:"url"`
	Events      string    `gorm:"not null;default:'[]'" json:"events"` // JSON array of event types
	Headers     string    `gorm:"type:text;not null;default:'{}'" json:"headers"` // JSON object
	Enabled     bool      `gorm:"not null;default:true" json:"enabled"`
	Secret      string    `gorm:"type:text" json:"-"` // HMAC signing secret (never exposed in API responses)
	LastError   string    `json:"last_error"`
	LastDeliveredAt *time.Time `json:"last_delivered_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// WebhookDelivery tracks a webhook delivery attempt.
type WebhookDelivery struct {
	ID            string     `gorm:"type:uuid;primaryKey" json:"id"`
	WebhookID     string     `gorm:"type:uuid;not null;index" json:"webhook_id"`
	Event         string     `gorm:"not null" json:"event"`
	Payload       string     `gorm:"type:text;not null" json:"payload"`
	Status        string     `gorm:"not null;default:pending" json:"status"` // pending, delivered, failed
	StatusCode    int        `json:"status_code"`
	ResponseBody  string     `gorm:"type:text" json:"response_body"`
	Attempts      int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt   *time.Time `json:"next_retry_at"`
	DeliveredAt   *time.Time `json:"delivered_at"`
	CreatedAt     time.Time  `json:"created_at"`
}
