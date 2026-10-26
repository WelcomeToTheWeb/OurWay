package session

// macOS virtual key codes and the browser → macOS key mapping.
//
// This file carries no build tag on purpose: the mapping is pure data, and the
// only way to test "Ctrl+Shift+K lands on the macOS key code for K" is to
// compile it where tests can run (Linux CI) instead of only inside a cgo darwin
// build. The implementation that consumes it lives in input_darwin_cgo.go.
//
// Values are the Carbon kVK_* constants (Carbon/CarbonEvents.h,
// HIToolbox/Events.h). The browser's KeyboardEvent.code — a *physical* key
// identifier — is the primary lookup, because KeyboardEvent.key changes with
// shift and layout ("A" vs "a", "Question" for '/'), and a remote session must
// press the key the technician sees, not the character their layout produced.

// macKeyCode is one macOS virtual key code.
type macKeyCode uint16

const (
	macKeyNone macKeyCode = 0xFFFF

	macKeyA macKeyCode = 0x00
	macKeyS macKeyCode = 0x01
	macKeyD macKeyCode = 0x02
	macKeyF macKeyCode = 0x03
	macKeyH macKeyCode = 0x04
	macKeyG macKeyCode = 0x05
	macKeyZ macKeyCode = 0x06
	macKeyX macKeyCode = 0x07
	macKeyC macKeyCode = 0x08
	macKeyV macKeyCode = 0x09
	macKeyB macKeyCode = 0x0B
	macKeyQ macKeyCode = 0x0C
	macKeyW macKeyCode = 0x0D
	macKeyE macKeyCode = 0x0E
	macKeyR macKeyCode = 0x0F
	macKeyY macKeyCode = 0x10
	macKeyT macKeyCode = 0x11

	macKey1 macKeyCode = 0x12
	macKey2 macKeyCode = 0x13
	macKey3 macKeyCode = 0x14
	macKey4 macKeyCode = 0x15
	macKey6 macKeyCode = 0x16
	macKey5 macKeyCode = 0x17
	macKeyEqual macKeyCode = 0x18
	macKey9 macKeyCode = 0x19
	macKey7 macKeyCode = 0x1A
	macKeyMinus macKeyCode = 0x1B
	macKey8 macKeyCode = 0x1C
	macKey0 macKeyCode = 0x1D
	macKeyBracketRight macKeyCode = 0x1E
	macKeyO macKeyCode = 0x1F
	macKeyU macKeyCode = 0x20
	macKeyBracketLeft macKeyCode = 0x21
	macKeyI macKeyCode = 0x22
	macKeyP macKeyCode = 0x23
	macKeyReturn macKeyCode = 0x24
	macKeyL macKeyCode = 0x25
	macKeyJ macKeyCode = 0x26
	macKeyQuote macKeyCode = 0x27
	macKeyK macKeyCode = 0x28
	macKeySemicolon macKeyCode = 0x29
	macKeyComma macKeyCode = 0x2B
	macKeySlash macKeyCode = 0x2C
	macKeyBackslash macKeyCode = 0x2A
	macKeyN macKeyCode = 0x2D
	macKeyM macKeyCode = 0x2E
	macKeyPeriod macKeyCode = 0x2F

	macKeyGrave macKeyCode = 0x32
	macKeyTab macKeyCode = 0x30
	macKeySpace macKeyCode = 0x31
	macKeyDelete macKeyCode = 0x33 // the Backspace key on macOS
	macKeyEscape macKeyCode = 0x35
	macKeyCommand macKeyCode = 0x37
	macKeyShift macKeyCode = 0x38
	macKeyCapsLock macKeyCode = 0x39
	macKeyOption macKeyCode = 0x3A
	macKeyControl macKeyCode = 0x3B
	macKeyRightShift macKeyCode = 0x3C
	macKeyRightOption macKeyCode = 0x3D
	macKeyRightControl macKeyCode = 0x3E

	macKeyF5 macKeyCode = 0x60
	macKeyF6 macKeyCode = 0x61
	macKeyF7 macKeyCode = 0x62
	macKeyF3 macKeyCode = 0x63
	macKeyF8 macKeyCode = 0x64
	macKeyF9 macKeyCode = 0x65
	macKeyF11 macKeyCode = 0x67
	macKeyF13 macKeyCode = 0x68
	macKeyF14 macKeyCode = 0x69
	macKeyF16 macKeyCode = 0x6B
	macKeyF15 macKeyCode = 0x6C
	macKeyF10 macKeyCode = 0x6D
	macKeyF12 macKeyCode = 0x6F
	macKeyF2 macKeyCode = 0x78
	macKeyPageDown macKeyCode = 0x79
	macKeyF4 macKeyCode = 0x76
	macKeyHome macKeyCode = 0x73
	macKeyPageUp macKeyCode = 0x74
	macKeyForwardDelete macKeyCode = 0x75
	macKeyEnd macKeyCode = 0x77
	macKeyF1 macKeyCode = 0x7A
	macKeyLeftArrow macKeyCode = 0x7B
	macKeyRightArrow macKeyCode = 0x7C
	macKeyUpArrow macKeyCode = 0x7D
	macKeyDownArrow macKeyCode = 0x7E

	macKeyKeypadDecimal macKeyCode = 0x41
	macKeyKeypadMultiply macKeyCode = 0x43
	macKeyKeypadPlus macKeyCode = 0x45
	macKeyKeypadClear macKeyCode = 0x47
	macKeyKeypadDivide macKeyCode = 0x4B
	macKeyKeypadEnter macKeyCode = 0x4C
	macKeyKeypadMinus macKeyCode = 0x4E
	macKeyKeypadEquals macKeyCode = 0x51
	macKeyKeypad0 macKeyCode = 0x52
	macKeyKeypad1 macKeyCode = 0x53
	macKeyKeypad2 macKeyCode = 0x54
	macKeyKeypad3 macKeyCode = 0x55
	macKeyKeypad4 macKeyCode = 0x56
	macKeyKeypad5 macKeyCode = 0x57
	macKeyKeypad6 macKeyCode = 0x58
	macKeyKeypad7 macKeyCode = 0x59
	macKeyKeypad8 macKeyCode = 0x5A
	macKeyKeypad9 macKeyCode = 0x5B
)

