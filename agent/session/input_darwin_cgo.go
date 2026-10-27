//go:build darwin && cgo

package session

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// The C side of remote input is four posting helpers. Keeping them as static C
// functions, rather than calling CGEvent* from Go, means each CGEventRef is
// created, flagged, posted and released inside one C sequence: no Go pointer
// handling, which is what the CoreGraphics contract wants.

static void postKey(CGKeyCode code, int down, CGEventFlags flags) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, code, down ? 1 : 0);
    if (event == NULL) return;
    CGEventSetFlags(event, flags);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static void postMove(CGFloat x, CGFloat y, int dragged) {
    CGEventType type = dragged ? kCGEventLeftMouseDragged : kCGEventMouseMoved;
    CGEventRef event = CGEventCreateMouseEvent(NULL, type, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event == NULL) return;
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static void postButton(CGFloat x, CGFloat y, int right, int down, int clickCount) {
    CGMouseButton which = right ? kCGMouseButtonRight : kCGMouseButtonLeft;
    CGEventType type;
    if (right) {
        type = down ? kCGEventRightMouseDown : kCGEventRightMouseUp;
    } else {
        type = down ? kCGEventLeftMouseDown : kCGEventLeftMouseUp;
    }
    CGEventRef event = CGEventCreateMouseEvent(NULL, type, CGPointMake(x, y), which);
    if (event == NULL) return;
    // The click state is what separates a double click from two clicks.
    CGEventSetIntegerValueField(event, kCGMouseEventClickState, clickCount);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static void postScroll(int lines) {
    if (lines == 0) return;
    // The scroll unit constant is kCGScrollEventUnitLine (CGEventTypes.h);
    // `kCGUnitLine` does not exist and the darwin cgo build says so on the
    // macOS runner, which is the only place this file is compiled.
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 1, lines);
    if (event == NULL) return;
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static int accessibilityTrusted(void) {
    return AXIsProcessTrusted() ? 1 : 0;
}

static void requestAccessibilityTrust(void) {
    // kAXTrustedCheckOptionPrompt is the documented way to ask: macOS shows the
    // "add this application" prompt naming the binary. It is requested by
    // `ourway-agent --grant-input-permission`, never implicitly — the grant is
    // per binary *and* per path, see docs/agent.md.
    // The options argument is a CFDictionary keyed by that constant, not a
    // CFArray: an array compiles (with a pointer-type warning) and then hands
    // the framework a struct it reads as a dictionary.
    const void *keys[1] = { kAXTrustedCheckOptionPrompt };
    const void *values[1] = { kCFBooleanTrue };
    CFDictionaryRef opts = CFDictionaryCreate(NULL, (const void **)keys,
                                              (const void **)values, 1,
                                              &kCFTypeDictionaryKeyCallBacks,
                                              &kCFTypeDictionaryValueCallBacks);
    if (opts == NULL) return;
    AXIsProcessTrustedWithOptions(opts);
    CFRelease(opts);
}
*/
import "C"

import (
	"sync"
	"time"
)

// macOS remote input: CGEvent posting through the HID event tap.
//
// This requires Accessibility (TCC) trust for the agent binary. Without it,
// CGEvent posts are accepted and silently discarded — the exact failure mode
// this file replaces (buglist.md H8). So the state is reported to the server
// through inputCapability() and named in the console, rather than discovered by
// a technician in front of a live machine.

var (
	mouseMu     sync.Mutex
	pressing    bool
	pressedBtn  int
	clickCount  int
	lastClickAt time.Time
)

// synthesizeKey posts a key press/release with the technician's modifier state.
func synthesizeKey(key, code, event string, mods inputModifiers) {
	codeKey := macResolveKey(key, code)
	if codeKey == macKeyNone {
		logThrottled("key", "no macOS key code for "+key+" — event dropped")
		return
	}
	if !accessibilityGranted() {
		logInputDenied("key", key)
		return
	}
	C.postKey(C.CGKeyCode(codeKey), boolToInt(event == "down"), C.CGEventFlags(mods.flags()))
}

// synthesizeMouse moves, clicks, drags and scrolls.
//
// x and y arrive as 0-100 percentages of the captured frame (the web client
// contract) and become screen pixels through toScreenCoords.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	px, py := toScreenCoords(x, y)
	if !accessibilityGranted() {
		logInputDenied("mouse", event)
		return
	}

	right := 0
	if button == "right" {
		right = 1
	}

	mouseMu.Lock()
	defer mouseMu.Unlock()

	switch event {
	case "move":
		dragged := 0
		if pressing {
			dragged = 1
		}
		C.postMove(C.CGFloat(px), C.CGFloat(py), C.int(dragged))
	case "down":
		pressing = true
		pressedBtn = right
		C.postButton(C.CGFloat(px), C.CGFloat(py), C.int(right), 1, C.int(nextClickCount()))
	case "up":
		pressing = false
		C.postButton(C.CGFloat(px), C.CGFloat(py), C.int(pressedBtn), 0, C.int(clickCount))
	case "click":
		// The console sends a complete gesture: place the pointer, press, release.
		C.postMove(C.CGFloat(px), C.CGFloat(py), 0)
		count := nextClickCount()
		C.postButton(C.CGFloat(px), C.CGFloat(py), C.int(right), 1, C.int(count))
		C.postButton(C.CGFloat(px), C.CGFloat(py), C.int(right), 0, C.int(count))
	case "scroll":
		// The browser reports deltaY in pixels; macOS scrolls in lines.
		lines := int(delta / 40)
		if lines == 0 && delta != 0 {
			if delta < 0 {
				lines = -1
			} else {
				lines = 1
			}
		}
		C.postScroll(C.int(lines))
	default:
		logThrottled("mouse", "unhandled macOS mouse event "+event)
	}
}

// nextClickCount tracks rapid repeats so macOS sees a double click as one.
func nextClickCount() int {
	if time.Since(lastClickAt) <= 400*time.Millisecond {
		clickCount++
	} else {
		clickCount = 1
	}
	lastClickAt = time.Now()
	return clickCount
}

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// accessibilityGranted reports the Accessibility (TCC) trust for this binary.
func accessibilityGranted() bool {
	return C.accessibilityTrusted() == 1
}

// requestInputGrant asks macOS to show the Accessibility grant prompt for this
// binary: `ourway-agent --grant-input-permission`.
func requestInputGrant() {
	C.requestAccessibilityTrust()
	logThrottled("grant", "Accessibility prompt shown — approve OurWay Agent in System Settings → Privacy & Security → Accessibility")
}
