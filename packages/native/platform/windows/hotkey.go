package windows

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Global shortcut logic (pure; the RegisterHotKey glue is in
// hotkey_windows.go).

// RegisterHotKey modifier flags (winuser.h).
const (
	modAlt       = 0x0001
	modControl   = 0x0002
	modShift     = 0x0004
	modWin       = 0x0008
	modNoRepeat  = 0x4000
	wmHotkey     = 0x0312
	errHotkeyDup = 1409 // ERROR_HOTKEY_ALREADY_REGISTERED
	errHotkeyBad = 1419 // ERROR_HOTKEY_NOT_REGISTERED
)

// hotkeyMods maps an accelerator's modifiers to MOD_* flags. MOD_NOREPEAT is
// always set: holding the keys down must not fire the shortcut repeatedly.
func hotkeyMods(a caps.Accelerator) uint32 {
	m := uint32(modNoRepeat)
	if a.Alt {
		m |= modAlt
	}
	if a.Ctrl {
		m |= modControl
	}
	if a.Shift {
		m |= modShift
	}
	if a.Super {
		m |= modWin
	}
	return m
}

// namedKeys maps lower-case key names to virtual-key codes (winuser.h VK_*).
var namedKeys = map[string]uint32{
	"space": 0x20, "escape": 0x1B, "esc": 0x1B, "enter": 0x0D, "return": 0x0D,
	"tab": 0x09, "backspace": 0x08, "delete": 0x2E, "del": 0x2E, "insert": 0x2D,
	"home": 0x24, "end": 0x23, "pageup": 0x21, "pagedown": 0x22,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"printscreen": 0x2C, "pause": 0x13, "capslock": 0x14,
	// VK_OEM_PLUS, VK_OEM_MINUS, VK_OEM_COMMA, VK_OEM_PERIOD: the keys next
	// to M and 0 on a US layout. Other layouts place them elsewhere; the
	// virtual-key code still names the same logical character key.
	"plus": 0xBB, "minus": 0xBD, "comma": 0xBC, "period": 0xBE,
	"mediaplaypause": 0xB3, "medianext": 0xB0, "mediaprev": 0xB1, "mediaprevious": 0xB1,
	"mediastop": 0xB2, "volumeup": 0xAF, "volumedown": 0xAE, "volumemute": 0xAD,
}

// virtualKey maps an accelerator key name ("K", "7", "F5", "Space",
// "Numpad3") to a virtual-key code.
func virtualKey(name string) (uint32, bool) {
	k := strings.ToLower(strings.TrimSpace(name))
	if k == "" {
		return 0, false
	}
	if v, ok := namedKeys[k]; ok {
		return v, true
	}
	if len(k) == 1 {
		switch c := k[0]; {
		case c >= 'a' && c <= 'z':
			return uint32(c-'a') + 0x41, true
		case c >= '0' && c <= '9':
			return uint32(c-'0') + 0x30, true
		}
		return 0, false
	}
	var n int
	if rest, ok := strings.CutPrefix(k, "numpad"); ok {
		if _, err := fmt.Sscanf(rest, "%d", &n); err == nil && n >= 0 && n <= 9 && len(rest) == 1 {
			return uint32(n) + 0x60, true
		}
		return 0, false
	}
	if rest, ok := strings.CutPrefix(k, "f"); ok {
		if _, err := fmt.Sscanf(rest, "%d", &n); err == nil && n >= 1 && n <= 24 && fmt.Sprint(n) == rest {
			return uint32(n-1) + 0x70, true
		}
	}
	return 0, false
}

// hotkeySpec converts an accelerator to RegisterHotKey arguments.
func hotkeySpec(a caps.Accelerator) (mods, vk uint32, err error) {
	vk, ok := virtualKey(a.Key)
	if !ok {
		return 0, 0, fmt.Errorf("windows: unknown shortcut key %q", a.Key)
	}
	return hotkeyMods(a), vk, nil
}

// hotkeyError turns the GetLastError value of a failed RegisterHotKey into
// an error that tells the user what to do.
func hotkeyError(accel caps.Accelerator, code uint32) error {
	name := accelName(accel)
	switch code {
	case errHotkeyDup:
		return fmt.Errorf("windows: the shortcut %s is already registered by another app (or reserved by Windows); choose another combination", name)
	case 5: // ERROR_ACCESS_DENIED
		return fmt.Errorf("windows: the shortcut %s cannot be registered: access denied", name)
	case 87: // ERROR_INVALID_PARAMETER
		return fmt.Errorf("windows: the shortcut %s is not a valid key combination", name)
	}
	return fmt.Errorf("windows: registering the shortcut %s failed (error %d)", name, code)
}

func accelName(a caps.Accelerator) string {
	var p []string
	if a.Ctrl {
		p = append(p, "Ctrl")
	}
	if a.Alt {
		p = append(p, "Alt")
	}
	if a.Shift {
		p = append(p, "Shift")
	}
	if a.Super {
		p = append(p, "Win")
	}
	return strings.Join(append(p, a.Key), "+")
}

// hotkeyTable maps the caller's handles to the integer ids RegisterHotKey
// wants and back. It is used on the main thread only.
type hotkeyTable struct {
	next    int32
	byID    map[int32]string
	byName  map[string]int32
	pending map[string]caps.Accelerator
}

func newHotkeyTable() *hotkeyTable {
	return &hotkeyTable{byID: map[int32]string{}, byName: map[string]int32{}, pending: map[string]caps.Accelerator{}}
}

// add reserves an id for handle. Ids start at 1 and are never reused within
// one process, so a late WM_HOTKEY for a released id cannot hit a new owner.
func (t *hotkeyTable) add(handle string, a caps.Accelerator) (int32, error) {
	if _, dup := t.byName[handle]; dup {
		return 0, fmt.Errorf("windows: shortcut handle %q is already registered", handle)
	}
	t.next++
	t.byID[t.next] = handle
	t.byName[handle] = t.next
	t.pending[handle] = a
	return t.next, nil
}

// remove releases handle and returns its id (0 when unknown).
func (t *hotkeyTable) remove(handle string) int32 {
	id, ok := t.byName[handle]
	if !ok {
		return 0
	}
	delete(t.byName, handle)
	delete(t.byID, id)
	delete(t.pending, handle)
	return id
}

func (t *hotkeyTable) handle(id int32) (string, bool) {
	h, ok := t.byID[id]
	return h, ok
}

// ids lists the registered ids (for shutdown).
func (t *hotkeyTable) ids() []int32 {
	out := make([]int32, 0, len(t.byID))
	for id := range t.byID {
		out = append(out, id)
	}
	return out
}
