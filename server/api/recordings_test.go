package api

// 2.4-8 · remote-control session recording, tested through the routes.
//
// The tests run the real flow: a device that is connected on the WebSocket, a
// session started by a technician, frames posted by the agent with its
// per-session token, control input, and the recording the installation keeps
// because its `RECORD_SESSIONS` policy says to.
//
// What the file is particular about: the **installation's** policy is the reason
// a recording exists (a participant cannot turn one off, and `off` is the
// default); the frames come from the path the live session already uses, so a
// recording exists for a session the console watched; playback is scoped by
// 2.3.1 and reading a recording is audited; and a recording's manifest states
// what the file cannot — interruptions, dropped frames, a file that is gone.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"nhooyr.io/websocket"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/recordings"
	"ourway/server/store"
	"ourway/server/ws"
)

type recordingFixture struct {
	st      *store.Store
	policy  recordings.Policy
	writer  *recordings.Writer
	client  *models.Client
	device  *models.Device
	baseURL string
	hub     *ws.Hub
	server  *httptest.Server
}

// recordingServer is one running installation. A restart is a new server over
// the same store and the same recording directory, which is the only faithful
// way to test what a reboot does to a recording in progress.
type recordingServer struct {
	ts      *httptest.Server
	hub     *ws.Hub
	baseURL string
}

func newRecordingFixture(t *testing.T, mode string) *recordingFixture {
	return newRecordingFixtureWithPolicy(t, recordings.Policy{
		Mode:      mode,
		Dir:       t.TempDir(),
		Retention: 30 * 24 * time.Hour,
		MaxBytes:  1 << 20,
	})
}

// newRecordingFixtureWithPolicy starts an installation whose recording policy is
// exactly `policy` — the cap test needs a cap the frames can reach.
func newRecordingFixtureWithPolicy(t *testing.T, policy recordings.Policy) *recordingFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	policy.Normalize()

	writer := recordings.NewWriter(policy)
	// The policy is installed before the router is built, because the session
	// handler reads the wiring once at construction — the order `main` uses.
	SetRecording(policy, writer)
	t.Cleanup(func() {
		writer.Close()
		// Restore the product default so a later test never inherits a policy.
		SetRecording(recordings.Policy{Mode: models.RecordingModeOff}, nil)
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	f := &recordingFixture{st: st, policy: policy, writer: writer}
	f.install(t)

	client, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Rec Co", Slug: "rec-co-" + policy.Mode + "-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	device := &models.Device{
		ID: uuid.NewString(), Name: "rec-unit", Hostname: "rec-unit", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(), ClientID: &client.ID,
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("create device: %v", err)
	}
	f.client, f.device = client, device
	return f
}

// install starts (or restarts) the HTTP server over the fixture's store, with the
// WebSocket endpoint `main` wires outside `SetupRouter`.
func (f *recordingFixture) install(t *testing.T) {
	t.Helper()
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	go hub.Run()
	engine := alerts.NewEngine(f.st.Alerts)

	router := SetupRouter(context.Background(), f.st, jwtAuth, hub, engine, "http://localhost:3000", "", 1000, 100)
	router.GET("/ws", func(c *gin.Context) { hub.ServeHTTP(c, f.st, jwtAuth) })
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	f.baseURL, f.hub = ts.URL, hub
	f.server = ts
}

// restart is the reboot case: the writer's files close, the policy is re-read,
// and a new server takes over the same store and the same recording directory.
func (f *recordingFixture) restart(t *testing.T) {
	t.Helper()
	f.writer.Close()
	f.writer = recordings.NewWriter(f.policy)
	SetRecording(f.policy, f.writer)
	f.server.Close()
	f.install(t)
}

// connectDevice opens the agent's WebSocket connection, which is what makes a
// unit "connected" enough for a session to start on it.
func (f *recordingFixture) connectDevice(t *testing.T) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(f.baseURL, "http") + "/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", f.device.DeviceKey},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	for !f.hub.IsDeviceConnected(f.device.DeviceKey) {
		time.Sleep(10 * time.Millisecond)
	}
	return conn
}

