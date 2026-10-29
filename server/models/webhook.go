package models

import (
	"time"
)

// Channel kinds (2.4-6). The row is one ledger, not a table per destination: an
// operator reads "what reaches a person" in one place, and the delivery history
// stays comparable across kinds.
const (
	ChannelKindWebhook = "webhook"
	ChannelKindEmail   = "email"
	ChannelKindSlack   = "slack"
	ChannelKindTeams   = "teams"
)

// ChannelKinds is every kind a channel may have.
var ChannelKinds = []string{ChannelKindWebhook, ChannelKindEmail, ChannelKindSlack, ChannelKindTeams}

// ValidChannelKind reports whether kind is a channel kind.
func ValidChannelKind(kind string) bool {
	for _, k := range ChannelKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// SeverityRank orders severities so a channel's floor is a comparison, not a
// string match: a channel that wants criticals must also receive them when the
// event says "critical".
func SeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// ChannelNeedsURL states, in one place, which kinds are an HTTP endpoint. The
// API validates per kind, and the console builds its form from this rule.
func ChannelNeedsURL(kind string) bool {
	return kind != ChannelKindEmail
}

// Webhook represents a configured notification channel.
//
// v2.3.x called this a webhook and knew one destination shape. Since 2.4-6 the
// same row carries an email channel or a chat endpoint, and `URL` is
// meaningless for email — which is why it is nullable.
type Webhook struct {
	ID              string     `gorm:"type:uuid;primaryKey" json:"id"`
	Name            string     `gorm:"not null" json:"name"`
	URL             string     `gorm:"not null" json:"url"`
	Events          string     `gorm:"not null;default:'[]'" json:"events"`            // JSON array of event types
	Headers         string     `gorm:"type:text;not null;default:'{}'" json:"headers"` // JSON object
	Enabled         bool       `gorm:"not null;default:true" json:"enabled"`
	Secret          string     `gorm:"type:text" json:"-"` // HMAC signing secret (never exposed in API responses)
	LastError       string     `json:"last_error"`
	LastDeliveredAt *time.Time `json:"last_delivered_at"`

	// 2.4-6: the channel's kind, its per-kind settings, and its routing.
	Kind         string `gorm:"not null;default:'webhook'" json:"kind"`
	Config       string `gorm:"type:text;not null;default:'{}'" json:"config"`        // JSON: per-kind settings (email recipients, chat channel)
	ScopeClients string `gorm:"type:text;not null;default:'[]'" json:"scope_clients"` // JSON array of client ids; empty is every tenant
	MinSeverity  string `gorm:"type:text;not null;default:''" json:"min_severity"`    // '' is no floor; else info|warning|critical
	Tags         string `gorm:"type:text;not null;default:'[]'" json:"tags"`          // JSON array of tags; empty is every unit

	CreatedAt time.Time `json:"created_at"`
}

// WebhookDelivery tracks a delivery attempt.
type WebhookDelivery struct {
	ID           string     `gorm:"type:uuid;primaryKey" json:"id"`
	WebhookID    string     `gorm:"type:uuid;not null;index" json:"webhook_id"`
	Event        string     `gorm:"not null" json:"event"`
	Payload      string     `gorm:"type:text;not null" json:"payload"`
	Status       string     `gorm:"not null;default:pending" json:"status"` // pending, delivered, failed
	StatusCode   int        `json:"status_code"`
	ResponseBody string     `gorm:"type:text" json:"response_body"`
	Attempts     int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt  *time.Time `json:"next_retry_at"`
	DeliveredAt  *time.Time `json:"delivered_at"`
	CreatedAt    time.Time  `json:"created_at"`
}