// macCodeMap keys KeyboardEvent.code (the physical key) to its macOS code.
var macCodeMap = map[string]macKeyCode{
	"KeyA": macKeyA, "KeyB": macKeyB, "KeyC": macKeyC, "KeyD": macKeyD,
	"KeyE": macKeyE, "KeyF": macKeyF, "KeyG": macKeyG, "KeyH": macKeyH,
	"KeyI": macKeyI, "KeyJ": macKeyJ, "KeyK": macKeyK, "KeyL": macKeyL,
	"KeyM": macKeyM, "KeyN": macKeyN, "KeyO": macKeyO, "KeyP": macKeyP,
	"KeyQ": macKeyQ, "KeyR": macKeyR, "KeyS": macKeyS, "KeyT": macKeyT,
	"KeyU": macKeyU, "KeyV": macKeyV, "KeyW": macKeyW, "KeyX": macKeyX,
	"KeyY": macKeyY, "KeyZ": macKeyZ,

	"Digit0": macKey0, "Digit1": macKey1, "Digit2": macKey2, "Digit3": macKey3,
	"Digit4": macKey4, "Digit5": macKey5, "Digit6": macKey6, "Digit7": macKey7,
	"Digit8": macKey8, "Digit9": macKey9,

	"Minus": macKeyMinus, "Equal": macKeyEqual,
	"BracketLeft": macKeyBracketLeft, "BracketRight": macKeyBracketRight,
	"Backslash": macKeyBackslash, // kVK_ANSI_Backslash, distinct from ISO/section
	"Semicolon": macKeySemicolon, "Quote": macKeyQuote,
	"Comma": macKeyComma, "Period": macKeyPeriod, "Slash": macKeySlash,
	"Backquote": macKeyGrave,

	"Enter": macKeyReturn, "NumpadEnter": macKeyKeypadEnter,
	"Space": macKeySpace, "Tab": macKeyTab,
	"Backspace": macKeyDelete, "Delete": macKeyForwardDelete, "Escape": macKeyEscape,

	"ArrowUp": macKeyUpArrow, "ArrowDown": macKeyDownArrow,
	"ArrowLeft": macKeyLeftArrow, "ArrowRight": macKeyRightArrow,
	"PageUp": macKeyPageUp, "PageDown": macKeyPageDown,
	"Home": macKeyHome, "End": macKeyEnd,

	"ControlLeft": macKeyControl, "ControlRight": macKeyRightControl,
	"ShiftLeft": macKeyShift, "ShiftRight": macKeyRightShift,
	"AltLeft": macKeyOption, "AltRight": macKeyRightOption,
	"MetaLeft": macKeyCommand, "MetaRight": macKeyCommand,
	"CapsLock": macKeyCapsLock,

	"F1": macKeyF1, "F2": macKeyF2, "F3": macKeyF3, "F4": macKeyF4,
	"F5": macKeyF5, "F6": macKeyF6, "F7": macKeyF7, "F8": macKeyF8,
	"F9": macKeyF9, "F10": macKeyF10, "F11": macKeyF11, "F12": macKeyF12,

	"Numpad0": macKeyKeypad0, "Numpad1": macKeyKeypad1, "Numpad2": macKeyKeypad2,
	"Numpad3": macKeyKeypad3, "Numpad4": macKeyKeypad4, "Numpad5": macKeyKeypad5,
	"Numpad6": macKeyKeypad6, "Numpad7": macKeyKeypad7, "Numpad8": macKeyKeypad8,
	"Numpad9": macKeyKeypad9,
	"NumpadAdd": macKeyKeypadPlus, "NumpadSubtract": macKeyKeypadMinus,
	"NumpadMultiply": macKeyKeypadMultiply, "NumpadDivide": macKeyKeypadDivide,
	"NumpadDecimal": macKeyKeypadDecimal, "NumpadEqual": macKeyKeypadEquals,
}