// awaitRemoteToken reads the device's WebSocket until the session's
// `session_start` arrives, and returns the per-session token it carries. This is
// how the real agent gets the token it posts frames with: the token is delivered
// to the device over its connection, never in the console's session response.
func (f *recordingFixture) awaitRemoteToken(t *testing.T, conn *websocket.Conn, sessionID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var msg struct {
			Type    string `json:"type"`
			Payload struct {
				SessionID   string `json:"session_id"`
				RemoteToken string `json:"remote_token"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "session_start" && msg.Payload.SessionID == sessionID {
			if msg.Payload.RemoteToken == "" {
				t.Fatal("session_start carried no remote token")
			}
			return msg.Payload.RemoteToken
		}
	}
}

// makeUser creates an account with the named role, scoped to `clientIDs`, and
// returns its token. Directly in the store, because the registration endpoint
// hands `admin` to the first account in an installation — a fixture that
// registers its technicians first tests admins while it thinks it tests
// technicians.
func (f *recordingFixture) makeUser(t *testing.T, username, role string, clientIDs ...string) string {
	t.Helper()
	const password = "Passw0rd-2.4!"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &models.User{
		ID: uuid.NewString(), Username: username, Email: username + "@example.test",
		PasswordHash: string(hash), Provider: "local",
	}
	if err := f.st.Users.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	roleRow, err := f.st.Roles.GetRoleByName(role)
	if err != nil {
		t.Fatalf("role %s: %v", role, err)
	}
	if err := f.st.UserRoles.AssignRole(user.ID, roleRow.ID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	for _, cid := range clientIDs {
		if err := f.st.Clients.AddUserClient(user.ID, cid); err != nil {
			t.Fatalf("scope user: %v", err)
		}
	}
	return loginUser(t, f.baseURL, username, password)
}

func (f *recordingFixture) do(t *testing.T, method, path, token string, body any, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	return f.send(t, method, path, token, body, nil, headers)
}

// send issues a request whose body is JSON (`body`), raw bytes (`raw`), or
// empty — the frame endpoint takes raw JPEG, every other route takes JSON.
func (f *recordingFixture) send(t *testing.T, method, path, token string, body any, raw []byte, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		payload = bytes.NewReader(encoded)
	} else if raw != nil {
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, f.baseURL+path, payload)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out, res.Header
}

func (f *recordingFixture) get(t *testing.T, path, token string) (int, []byte, http.Header) {
	t.Helper()
	return f.do(t, http.MethodGet, path, token, nil, nil)
}

// startSession opens a remote-control session and returns its id with the viewer
// token the console hands the native viewer. The agent's per-session token travels
// to the device over its WebSocket, so the fixture reads it with `awaitRemoteToken`.
func (f *recordingFixture) startSession(t *testing.T, token, deviceID string) (string, string) {
	t.Helper()
	code, body, _ := f.do(t, http.MethodPost, "/api/devices/"+deviceID+"/sessions", token, map[string]any{}, nil)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("start session = %d: %s", code, body)
	}
	var answer struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		ViewerToken string `json:"viewer_token"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode session: %v\n%s", err, body)
	}
	if answer.Session.ID == "" || answer.ViewerToken == "" {
		t.Fatalf("session start returned no id or viewer token:\n%s", body)
	}
	return answer.Session.ID, answer.ViewerToken
}

// postFrame uploads one frame the way the agent does: a raw JPEG body and the
// session's own token.
func (f *recordingFixture) postFrame(t *testing.T, sessionID, remoteToken string, jpeg []byte) int {
	t.Helper()
	code, body, _ := f.send(t, http.MethodPost, "/api/sessions/"+sessionID+"/frame", "", nil, jpeg, map[string]string{
		"X-Session-Token": remoteToken,
		"Content-Type":    "image/jpeg",
	})
	if code >= 400 {
		t.Fatalf("frame = %d: %s", code, body)
	}
	return code
}

func (f *recordingFixture) sendInput(t *testing.T, token, sessionID, inputType string) int {
	t.Helper()
	code, body, _ := f.do(t, http.MethodPost, "/api/sessions/"+sessionID+"/input", token, map[string]any{
		"type": inputType, "payload": map[string]any{"key": "a", "x": 10, "y": 20},
	}, nil)
	if code >= 400 {
		t.Fatalf("input = %d: %s", code, body)
	}
	return code
}

func (f *recordingFixture) endSession(t *testing.T, token, sessionID string) {
	t.Helper()
	code, body, _ := f.do(t, http.MethodDelete, "/api/sessions/"+sessionID, token, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("end session = %d: %s", code, body)
	}
}

func (f *recordingFixture) recordingFor(t *testing.T, token, sessionID string) (int, []byte) {
	t.Helper()
	code, body, _ := f.get(t, "/api/sessions/"+sessionID+"/recording", token)
	return code, body
}

type recordingAnswer struct {
	Recording struct {
		ID            string     `json:"id"`
		SessionID     string     `json:"session_id"`
		Status        string     `json:"status"`
		Reason        string     `json:"reason"`
		Policy        string     `json:"policy"`
		Frames        int64      `json:"frame_count"`
		Bytes         int64      `json:"bytes"`
		Interruptions int        `json:"interruptions"`
		Dropped       int64      `json:"dropped_frames"`
		Playable      bool       `json:"playable"`
		EndedAt       *time.Time `json:"ended_at"`
		UserID        string     `json:"user_id"`
		DeviceName    string     `json:"device_name"`
		Username      string     `json:"username"`
	} `json:"recording"`
	Policy   string `json:"recording_policy"`
	Playable bool   `json:"playable"`
}

// recordedSession runs the smallest true session: a connected unit, a session,
// frames, and the agent's per-session token. The recording under test is the one
// this installation produced.
func (f *recordingFixture) recordedSession(t *testing.T, token string, frames int) (string, string) {
	t.Helper()
	conn := f.connectDevice(t)
	sessionID, _ := f.startSession(t, token, f.device.ID)
	remoteToken := f.awaitRemoteToken(t, conn, sessionID)
	for i := 0; i < frames; i++ {
		if code := f.postFrame(t, sessionID, remoteToken, frameBytes(i)); code != http.StatusOK {
			t.Fatalf("frame %d = %d", i, code)
		}
	}
	return sessionID, remoteToken
}

func frameBytes(i int) []byte {
	return []byte("JPEG-frame-" + string(rune('A'+i)) + "-" + strings.Repeat("x", 40+i))
}

func auditRowsForAction(t *testing.T, st *store.Store, action string) []models.AuditLog {
	t.Helper()
	rows, err := st.Audit.List(store.AuditFilter{Action: action, Limit: 200})
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	return rows
}

// `RECORD_SESSIONS` defaults to `off`, and an installation that records nothing
// must say so on the routes the console reads — not answer as if recordings
// exist and are empty.
func TestRecordingIsOffUnlessTheInstallationSaysSo(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOff)
	tech := f.makeUser(t, "rec-off-tech", "technician", f.client.ID)

	sessionID, _ := f.recordedSession(t, tech, 2)

	if code, body := f.recordingFor(t, tech, sessionID); code != http.StatusNotFound {
		t.Fatalf("recording of a session in an installation that does not record = %d: %s", code, body)
	}

	code, body, _ := f.do(t, http.MethodPost, "/api/sessions/"+sessionID+"/recording", tech, map[string]any{}, nil)
	if code != http.StatusConflict {
		t.Fatalf("toggle under off = %d, want 409: %s", code, body)
	}
	if !bytes.Contains(body, []byte("recording_disabled")) {
		t.Fatalf("the refusal is not named: %s", body)
	}

	code, body, _ = f.get(t, "/api/recordings", tech)
	if code != http.StatusOK {
		t.Fatalf("list = %d: %s", code, body)
	}
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Policy != models.RecordingModeOff {
		t.Fatalf("the console is told the policy is %q, want off", answer.Policy)
	}
}

