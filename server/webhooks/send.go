package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ourway/server/i18n"
	"ourway/server/models"
)

// OutboundEvent is one event on its way to a channel. It is the shape stored in
// the delivery row, the shape signed for a webhook, and the shape a chat sender
// turns into a card.
type OutboundEvent struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// Outbound is the prepared send for one channel: what goes on the wire, and what
// the delivery ledger records as the payload. They are the same bytes for every
// HTTP kind; for an email the ledger records the message the person received,
// not the SMTP conversation.
type Outbound struct {
	Payload     []byte
	Ledger      []byte
	ContentType string
}

// BuildOutbound prepares an event for one channel kind.
//
// The webhook kind sends the event verbatim, as it always has — a receiver
// written against v2.3 keeps parsing the same bytes. Slack and Teams get the
// same event, wrapped in the card their endpoints expect, with the alert's own
// words so a person reading the notification does not have to open the console
// to know what happened.
func BuildOutbound(kind string, channel *models.Webhook, event OutboundEvent) (*Outbound, error) {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	switch kind {
	case models.ChannelKindWebhook:
		return &Outbound{Payload: eventJSON, Ledger: eventJSON, ContentType: "application/json"}, nil
	case models.ChannelKindSlack:
		body, err := slackPayload(channel, event)
		if err != nil {
			return nil, err
		}
		return &Outbound{Payload: body, Ledger: body, ContentType: "application/json"}, nil
	case models.ChannelKindTeams:
		body, err := teamsPayload(channel, event)
		if err != nil {
			return nil, err
		}
		return &Outbound{Payload: body, Ledger: body, ContentType: "application/json"}, nil
	case models.ChannelKindEmail:
		config, err := ParseChannelConfig(channel.Config)
		if err != nil {
			return nil, err
		}
		if len(config.To) == 0 {
			return nil, fmt.Errorf("the email channel names no recipient (config.to)")
		}
		locale := config.Locale
		if locale == "" {
			locale = i18n.FallbackLocale
		}
		subject, text := RenderEmail(locale, event, config.To)
		// The ledger records the message the person received — subject, body, and
		// who it went to — not the SMTP conversation, which is the transport's
		// business. It is also the exact record a retry re-sends.
		record, err := json.Marshal(map[string]interface{}{
			"subject": subject, "body": text, "to": config.To,
			"locale": locale,
		})
		if err != nil {
			return nil, err
		}
		return &Outbound{Payload: record, Ledger: record, ContentType: "text/plain; charset=utf-8"}, nil
	default:
		return nil, fmt.Errorf("unknown channel kind %q", kind)
	}
}

// Summary is the one-line statement a chat card carries: the event, the unit it
// is about, and the event's own words. It is built from the payload the engine
// published, so a notification cannot say something the alert does not.
func Summary(event OutboundEvent) (title string, lines []string) {
	data := dataMap(event.Data)
	title = event.Type
	if message, _ := data["message"].(string); message != "" {
		title = message
	}
	for _, key := range []string{"device", "severity", "metric", "value", "threshold", "client", "user", "name"} {
		if value, ok := data[key]; ok && value != nil && fmt.Sprint(value) != "" {
			lines = append(lines, fmt.Sprintf("%s: %v", key, value))
		}
	}
	return title, lines
}

func dataMap(data interface{}) map[string]interface{} {
	switch typed := data.(type) {
	case map[string]interface{}:
		return typed
	case map[string]string:
		out := map[string]interface{}{}
		for k, v := range typed {
			out[k] = v
		}
		return out
	default:
		// A payload that went through JSON (the delivery row) decodes back to a
		// generic map.
		encoded, err := json.Marshal(data)
		if err != nil {
			return map[string]interface{}{}
		}
		var out map[string]interface{}
		if json.Unmarshal(encoded, &out) != nil {
			return map[string]interface{}{}
		}
		return out
	}
}

// ChannelConfig is a channel's per-kind settings.
type ChannelConfig struct {
	To      []string `json:"to,omitempty"`
	From    string   `json:"from,omitempty"`
	Locale  string   `json:"locale,omitempty"`
	Channel string   `json:"channel,omitempty"`
}

// ParseChannelConfig decodes a channel's config JSON. An empty column decodes to
// an empty config, which every sender reads as "use the installation default".
func ParseChannelConfig(raw string) (ChannelConfig, error) {
	var config ChannelConfig
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return config, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return config, fmt.Errorf("config: %w", err)
	}
	return config, nil
}

// --- payloads -------------------------------------------------------------

