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

func TestParseWindowsScan(t *testing.T) {
	arr := `[{"Id":"11111111-1111-1111-1111-111111111111","Title":"2026-10 Cumulative Update","KB":"5099999","Severity":"Critical","Size":123,"Category":"Security Updates"},{"Id":"x","Title":""}]`
	got, err := parseWindowsScan(arr)
	if err != nil || len(got) != 1 {
		t.Fatalf("array: %v %v", got, err)
	}
	if got[0].Severity != "critical" || got[0].KB != "5099999" || got[0].ExternalID == "" || got[0].Source != "windows-update" {
		t.Fatalf("bad mapping: %+v", got[0])
	}
	one, err := parseWindowsScan(`{"Id":"22222222-2222-2222-2222-222222222222","Title":"Defender"}`)
	if err != nil || len(one) != 1 {
		t.Fatalf("single object: %v %v", one, err)
	}
	if none, err := parseWindowsScan(""); err != nil || none != nil {
		t.Fatalf("empty: %v %v", none, err)
	}
}

func TestParseWindowsInstall(t *testing.T) {
	reboot, err := parseWindowsInstall(`{"ok":true,"resultCode":2,"rebootRequired":true,"installed":2,"failed":[]}`)
	if err != nil || !reboot {
		t.Fatalf("success: %v %v", reboot, err)
	}
	if _, err := parseWindowsInstall(`WARNING: x` + "\n" + `{"ok":false,"resultCode":4,"failed":["KB1"]}`); err == nil {
		t.Fatal("failure must error")
	}
	if _, err := parseWindowsInstall(`{"ok":false,"error":"none available"}`); err == nil {
		t.Fatal("error field must error")
	}
	if _, err := parseWindowsInstall("garbage"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestWindowsUpdateIDs(t *testing.T) {
	good := Update{ExternalID: "11111111-1111-1111-1111-111111111111"}
	if ids, err := windowsUpdateIDs([]Update{good}); err != nil || len(ids) != 1 {
		t.Fatalf("good: %v %v", ids, err)
	}
	if _, err := windowsUpdateIDs([]Update{{Title: "t"}}); err == nil {
		t.Fatal("missing id must error")
	}
	if _, err := windowsUpdateIDs([]Update{{ExternalID: "1; calc"}}); err == nil {
		t.Fatal("non-guid must error")
	}
}
