package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

type memClip struct {
	mu   sync.Mutex
	seq  uint32
	text string
}

func (m *memClip) Seq() uint32          { m.mu.Lock(); defer m.mu.Unlock(); return m.seq }
func (m *memClip) Get() (string, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.text, nil }
func (m *memClip) Set(s string) error   { m.mu.Lock(); m.text = s; m.seq++; m.mu.Unlock(); return nil }

func TestClipSyncNoEcho(t *testing.T) {
	m := &memClip{}
	var sent []string
	cs := &ClipSync{Clip: m, Send: func(s string) { sent = append(sent, s) }}
	cs.Prime()
	m.Set("local copy") // user copies locally
	cs.Poll()
	cs.Remote("from remote")
	cs.Poll() // our own Set must not bounce back
	cs.Poll()
	if len(sent) != 1 || sent[0] != "local copy" {
		t.Errorf("sent = %q, want only the local copy", sent)
	}
	if m.text != "from remote" {
		t.Errorf("remote text not applied: %q", m.text)
	}
}

func TestClientRoundTrip(t *testing.T) {
	var got []string
	var mu sync.Mutex
	gotSub := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSub = r.Header.Get("Sec-WebSocket-Protocol")
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ourway-auth"}})
		if err != nil {
			return
		}
		ctx := context.Background()
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"monitor_list","payload":[{"id":0,"w":1920,"h":1080,"primary":true},{"id":1,"x":1920,"w":1280,"h":1024}]}`))
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"clipboard","payload":{"text":"hi"}}`))
		c.Write(ctx, websocket.MessageBinary, []byte{frameTiles, 0, 0, 1, 0, 1, 0, 0}) // no base frame
		for i := 0; i < 2; i++ {
			_, d, err := c.Read(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			got = append(got, string(d))
			mu.Unlock()
		}
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"session_end"}`))
		c.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()

	var mons []Monitor
	var clip string
	ended := make(chan string, 1)
	var s Screen
	cl, err := Dial(context.Background(), Launch{Server: srv.URL, Token: "tok123"}, &s, Callbacks{
		Monitors:  func(m []Monitor) { mons = m },
		Clipboard: func(t string) { clip = t },
		Ended:     func(r string) { ended <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	go cl.Run()
	cl.Mouse("click", 50, 25, "left")

	select {
	case r := <-ended:
		if r != "session ended" {
			t.Errorf("reason = %q", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	if !strings.Contains(gotSub, "ourway-auth") || !strings.Contains(gotSub, "tok123") || !strings.Contains(gotSub, "viewer") {
		t.Errorf("subprotocols = %q", gotSub)
	}
	if len(mons) != 2 || mons[1].W != 1280 || clip != "hi" {
		t.Errorf("monitors=%v clip=%q", mons, clip)
	}
	mu.Lock()
	defer mu.Unlock()
	// The tile frame without a base must trigger a keyframe request, and
	// the click must use the input envelope.
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, `"type":"request_keyframe"`) {
		t.Errorf("no keyframe request in %s", joined)
	}
	var in struct {
		Type    string
		Payload struct {
			Type    string
			Payload map[string]any
		}
	}
	for _, g := range got {
		if json.Unmarshal([]byte(g), &in) == nil && in.Type == "input" {
			if in.Payload.Type != "mouse" || in.Payload.Payload["event"] != "click" || in.Payload.Payload["x"] != 50.0 {
				t.Errorf("bad input envelope: %s", g)
			}
			return
		}
	}
	t.Errorf("no input message in %s", joined)
}