func slackPayload(channel *models.Webhook, event OutboundEvent) ([]byte, error) {
	title, lines := Summary(event)
	config, err := ParseChannelConfig(channel.Config)
	if err != nil {
		return nil, err
	}
	text := title
	if len(lines) > 0 {
		text += "\n" + strings.Join(lines, "\n")
	}
	payload := map[string]interface{}{"text": text}
	if config.Channel != "" {
		payload["channel"] = strings.TrimPrefix(config.Channel, "#")
	}
	if severity, _ := dataMap(event.Data)["severity"]; severity != nil {
		payload["attachments"] = []map[string]interface{}{{
			"color": slackColor(fmt.Sprint(severity)),
			"text":  title,
			"fields": []map[string]interface{}{
				{"title": "event", "value": event.Type, "short": true},
				{"title": "severity", "value": fmt.Sprint(severity), "short": true},
			},
		}}
	}
	return json.Marshal(payload)
}

func slackColor(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	default:
		return "good"
	}
}

func teamsPayload(channel *models.Webhook, event OutboundEvent) ([]byte, error) {
	title, lines := Summary(event)
	section := map[string]interface{}{
		"text": title,
	}
	facts := []map[string]interface{}{}
	for _, line := range lines {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			facts = append(facts, map[string]interface{}{"name": parts[0], "value": parts[1]})
		}
	}
	if len(facts) > 0 {
		section["facts"] = facts
	}
	return json.Marshal(map[string]interface{}{
		"@type":           "MessageCard",
		"@context":        "https://schema.org/extensions",
		"themeColor":      teamsColor(event),
		"title":           title,
		"text":            strings.Join(lines, "\n"),
		"sections":        []map[string]interface{}{section},
		"potentialAction": []interface{}{},
	})
}

func teamsColor(event OutboundEvent) string {
	switch fmt.Sprint(dataMap(event.Data)["severity"]) {
	case "critical":
		return "FF0000"
	case "warning":
		return "FFA500"
	default:
		return "00B0F0"
	}
}

// --- HTTP sender ----------------------------------------------------------

// HTTPSender delivers to a URL. It is the webhook kind's sender exactly as
// v2.3 delivered — same headers, same HMAC signature, custom headers applied
// before the reserved ones — and the Slack/Teams sender, which posts the card
// with no signature because neither endpoint verifies one.
type HTTPSender struct {
	Client *http.Client
}

// NewHTTPSender returns a sender with a sane client. A chat endpoint that is
// down must not park a goroutine for thirty seconds.
func NewHTTPSender() *HTTPSender {
	return &HTTPSender{Client: &http.Client{Timeout: 30 * time.Second}}
}

// Send posts the payload to the channel's URL and reports what came back.
func (s *HTTPSender) Send(req *http.Request) Outcome {
	resp, err := s.Client.Do(req)
	if err != nil {
		return Outcome{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	return Outcome{StatusCode: resp.StatusCode, Response: string(body)}
}

// BuildRequest shapes the HTTP request for one channel kind.
func BuildRequest(kind string, channel *models.Webhook, outbound *Outbound, event OutboundEvent) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, channel.URL, bytes.NewReader(outbound.Payload))
	if err != nil {
		return nil, err
	}
	for k, v := range customHeaders(channel) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", outbound.ContentType)
	req.Header.Set("X-OurWay-Event", event.Type)
	if kind == models.ChannelKindWebhook && channel.Secret != "" {
		req.Header.Set("X-OurWay-Signature", signPayload(channel.Secret, outbound.Payload))
	}
	return req, nil
}

func customHeaders(channel *models.Webhook) map[string]string {
	var headers map[string]string
	if channel.Headers != "" && channel.Headers != "{}" {
		json.Unmarshal([]byte(channel.Headers), &headers)
	}
	return headers
}

// Outcome is what a send produced.
type Outcome struct {
	StatusCode int
	Response   string
	Err        error
	// Permanent marks a failure that retrying cannot fix: the destination
	// refused the request itself (4xx), or the installation is misconfigured.
	// Retrying those produces a ledger full of identical refusals, so they end
	// on the first attempt.
	Permanent bool
}

// OK reports a 2xx with no transport error.
func (o Outcome) OK() bool {
	return o.Err == nil && o.StatusCode >= 200 && o.StatusCode < 300
}

// Code is the peer's answer as text, for the ledger column that records it.
func (o Outcome) Code() string {
	if o.StatusCode == 0 {
		return ""
	}
	return fmt.Sprint(o.StatusCode)
}

// signPayload returns "sha256=<hex>" where <hex> is the HMAC-SHA256 of the
// payload under the channel's secret. Receivers verify the same HMAC to
// authenticate the event. v2.3's scheme, unchanged: a receiver written against
// v2.3 keeps verifying.
func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Detail is the text recorded in the ledger when the send did not succeed: the
// peer's answer if there was one, the transport error if there was not.
func (o Outcome) Detail() string {
	if o.Err != nil {
		return o.Err.Error()
	}
	return o.Response
}
