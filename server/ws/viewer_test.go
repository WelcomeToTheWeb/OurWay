package ws

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"ourway/server/models"
)

// dialRole opens a /ws connection with an explicit role marker.
func dialRole(t *testing.T, tsURL, cred, role string) (*websocket.Conn, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(tsURL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", cred, role},
	})
	if err == nil {
		conn.SetReadLimit(maxFrameBytes)
		t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	}
	return conn, err
}

// readBinary reads until a binary message arrives.
func readBinary(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for binary message: %v", err)
		}
		if typ == websocket.MessageBinary {
			return data
		}
	}
}

func writeBinary(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("binary write: %v", err)
	}
}

func TestViewerSessionRelay(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	dev := addDevice(t, st, "target", "target-key")

	remoteTok, remoteHash, _ := models.NewRemoteToken()
	viewerTok, viewerHash, _ := models.NewRemoteToken()
	sess := &models.Session{
		ID: "11111111-1111-1111-1111-111111111111", DeviceID: dev.ID,
		UserID: "33333333-3333-3333-3333-333333333333", Status: "pending",
		RemoteTokenHash: remoteHash, ViewerTokenHash: viewerHash,
	}
	if err := st.Sessions.Create(sess); err != nil {
		t.Fatal(err)
	}

	// Credentials that must not open a viewer connection.
	for name, cred := range map[string]string{"remote token": remoteTok, "device key": dev.DeviceKey, "garbage": "nope"} {
		if _, err := dialRole(t, ts.URL, cred, "viewer"); err == nil {
			t.Errorf("viewer connect with %s must be rejected", name)
		}
	}

	agent := dialAgent(t, ts.URL, dev.DeviceKey)
	remote, err := dialRole(t, ts.URL, remoteTok, "remote")
	if err != nil {
		t.Fatalf("remote dial: %v", err)
	}
	viewer, err := dialRole(t, ts.URL, viewerTok, "viewer")
	if err != nil {
		t.Fatalf("viewer dial: %v", err)
	}

	// The exe is told to switch to the framed protocol.
	readUntil(t, remote, 5*time.Second, func(m Message) bool { return m.Type == "viewer_attach" })

	// Framed frames pass through verbatim; legacy JPEGs are wrapped.
	rich := []byte{frameKindTiles, 0, 0, 10, 0, 10, 0, 0}
	writeBinary(t, remote, rich)
	if got := readBinary(t, viewer); !bytes.Equal(got, rich) {
		t.Errorf("framed frame altered: %x", got)
	}
	jpeg := []byte{0xFF, 0xD8, 1, 2, 3}
	writeBinary(t, remote, jpeg)
	if got := readBinary(t, viewer); !bytes.Equal(got, append([]byte{frameKindFull, 0}, jpeg...)) {
		t.Errorf("legacy frame not wrapped: %x", got)
	}

	// Viewer -> exe: allowed types only, order preserved.
	wsSend(t, viewer, "command", map[string]string{"cmd": "format c:"})
	wsSend(t, viewer, "clipboard", map[string]string{"text": "hello"})
	m := readUntil(t, remote, 5*time.Second, func(m Message) bool { return m.Type != "ping" && m.Type != "viewer_attach" })
	if m.Type != "clipboard" {
		t.Errorf("exe got %q first; disallowed type must not be relayed", m.Type)
	}

	// Ctrl+Alt+Del goes to the agent service, not the exe.
	wsSend(t, viewer, "special_key", map[string]string{"key": "ctrl_alt_del"})
	got := readUntil(t, agent, 5*time.Second, func(m Message) bool { return m.Type == "send_sas" })
	if got == nil {
		t.Fatal("agent did not receive send_sas")
	}
	// Other special keys are refused.
	wsSend(t, viewer, "special_key", map[string]string{"key": "win_l"})

	// Exe -> viewer: allowed types only.
	wsSend(t, remote, "command", nil)
	wsSend(t, remote, "monitor_list", []map[string]int{{"id": 0, "w": 1920, "h": 1080}})
	ml := readUntil(t, viewer, 5*time.Second, func(m Message) bool { return m.Type == "monitor_list" })
	if ml == nil {
		t.Fatal("viewer did not receive monitor_list")
	}

	// Ending the session closes the viewer.
	if err := st.Sessions.EndSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	hub.SendToViewer(sess.ID, "session_end", nil)
	readUntil(t, viewer, 5*time.Second, func(m Message) bool { return m.Type == "session_end" })
}

// A slow viewer loses the oldest frames; since later frames may be
// diffs against a dropped one, the exe must be asked for a keyframe.
func TestViewerDropRequestsKeyframe(t *testing.T) {
	h := NewHub("", "")
	remote := &Client{ID: "remote:k", Type: "remote", SendCh: make(chan []byte, 8)}
	viewer := &Client{ID: "viewer:s1", Type: "viewer", DeviceKey: "k", SendCh: make(chan []byte, 8), BinCh: make(chan []byte, 4)}
	h.clients[remote.ID], h.clients[viewer.ID] = remote, viewer

	for i := 0; i < 6; i++ {
		h.sendViewerFrame("s1", []byte{frameKindFull, 0, byte(i)})
	}
	select {
	case m := <-remote.SendCh:
		if !strings.Contains(string(m), "request_keyframe") {
			t.Errorf("unexpected message to exe: %s", m)
		}
	default:
		t.Fatal("no keyframe request after dropping frames")
	}
	// Newest frames are kept.
	var last byte
	for len(viewer.BinCh) > 0 {
		last = (<-viewer.BinCh)[2]
	}
	if last != 5 {
		t.Errorf("newest frame not kept, last=%d", last)
	}
	// Rate limited: more drops within a second send no second request.
	for i := 0; i < 6; i++ {
		h.sendViewerFrame("s1", []byte{frameKindFull, 0, 9})
	}
	if len(remote.SendCh) != 0 {
		t.Error("keyframe request not rate limited")
	}
}
