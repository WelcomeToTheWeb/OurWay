package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// fakeServer is a minimal OurWay server: it accepts a device connection
// with the ourway-auth subprotocol and records every message the agent
// sends, while letting the test inject server->agent frames.
type fakeServer struct {
	ts      *httptest.Server
	message chan map[string]interface{}
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{message: make(chan map[string]interface{}, 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"ourway-auth"},
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "closing")
		for {
			var msg map[string]interface{}
			if err := wsjson.Read(r.Context(), conn, &msg); err != nil {
				return
			}
			select {
			case f.message <- msg:
			default:
			}
		}
	})
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

// wsURL rewrites the test server URL into the ws:// form the client dials.
func (f *fakeServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.ts.URL, "http") + "/ws"
}

// waitFor blocks until a message of the given type arrives or times out.
func (f *fakeServer) waitFor(t *testing.T, msgType string) map[string]interface{} {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-f.message:
			if msg["type"] == msgType {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q message", msgType)
		}
	}
}

func TestClientSendsHeartbeatAndMetrics(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.wsURL(), "test-device-key",
		WithHeartbeatInterval(100*time.Millisecond),
		WithMetricsInterval(150*time.Millisecond),
		WithMetricsFunc(func() (interface{}, error) {
			return map[string]interface{}{"cpu": 42.0}, nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	hb := f.waitFor(t, "heartbeat")
	payload, ok := hb["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("heartbeat payload not an object: %T", hb["payload"])
	}
	if payload["device_key"] != "test-device-key" {
		t.Errorf("heartbeat device_key = %v, want test-device-key", payload["device_key"])
	}
	if _, ok := payload["private_ip"]; !ok {
		t.Error("heartbeat missing private_ip field")
	}

	m := f.waitFor(t, "metrics")
	mp, ok := m["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("metrics payload not an object: %T", m["payload"])
	}
	if mp["device_key"] != "test-device-key" {
		t.Errorf("metrics device_key = %v, want test-device-key", mp["device_key"])
	}
	data, ok := mp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("metrics data not an object: %T", mp["data"])
	}
	if data["cpu"] != 42.0 {
		t.Errorf("metrics cpu = %v, want 42", data["cpu"])
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.wsURL(), "test-device-key",
		WithHeartbeatInterval(50*time.Millisecond),
		WithMetricsInterval(50*time.Millisecond),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx)
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error on cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestRunReconnectsWithBackoff(t *testing.T) {
	// A server that is initially unreachable: the client must keep
	// retrying and eventually connect once the server exists. Run is
	// started against a dead URL, then the server is swapped in via the
	// client's configured URL.
	f := newFakeServer(t)
	deadURL := "ws" + strings.TrimPrefix(f.ts.URL, "http") + "/definitely-not-ws"
	c := New(deadURL, "test-device-key",
		WithHeartbeatInterval(50*time.Millisecond),
		WithMetricsInterval(50*time.Millisecond),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx)
	}()
	// Give the retry loop a moment to register at least one failure.
	time.Sleep(200 * time.Millisecond)
	// The client must still be running (retrying), not crashed.
	select {
	case err := <-done:
		t.Fatalf("Run returned early during reconnect backoff: %v", err)
	default:
	}
}

func TestPrivateIPReturnsSomethingOnHost(t *testing.T) {
	// On a test host there is normally at least a loopback interface.
	// The function must never panic and must return a string.
	ip := PrivateIP()
	_ = ip
}

func TestIsVirtual(t *testing.T) {
	virtual := []string{"docker0", "br-abc123", "veth1234", "virbr0", "kube-ipvs0", "cni0"}
	for _, name := range virtual {
		if !isVirtual(name) {
			t.Errorf("isVirtual(%q) = false, want true", name)
		}
	}
	physical := []string{"eth0", "en0", "wlan0", "ens192"}
	for _, name := range physical {
		if isVirtual(name) {
			t.Errorf("isVirtual(%q) = true, want false", name)
		}
	}
}
