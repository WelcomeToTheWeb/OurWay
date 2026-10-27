package session

import "testing"

// The macOS input path is implemented in cgo (input_darwin_cgo.go) and cannot
// run in CI, so the parts that are pure mapping are tested here, where they
// compile: the browser → macOS key translation and the frame-percentage →
// pixel conversion. Both are where remote input silently goes wrong.

func TestMacResolveKeyPrefersPhysicalCode(t *testing.T) {
	// A French layout sends key "A" for the physical Q key; the far machine
	// must get what the technician sees, which is the physical key.
	cases := []struct {
		key, code string
		want      macKeyCode
		why       string
	}{
		{"a", "KeyA", macKeyA, "plain letter"},
		{"A", "KeyA", macKeyA, "shifted letter keeps the same physical key"},
		{"Q", "KeyA", macKeyA, "code wins over a layout-shifted key value"},
		{"Enter", "", macKeyReturn, "named key without a code"},
		{"?", "Slash", macKeySlash, "shifted symbol resolved by code"},
		{"Escape", "Escape", macKeyEscape, "escape"},
		{"ArrowUp", "ArrowUp", macKeyUpArrow, "arrow"},
		{"Backspace", "Backspace", macKeyDelete, "macOS Backspace is kVK_Delete"},
		{"Delete", "Delete", macKeyForwardDelete, "macOS ForwardDelete is kVK_Delete+fn"},
		{"Numpad5", "Numpad5", macKeyKeypad5, "keypad"},
		{"", "Nope", macKeyNone, "unknown code is none, not a silent guess"},
	}
	for _, tc := range cases {
		if got := macResolveKey(tc.key, tc.code); got != tc.want {
			t.Errorf("macResolveKey(%q, %q) = 0x%X, want 0x%X (%s)", tc.key, tc.code, got, tc.want, tc.why)
		}
	}
}

func TestMacResolveKeyFallsBackToKeyValue(t *testing.T) {
	// Older console builds send key only.
	if got := macResolveKey("c", ""); got != macKeyC {
		t.Errorf("key-only fallback: got 0x%X, want 0x%X", got, macKeyC)
	}
	if got := macResolveKey("F12", ""); got != macKeyF12 {
		t.Errorf("function key fallback: got 0x%X, want 0x%X", got, macKeyF12)
	}
	if got := macResolveKey("Z", ""); got != macKeyZ {
		t.Errorf("shifted letter fallback: got 0x%X, want 0x%X", got, macKeyZ)
	}
}

func TestMacModifierFlags(t *testing.T) {
	if got := (inputModifiers{}).flags(); got != 0 {
		t.Fatalf("no modifiers: got 0x%X, want 0", got)
	}
	got := inputModifiers{Shift: true, Ctrl: true, Meta: true}.flags()
	want := macFlagShift | macFlagControl | macFlagCommand
	if got != want {
		t.Errorf("ctrl+shift+command: got 0x%X, want 0x%X", got, want)
	}
	if got := (inputModifiers{Alt: true}).flags(); got != macFlagOption {
		t.Errorf("option: got 0x%X, want 0x%X", got, macFlagOption)
	}
}

// withDisplaySize pins the display for coordinate tests. displaySize() caches
// through a sync.Once, so the Once is consumed here before the values are set.
func withDisplaySize(t *testing.T, w, h int) {
	t.Helper()
	displaySizeOnce.Do(func() {})
	displaySizeW, displaySizeH = w, h
	t.Cleanup(func() { displaySizeW, displaySizeH = 0, 0 })
}

func TestToScreenCoordsAtTheEdges(t *testing.T) {
	withDisplaySize(t, 1920, 1080)

	cases := []struct {
		x, y   float64
		wantPx int
		wantPy int
		why    string
	}{
		{0, 0, 0, 0, "top-left corner"},
		{50, 50, 960, 540, "centre"},
		{100, 100, 1919, 1079, "bottom-right clamps to the last pixel"},
		{-5, 33.33, 0, 360, "out of range clamps low, fractional rounds"},
		{120, -10, 1919, 0, "out of range clamps both ways"},
	}
	for _, tc := range cases {
		px, py := toScreenCoords(tc.x, tc.y)
		if px != tc.wantPx || py != tc.wantPy {
			t.Errorf("toScreenCoords(%.2f, %.2f) = (%d, %d), want (%d, %d) (%s)",
				tc.x, tc.y, px, py, tc.wantPx, tc.wantPy, tc.why)
		}
	}
}

func TestToScreenCoordsFractionalScale(t *testing.T) {
	// A non-round scale catches truncation bugs (1366x768: 50% is 683.0 x 384.0,
	// 33% is 450.78 → 451).
	withDisplaySize(t, 1366, 768)

	px, py := toScreenCoords(50, 50)
	if px != 683 || py != 384 {
		t.Errorf("1366x768 centre: got (%d, %d), want (683, 384)", px, py)
	}
	px, py = toScreenCoords(33, 33)
	if px != 451 || py != 253 {
		t.Errorf("1366x768 at 33%%: got (%d, %d), want (451, 253)", px, py)
	}
}