// `on` means every session is recorded from its start: the recording exists
// before the first frame (so the session panel can state the promise), the
// frames the agent posted land in it, and the manifest says who, where, why, and
// under which policy.
func TestPolicyOnRecordsEverySessionFromItsStart(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-on-tech", "technician", f.client.ID)
	admin := f.makeUser(t, "rec-on-admin", "admin")

	sessionID, _ := f.recordedSession(t, tech, 3)

	code, body := f.recordingFor(t, tech, sessionID)
	if code != http.StatusOK {
		t.Fatalf("recording = %d: %s", code, body)
	}
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	rec := answer.Recording
	if rec.Policy != models.RecordingModeOn || rec.Reason != models.RecordingReasonPolicyOn {
		t.Fatalf("recording is %s / %s, want the installation's policy and its reason: %s", rec.Policy, rec.Reason, body)
	}
	if rec.Status != models.RecordingStatusRecording {
		t.Fatalf("status = %q while the session is live, want recording", rec.Status)
	}
	if rec.Frames != 3 {
		t.Fatalf("frame_count = %d, want 3 (the frames the agent posted)", rec.Frames)
	}
	if rec.DeviceName == "" || rec.Username == "" {
		t.Fatalf("the manifest does not name the unit and the account: %+v", rec)
	}

	// Ending the session ends the recording: a row with no `ended_at` is a
	// recording the console reports as live forever.
	f.endSession(t, tech, sessionID)
	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Status != models.RecordingStatusComplete {
		t.Fatalf("status = %q after the session ended, want complete", answer.Recording.Status)
	}
	if answer.Recording.EndedAt == nil {
		t.Fatal("the recording has no ended_at after its session ended")
	}
	if !answer.Recording.Playable {
		t.Fatalf("the recording of a session that ran is not playable: %s", body)
	}

	// The manifest is what "playing a recording" starts with, and the act of
	// playing is audited by name.
	code, body, _ = f.get(t, "/api/recordings/"+answer.Recording.ID, admin)
	if code != http.StatusOK {
		t.Fatalf("manifest = %d: %s", code, body)
	}
	var manifest struct {
		Frames []struct {
			Index int       `json:"index"`
			At    time.Time `json:"at"`
		} `json:"frames"`
		Checksum  string `json:"checksum_on_disk"`
		Recording struct {
			Checksum string `json:"checksum"`
		} `json:"recording"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, body)
	}
	if len(manifest.Frames) != 3 {
		t.Fatalf("the manifest lists %d frames, want 3", len(manifest.Frames))
	}
	for i, frame := range manifest.Frames {
		if frame.Index != i {
			t.Fatalf("frame %d has index %d", i, frame.Index)
		}
	}
	if manifest.Checksum == "" || manifest.Checksum != manifest.Recording.Checksum {
		t.Fatalf("the row says checksum %q, the file says %q", manifest.Recording.Checksum, manifest.Checksum)
	}

	played := auditRowsForAction(t, f.st, models.AuditSessionRecordingPlay)
	if len(played) == 0 {
		t.Fatal("playing a recording wrote no ledger row")
	}
	if played[0].ActorUserID != adminUserID(t, f.st, "rec-on-admin") {
		t.Fatalf("the playback row names actor %q, want the admin who played it", played[0].ActorUsername)
	}
}

func adminUserID(t *testing.T, st *store.Store, username string) string {
	t.Helper()
	user, err := st.Users.GetByUsername(username)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	return user.ID
}

// `control-only` is the policy an MSP actually runs: a session where somebody
// drives the machine is the act the audit question is about. The recording
// begins at the control input, and a view-only session has no recording — stated,
// not silently absent.
func TestControlOnlyStartsAtTheFirstControlInput(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeControl)
	tech := f.makeUser(t, "rec-ctl-tech", "technician", f.client.ID)

	sessionID, remoteToken := f.recordedSession(t, tech, 2)

	if code, _ := f.recordingFor(t, tech, sessionID); code != http.StatusNotFound {
		t.Fatalf("a view-only session under control-only = %d, want 404 (nothing was controlled)", code)
	}

	f.sendInput(t, tech, sessionID, "key")

	code, body := f.recordingFor(t, tech, sessionID)
	if code != http.StatusOK {
		t.Fatalf("recording after control input = %d: %s", code, body)
	}
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Reason != models.RecordingReasonPolicyControl {
		t.Fatalf("reason = %q, want the control-only policy", answer.Recording.Reason)
	}
	if answer.Recording.Policy != models.RecordingModeControl {
		t.Fatalf("policy = %q, want control-only", answer.Recording.Policy)
	}

	// Frames keep arriving after control starts: they belong to this recording.
	for i := 4; i < 10; i++ {
		if code := f.postFrame(t, sessionID, remoteToken, frameBytes(i)); code != http.StatusOK {
			t.Fatalf("frame = %d", code)
		}
	}
	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Frames == 0 {
		t.Fatalf("the control-only recording has no frames after control input: %s", body)
	}
}

// The toggle is the operator's request to record a session. It starts one, it
// **never stops one**, and asking twice produces one recording — not a second
// recording of the same screen.
func TestTheToggleStartsARecordingAndNothingStopsIt(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOff)
	// The installation records nothing by default, so the toggle refuses: a
	// technician cannot make an installation capture screens it declined to.
	tech := f.makeUser(t, "rec-toggle-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 1)
	code, body, _ := f.do(t, http.MethodPost, "/api/sessions/"+sessionID+"/recording", tech, map[string]any{}, nil)
	if code != http.StatusConflict {
		t.Fatalf("toggle under off = %d, want 409: %s", code, body)
	}
}

func TestTheToggleRecordsForTheTechnicianOnThePolicyTheirInstallationRuns(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeControl)
	tech := f.makeUser(t, "rec-toggle-on-tech", "technician", f.client.ID)

	sessionID, remoteToken := f.recordedSession(t, tech, 1)

	code, body, _ := f.do(t, http.MethodPost, "/api/sessions/"+sessionID+"/recording", tech, map[string]any{}, nil)
	if code != http.StatusCreated {
		t.Fatalf("toggle = %d, want 201: %s", code, body)
	}
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID
	if answer.Recording.Reason != models.RecordingReasonSelfRecorded {
		t.Fatalf("reason = %q, want the technician's own request", answer.Recording.Reason)
	}

	// Asking twice does not produce a second recording of the same screen.
	code, body, _ = f.do(t, http.MethodPost, "/api/sessions/"+sessionID+"/recording", tech, map[string]any{}, nil)
	if code != http.StatusCreated {
		t.Fatalf("second toggle = %d, want 201 with the existing recording: %s", code, body)
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.ID != recordingID {
		t.Fatalf("the same session produced a second recording: %q then %q", recordingID, answer.Recording.ID)
	}
	rows, err := f.st.Recordings.ListAll(50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d recording rows for one session, want 1", len(rows))
	}

	// The only end a recording knows is its session's end.
	f.postFrame(t, sessionID, remoteToken, frameBytes(2))
	f.endSession(t, tech, sessionID)
	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Status != models.RecordingStatusComplete {
		t.Fatalf("status = %q, want complete: the session ended", answer.Recording.Status)
	}

	// Starting a recording is audited: it begins capturing a person's screen.
	found := false
	for _, row := range auditRowsForAction(t, f.st, models.AuditSessionRecordStart) {
		if row.TargetID == recordingID {
			found = true
		}
	}
	if !found {
		t.Fatal("starting a recording wrote no ledger row naming it")
	}
}

// A restart is the shape of a real deployment: the server comes back mid-session
// and the session keeps running. The recording continues in the same file, and
// says it was interrupted — a recording that pretends continuity hides the
// minutes nobody recorded.
func TestARestartResumesTheSameRecordingAndStatesTheInterruption(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-restart-tech", "technician", f.client.ID)

	sessionID, remoteToken := f.recordedSession(t, tech, 2)
	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID

	// The server restarts: the writer's files close, a new server takes over the
	// same store and the same recording directory, the session is still live, and
	// the row names the file the new writer reopens.
	f.restart(t)
	f.connectDevice(t)
	for i := 5; i < 10; i++ {
		if code := f.postFrame(t, sessionID, remoteToken, frameBytes(i)); code != http.StatusOK {
			t.Fatalf("frame after restart = %d", code)
		}
	}

	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.ID != recordingID {
		t.Fatalf("the restart produced a new recording: %q then %q", recordingID, answer.Recording.ID)
	}
	if answer.Recording.Interruptions == 0 {
		t.Fatalf("the resumed recording states no interruption: %s", body)
	}
	// The live manifest is sampled every fifth frame, so mid-session it may lag by
	// a few frames. What it must never do is read as a recording that started
	// over: the frames written before the restart are counted.
	if answer.Recording.Frames < 2 {
		t.Fatalf("the resumed recording states %d frames; the file continues with the frames written before the restart", answer.Recording.Frames)
	}

	f.endSession(t, tech, sessionID)
	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Frames != 7 {
		t.Fatalf("the finished recording states %d frames, want 7 (2 before the restart, 5 after)", answer.Recording.Frames)
	}

	_, manifest, _ := f.get(t, "/api/recordings/"+recordingID, tech)
	if !bytes.Contains(manifest, []byte("resume")) {
		t.Fatalf("the sequence has no resume marker for playback to show:\n%s", manifest)
	}

	// The frames before and after the break are one sequence, in order.
	seq, err := recordings.Read(f.policy.PathFor(recordingID))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if int64(seq.FrameCount) != answer.Recording.Frames {
		t.Fatalf("the file holds %d frames, the manifest states %d", seq.FrameCount, answer.Recording.Frames)
	}
	if seq.Interruptions == 0 {
		t.Fatal("the file carries no interruption")
	}
}

// 2.3.1's rule on the recording door: a technician scoped to one tenant reads
// another tenant's recording as a 404 that is indistinguishable from a recording
// that never existed. A body that distinguishes them says the recording exists.
func TestRecordingsAreScopedToTheTechniciansClients(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	inside := f.makeUser(t, "rec-scope-inside", "technician", f.client.ID)

	other, err := f.st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Other Co", Slug: "other-co"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	foreign := &models.Device{
		ID: uuid.NewString(), Name: "foreign-unit", Hostname: "foreign-unit", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(), ClientID: &other.ID,
	}
	if err := f.st.Devices.Create(foreign); err != nil {
		t.Fatalf("create device: %v", err)
	}

	// A recording inside the technician's tenant, and one outside it.
	sessionID, _ := f.recordedSession(t, inside, 1)
	_, body := f.recordingFor(t, inside, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ownRecording := answer.Recording.ID

	foreignSession := &models.Session{
		ID: uuid.NewString(), DeviceID: foreign.ID, UserID: adminUserID(t, f.st, "rec-scope-inside"), Status: "active",
	}
	if err := f.st.Sessions.Create(foreignSession); err != nil {
		t.Fatalf("create session: %v", err)
	}
	foreignRecording := &models.SessionRecording{
		ID: uuid.NewString(), SessionID: foreignSession.ID, DeviceID: foreign.ID,
		UserID: adminUserID(t, f.st, "rec-scope-inside"), Path: f.policy.PathFor(foreignSession.ID),
		StartedAt: time.Now().UTC(), FrameCount: 1, Policy: models.RecordingModeOn,
		Reason: models.RecordingReasonPolicyOn,
	}
	if err := f.st.Recordings.Create(foreignRecording); err != nil {
		t.Fatalf("create recording: %v", err)
	}

	// Somebody else's recording, in a tenant the caller is not granted: 404, and
	// the byte-identical answer for a recording that does not exist.
	outsideTech := f.makeUser(t, "rec-scope-outside", "technician", other.ID)
	_, missingBody, _ := f.get(t, "/api/recordings/"+uuid.NewString(), outsideTech)
	code, outOfScopeBody, _ := f.get(t, "/api/recordings/"+ownRecording, outsideTech)
	if code != http.StatusNotFound {
		t.Fatalf("out-of-scope recording = %d, want 404: %s", code, outOfScopeBody)
	}
	if !bytes.Equal(missingBody, outOfScopeBody) {
		t.Fatalf("the answers differ.\n nonexistent:   %s\n out of scope: %s", missingBody, outOfScopeBody)
	}

	// The list is scoped too: the technician granted to the other tenant reads no
	// rows from this one.
	code, listBody, _ := f.get(t, "/api/recordings", outsideTech)
	if code != http.StatusOK {
		t.Fatalf("list = %d: %s", code, listBody)
	}
	if bytes.Contains(listBody, []byte(ownRecording)) {
		t.Fatalf("the list leaks a recording from a tenant the caller is not granted: %s", listBody)
	}

	// Admin reads the installation, including the foreign tenant's recording.
	admin := f.makeUser(t, "rec-scope-admin", "admin")
	code, adminBody, _ := f.get(t, "/api/recordings/"+foreignRecording.ID, admin)
	if code != http.StatusOK {
		t.Fatalf("admin reading a tenant's recording = %d: %s", code, adminBody)
	}

	// A manager granted the tenant reads the tenant's recordings, and may read a
	// recording they made themselves even on a unit they cannot see (the record of
	// what you did does not disappear when your grants change).
	manager := f.makeUser(t, "rec-scope-manager", "manager", f.client.ID)
	code, managerBody, _ := f.get(t, "/api/recordings/"+ownRecording, manager)
	if code != http.StatusOK {
		t.Fatalf("manager of the tenant = %d: %s", code, managerBody)
	}
	code, managerList, _ := f.get(t, "/api/recordings", manager)
	if code != http.StatusOK || !bytes.Contains(managerList, []byte(ownRecording)) {
		t.Fatalf("the tenant's manager does not read its recording: %d %s", code, managerList)
	}
}

// Input is the act the recording is made for, and the overlay shows it over the
// frames. The events come from the ledger, in the recording's own window, oldest
// first.
func TestInputIsItsOwnActInTheLedgerAndTheOverlayReadsIt(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeControl)
	tech := f.makeUser(t, "rec-input-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 1)

	f.sendInput(t, tech, sessionID, "key")
	f.sendInput(t, tech, sessionID, "mouse")

	inputRows := auditRowsForAction(t, f.st, models.AuditSessionInput)
	var mine int
	for _, row := range inputRows {
		details := decodeAuditDetails(row.Details)
		if session, _ := details["session_id"].(string); session == sessionID {
			mine++
		}
	}
	if mine != 2 {
		t.Fatalf("%d ledger rows of input for this session, want 2", mine)
	}

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	code, eventsBody, _ := f.get(t, "/api/recordings/"+answer.Recording.ID+"/events", tech)
	if code != http.StatusOK {
		t.Fatalf("events = %d: %s", code, eventsBody)
	}
	var overlay struct {
		Events []struct {
			At        time.Time `json:"at"`
			Action    string    `json:"action"`
			InputType string    `json:"input_type"`
		} `json:"events"`
	}
	if err := json.Unmarshal(eventsBody, &overlay); err != nil {
		t.Fatalf("decode events: %v\n%s", err, eventsBody)
	}
	var control int
	var previous time.Time
	for _, event := range overlay.Events {
		if event.Action != models.AuditSessionInput {
			continue
		}
		control++
		if event.InputType == "" {
			t.Fatalf("an input event names no input type: %+v", event)
		}
		if event.At.Before(previous) {
			t.Fatalf("the overlay is not in playback order: %s after %s", event.At, previous)
		}
		previous = event.At
	}
	if control != 2 {
		t.Fatalf("the overlay carries %d input events, want 2", control)
	}
}

// Playing a recording is reading a person's screen out of a file, so it is
// audited. The frames are the playback stream: a ledger row per frame would be a
// log of watching, and the manifest row already names the act.
func TestPlayingARecordingIsAuditedAndFetchingFramesIsNot(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-play-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 2)
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID

	before := len(auditRowsForAction(t, f.st, models.AuditSessionRecordingPlay))

	// The frame stream: bytes, in order, with the media type the browser needs.
	code, frame, header := f.get(t, "/api/recordings/"+recordingID+"/frames/1", tech)
	if code != http.StatusOK {
		t.Fatalf("frame = %d", code)
	}
	if ctype := header.Get("Content-Type"); ctype != "image/jpeg" {
		t.Fatalf("frame content type is %q, want image/jpeg", ctype)
	}
	if !bytes.Contains(frame, []byte("JPEG-frame-B")) {
		t.Fatalf("frame 1 is not the frame the index names: %q", frame)
	}

	// A frame past the end answers the 404 with a named reason.
	code, frameBody, _ := f.get(t, "/api/recordings/"+recordingID+"/frames/99", tech)
	if code != http.StatusNotFound || !bytes.Contains(frameBody, []byte("frame_not_found")) {
		t.Fatalf("frame past the end = %d: %s", code, frameBody)
	}

	if after := len(auditRowsForAction(t, f.st, models.AuditSessionRecordingPlay)); after != before {
		t.Fatalf("fetching a frame wrote %d playback rows: the stream is not the act", after-before)
	}

	// The manifest read is the act, and it names the player and the recording.
	code, manifest, _ := f.get(t, "/api/recordings/"+recordingID, tech)
	if code != http.StatusOK {
		t.Fatalf("manifest = %d: %s", code, manifest)
	}
	plays := auditRowsForAction(t, f.st, models.AuditSessionRecordingPlay)
	if len(plays) != before+1 {
		t.Fatalf("%d playback rows after a manifest read, want %d", len(plays), before+1)
	}
	if plays[0].TargetID != recordingID {
		t.Fatalf("the playback row names target %q, want the recording %q", plays[0].TargetID, recordingID)
	}
	if plays[0].ActorUserID != adminUserID(t, f.st, "rec-play-tech") {
		t.Fatalf("the playback row names actor %q", plays[0].ActorUsername)
	}

	// Exporting the recording out of the installation is its own act.
	code, _, _ = f.get(t, "/api/recordings/"+recordingID+"/manifest", tech)
	if code != http.StatusOK {
		t.Fatalf("manifest download = %d", code)
	}
	code, file, _ := f.get(t, "/api/recordings/"+recordingID+"/file", tech)
	if code != http.StatusOK {
		t.Fatalf("file download = %d", code)
	}
	if !bytes.HasPrefix(file, []byte(recordings.Magic)) {
		t.Fatal("the downloaded file is not a recording file")
	}
	fetches := auditRowsForAction(t, f.st, models.AuditSessionRecordingFetch)
	if len(fetches) != 2 {
		t.Fatalf("%d recording_fetch rows, want 2 (manifest and sequence)", len(fetches))
	}
}

// A recording whose file is gone is stated as missing: the console must not offer
// playback of media that is not there, and the ledger row is the only record left.
func TestARecordingWhoseFileIsGoneIsMissingAndNotPlayable(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-missing-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 2)
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID
	if !answer.Recording.Playable {
		t.Fatalf("a recording with frames is not playable: %s", body)
	}

	if err := os.Remove(f.policy.PathFor(recordingID)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	_, body = f.recordingFor(t, tech, sessionID)
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Status != models.RecordingStatusMissing {
		t.Fatalf("status = %q with no file, want missing", answer.Recording.Status)
	}
	if answer.Recording.Frames == 0 {
		t.Fatal("a missing recording lost its frame count: the manifest is all that is left")
	}

	code, frames, _ := f.get(t, "/api/recordings/"+recordingID+"/frames", tech)
	if code != http.StatusNotFound || !bytes.Contains(frames, []byte("recording_unreadable")) {
		t.Fatalf("frame index of a missing recording = %d: %s", code, frames)
	}
}

// The live session panel is where the promise is read, so the session list
// carries it: which sessions **are** being recorded, and under which policy.
func TestTheSessionListStatesWhichSessionsAreRecorded(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeControl)
	tech := f.makeUser(t, "rec-list-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 1)

	code, body, _ := f.get(t, "/api/sessions", tech)
	if code != http.StatusOK {
		t.Fatalf("sessions = %d: %s", code, body)
	}
	var list struct {
		Sessions   []models.Session          `json:"sessions"`
		Recordings map[string]map[string]any `json:"recordings"`
		Policy     string                    `json:"recording_policy"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if list.Policy != models.RecordingModeControl {
		t.Fatalf("the console is told the policy is %q", list.Policy)
	}
	if _, ok := list.Recordings[sessionID]; ok {
		t.Fatal("a view-only session under control-only is shown as recorded")
	}

	f.sendInput(t, tech, sessionID, "key")
	_, body, _ = f.get(t, "/api/sessions", tech)
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	info, ok := list.Recordings[sessionID]
	if !ok {
		t.Fatalf("the panel does not show the recording the control input started: %s", body)
	}
	if info["status"] != models.RecordingStatusRecording {
		t.Fatalf("the panel shows status %v, want recording", info["status"])
	}
}

// Retention is the operator's number, and the sweep is the only hand that
// removes recordings: the rows go first, then the files, and the counters say
// what it did.
func TestRetentionSweepPrunesRecordingsAndTheirFiles(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-sweep-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 2)
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID
	path := f.policy.PathFor(recordingID)
	if !recordings.Exists(path) {
		t.Fatal("the sweep's fixture has no file to prune")
	}

	// The window closed: the date is what the sweep reads, not its length.
	past := time.Now().UTC().Add(-time.Hour)
	if err := f.st.DB.Model(&models.SessionRecording{}).Where("id = ?", recordingID).
		Update("retention_until", past).Error; err != nil {
		t.Fatalf("age the recording: %v", err)
	}

	sweep := recordings.NewSweep(f.st, f.writer, 30*24*time.Hour)
	result := sweep.Run()
	if result.Errored {
		t.Fatal("the sweep reported an error")
	}
	if result.RecordingsDeleted == 0 {
		t.Fatal("the sweep pruned nothing past its retention date")
	}
	if result.FilesRemoved == 0 {
		t.Fatal("the sweep removed no files: the rows are gone and the disk is not")
	}
	if recordings.Exists(path) {
		t.Fatal("the recording file is still on disk after its row was pruned")
	}
	if _, err := f.st.Recordings.GetByID(recordingID); err == nil {
		t.Fatal("the manifest row survived the sweep")
	}
}

