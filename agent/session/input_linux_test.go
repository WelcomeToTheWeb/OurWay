package session

import (
	"strings"
	"testing"
)

// The Linux input path is xdotool with a key-name translation table: the
// browser's KeyboardEvent.key values and xdotool's names are not the same
// ("Enter" vs "Return", " " vs "space"). A key missing from the table is
// passed through and silently does nothing on the unit, so the table and the
// modifier prefix are tested here.

func TestXdotoolKeyMapCoversTheNamedKeys(t *testing.T) {
	for _, key := range []string{
		"Enter", " ", "Backspace", "Tab", "Escape", "Delete", "Insert",
		"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
		"PageUp", "PageDown", "Home", "End",
		"Control", "Shift", "Alt", "Meta",
		"CapsLock", "PrintScreen", "Pause", "ScrollLock", "NumLock",
		"F1", "F6", "F12",
	} {
		want, ok := xdotoolKeyMap[key]
		if !ok {
			t.Errorf("xdotoolKeyMap has no entry for %q — the key is passed through and dropped", key)
			continue
		}
		if want == "" {
			t.Errorf("xdotoolKeyMap maps %q to an empty name", key)
		}
	}
}

func TestXdotoolComboRendersEveryModifier(t *testing.T) {
	mods := ModifiersFrom(map[string]interface{}{
		"ctrlKey": true, "shiftKey": true, "altKey": true, "metaKey": true,
	})
	combo := xdotoolCombo(mods)
	for _, want := range []string{"ctrl+", "shift+", "alt+", "super+"} {
		if !strings.Contains(combo, want) {
			t.Errorf("combo %q is missing %q", combo, want)
		}
	}
	if got := xdotoolCombo(inputModifiers{}); got != "" {
		t.Errorf("no modifiers must render an empty prefix, got %q", got)
	}
}

func TestXdotoolPathReportsAbsence(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := xdotoolPath(); err == nil {
		t.Fatal("xdotoolPath must fail when nothing is on PATH")
	}
}

// TestInputCapabilityReportsMissingTooling covers the Linux case the console
// must be able to name: an agent with no xdotool cannot synthesize input, and
// it must report that rather than accept events and discard them.
//
// It lives in this _linux_test.go file (not capability_test.go) because it
// reaches for xdotoolPath, which exists only on Linux: a shared test file that
// names it breaks `go vet` for windows/amd64 and darwin/arm64, and both are in
// the release gate.
func TestInputCapabilityReportsMissingTooling(t *testing.T) {
	if _, err := xdotoolPath(); err != nil {
		t.Skip("xdotool is not installed here; the unavailable case is already what capability reports")
	}
	if got := InputCapability(); got != CapabilityGranted {
		t.Fatalf("with xdotool installed expected %q, got %q", CapabilityGranted, got)
	}

	t.Setenv("PATH", t.TempDir())
	if got := InputCapability(); got != CapabilityUnavailable {
		t.Fatalf("with no xdotool on PATH expected %q, got %q", CapabilityUnavailable, got)
	}
}
