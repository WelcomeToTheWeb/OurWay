package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/webhooks/testsmtp"
)

// The 2.4-6 fixture: one installation, one tenant with a unit, and channels that
// reach an HTTP webhook, a Slack endpoint, a Teams endpoint and a mailbox. The
// mail server is a real SMTP server (`testsmtp`), the chat endpoints are real
// HTTP servers: what is under test is OurWay's sending, not a stub of it.

type fixture struct {
	st         *store.Store
	dispatcher *Dispatcher
	smtp       *testsmtp.Server
	client     *models.Client
	device     *models.Device
}

func newFixture(t *testing.T, smtpOpts ...testsmtp.Option) *fixture {
	t.Helper()
	db := openTestDB(t)
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	server, err := testsmtp.New(smtpOpts...)
	if err != nil {
		t.Fatalf("mock smtp: %v", err)
	}
	t.Cleanup(server.Close)

	client, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha Co", Slug: "alpha-co"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	device := &models.Device{
		ID: uuid.NewString(), Name: "web-01", Hostname: "web-01", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{"prod"}, DeviceKey: uuid.NewString(), ClientID: &client.ID,
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("device: %v", err)
	}

	dispatcher := NewDispatcherWithSMTP(st, SMTP{
		Host: server.Host(), Port: server.Port(), From: "ourway@corp.example",
	})
	return &fixture{st: st, dispatcher: dispatcher, smtp: server, client: client, device: device}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	return db
}

// channel seeds a channel row.
func (f *fixture) channel(t *testing.T, row *models.Webhook) *models.Webhook {
	t.Helper()
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	if row.Events == "" {
		row.Events = `["alert_created"]`
	}
	if row.Config == "" {
		row.Config = "{}"
	}
	if row.ScopeClients == "" {
		row.ScopeClients = "[]"
	}
	if row.Tags == "" {
		row.Tags = "[]"
	}
	if row.Headers == "" {
		row.Headers = "{}"
	}
	if err := f.st.Webhooks.Create(row); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return row
}

// alertEvent is the payload the alert engine publishes.
func (f *fixture) alertEvent(severity string) Event {
	return Event{
		Type: "alert_created",
		Data: map[string]interface{}{
			"alert_id":  uuid.NewString(),
			"device_id": f.device.ID,
			"device":    f.device.Name,
			"severity":  severity,
			"message":   fmt.Sprintf("CPU usage elevated: 91.0%% (threshold: 80%%)"),
			"metric":    "cpu",
			"value":     91.0,
			"threshold": 80.0,
		},
		Timestamp: time.Now(),
	}
}