// A server that stopped mid-session leaves a row that says "recording" with
// nobody writing it. The console would show a live recording forever, so the
// sweep closes what the sessions left open — and never a recording this process
// is appending to.
func TestSweepClosesAbandonedRecordingsAndLeavesLiveOnes(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-abandon-tech", "technician", f.client.ID)

	liveSession, _ := f.recordedSession(t, tech, 2)
	_, body := f.recordingFor(t, tech, liveSession)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	liveRecording := answer.Recording.ID

	// A row whose session is over, left open by a restart.
	ended := &models.Session{ID: uuid.NewString(), DeviceID: f.device.ID, UserID: adminUserID(t, f.st, "rec-abandon-tech"), Status: "ended"}
	if err := f.st.Sessions.Create(ended); err != nil {
		t.Fatalf("create session: %v", err)
	}
	abandoned := &models.SessionRecording{
		ID: uuid.NewString(), SessionID: ended.ID, DeviceID: f.device.ID, UserID: ended.UserID,
		Path: f.policy.PathFor(ended.ID), StartedAt: time.Now().UTC().Add(-time.Hour), FrameCount: 4,
		Policy: models.RecordingModeOn, Reason: models.RecordingReasonPolicyOn,
	}
	if err := f.st.Recordings.Create(abandoned); err != nil {
		t.Fatalf("create recording: %v", err)
	}

	sweep := recordings.NewSweep(f.st, f.writer, 30*24*time.Hour)
	result := sweep.Run()
	if result.RecordingsClosed != 1 {
		t.Fatalf("the sweep closed %d recordings, want 1 (the abandoned row)", result.RecordingsClosed)
	}
	if live, err := f.st.Recordings.GetByID(liveRecording); err != nil {
		t.Fatalf("read the live recording: %v", err)
	} else if live.EndedAt != nil {
		t.Fatal("the sweep closed a recording this process is appending to")
	}
	if closed, err := f.st.Recordings.GetByID(abandoned.ID); err != nil {
		t.Fatalf("read the abandoned recording: %v", err)
	} else if closed.EndedAt == nil {
		t.Fatal("the abandoned recording is still open")
	}
}

