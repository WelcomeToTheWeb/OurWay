package webhooks

import (
	"encoding/json"
	"strings"

	"ourway/server/models"
)

// RouteFacts is what a channel's routing is matched against (2.4-6): the
// severity the event carries, the tenant the event is about, and the tags on the
// unit it is about.
//
// The dispatcher resolves these from the event's `device_id` through the device
// store. That is deliberate: publishers stay ignorant of routing, a channel
// cannot be routed by a publisher's omission, and an event that names no unit
// has no tenant and no tags — so it reaches the channels that name no
// restrictions, which is exactly what a pre-2.4 webhook received.
type RouteFacts struct {
	Severity string
	ClientID string
	Tags     []string
}

// Matches reports whether this event should produce a delivery on this channel,
// and names the reason when it should not. "No row at all" is the criterion:
// an event below a channel's floor, from a tenant the channel does not list, or
// from a unit the channel's tags do not cover must not appear in the delivery
// ledger as a failure the operator then has to explain.
func Matches(channel *WebhookChannel, facts RouteFacts) (bool, string) {
	if !channel.Enabled {
		return false, "channel disabled"
	}
	if floor := strings.TrimSpace(channel.MinSeverity); floor != "" {
		want := SeverityRank(floor)
		got := SeverityRank(facts.Severity)
		if want > 0 && got < want {
			return false, "severity below the channel's floor"
		}
	}
	if scopes := channel.ScopeClients; len(scopes) > 0 {
		if facts.ClientID == "" {
			return false, "the event names no tenant and the channel is tenant-scoped"
		}
		found := false
		for _, id := range scopes {
			if id == facts.ClientID {
				found = true
				break
			}
		}
		if !found {
			return false, "the event's tenant is not on the channel"
		}
	}
	if tags := channel.Tags; len(tags) > 0 {
		if len(facts.Tags) == 0 {
			return false, "the event names no unit tags and the channel is tag-scoped"
		}
		found := false
		for _, want := range tags {
			for _, have := range facts.Tags {
				if normalizeTag(want) == normalizeTag(have) {
					found = true
					break
				}
			}
		}
		if !found {
			return false, "the unit carries none of the channel's tags"
		}
	}
	return true, ""
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// WebhookChannel is the routing view of a channel row: the fields routing
// reads, with the JSON columns decoded once.
type WebhookChannel struct {
	ID           string
	Kind         string
	Enabled      bool
	MinSeverity  string
	ScopeClients []string
	Tags         []string
}

// ChannelOf decodes a channel row's routing columns. A column that fails to
// unmarshal decodes to empty, which reads as "no restriction": the safe reading
// for a row an older release wrote, and unreachable for rows the 2.4-6 API
// writes, which validates each column.
func ChannelOf(row *models.Webhook) *WebhookChannel {
	channel := &WebhookChannel{
		ID:          row.ID,
		Kind:        row.Kind,
		Enabled:     row.Enabled,
		MinSeverity: row.MinSeverity,
	}
	if channel.Kind == "" {
		channel.Kind = KindWebhook
	}
	var scoped []string
	if json.Unmarshal([]byte(row.ScopeClients), &scoped) == nil {
		channel.ScopeClients = scoped
	}
	var tags []string
	if json.Unmarshal([]byte(row.Tags), &tags) == nil {
		channel.Tags = tags
	}
	return channel
}

// Kind constants mirror the model's, so a sender can name a kind without
// importing the model package.
const (
	KindWebhook = "webhook"
	KindEmail   = "email"
	KindSlack   = "slack"
	KindTeams   = "teams"
)

// SeverityRank orders severities, mirroring models.SeverityRank so routing has
// one implementation to read.
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
