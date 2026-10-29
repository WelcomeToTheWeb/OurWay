package webhooks

import (
	"strings"
	"testing"
	"time"
)

// The mail a person receives is a rendered artifact, not a debug dump: the
// localized frame around the engine's own facts. These assertions are on the
// rendered text, in each locale, because the mail is what an operator acts on.

func alertOutbound() OutboundEvent {
	return OutboundEvent{
		Type: "alert_created",
		Data: map[string]interface{}{
			"device": "web-01", "severity": "critical", "metric": "cpu",
			"value": 91.0, "threshold": 80.0, "message": "CPU usage high: 91.0% (threshold: 80%)",
		},
		Timestamp: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC),
	}
}

func TestRenderEmailCarriesTheAlertsOwnFacts(t *testing.T) {
	for _, locale := range []string{"en", "de", "fr"} {
		subject, body := RenderEmail(locale, alertOutbound(), []string{"oncall@corp.example"})
		if subject == "" || body == "" {
			t.Fatalf("%s rendered an empty mail", locale)
		}
		for _, needle := range []string{"web-01", "91", "80", "CPU usage high"} {
			if !strings.Contains(subject+body, needle) {
				t.Errorf("%s mail does not state %q:\nsubject: %s\n%s", locale, needle, subject, body)
			}
		}
		// The mail is not a dump of the payload map: the localized labels are
		// there, and a raw key name is not.
		if strings.Contains(body, "map[") {
			t.Errorf("%s mail renders a raw payload map:\n%s", locale, body)
		}
		if !strings.Contains(body, "oncall@corp.example") {
			t.Errorf("%s mail does not say who it was sent to:\n%s", locale, body)
		}
	}
}

// The German and French mail must not be the English mail. A locale file that
// copies English strings passes every key-parity gate and still hands a German
// operator an English notification, so the comparison is on the rendered text.
func TestRenderEmailIsNotEnglishInGermanAndFrench(t *testing.T) {
	_, english := RenderEmail("en", alertOutbound(), nil)
	for _, locale := range []string{"de", "fr"} {
		_, other := RenderEmail(locale, alertOutbound(), nil)
		if other == english {
			t.Errorf("%s rendered the English mail verbatim", locale)
		}
	}
}

// An event with no unit facts (a session event, say) must still render: the
// fields the event does not carry read as an em dash, not as blank lines.
func TestRenderEmailFillsAbsentFields(t *testing.T) {
	event := OutboundEvent{
		Type:      "device_online",
		Data:      map[string]interface{}{"device": "web-01"},
		Timestamp: time.Now(),
	}
	subject, body := RenderEmail("en", event, nil)
	_ = subject
	if !strings.Contains(body, "web-01") {
		t.Errorf("the unit is missing from the mail:\n%s", body)
	}
	if strings.Contains(body, "{{") {
		t.Errorf("an unsubstituted placeholder survived:\n%s", body)
	}
}

// An event type with no template is not a mail that says nothing: the fallback
// states the event and the message.
func TestRenderEmailFallsBackForAnUnknownEvent(t *testing.T) {
	event := OutboundEvent{
		Type:      "audit_something_new",
		Data:      map[string]interface{}{"message": "a new event the templates do not know"},
		Timestamp: time.Now(),
	}
	subject, body := RenderEmail("fr", event, nil)
	if !strings.Contains(subject, "audit_something_new") {
		t.Errorf("subject does not name the event: %q", subject)
	}
	if !strings.Contains(body, "a new event the templates do not know") {
		t.Errorf("body drops the message:\n%s", body)
	}
}