// The recording must survive being read back exactly: the frames the agent
// posted are the frames playback serves, in order, with the manifest's checksum
// describing the same bytes.
func TestRecordedFramesPlayBackInOrderWithTheManifestsChecksum(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-order-tech", "technician", f.client.ID)
	sessionID, remoteToken := f.recordedSession(t, tech, 0)
	for i := 0; i < 4; i++ {
		if code := f.postFrame(t, sessionID, remoteToken, frameBytes(i)); code != http.StatusOK {
			t.Fatalf("frame %d = %d", i, code)
		}
	}
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID
	if answer.Recording.Frames != 4 {
		t.Fatalf("frame_count = %d, want 4", answer.Recording.Frames)
	}

	seq, err := recordings.Read(f.policy.PathFor(recordingID))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if seq.FrameCount != 4 {
		t.Fatalf("the file has %d frames, want 4", seq.FrameCount)
	}
	for i, frame := range seq.Frames {
		code, served, _ := f.get(t, "/api/recordings/"+recordingID+"/frames/"+itoa(i), tech)
		if code != http.StatusOK {
			t.Fatalf("frame %d = %d", i, code)
		}
		if !bytes.Equal(served, frame.JPEG) {
			t.Fatalf("frame %d: the route served %d bytes, the file holds %d", i, len(served), len(frame.JPEG))
		}
	}

	_, manifest, _ := f.get(t, "/api/recordings/"+recordingID, tech)
	if !bytes.Contains(manifest, []byte(seq.Checksum)) {
		t.Fatalf("the manifest does not state the file's checksum %q:\n%s", seq.Checksum, manifest)
	}
}

