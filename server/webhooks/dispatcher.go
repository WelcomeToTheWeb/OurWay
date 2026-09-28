package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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

// signPayload returns "sha256=<hex>" where <hex> is the HMAC-SHA256 of the
// payload under the webhook's secret. Receivers can verify the signature
// with the same HMAC to authenticate the event.
func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// deliver sends a single webhook delivery. It first claims the delivery
// (pending -> in_flight) so the initial delivery and the retry loop can
// never POST the same event to the target concurrently.
func (d *Dispatcher) deliver(ctx context.Context, webhook *models.Webhook, delivery *models.WebhookDelivery) {
	claimed, err := d.store.WebhookDeliveries.ClaimPending(delivery.ID)
	if err != nil {
		log.Printf("webhooks: claim failed for %s: %v", delivery.ID, err)
		return
	}
	if !claimed {
		// Another worker is already delivering (or finished) this row.
		return
	}

	payload := []byte(delivery.Payload)

	// Parse custom headers
	var headers map[string]string
	if webhook.Headers != "" && webhook.Headers != "{}" {
		json.Unmarshal([]byte(webhook.Headers), &headers)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhook.URL, bytes.NewReader(payload))
	if err != nil {
		d.markFailed(delivery, "", err.Error(), delivery.Attempts+1)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OurWay-Event", delivery.Event)
	if webhook.Secret != "" {
		req.Header.Set("X-OurWay-Signature", signPayload(webhook.Secret, payload))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.markFailed(delivery, "", err.Error(), delivery.Attempts+1)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.markDelivered(delivery, resp.StatusCode, string(body))
	} else {
		d.markFailed(delivery, strconv.Itoa(resp.StatusCode), string(body), delivery.Attempts+1)
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
		if code, err := strconv.Atoi(status); err == nil {
			delivery.StatusCode = code
		}
	}
	delivery.ResponseBody = body

	if attempt < 5 {
		// Exponential backoff: 1m, 2m, 4m, 8m (delivery terminates after the 5th attempt)
		backoff := time.Duration(1<<uint(attempt-1)) * time.Minute
		nextRetry := time.Now().Add(backoff)
		delivery.NextRetryAt = &nextRetry
	} else {
		delivery.Status = "failed"
		delivery.NextRetryAt = nil
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
