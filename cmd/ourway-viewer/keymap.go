package main

import "strings"

// browserKey converts a Gio key name to the KeyboardEvent.key value
// the agent's key table understands. ok is false for keys with no
// equivalent.
func browserKey(name string) (string, bool) {
	switch name {
	case "⏎", "⌤":
		return "enter", true
	case "⎋":
		return "escape", true
	case "⌫":
		return "backspace", true
	case "⌦":
		return "delete", true
	case "Tab":
		return "tab", true
	case "Space":
		return "space", true
	case "←":
		return "arrowleft", true
	case "→":
		return "arrowright", true
	case "↑":
		return "arrowup", true
	case "↓":
		return "arrowdown", true
	case "⇱":
		return "home", true
	case "⇲":
		return "end", true
	case "⇞":
		return "pageup", true
	case "⇟":
		return "pagedown", true
	case "Ctrl":
		return "control", true
	case "Shift":
		return "shift", true
	case "Alt":
		return "alt", true
	case "Super", "⌘":
		return "meta", true
	}
	if len(name) >= 2 && name[0] == 'F' && len(name) <= 3 { // F1..F24
		if n := strings.TrimPrefix(name, "F"); n != "" && n[0] >= '1' && n[0] <= '9' {
			return strings.ToLower(name), true
		}
	}
	if len([]rune(name)) == 1 {
		r := []rune(name)[0]
		switch {
		case r >= 'A' && r <= 'Z':
			return strings.ToLower(name), true
		case r >= '0' && r <= '9':
			return name, true
		case strings.ContainsRune(";=,-./`[\\]'", r):
			return name, true
		}
	}
	return "", false
}