// The cap is the installation's disk promise. Hitting it must be visible on the
// manifest, and must not affect the live session: remote control is the product,
// the recording is its record.
func TestARecordingThatReachesItsCapSaysSoAndKeepsTheSessionAlive(t *testing.T) {
	policy := recordings.Policy{
		Mode:      models.RecordingModeOn,
		Dir:       t.TempDir(),
		Retention: 30 * 24 * time.Hour,
		MaxBytes:  200, // a frame is ~50 bytes: the cap is reachable in one session
	}
	f := newRecordingFixtureWithPolicy(t, policy)
	tech := f.makeUser(t, "rec-cap-tech", "technician", f.client.ID)
	sessionID, remoteToken := f.recordedSession(t, tech, 0)
	for i := 0; i < 25; i++ {
		if code := f.postFrame(t, sessionID, remoteToken, frameBytes(i)); code != http.StatusOK {
			t.Fatalf("frame %d = %d (the cap must not fail the live session)", i, code)
		}
	}
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Recording.Frames == 0 {
		t.Fatalf("the capped recording has no frames: %s", body)
	}
	if answer.Recording.Frames >= 25 {
		t.Fatalf("the cap dropped nothing: %d frames stored under a 200 byte limit", answer.Recording.Frames)
	}
	if answer.Recording.Dropped == 0 {
		t.Fatalf("the manifest states no dropped frames, though the cap refused %d: %s", 25-answer.Recording.Frames, body)
	}
	if answer.Recording.Bytes > policy.MaxBytes {
		t.Fatalf("the file is %d bytes under a %d byte cap", answer.Recording.Bytes, policy.MaxBytes)
	}
	_, manifest, _ := f.get(t, "/api/recordings/"+answer.Recording.ID, tech)
	if !bytes.Contains(manifest, []byte("cap")) {
		t.Fatalf("the sequence has no cap marker: %s", manifest)
	}
}

