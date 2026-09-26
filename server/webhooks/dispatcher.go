package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
)

// Event represents a webhook event.
type Event struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// Dispatcher publishes events to webhook subscribers.
type Dispatcher struct {
	store  *store.Store
	client *http.Client
}

// NewDispatcher creates a new webhook dispatcher.
func NewDispatcher(store *store.Store) *Dispatcher {
	return &Dispatcher{
		store: store,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Publish sends an event to all subscribed webhooks.
func (d *Dispatcher) Publish(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	webhooks, err := d.store.Webhooks.ListByEvent(event.Type)
	if err != nil {
		return
	}

	for _, webhook := range webhooks {
		payload, _ := json.Marshal(event)

		delivery := &models.WebhookDelivery{
			ID:        uuid.New().String(),
			WebhookID: webhook.ID,
			Event:     event.Type,
			Payload:   string(payload),
			Status:    "pending",
			Attempts:  0,
			CreatedAt: time.Now(),
		}

		if err := d.store.WebhookDeliveries.Create(delivery); err != nil {
			continue
		}

		// Deliver asynchronously
		go d.deliver(ctx, &webhook, delivery)
	}
}

// deliver sends a single webhook delivery.
func (d *Dispatcher) deliver(ctx context.Context, webhook *models.Webhook, delivery *models.WebhookDelivery) {
	payload := []byte(delivery.Payload)

	// Parse custom headers
	var headers map[string]string
	if webhook.Headers != "" && webhook.Headers != "{}" {
		json.Unmarshal([]byte(webhook.Headers), &headers)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhook.URL, bytes.NewReader(payload))
	if err != nil {
		d.markFailed(delivery, "", err.Error(), 1)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OurWay-Event", delivery.Event)
	req.Header.Set("X-OurWay-Signature", "") // TODO: add HMAC signature
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.markFailed(delivery, "", err.Error(), 1)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.markDelivered(delivery, resp.StatusCode, string(body))
	} else {
		d.markFailed(delivery, fmt.Sprintf("HTTP %d", resp.StatusCode), string(body), 1)
	}
}

// markDelivered marks a delivery as successful.
func (d *Dispatcher) markDelivered(delivery *models.WebhookDelivery, statusCode int, body string) {
	delivery.Status = "delivered"
	delivery.StatusCode = statusCode
	delivery.ResponseBody = body
	delivery.Attempts++
	now := time.Now()
	delivery.DeliveredAt = &now
	d.store.WebhookDeliveries.Update(delivery)
}

// markFailed marks a delivery as failed and schedules a retry.
func (d *Dispatcher) markFailed(delivery *models.WebhookDelivery, status string, body string, attempt int) {
	delivery.Attempts = attempt
	if status != "" {
		delivery.StatusCode, _ = parseInt(status)
	}
	delivery.ResponseBody = body

	if attempt < 5 {
		// Exponential backoff: 1m, 2m, 4m, 8m, 16m
		backoff := time.Duration(1<<uint(attempt)) * time.Minute
		nextRetry := time.Now().Add(backoff)
		delivery.NextRetryAt = &nextRetry
	} else {
		delivery.Status = "failed"
	}

	d.store.WebhookDeliveries.Update(delivery)
}

// RetryPending retries all pending deliveries that are due.
func (d *Dispatcher) RetryPending(ctx context.Context) {
	deliveries, err := d.store.WebhookDeliveries.FindPending()
	if err != nil {
		return
	}

	for _, delivery := range deliveries {
		webhook, err := d.store.Webhooks.GetByID(delivery.WebhookID)
		if err != nil {
			continue
		}

		go d.deliver(ctx, webhook, &delivery)
	}
}

// SendTestEvent sends a test event to a specific webhook.
func (d *Dispatcher) SendTestEvent(ctx context.Context, webhook *models.Webhook) error {
	event := Event{
		Type:      "test",
		Data:      map[string]string{"message": "This is a test webhook from OurWay"},
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(event)
	req, err := http.NewRequestWithContext(ctx, "POST", webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OurWay-Event", "test")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n, nil
}
