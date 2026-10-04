//go:build windows

package session

import (
	"image"
	"log"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// vkMap maps browser key names (KeyboardEvent.key, lower-cased) to
// Windows virtual key codes.
var vkMap = map[string]uint16{
	"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45, "f": 0x46,
	"g": 0x47, "h": 0x48, "i": 0x49, "j": 0x4A, "k": 0x4B, "l": 0x4C,
	"m": 0x4D, "n": 0x4E, "o": 0x4F, "p": 0x50, "q": 0x51, "r": 0x52,
	"s": 0x53, "t": 0x54, "u": 0x55, "v": 0x56, "w": 0x57, "x": 0x58,
	"y": 0x59, "z": 0x5A,
	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34, "5": 0x35,
	"6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
	"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
	"f13": 0x7C, "f14": 0x7D, "f15": 0x7E, "f16": 0x7F, "f17": 0x80, "f18": 0x81,
	"f19": 0x82, "f20": 0x83, "f21": 0x84, "f22": 0x85, "f23": 0x86, "f24": 0x87,
	"enter":      windows.VK_RETURN,
	"backspace":  windows.VK_BACK,
	"tab":        windows.VK_TAB,
	"space":      windows.VK_SPACE,
	"escape":     windows.VK_ESCAPE,
	"delete":     windows.VK_DELETE,
	"insert":     windows.VK_INSERT,
	"arrowup":    windows.VK_UP,
	"arrowdown":  windows.VK_DOWN,
	"arrowleft":  windows.VK_LEFT,
	"arrowright": windows.VK_RIGHT,
	"pageup":     windows.VK_PRIOR,
	"pagedown":   windows.VK_NEXT,
	"home":       windows.VK_HOME,
	"end":        windows.VK_END,
	"shift":      windows.VK_SHIFT,
	"control":    windows.VK_CONTROL,
	"alt":        windows.VK_MENU,
	"capslock":   windows.VK_CAPITAL,
	"num0":       windows.VK_NUMPAD0, "num1": windows.VK_NUMPAD1, "num2": windows.VK_NUMPAD2,
	"num3": windows.VK_NUMPAD3, "num4": windows.VK_NUMPAD4, "num5": windows.VK_NUMPAD5,
	"num6": windows.VK_NUMPAD6, "num7": windows.VK_NUMPAD7, "num8": windows.VK_NUMPAD8,
	"num9":     windows.VK_NUMPAD9,
	"multiply": windows.VK_MULTIPLY,
	"add":      windows.VK_ADD,
	"subtract": windows.VK_SUBTRACT,
	"decimal":  windows.VK_DECIMAL,
	"divide":   windows.VK_DIVIDE,

	// Windows key and OEM punctuation (KeyboardEvent.key values on a US
	// layout); sent by the native viewer.
	"meta": windows.VK_LWIN, "os": windows.VK_LWIN,
	";": 0xBA, "=": 0xBB, ",": 0xBC, "-": 0xBD, ".": 0xBE, "/": 0xBF,
	"`": 0xC0, "[": 0xDB, "\\": 0xDC, "]": 0xDD, "'": 0xDE,
}

// synthesizeKey injects a key event via SendInput. x and y are unused
// on this platform.
func synthesizeKey(key, event string) {
	vk, ok := vkMap[strings.ToLower(key)]
	if !ok {
		log.Printf("session: no VK mapping for key %q", key)
		return
	}

	var in input
	in.Type = inputKeyboard
	in.ki().WvVk = vk
	if event == "up" {
		in.ki().DwFlags = keyeventfKeyup
	}
	if err := sendInputOne(&in); err != nil {
		log.Printf("session: SendInput key %q %s failed: %v", key, event, err)
	}
}

// inputRegion, when set, is the monitor (virtual-desktop pixels) that
// mouse percentages are relative to; nil keeps the primary-monitor
// mapping the browser viewer uses.
var inputRegion atomic.Pointer[image.Rectangle]

func setInputRegion(r *image.Rectangle) { inputRegion.Store(r) }

// synthesizeMouse injects a mouse event via SendInput. x and y are
// 0-100 percentages of the captured frame; the browser client converts
// to absolute screen pixels so a native implementation can pass them
// straight to SendInput.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	var (
		flags  uint32
		mouseD int32
	)

	switch event {
	case "move":
		flags = mouseeventfMove | mouseeventfAbsolute
	case "down", "up":
		// Separate press/release so the viewer can drag and select.
		down := event == "down"
		var f uint32
		switch button {
		case "right":
			f = mouseeventfRightup
			if down {
				f = mouseeventfRightdown
			}
		case "middle":
			f = mouseeventfMiddleup
			if down {
				f = mouseeventfMiddledown
			}
		default:
			f = mouseeventfLeftup
			if down {
				f = mouseeventfLeftdown
			}
		}
		flags = mouseeventfMove | mouseeventfAbsolute | f
	case "click":
		switch button {
		case "right":
			flags = mouseeventfMove | mouseeventfAbsolute |
				mouseeventfRightdown | mouseeventfRightup
		case "middle":
			flags = mouseeventfMove | mouseeventfAbsolute |
				mouseeventfMiddledown | mouseeventfMiddleup
		default:
			flags = mouseeventfMove | mouseeventfAbsolute |
				mouseeventfLeftdown | mouseeventfLeftup
		}
	case "wheel", "scroll":
		// Viewers send deltaY in pixels (~100 per notch, positive =
		// scroll down); SendInput expects 120 per notch with positive
		// = scroll up (away from the user), so the sign flips.
		steps := -int32(delta * 120 / 100)
		const limit = 120 * 5
		if steps > limit {
			steps = limit
		}
		if steps < -limit {
			steps = -limit
		}
		flags = mouseeventfWheel
		mouseD = steps
	default:
		log.Printf("session: unsupported mouse event %q", event)
		return
	}

	var in input
	in.Type = inputMouse
	if region := inputRegion.Load(); region != nil && flags&mouseeventfAbsolute != 0 {
		dx, dy := absVirtualCoords(*region, virtualScreenRect(), x, y)
		in.mi().Dx, in.mi().Dy = dx, dy
		flags |= mouseeventfVirtualDesk
	} else {
		in.mi().Dx = int32(x / 100 * 65535)
		in.mi().Dy = int32(y / 100 * 65535)
	}
	in.mi().MouseData = uint32(mouseD)
	in.mi().DwFlags = flags
	if err := sendInputOne(&in); err != nil {
		log.Printf("session: SendInput mouse %s failed: %v", event, err)
	}
}
