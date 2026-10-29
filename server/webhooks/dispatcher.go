package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
)

// Event represents an event on the bus.
type Event struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// Dispatcher publishes events to notification channels.
//
// v2.4-6 generalizes it: one dispatcher, one delivery ledger, one retry loop, for
// an HTTP webhook, an email, a Slack endpoint and a Teams webhook. The kind
// decides how the payload is built and which sender carries it; routing, claim,
// backoff and the ledger are shared, because "what reached a person, and when"
// is one question.
type Dispatcher struct {
	store *store.Store
	http  *HTTPSender
	smtp  SMTP
}

// NewDispatcher creates a dispatcher for HTTP kinds, with no mail transport.
func NewDispatcher(store *store.Store) *Dispatcher {
	return &Dispatcher{store: store, http: NewHTTPSender()}
}

// NewDispatcherWithSMTP is NewDispatcher with the installation's mail transport.
// An email channel on a dispatcher without one states that at save time.
func NewDispatcherWithSMTP(store *store.Store, smtp SMTP) *Dispatcher {
	return &Dispatcher{store: store, http: NewHTTPSender(), smtp: smtp}
}

// SMTPEnabled states whether this installation can send mail, for the API's
// save-time refusal and the console's notice.
func (d *Dispatcher) SMTPEnabled() bool { return d.smtp.Enabled() }

// SMTPSummary is what the console may show about the mail transport: where mail
// goes and under which sender. Credentials are not part of it.
func (d *Dispatcher) SMTPSummary() map[string]any {
	return map[string]any{
		"configured": d.smtp.Enabled(),
		"host":       d.smtp.Host,
		"port":       d.smtp.Port,
		"sender":     d.smtp.From,
	}
}

// Publish routes an event to the channels that asked for it, then delivers.
//
// Routing happens **before** a delivery row is written. An event below a
// channel's severity floor, from a tenant the channel does not list, or from a
// unit carrying none of its tags produces no row at all — not a failed row the
// operator has to explain.
func (d *Dispatcher) Publish(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	channels, err := d.store.Webhooks.ListByEvent(event.Type)
	if err != nil {
		return
	}
	facts := d.Facts(event)

	for i := range channels {
		row := channels[i]
		channel := ChannelOf(&row)
		if ok, _ := Matches(channel, facts); !ok {
			continue
		}
		d.enqueue(ctx, &row, event)
	}
}

// enqueue prepares one send and records it before attempting it, so a send that
// dies with the process leaves a row saying it was attempted.
func (d *Dispatcher) enqueue(ctx context.Context, row *models.Webhook, event Event) {
	outbound, err := BuildOutbound(channelKind(row), row, OutboundEvent{
		Type: event.Type, Data: event.Data, Timestamp: event.Timestamp,
	})
	delivery := &models.WebhookDelivery{
		ID:        uuid.New().String(),
		WebhookID: row.ID,
		Event:     event.Type,
		Status:    "pending",
		Attempts:  0,
		CreatedAt: time.Now(),
	}
	if err != nil {
		// A channel that cannot be built is a configuration the operator can fix,
		// and the refusal is the record they need: the row is written failed, with
		// the reason, and not retried — retrying a misconfiguration is noise.
		delivery.Payload = string(marshalJSON(map[string]string{"error": err.Error()}))
		delivery.Status = "failed"
		delivery.ResponseBody = err.Error()
		delivery.Attempts = 1
		if createErr := d.store.WebhookDeliveries.Create(delivery); createErr == nil {
			log.Printf("webhooks: channel %s cannot deliver %s: %v", row.ID, event.Type, err)
		}
		return
	}
	delivery.Payload = string(outbound.Ledger)
	if err := d.store.WebhookDeliveries.Create(delivery); err != nil {
		return
	}
	go d.deliver(ctx, row, delivery)
}

// deliver sends one delivery. It claims the row first (pending → in_flight), so
// the first delivery and the retry loop can never send the same event twice.
func (d *Dispatcher) deliver(ctx context.Context, row *models.Webhook, delivery *models.WebhookDelivery) {
	claimed, err := d.store.WebhookDeliveries.ClaimPending(delivery.ID)
	if err != nil {
		log.Printf("webhooks: claim failed for %s: %v", delivery.ID, err)
		return
	}
	if !claimed {
		return
	}

	outcome := d.send(ctx, row, delivery)
	if outcome.OK() {
		d.markDelivered(delivery, outcome.StatusCode, outcome.Response)
		return
	}
	d.markFailed(delivery, outcome.Code(), outcome.Detail(), delivery.Attempts+1, outcome.Permanent)
}

