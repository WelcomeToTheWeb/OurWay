package automation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestRunCommandExecutesAndReports verifies the command round-trip: the
// handler runs the command with the system shell and posts the result
// (with X-Device-Key auth) to /api/agent/runbooks/result.
func TestRunCommandExecutesAndReports(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	var gotBody map[string]interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/runbooks/result" {
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		mu.Lock()
		gotAuth = r.Header.Get("X-Device-Key")
		json.NewDecoder(r.Body).Decode(&gotBody)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer ts.Close()

	h := NewHandler("test-key", ts.URL, "dev-1")
	h.RunCommand(map[string]interface{}{
		"run_id":    "run-1",
		"device_id": "dev-1",
		"command":   "echo hello",
	})

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "test-key" {
		t.Errorf("X-Device-Key = %q, want test-key", gotAuth)
	}
	if gotBody["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", gotBody["run_id"])
	}
	if success, _ := gotBody["success"].(bool); !success {
		t.Errorf("echo should succeed, body: %+v", gotBody)
	}
	if code, _ := gotBody["exit_code"].(float64); code != 0 {
		t.Errorf("exit_code = %v, want 0", gotBody["exit_code"])
	}
	if out, _ := gotBody["output"].(string); out == "" {
		t.Error("expected captured output")
	}
}

// TestRunCommandRefusesForeignDevice verifies the agent refuses commands
// addressed to another device (defense in depth behind the WS auth).
func TestRunCommandRefusesForeignDevice(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer ts.Close()

	h := NewHandler("test-key", ts.URL, "dev-1")
	h.RunCommand(map[string]interface{}{
		"run_id":    "run-2",
		"device_id": "dev-other",
		"command":   "echo pwned",
	})
	if called {
		t.Error("agent must not execute or report a command for another device")
	}
}

// TestRunCommandReportsFailure verifies non-zero exits are reported as
// failures with the exit code.
func TestRunCommandReportsFailure(t *testing.T) {
	var mu sync.Mutex
	var gotBody map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		json.NewDecoder(r.Body).Decode(&gotBody)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer ts.Close()

	h := NewHandler("test-key", ts.URL, "dev-1")
	h.RunCommand(map[string]interface{}{
		"run_id":    "run-3",
		"device_id": "dev-1",
		"command":   "exit 3",
	})

	mu.Lock()
	defer mu.Unlock()
	if success, _ := gotBody["success"].(bool); success {
		t.Error("exit 3 must be reported as failure")
	}
	if code, _ := gotBody["exit_code"].(float64); code != 3 {
		t.Errorf("exit_code = %v, want 3", gotBody["exit_code"])
	}
}
