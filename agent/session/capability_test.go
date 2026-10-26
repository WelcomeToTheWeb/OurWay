package session

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// The capability fields are the console's only source of truth about what
// remote control can do on a unit (2.2-1), so the reporting side is tested:
// the default, the transition, and the throttle that keeps a failing unit from
// filling its own log (buglist.md M20).

func TestCaptureStateStartsUnknownAndTracksTheLoop(t *testing.T) {
	// The process-wide default before any capture has run.
	if got := CaptureState(); got != CaptureStateUnknown && got != CaptureStateOK && got != CaptureStateError {
		t.Fatalf("CaptureState() returned a value outside the contract: %q", got)
	}

	setCaptureState(CaptureStateError)
	if got := CaptureState(); got != CaptureStateError {
		t.Fatalf("after a failed capture expected %q, got %q", CaptureStateError, got)
	}
	setCaptureState(CaptureStateOK)
	if got := CaptureState(); got != CaptureStateOK {
		t.Fatalf("after a successful capture expected %q, got %q", CaptureStateOK, got)
	}
}

func TestInputCapabilityIsOneOfTheStatedValues(t *testing.T) {
	switch cap := InputCapability(); cap {
	case CapabilityGranted, CapabilityDenied, CapabilityUnavailable, CapabilityUnknown:
		// fine
	default:
		t.Fatalf("InputCapability() returned %q, which the console cannot render", cap)
	}
}

func TestLogThrottledEmitsOncePerWindow(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(nil) })

	first := "remote input dropped: no Accessibility grant"
	second := "remote input dropped: this is the 15th identical line"

	logThrottled("denied-test", first)
	logThrottled("denied-test", second)

	out := buf.String()
	if !strings.Contains(out, first) {
		t.Errorf("the first line of a reason must be logged, got: %q", out)
	}
	if strings.Contains(out, second) {
		t.Errorf("a repeat inside the throttle window must be suppressed, got: %q", out)
	}

	// A different reason is a different line: the throttle must not swallow
	// the second failure mode behind the first.
	logThrottled("capture-test", "capture error: display unreachable")
	if !strings.Contains(buf.String(), "capture error: display unreachable") {
		t.Errorf("a distinct reason must be logged: %q", buf.String())
	}
}

func TestModifiersFromReadsThePayload(t *testing.T) {
	mods := ModifiersFrom(map[string]interface{}{
		"key": "K", "code": "KeyK",
		"ctrlKey": true, "shiftKey": true,
	})
	if !mods.Ctrl || !mods.Shift {
		t.Fatalf("expected ctrl+shift from the browser payload, got %+v", mods)
	}
	if mods.Alt || mods.Meta {
		t.Errorf("unpressed modifiers must be false: %+v", mods)
	}

	// The console's short form, and the absence of both: an older console
	// sends no modifier keys at all, which must mean "not pressed".
	mods = ModifiersFrom(map[string]interface{}{"ctrl": true, "alt": true})
	if !mods.Ctrl || !mods.Alt {
		t.Fatalf("expected ctrl+alt from the short form, got %+v", mods)
	}
	if got := ModifiersFrom(map[string]interface{}{"key": "A"}); got.Ctrl || got.Shift || got.Alt || got.Meta {
		t.Errorf("a payload with no modifiers must produce none, got %+v", got)
	}
}
