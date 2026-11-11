package darwin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Global shortcuts use Carbon's RegisterEventHotKey, which needs no
// Accessibility permission. This file is the part without Objective-C or
// Carbon calls: the key table, the modifier masks and the status mapping.

// Carbon modifier masks (Events.h).
const (
	carbonCmdKey     = 1 << 8
	carbonShiftKey   = 1 << 9
	carbonOptionKey  = 1 << 11
	carbonControlKey = 1 << 12
)

// OSStatus values RegisterEventHotKey can return (CarbonEvents.h).
const (
	carbonNoErr                 int32 = 0
	carbonEventHotKeyExistsErr  int32 = -9878
	carbonEventHotKeyInvalidErr int32 = -9879
)

// carbonKeys maps a lower-case accelerator key name to a kVK_ANSI_* virtual
// key code (Events.h). Codes name physical key positions on an ANSI keyboard,
// not characters, so on other layouts a letter means the key at that position.
var carbonKeys = map[string]uint32{
	"a": 0x00, "s": 0x01, "d": 0x02, "f": 0x03, "h": 0x04, "g": 0x05, "z": 0x06, "x": 0x07,
	"c": 0x08, "v": 0x09, "b": 0x0B, "q": 0x0C, "w": 0x0D, "e": 0x0E, "r": 0x0F, "y": 0x10,
	"t": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15, "6": 0x16, "5": 0x17, "=": 0x18,
	"9": 0x19, "7": 0x1A, "-": 0x1B, "8": 0x1C, "0": 0x1D, "]": 0x1E, "o": 0x1F, "u": 0x20,
	"[": 0x21, "i": 0x22, "p": 0x23, "l": 0x25, "j": 0x26, "'": 0x27, "k": 0x28, ";": 0x29,
	"\\": 0x2A, ",": 0x2B, "/": 0x2C, "n": 0x2D, "m": 0x2E, ".": 0x2F, "`": 0x32,

	"return": 0x24, "tab": 0x30, "space": 0x31, "backspace": 0x33, "escape": 0x35,
	"delete": 0x75, "home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79, "insert": 0x72,
	"left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,

	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60, "f6": 0x61, "f7": 0x62,
	"f8": 0x64, "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F, "f13": 0x69, "f14": 0x6B,
	"f15": 0x71, "f16": 0x6A, "f17": 0x40, "f18": 0x4F, "f19": 0x50, "f20": 0x5A,

	"num0": 0x52, "num1": 0x53, "num2": 0x54, "num3": 0x55, "num4": 0x56,
	"num5": 0x57, "num6": 0x58, "num7": 0x59, "num8": 0x5B, "num9": 0x5C,
}

// keyAliases maps other spellings of a key to the names in carbonKeys.
var keyAliases = map[string]string{
	"esc": "escape", "enter": "return", "pgup": "pageup", "pgdn": "pagedown",
	"del": "delete", "ins": "insert", "arrowleft": "left", "arrowright": "right",
	"arrowup": "up", "arrowdown": "down",
	"minus": "-", "equal": "=", "comma": ",", "period": ".", "slash": "/", "backslash": "\\",
	"semicolon": ";", "quote": "'", "grave": "`", "backtick": "`",
	"leftbracket": "[", "rightbracket": "]",
}

// carbonHotKey is a Carbon key code and modifier mask.
type carbonHotKey struct {
	Code uint32
	Mods uint32
}

// toCarbon converts an accelerator. Ctrl is the Control key and Super is
// Command (the SDK parser maps Cmd and CmdOrCtrl to Super on macOS). "plus"
// is the = key with Shift added.
func toCarbon(a caps.Accelerator) (carbonHotKey, error) {
	name := strings.ToLower(strings.TrimSpace(a.Key))
	if alias, ok := keyAliases[name]; ok {
		name = alias
	}
	extraShift := false
	if name == "plus" || name == "+" {
		name, extraShift = "=", true
	}
	code, ok := carbonKeys[name]
	if !ok {
		return carbonHotKey{}, fmt.Errorf("krate: unknown key %q for a macOS global shortcut", a.Key)
	}
	var mods uint32
	if a.Super {
		mods |= carbonCmdKey
	}
	if a.Shift || extraShift {
		mods |= carbonShiftKey
	}
	if a.Alt {
		mods |= carbonOptionKey
	}
	if a.Ctrl {
		mods |= carbonControlKey
	}
	return carbonHotKey{Code: code, Mods: mods}, nil
}

// errShortcutTaken is returned when the combination is already registered.
var errShortcutTaken = errors.New("krate: this key combination is already registered as a global shortcut (by this app or another one)")

// carbonStatusError turns the OSStatus of RegisterEventHotKey into an error.
func carbonStatusError(status int32, accel string) error {
	switch status {
	case carbonNoErr:
		return nil
	case carbonEventHotKeyExistsErr:
		return fmt.Errorf("%w: %s (eventHotKeyExistsErr); choose another combination", errShortcutTaken, accel)
	case carbonEventHotKeyInvalidErr:
		return fmt.Errorf("krate: macOS rejected the global shortcut %s (eventHotKeyInvalidErr): the key and modifiers are not a valid hot key", accel)
	}
	return fmt.Errorf("krate: RegisterEventHotKey failed for %s with OSStatus %d", accel, status)
}

// accelString formats an accelerator for messages.
func accelString(a caps.Accelerator) string {
	var parts []string
	if a.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if a.Alt {
		parts = append(parts, "Alt")
	}
	if a.Shift {
		parts = append(parts, "Shift")
	}
	if a.Super {
		parts = append(parts, "Cmd")
	}
	return strings.Join(append(parts, a.Key), "+")
}

// hotKeyTable is the registry of handles and Carbon ids. It holds no Carbon
// references itself; the caller stores those alongside.
type hotKeyTable struct {
	byHandle map[string]uint32
	byID     map[uint32]string
	next     uint32
}

func newHotKeyTable() *hotKeyTable {
	return &hotKeyTable{byHandle: map[string]uint32{}, byID: map[uint32]string{}}
}

// add reserves an id for handle; it fails when the handle is in use.
func (t *hotKeyTable) add(handle string) (uint32, error) {
	if _, dup := t.byHandle[handle]; dup {
		return 0, fmt.Errorf("krate: shortcut %q is already registered", handle)
	}
	t.next++
	t.byHandle[handle] = t.next
	t.byID[t.next] = handle
	return t.next, nil
}

// remove frees the id of handle and returns it.
func (t *hotKeyTable) remove(handle string) (uint32, bool) {
	id, ok := t.byHandle[handle]
	if ok {
		delete(t.byHandle, handle)
		delete(t.byID, id)
	}
	return id, ok
}

// handleOf returns the handle for a Carbon id.
func (t *hotKeyTable) handleOf(id uint32) (string, bool) {
	h, ok := t.byID[id]
	return h, ok
}
