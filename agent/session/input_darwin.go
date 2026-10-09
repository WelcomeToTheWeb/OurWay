//go:build darwin && cgo

package session

/*
#include <ApplicationServices/ApplicationServices.h>

// The CGEvent APIs are defined as CGEventCreateKeyboardEvent(virtualKey, keyDown)
// and CGEventCreateMouseEvent(source, mouseType, mouseCursorPosition, mouseButton)
// with CGEventPost(tap, event) to deliver. These wrappers keep the Go-side
// cgo calls small and avoid passing structs across the boundary.
static CGEventRef newKeyEvent(CGKeyCode code, bool down) {
	return CGEventCreateKeyboardEvent(NULL, code, down);
}

static CGEventRef newMouseEvent(CGEventType type, CGPoint pt, CGMouseButton button) {
	return CGEventCreateMouseEvent(NULL, type, pt, button);
}

static void postEvent(CGEventTapLocation tap, CGEventRef event) {
	if (event != NULL) {
		CGEventPost(tap, event);
		CFRelease(event);
	}
}

static CGPoint makePoint(double x, double y) {
	CGPoint p;
	p.x = x;
	p.y = y;
	return p;
}
*/
import "C"

import "log"

// darwinKeyCodes maps lower-cased printable characters to macOS virtual
// key codes (HIToolbox Events.h). Uppercase input is matched lower-cased;
// case is carried by the Shift modifier event, which the browser sends
// as its own key press.
var darwinKeyCodes = map[byte]uint16{
	'a': 0, 's': 1, 'd': 2, 'f': 3, 'h': 4, 'g': 5, 'z': 6, 'x': 7,
	'c': 8, 'v': 9, 'b': 11, 'q': 12, 'w': 13, 'e': 14, 'r': 15, 'y': 16,
	't': 17, '1': 18, '2': 19, '3': 20, '4': 21, '6': 22, '5': 23,
	'=': 24, '9': 25, '7': 26, '-': 27, '8': 28, '0': 29, ']': 30,
	'o': 31, 'u': 32, '[': 33, 'i': 34, 'p': 35, 'l': 37, 'j': 38,
	'\'': 39, 'k': 40, ';': 41, '\\': 42, ',': 43, '/': 44, 'n': 45, 'm': 46,
	'.': 47, '`': 50,
}

// darwinNamedKeyCodes maps named/special keys to macOS virtual key codes.
var darwinNamedKeyCodes = map[string]uint16{
	"Return":     36,
	"Enter":      36,
	"Tab":        48,
	"Space":      49,
	"Backspace":  51,
	"Escape":     53,
	"Delete":     117,
	"Home":       115,
	"End":        119,
	"PageUp":     116,
	"PageDown":   121,
	"ArrowLeft":  123,
	"ArrowRight": 124,
	"ArrowDown":  125,
	"ArrowUp":    126,
	"F1":         122,
	"F2":         120,
	"F3":         99,
	"F4":         118,
	"F5":         96,
	"F6":         97,
	"F7":         98,
	"F8":         100,
	"F9":         101,
	"F10":        109,
	"F11":        103,
	"F12":        111,
}

// darwinModifierCodes maps modifier key names to virtual key codes.
var darwinModifierCodes = map[string]uint16{
	"Shift":   56,
	"Control": 59,
	"Alt":     58,
	"Meta":    55,
}

// keyEvent posts a raw key event for a virtual key code.
func keyEvent(code C.CGKeyCode, down bool) {
	ev := C.newKeyEvent(code, C.bool(down))
	if ev == nil {
		log.Printf("session: CGEventCreateKeyboardEvent failed")
		return
	}
	C.postEvent(C.kCGHIDEventTap, ev)
}

// synthesizeKey presses (or releases) a key using CGEventCreateKeyboardEvent.
// key is a browser KeyboardEvent.key value. Named keys (Enter, ArrowUp, ...)
// and modifiers use the fixed macOS virtual-key tables; single printable
// characters are matched lower-cased (the Shift key itself is synthesizable,
// so case is preserved through the modifier event, not the key code).
func synthesizeKey(key, event string) {
	down := event == "down" || event == "press"
	up := event == "up" || event == "press"
	if !down && !up {
		log.Printf("session: unknown key event %q on darwin", event)
		return
	}
	if code, ok := darwinNamedKeyCodes[key]; ok {
		if down {
			keyEvent(C.CGKeyCode(code), true)
		}
		if up {
			keyEvent(C.CGKeyCode(code), false)
		}
		return
	}
	if code, ok := darwinModifierCodes[key]; ok {
		if down {
			keyEvent(C.CGKeyCode(code), true)
		}
		if up {
			keyEvent(C.CGKeyCode(code), false)
		}
		return
	}
	if len(key) == 1 {
		ch := key[0]
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if code, ok := darwinKeyCodes[ch]; ok {
			if down {
				keyEvent(C.CGKeyCode(code), true)
			}
			if up {
				keyEvent(C.CGKeyCode(code), false)
			}
			return
		}
		log.Printf("session: no macOS key code for %q; key dropped", key)
		return
	}
	log.Printf("session: unmapped key %q on darwin; key dropped", key)
}

// synthesizeMouse moves the pointer, clicks, or scrolls using CGEventCreateMouseEvent.
// x and y arrive as 0-100 percentages of the captured frame and are converted
// to absolute screen pixels via toScreenCoords; delta is scroll steps
// (positive = down, negative = up) mapped to CGScrollEventUnit pixel deltas.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	px, py := toScreenCoords(x, y)
	pt := C.makePoint(C.double(px), C.double(py))
	switch event {
	case "move":
		ev := C.newMouseEvent(C.kCGMouseMoved, pt, C.kCGMouseButtonLeft)
		if ev == nil {
			log.Printf("session: CGEventCreateMouseEvent failed (move)")
			return
		}
		C.postEvent(C.kCGHIDEventTap, ev)
	case "click":
		var btn C.CGMouseButton
		var downType, upType C.CGEventType
		if button == "right" {
			btn = C.kCGMouseButtonRight
			downType = C.kCGRightMouseDown
			upType = C.kCGRightMouseUp
		} else {
			btn = C.kCGMouseButtonLeft
			downType = C.kCGLeftMouseDown
			upType = C.kCGLeftMouseUp
		}
		ev := C.newMouseEvent(downType, pt, btn)
		if ev == nil {
			log.Printf("session: CGEventCreateMouseEvent failed (click down)")
			return
		}
		C.postEvent(C.kCGHIDEventTap, ev)
		ev = C.newMouseEvent(upType, pt, btn)
		if ev == nil {
			log.Printf("session: CGEventCreateMouseEvent failed (click up)")
			return
		}
		C.postEvent(C.kCGHIDEventTap, ev)
	case "scroll":
		steps := int(delta)
		if steps < 0 {
			steps = -steps
		}
		if steps == 0 {
			steps = 1
		}
		// Wheel scroll: kCGScrollWheelEventPointDeltaAxis1 is in pixels;
		// positive scrolls up, negative scrolls down. 10 px per step.
		lines := steps * 10
		if delta > 0 {
			lines = -lines
		}
		ev := C.newMouseEvent(C.kCGScrollWheelEvent, pt, C.kCGMouseButtonLeft)
		if ev == nil {
			log.Printf("session: CGEventCreateMouseEvent failed (scroll)")
			return
		}
		C.CGEventSetDoubleValueField(ev, C.kCGScrollWheelEventPointDeltaAxis1, C.double(lines))
		C.postEvent(C.kCGHIDEventTap, ev)
	default:
		log.Printf("session: unknown mouse event %q on darwin", event)
	}
}
