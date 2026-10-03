package patch

import (
	"testing"
)

func TestVerifyDeviceID(t *testing.T) {
	t.Run("rejects payload addressed to another device", func(t *testing.T) {
		h := NewHandler("key", "http://localhost:8080", "device-a")
		if h.verifyDeviceID("scan", "device-b") {
			t.Error("expected verification to fail for a mismatched device_id")
		}
	})

	t.Run("accepts payload addressed to this device", func(t *testing.T) {
		h := NewHandler("key", "http://localhost:8080", "device-a")
		if !h.verifyDeviceID("scan", "device-a") {
			t.Error("expected verification to pass for a matching device_id")
		}
	})

	t.Run("fails open when own device id unknown", func(t *testing.T) {
		// The WebSocket connection is already key-authenticated; the
		// check is defense in depth and must not brick an agent whose
		// /api/agent/me lookup has not completed yet.
		h := NewHandler("key", "http://localhost:8080", "")
		if !h.verifyDeviceID("scan", "device-b") {
			t.Error("expected verification to pass (fail open) when own device id is empty")
		}
	})
}

func TestNewHandlerNormalizesServerURL(t *testing.T) {
	h := NewHandler("key", "http://localhost:8080/ws", "device-a")
	if h.serverURL != "http://localhost:8080" {
		t.Errorf("server URL not normalized: %q", h.serverURL)
	}
}