// send delivers one row. It posts **the bytes the ledger holds**, never a fresh
// rendering of the event: a retry must ship what the first attempt shipped, and
// the record an operator reads is the record that went out.
//
// The webhook kind keeps v2.3's shape exactly — event JSON, custom headers, and
// the HMAC signature over those bytes.
func (d *Dispatcher) send(ctx context.Context, row *models.Webhook, delivery *models.WebhookDelivery) Outcome {
	if channelKind(row) == models.ChannelKindEmail {
		return d.sendEmail(row, delivery)
	}
	payload := []byte(delivery.Payload)
	req, err := http.NewRequest(http.MethodPost, row.URL, bytes.NewReader(payload))
	if err != nil {
		return Outcome{Err: err}
	}
	for k, v := range customHeaders(row) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OurWay-Event", delivery.Event)
	if channelKind(row) == models.ChannelKindWebhook && row.Secret != "" {
		req.Header.Set("X-OurWay-Signature", signPayload(row.Secret, payload))
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	outcome := d.http.Send(req)
	// A destination that answers 4xx refused the request itself. Retrying the same
	// bytes reproduces the same answer, so it ends on the first attempt.
	if outcome.StatusCode >= 400 && outcome.StatusCode < 500 {
		outcome.Permanent = true
	}
	return outcome
}

func (d *Dispatcher) sendEmail(row *models.Webhook, delivery *models.WebhookDelivery) Outcome {
	var mail struct {
		Subject string   `json:"subject"`
		Body    string   `json:"body"`
		To      []string `json:"to"`
	}
	if err := json.Unmarshal([]byte(delivery.Payload), &mail); err != nil {
		return Outcome{Err: fmt.Errorf("email payload: %w", err)}
	}
	config, err := ParseChannelConfig(row.Config)
	if err != nil {
		return Outcome{Err: err}
	}
	recipients := config.To
	if len(mail.To) > 0 {
		recipients = mail.To
	}
	return d.smtp.SendMail(&ChannelConfig{To: recipients, From: config.From}, mail.Subject, mail.Body)
}

// Facts resolves what routing matches against. The event names a unit; the unit
// carries the tenant and the tags. Publishers stay ignorant of routing, and a
// channel cannot be routed by a publisher's omission.
func (d *Dispatcher) Facts(event Event) RouteFacts {
	data := dataMap(event.Data)
	facts := RouteFacts{}
	if severity, ok := data["severity"].(string); ok {
		facts.Severity = severity
	}
	deviceID, _ := data["device_id"].(string)
	if deviceID == "" {
		return facts
	}
	device, err := d.store.Devices.GetByID(deviceID)
	if err != nil {
		return facts
	}
	facts.Tags = device.Tags
	if device.ClientID != nil {
		facts.ClientID = *device.ClientID
	}
	return facts
}

// SendTestEvent sends a test event through one channel, through the same path a
// real event takes: routing is applied, a delivery row is written, and the
// result lands in the ledger the operator reads. A test that produces no row is
// a test the operator cannot point at.
func (d *Dispatcher) SendTestEvent(ctx context.Context, row *models.Webhook) error {
	event := Event{
		Type: "test",
		Data: map[string]interface{}{
			"message":  "This is a test notification from OurWay",
			"severity": "info",
		},
		Timestamp: time.Now(),
	}
	channels, err := d.store.Webhooks.ListByEvent("test")
	if err != nil {
		return err
	}
	subscribed := false
	for i := range channels {
		if channels[i].ID == row.ID {
			subscribed = true
			break
		}
	}
	if !subscribed {
		// The test event is not on the channel's event list, so routing would
		// refuse it and no row would exist. Say so: a test that silently does
		// nothing is the outcome an operator reads as "it works".
		return fmt.Errorf("the channel does not subscribe to the test event")
	}
	d.enqueue(ctx, row, event)
	return nil
}

// markDelivered records a successful send.
func (d *Dispatcher) markDelivered(delivery *models.WebhookDelivery, statusCode int, body string) {
	delivery.Status = "delivered"
	delivery.StatusCode = statusCode
	delivery.ResponseBody = body
	delivery.Attempts++
	now := time.Now()
	delivery.DeliveredAt = &now
	d.store.WebhookDeliveries.Update(delivery)
	d.touchChannel(delivery.WebhookID, "")
}

// markFailed records a failed send and schedules the next attempt.
//
// **Found in 2.4-6:** the claim step moves a row to `in_flight`, and v2.3 left it
// there after a failure — so the row the retry loop looks for (`pending`) was
// never produced, and a refused send was recorded once and never retried. The
// status now returns to `pending` when a retry is scheduled, which is what makes
// the documented 1m/2m/4m/8m backoff a thing that happens.
func (d *Dispatcher) markFailed(delivery *models.WebhookDelivery, status string, body string, attempt int, permanent bool) {
	delivery.Attempts = attempt
	if status != "" {
		if code, err := strconv.Atoi(status); err == nil {
			delivery.StatusCode = code
		}
	}
	delivery.ResponseBody = body

	retryable := !permanent && attempt < 5
	if permanent {
		delivery.ResponseBody = body + " (not retried: the destination refused the request, or the installation is misconfigured)"
	}

	if retryable {
		// Exponential backoff: 1m, 2m, 4m, 8m (delivery terminates after the 5th attempt)
		backoff := time.Duration(1<<uint(attempt-1)) * time.Minute
		nextRetry := time.Now().Add(backoff)
		delivery.NextRetryAt = &nextRetry
		delivery.Status = "pending"
	} else {
		delivery.Status = "failed"
		delivery.NextRetryAt = nil
	}

	d.store.WebhookDeliveries.Update(delivery)
	if delivery.Status == "failed" {
		d.touchChannel(delivery.WebhookID, body)
	}
}

func (d *Dispatcher) touchChannel(channelID, lastError string) {
	row, err := d.store.Webhooks.GetByID(channelID)
	if err != nil {
		return
	}
	now := time.Now()
	row.LastDeliveredAt = &now
	row.LastError = lastError
	d.store.Webhooks.Update(row)
}

// RetryPending retries all pending deliveries that are due.
func (d *Dispatcher) RetryPending(ctx context.Context) {
	deliveries, err := d.store.WebhookDeliveries.FindPending()
	if err != nil {
		return
	}

	for _, delivery := range deliveries {
		row, err := d.store.Webhooks.GetByID(delivery.WebhookID)
		if err != nil {
			continue
		}
		go d.deliver(ctx, row, &delivery)
	}
}

// channelKind reads a channel's kind, defaulting a pre-2.4 row to webhook.
func channelKind(row *models.Webhook) string {
	if row.Kind == "" {
		return models.ChannelKindWebhook
	}
	return row.Kind
}

func marshalJSON(value interface{}) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}
