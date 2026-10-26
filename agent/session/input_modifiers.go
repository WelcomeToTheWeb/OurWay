package session

// The platform-neutral part of the remote-input contract.
//
// The browser sends the technician's real modifier state with every key event
// (KeyboardEvent.ctrlKey/shiftKey/altKey/metaKey). Without it, Ctrl+Shift+K is
// a plain K on the far machine, and the remote session lies about what was
// typed. The mapping lives here so every platform implementation takes the
// same argument.

// InputModifiers is the exported form of the modifier state, for the
// per-session remote executable (cmd/ourway-remote), which relays keyboard
// events to the same platform synthesis.
type InputModifiers = inputModifiers

// inputModifiers is the modifier state carried with a key event.
type inputModifiers struct {
	Shift bool
	Alt   bool
	Ctrl  bool
	Meta  bool
	Fn    bool
}

// ModifiersFrom maps the input payload from the console onto the modifier
// state. Absent fields mean "not pressed", which is also what an older console
// sending no modifier keys produces.
func ModifiersFrom(input map[string]interface{}) inputModifiers {
	return modifiersFrom(input)
}

func modifiersFrom(input map[string]interface{}) inputModifiers {
	flag := func(keys ...string) bool {
		for _, k := range keys {
			if v, ok := input[k].(bool); ok && v {
				return true
			}
		}
		return false
	}
	return inputModifiers{
		Shift: flag("shift", "shiftKey"),
		Alt:   flag("alt", "altKey"),
		Ctrl:  flag("ctrl", "ctrlKey", "control"),
		Meta:  flag("meta", "metaKey", "command"),
		Fn:    flag("fn", "metaFunction"),
	}
}

// flags renders the modifier set as CGEvent flag bits (macOS). The constants
// are declared in macosmapping.go; keeping them here means the mapping compiles,
// and is tested, on every platform.
func (m inputModifiers) flags() uint64 {
	var f uint64
	if m.Shift {
		f |= macFlagShift
	}
	if m.Ctrl {
		f |= macFlagControl
	}
	if m.Alt {
		f |= macFlagOption
	}
	if m.Meta {
		f |= macFlagCommand
	}
	if m.Fn {
		f |= macFlagSecondary
	}
	return f
}

// Capability values reported to the server. "unknown" is what a device that
// does not report at all means: an older agent, a device that has never run a
// session. It is never presented as "granted".
const (
	CapabilityUnknown     = "unknown"
	CapabilityGranted     = "granted"
	CapabilityDenied      = "denied"
	CapabilityUnavailable = "unavailable"
)