// deliveries reads the ledger once the dispatcher's goroutines have settled: it
// waits for the rows to reach a terminal status, not merely to exist. A row
// caught in `pending` is a send in flight, and a test that reads it there is a
// test that reads the middle of an action.
func (f *fixture) deliveries(t *testing.T, channelID string, want int) []models.WebhookDelivery {
	t.Helper()
	settled := map[string]bool{"delivered": true, "failed": true}
	for attempt := 0; attempt < 200; attempt++ {
		rows, err := f.st.WebhookDeliveries.ListByWebhook(channelID)
		if err != nil {
			t.Fatalf("deliveries: %v", err)
		}
		if len(rows) >= want {
			terminal := true
			for _, row := range rows {
				if !settled[row.Status] {
					terminal = false
				}
			}
			if terminal {
				return rows
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("channel %s never settled its deliveries", channelID)
	return nil
}

// await reads the ledger until a state the test is waiting for appears: the
// mid-action states (pending with a retry scheduled) are as real as the terminal
// ones, and the retry tests assert on them.
func (f *fixture) await(t *testing.T, channelID string, ready func([]models.WebhookDelivery) bool) []models.WebhookDelivery {
	t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		rows, err := f.st.WebhookDeliveries.ListByWebhook(channelID)
		if err != nil {
			t.Fatalf("deliveries: %v", err)
		}
		if ready(rows) {
			return rows
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("channel %s never reached the state the test waits for", channelID)
	return nil
}

// recorder is a mock Slack/Teams endpoint: it answers, captures the payload, and
// can be told to refuse the first N attempts.
type recorder struct {
	mu        sync.Mutex
	hits      int
	payloads  []string
	headers   []http.Header
	failFirst int
	server    *httptest.Server
}

func newRecorder(t *testing.T, failFirst int) *recorder {
	t.Helper()
	rec := &recorder{failFirst: failFirst}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.hits++
		rec.payloads = append(rec.payloads, string(body))
		rec.headers = append(rec.headers, r.Header.Clone())
		hits := rec.hits
		rec.mu.Unlock()
		if hits <= failFirst {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "endpoint refused")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func (r *recorder) hits_() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits
}

func (r *recorder) header(i int) http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.headers) {
		return nil
	}
	return r.headers[i]
}

func (r *recorder) payload(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.payloads) {
		return ""
	}
	return r.payloads[i]
}

// 2.4-6's headline criterion: one critical alert, one engine pass, and the
// mailbox, Slack and Teams all receive it — with the unit, metric, value and
// threshold in the text the person reads.
func TestOneAlertReachesEveryKindInOnePass(t *testing.T) {
	f := newFixture(t)

	mail := f.channel(t, &models.Webhook{
		Name: "on-call mailbox", Kind: models.ChannelKindEmail,
		Events: `["alert_created"]`,
		Config: `{"to":["oncall@corp.example"]}`,
	})
	// The chat endpoints are local servers, so the channel is seeded with the URL
	// they answer on: the dispatcher reads the stored row, as production does.
	slackRec := newRecorder(t, 0)
	slack := f.channel(t, &models.Webhook{
		Name: "ops slack", Kind: models.ChannelKindSlack,
		Events: `["alert_created"]`, URL: slackRec.server.URL,
	})
	teamsRec := newRecorder(t, 0)
	teams := f.channel(t, &models.Webhook{
		Name: "ops teams", Kind: models.ChannelKindTeams,
		Events: `["alert_created"]`, URL: teamsRec.server.URL,
	})

	f.dispatcher.Publish(context.Background(), f.alertEvent("critical"))

	mailRows := f.deliveries(t, mail.ID, 1)
	if mailRows[0].Status != "delivered" {
		t.Errorf("email delivery = %s (%s), want delivered", mailRows[0].Status, mailRows[0].ResponseBody)
	}
	messages := f.smtp.Messages()
	if len(messages) != 1 {
		t.Fatalf("mock SMTP accepted %d messages, want 1", len(messages))
	}
	for _, needle := range []string{"web-01", "cpu", "91", "80", "oncall@corp.example"} {
		if !strings.Contains(messages[0].Data+strings.Join(messages[0].To, ","), needle) {
			t.Errorf("the mail text does not state %q:\n%s", needle, messages[0].Data)
		}
	}
	if !strings.Contains(messages[0].Data, "CPU usage elevated: 91.0% (threshold: 80%)") {
		t.Errorf("the mail does not carry the alert's own message:\n%s", messages[0].Data)
	}

	for _, tc := range []struct {
		name string
		rows []models.WebhookDelivery
		rec  *recorder
	}{{"slack", f.deliveries(t, slack.ID, 1), slackRec}, {"teams", f.deliveries(t, teams.ID, 1), teamsRec}} {
		if tc.rows[0].Status != "delivered" {
			t.Errorf("%s delivery = %s (%s), want delivered", tc.name, tc.rows[0].Status, tc.rows[0].ResponseBody)
		}
		if tc.rec.hits_() != 1 {
			t.Errorf("%s endpoint was called %d times, want 1", tc.name, tc.rec.hits_())
		}
		for _, needle := range []string{"web-01", "critical", "cpu"} {
			if !strings.Contains(tc.rec.payload(0), needle) {
				t.Errorf("the %s card does not state %q: %s", tc.name, needle, tc.rec.payload(0))
			}
		}
	}
}

// Routing is applied **before** a row exists. A delivery row for an event the
// operator never asked to be notified about is noise an operator has to explain.
func TestRoutingRefusalsWriteNoDeliveryRow(t *testing.T) {
	f := newFixture(t)
	other := mustCreate(t, f.st, &models.Client{ID: models.NewClientID(), Name: "Beta Co", Slug: "beta-co"})

	criticalOnly := f.channel(t, &models.Webhook{
		Name: "criticals", Kind: models.ChannelKindWebhook, URL: "https://hook.invalid/criticals",
		Events: `["alert_created"]`, MinSeverity: "critical",
	})
	alphaOnly := f.channel(t, &models.Webhook{
		Name: "alpha only", Kind: models.ChannelKindWebhook, URL: "https://hook.invalid/alpha",
		Events: `["alert_created"]`, ScopeClients: fmt.Sprintf(`["%s"]`, other.ID),
	})
	stagingOnly := f.channel(t, &models.Webhook{
		Name: "staging only", Kind: models.ChannelKindWebhook, URL: "https://hook.invalid/staging",
		Events: `["alert_created"]`, Tags: `["staging"]`,
	})

	f.dispatcher.Publish(context.Background(), f.alertEvent("warning"))

	for _, id := range []string{criticalOnly.ID, alphaOnly.ID, stagingOnly.ID} {
		rows, err := f.st.WebhookDeliveries.ListByWebhook(id)
		if err != nil {
			t.Fatalf("deliveries: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("channel %s recorded %d rows for an event routing refused", id, len(rows))
		}
	}

	// The same channel **does** deliver when the facts match: the refusals above
	// are the routing, not a broken sender.
	criticalWebhook := newRecorder(t, 0)
	f.retarget(t, criticalOnly, criticalWebhook.server.URL)
	f.dispatcher.Publish(context.Background(), f.alertEvent("critical"))
	rows := f.deliveries(t, criticalOnly.ID, 1)
	if rows[0].Status != "delivered" {
		t.Errorf("critical delivery = %s, want delivered", rows[0].Status)
	}
}

// A mail channel naming several addresses must reach all of them: an operator who
// added the on-call rota to the channel needs to know the rota got it, not the
// first name in the list.
func TestMailChannelDeliversToEveryAddressItNames(t *testing.T) {
	f := newFixture(t)
	mail := f.channel(t, &models.Webhook{
		Name: "rota", Kind: models.ChannelKindEmail,
		Events: `["alert_created"]`,
		Config: `{"to":["first@corp.example","second@corp.example"]}`,
	})
	f.dispatcher.Publish(context.Background(), f.alertEvent("critical"))
	rows := f.deliveries(t, mail.ID, 1)
	if rows[0].Status != "delivered" {
		t.Fatalf("mail delivery = %s (%s), want delivered", rows[0].Status, rows[0].ResponseBody)
	}
	messages := f.smtp.Messages()
	if len(messages) != 1 {
		t.Fatalf("mock SMTP accepted %d messages, want 1", len(messages))
	}
	for _, address := range []string{"first@corp.example", "second@corp.example"} {
		if !contains(messages[0].To, address) {
			t.Errorf("the rota address %s did not receive the alert: %v", address, messages[0].To)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// retarget points a channel at a local mock endpoint.
func (f *fixture) retarget(t *testing.T, row *models.Webhook, url string) {
	t.Helper()
	row.URL = url
	if err := f.st.Webhooks.Update(row); err != nil {
		t.Fatalf("retarget: %v", err)
	}
}

// The retry contract, stated for the new kinds: the backoff and the ledger row
// shape are what a webhook produces today, and a channel that succeeds on the
// second attempt is marked delivered with attempt 2.
func TestRetryKeepsTheWebhookShapeForEveryKind(t *testing.T) {
	f := newFixture(t)
	rec := newRecorder(t, 1) // the first attempt is refused, the second succeeds
	channel := f.channel(t, &models.Webhook{
		Name: "flaky", Kind: models.ChannelKindSlack, URL: rec.server.URL,
		Events: `["alert_created"]`,
	})

	f.dispatcher.Publish(context.Background(), f.alertEvent("critical"))
	first := f.await(t, channel.ID, func(rows []models.WebhookDelivery) bool {
		return len(rows) == 1 && rows[0].Attempts == 1 && rows[0].NextRetryAt != nil
	})[0]
	if first.Status != "pending" {
		t.Errorf("a send awaiting its retry is %q, want pending", first.Status)
	}
	if !strings.Contains(first.ResponseBody, "endpoint refused") {
		t.Errorf("the ledger does not carry the peer's answer: %q", first.ResponseBody)
	}

	// The backoff is a minute; make it due, and run the same loop production runs.
	due := time.Now().Add(-time.Minute)
	first.NextRetryAt = &due
	first.Status = "pending"
	if err := f.st.WebhookDeliveries.Update(&first); err != nil {
		t.Fatalf("reset: %v", err)
	}
	f.dispatcher.RetryPending(context.Background())

	settled := f.deliveries(t, channel.ID, 1)
	for _, row := range settled {
		if row.ID == first.ID && row.Status == "delivered" {
			if row.Attempts != 2 {
				t.Errorf("retry delivered with attempts=%d, want 2", row.Attempts)
			}
			if row.StatusCode != 200 {
				t.Errorf("retry recorded status %d, want 200", row.StatusCode)
			}
			return
		}
	}
	t.Fatal("the retry never delivered")
}

// An email channel on an installation with no mail server is a configuration the
// operator can see, not a send that dies quietly.
func TestEmailChannelWithoutMailServerStatesTheRefusal(t *testing.T) {
	f := newFixture(t)
	mail := f.channel(t, &models.Webhook{
		Name: "no transport", Kind: models.ChannelKindEmail,
		Events: `["alert_created"]`, Config: `{"to":["oncall@corp.example"]}`,
	})
	f.dispatcher.smtp = SMTP{}

	f.dispatcher.Publish(context.Background(), f.alertEvent("critical"))
	rows := f.deliveries(t, mail.ID, 1)
	if rows[0].Status != "failed" {
		t.Fatalf("delivery = %s, want failed", rows[0].Status)
	}
	if !strings.Contains(rows[0].ResponseBody, "SMTP_HOST") {
		t.Errorf("the ledger does not name the missing configuration: %q", rows[0].ResponseBody)
	}
	if rows[0].NextRetryAt != nil {
		t.Error("a misconfiguration was scheduled for retry; retrying it is noise")
	}
}

// STARTTLS is not decoration: an installation that requires encryption must
// refuse a server that does not offer it, rather than send credentials in clear.
func TestStartTLSRequirementRefusesAPlainServer(t *testing.T) {
	server, err := testsmtp.New(testsmtp.WithStartTLS("none"))
	if err != nil {
		t.Fatalf("mock smtp: %v", err)
	}
	db := openTestDB(t)
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	mail := &models.Webhook{
		ID: uuid.NewString(), Name: "strict", Kind: models.ChannelKindEmail,
		Events: `["alert_created"]`, Config: `{"to":["oncall@corp.example"]}`,
		ScopeClients: "[]", Tags: "[]", Headers: "{}", Enabled: true,
	}
	if err := st.Webhooks.Create(mail); err != nil {
		t.Fatalf("create: %v", err)
	}
	dispatcher := NewDispatcherWithSMTP(st, SMTP{
		Host: server.Host(), Port: server.Port(), From: "ourway@corp.example",
		Username: "ourway", Password: "secret", StartTLS: true,
	})
	dispatcher.Publish(context.Background(), Event{
		Type:      "alert_created",
		Data:      map[string]interface{}{"device": "web-01", "severity": "critical", "message": "CPU high"},
		Timestamp: time.Now(),
	})
	// The settlement read is the same one every other test here uses: the ledger
	// row is only meaningful once the send has a terminal status.
	for attempt := 0; attempt < 200; attempt++ {
		rows, _ := st.WebhookDeliveries.ListByWebhook(mail.ID)
		if len(rows) > 0 && rows[0].Status == "failed" {
			if !strings.Contains(rows[0].ResponseBody, "STARTTLS") {
				t.Errorf("the ledger does not name STARTTLS: %q", rows[0].ResponseBody)
			}
			if len(server.Messages()) != 0 {
				t.Error("a plain server accepted mail the installation said it would not use")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("no failed delivery row was recorded")
}

// A test send must be pointable: it goes through the same routing and writes the
// same ledger.
func TestTestSendWritesARowTheOperatorCanPointAt(t *testing.T) {
	f := newFixture(t)
	rec := newRecorder(t, 0)
	channel := f.channel(t, &models.Webhook{
		Name: "testable", Kind: models.ChannelKindWebhook, URL: rec.server.URL,
		Events: `["alert_created","test"]`,
	})
	row, err := f.st.Webhooks.GetByID(channel.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := f.dispatcher.SendTestEvent(context.Background(), row); err != nil {
		t.Fatalf("test send: %v", err)
	}
	rows := f.deliveries(t, channel.ID, 1)
	if rows[0].Event != "test" {
		t.Errorf("test delivery event = %q, want test", rows[0].Event)
	}
	if rows[0].Status != "delivered" {
		t.Errorf("test delivery = %s, want delivered", rows[0].Status)
	}
	if !strings.Contains(rec.payload(0), "test notification from OurWay") {
		t.Errorf("the endpoint received: %s", rec.payload(0))
	}
}

// A channel that does not subscribe to the test event cannot be tested. Saying
// "sent" and writing nothing is the outcome an operator reads as success.
func TestTestSendNamesAChannelThatCannotBeTested(t *testing.T) {
	f := newFixture(t)
	channel := f.channel(t, &models.Webhook{
		Name: "no test", Kind: models.ChannelKindWebhook, URL: "https://hook.invalid/x",
		Events: `["alert_created"]`,
	})
	row, err := f.st.Webhooks.GetByID(channel.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	err = f.dispatcher.SendTestEvent(context.Background(), row)
	if err == nil {
		t.Fatal("a channel with no test subscription reported success")
	}
	rows, _ := f.st.WebhookDeliveries.ListByWebhook(channel.ID)
	if len(rows) != 0 {
		t.Errorf("a refused test send wrote %d rows", len(rows))
	}
}

// The webhook kind's payload is byte-identical to v2.3's: a receiver written
// against the old release keeps parsing the same event, signature included.
func TestWebhookKindShipsTheV23EventShape(t *testing.T) {
	f := newFixture(t)
	rec := newRecorder(t, 0)
	channel := f.channel(t, &models.Webhook{
		Name: "legacy", Kind: models.ChannelKindWebhook, URL: rec.server.URL,
		Events: `["alert_created"]`, Secret: "shared-secret",
		Headers: `{"X-Custom":"kept"}`,
	})
	f.dispatcher.Publish(context.Background(), f.alertEvent("warning"))
	f.deliveries(t, channel.ID, 1)

	body := rec.payload(0)
	var event OutboundEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatalf("payload is not the event: %v\n%s", err, body)
	}
	if event.Type != "alert_created" {
		t.Errorf("event type = %q, want alert_created", event.Type)
	}
	data, _ := event.Data.(map[string]interface{})
	if data["device"] != "web-01" {
		t.Errorf("payload data = %v, want the unit the alert names", data)
	}
	headers := rec.header(0)
	if headers.Get("X-Custom") != "kept" {
		t.Error("the channel's custom header did not travel")
	}
	if !strings.HasPrefix(headers.Get("X-OurWay-Signature"), "sha256=") {
		t.Errorf("the HMAC signature header is absent: %v", headers)
	}
}

func mustCreate(t *testing.T, st *store.Store, client *models.Client) *models.Client {
	t.Helper()
	created, err := st.Clients.Create(client)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return created
}

var _ = strings.Contains