// macKeyMap is the fallback when the client sends only KeyboardEvent.key
// (older console builds send `key` and leave `code` empty).
var macKeyMap = map[string]macKeyCode{
	"Enter": macKeyReturn, "Tab": macKeyTab, " ": macKeySpace,
	"Backspace": macKeyDelete, "Delete": macKeyForwardDelete, "Escape": macKeyEscape,
	"ArrowUp": macKeyUpArrow, "ArrowDown": macKeyDownArrow,
	"ArrowLeft": macKeyLeftArrow, "ArrowRight": macKeyRightArrow,
	"PageUp": macKeyPageUp, "PageDown": macKeyPageDown,
	"Home": macKeyHome, "End": macKeyEnd,
	"Control": macKeyControl, "Shift": macKeyShift, "Alt": macKeyOption,
	"Meta": macKeyCommand, "CapsLock": macKeyCapsLock,
	"F1": macKeyF1, "F2": macKeyF2, "F3": macKeyF3, "F4": macKeyF4,
	"F5": macKeyF5, "F6": macKeyF6, "F7": macKeyF7, "F8": macKeyF8,
	"F9": macKeyF9, "F10": macKeyF10, "F11": macKeyF11, "F12": macKeyF12,
	"a": macKeyA, "b": macKeyB, "c": macKeyC, "d": macKeyD, "e": macKeyE,
	"f": macKeyF, "g": macKeyG, "h": macKeyH, "i": macKeyI, "j": macKeyJ,
	"k": macKeyK, "l": macKeyL, "m": macKeyM, "n": macKeyN, "o": macKeyO,
	"p": macKeyP, "q": macKeyQ, "r": macKeyR, "s": macKeyS, "t": macKeyT,
	"u": macKeyU, "v": macKeyV, "w": macKeyW, "x": macKeyX, "y": macKeyY,
	"z": macKeyZ,
	"0": macKey0, "1": macKey1, "2": macKey2, "3": macKey3, "4": macKey4,
	"5": macKey5, "6": macKey6, "7": macKey7, "8": macKey8, "9": macKey9,
	"-": macKeyMinus, "=": macKeyEqual, "[": macKeyBracketLeft,
	"]": macKeyBracketRight, "\\": macKeyBackslash, ";": macKeySemicolon,
	"'": macKeyQuote, ",": macKeyComma, ".": macKeyPeriod, "/": macKeySlash,
	"`": macKeyGrave,
}

// CGEventFlagMask values for the browser's modifier state. The browser reports
// what the technician physically holds, so it travels with every key event:
// without it, Ctrl+Shift+K is a plain K on the far machine. The mapping is
// consumed by inputModifiers.flags() (input_modifiers.go), which is
// platform-neutral so it compiles — and is tested — everywhere.
const (
	macFlagShift     uint64 = 1 << 9  // kCGEventFlagMaskShift
	macFlagControl   uint64 = 1 << 13 // kCGEventFlagMaskControl
	macFlagOption    uint64 = 1 << 11 // kCGEventFlagMaskAlternate
	macFlagCommand   uint64 = 1 << 20 // kCGEventFlagMaskCommand
	macFlagSecondary uint64 = 1 << 21 // kCGEventFlagMaskSecondaryFn
)

// macResolveKey maps the browser's key/code pair to a macOS virtual key code.
// code wins when present (the physical key the technician aimed at); key is the
// fallback for clients that send only the printed value.
func macResolveKey(key, code string) macKeyCode {
	if code != "" {
		if kc, ok := macCodeMap[code]; ok {
			return kc
		}
	}
	if kc, ok := macKeyMap[key]; ok {
		return kc
	}
	// Shifted characters arrive as their printed form ("A", "?"), and letters
	// are the common case by far: fold the key for the lookup.
	if len(key) == 1 {
		if kc, ok := macKeyMap[string([]byte{lowerASCII(key[0])})]; ok {
			return kc
		}
	}
	return macKeyNone
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
