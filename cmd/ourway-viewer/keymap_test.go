package main

import "testing"

func TestBrowserKey(t *testing.T) {
	for in, want := range map[string]string{
		"A": "a", "z": "", "7": "7", "⏎": "enter", "⌫": "backspace", "Space": "space",
		"←": "arrowleft", "F5": "f5", "F12": "f12", "Ctrl": "control", "Super": "meta",
		"-": "-", "\\": "\\", "💥": "", "Foo": "",
	} {
		got, ok := browserKey(in)
		if (want == "") == ok || got != want {
			t.Errorf("browserKey(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
}
