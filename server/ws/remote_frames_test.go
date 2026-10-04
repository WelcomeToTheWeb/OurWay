package ws

import (
	"context"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"ourway/server/auth"
	"ourway/server/models"
)

// The remote exe authenticates with a per-session token, sends JPEG frames
// as binary messages, and the session owner's browser must receive them as
// "session_frame" text messages (this is the path behind the web viewer).
func TestRemoteExeFramesReachBrowser(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	dev := addDevice(t, st, "pc", "pc-key")

	user := &models.User{ID: "user-1", Username: "tech", Email: "t@example.com", PasswordHash: "x"}
	if err := st.Users.Create(user); err != nil {
		t.Fatal(err)
	}
	token, hash, err := models.NewRemoteToken()
	if err != nil {
		t.Fatal(err)
	}
	sess := &models.Session{ID: "sess-1", DeviceID: dev.ID, UserID: user.ID, Status: "pending", RemoteTokenHash: hash}
	if err := st.Sessions.Create(sess); err != nil {
		t.Fatal(err)
	}

	jwt, err := auth.NewJWTAuth("test-secret-key").GenerateToken(user.ID, user.Username, []string{"technician"})
	if err != nil {
		t.Fatal(err)
	}
	browser := dialAgent(t, ts.URL, jwt)
	browser.SetReadLimit(4 << 20) // relayed frames are base64 JSON
	// The device page and the session viewer are both open for the same user.
	browser2 := dialAgent(t, ts.URL, jwt)
	browser2.SetReadLimit(4 << 20)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exe, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", token, "remote"},
	})
	if err != nil {
		t.Fatalf("exe dial: %v", err)
	}
	defer exe.Close(websocket.StatusNormalClosure, "done")

	// Wait until the hub has registered the browser (registration is async).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(hub.clientsFor("user:"+user.ID)) == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	jpeg := make([]byte, 200*1024) // well over the 32 KiB default read limit
	for i := range jpeg {
		jpeg[i] = byte(i)
	}
	if err := exe.Write(ctx, websocket.MessageBinary, jpeg); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	msg := readUntil(t, browser, 5*time.Second, func(m Message) bool { return m.Type == "session_frame" })
	p, _ := msg.Payload.(map[string]interface{})
	if p["session_id"] != "sess-1" || p["data"] == "" {
		t.Fatalf("bad frame payload: %v", msg.Payload)
	}

	// A second connection for the same user must not have displaced the
	// first (it used to, which left the session viewer blank).
	if m2 := readUntil(t, browser2, 5*time.Second, func(m Message) bool { return m.Type == "session_frame" }); m2 == nil {
		t.Fatal("second browser connection got no frame")
	}

	cur, _ := st.Sessions.GetByID("sess-1")
	if cur.Status != "active" {
		t.Fatalf("first frame must flip the session to active, got %q", cur.Status)
	}
}