func TestATechnicianAlwaysReadsTheirOwnRecording(t *testing.T) {
	f := newRecordingFixture(t, models.RecordingModeOn)
	tech := f.makeUser(t, "rec-own-tech", "technician", f.client.ID)
	sessionID, _ := f.recordedSession(t, tech, 2)
	f.endSession(t, tech, sessionID)

	_, body := f.recordingFor(t, tech, sessionID)
	var answer recordingAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	recordingID := answer.Recording.ID

	// The technician is removed from the tenant.
	if err := f.st.Clients.RemoveUserClient(adminUserID(t, f.st, "rec-own-tech"), f.client.ID); err != nil {
		t.Fatalf("remove scope: %v", err)
	}

	code, ownList, _ := f.get(t, "/api/recordings", tech)
	if code != http.StatusOK {
		t.Fatalf("list after losing scope = %d: %s", code, ownList)
	}
	if !bytes.Contains(ownList, []byte(recordingID)) {
		t.Fatalf("the technician cannot list their own recording after losing scope: %s", ownList)
	}
	code, manifest, _ := f.get(t, "/api/recordings/"+recordingID, tech)
	if code != http.StatusOK {
		t.Fatalf("playing their own recording = %d: %s", code, manifest)
	}

	// A recording of somebody else's session, with no scope, is not theirs to read.
	other := &models.SessionRecording{
		ID: uuid.NewString(), SessionID: uuid.NewString(), DeviceID: f.device.ID,
		UserID: uuid.NewString(), StartedAt: time.Now().UTC(), Path: f.policy.PathFor("not-mine"),
		Policy: models.RecordingModeOn, Reason: models.RecordingReasonPolicyOn,
	}
	if err := f.st.Recordings.Create(other); err != nil {
		t.Fatalf("create recording: %v", err)
	}
	code, _, _ = f.get(t, "/api/recordings/"+other.ID, tech)
	if code != http.StatusNotFound {
		t.Fatalf("an unscoped caller read somebody else's recording: %d", code)
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}
