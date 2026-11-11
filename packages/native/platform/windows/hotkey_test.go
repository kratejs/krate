package windows

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestVirtualKey(t *testing.T) {
	cases := map[string]uint32{
		"A": 0x41, "k": 0x4B, "Z": 0x5A, "0": 0x30, "9": 0x39,
		"F1": 0x70, "f12": 0x7B, "F24": 0x87, "Space": 0x20, "Escape": 0x1B, "Esc": 0x1B,
		"Enter": 0x0D, "Tab": 0x09, "Backspace": 0x08, "Delete": 0x2E, "Up": 0x26, "Left": 0x25,
		"PageDown": 0x22, "plus": 0xBB, "minus": 0xBD, "Numpad0": 0x60, "numpad9": 0x69,
		"MediaPlayPause": 0xB3, "VolumeMute": 0xAD, "PrintScreen": 0x2C,
	}
	for k, want := range cases {
		got, ok := virtualKey(k)
		if !ok || got != want {
			t.Errorf("virtualKey(%q) = %#x, %v; want %#x", k, got, ok, want)
		}
	}
	for _, k := range []string{"", "F0", "F25", "F01", "Numpad10", "NumpadX", "Foo", "!", "ab"} {
		if v, ok := virtualKey(k); ok {
			t.Errorf("virtualKey(%q) = %#x, want failure", k, v)
		}
	}
}

func TestHotkeySpec(t *testing.T) {
	mods, vk, err := hotkeySpec(caps.Accelerator{Ctrl: true, Shift: true, Key: "K"})
	if err != nil || vk != 0x4B || mods != modControl|modShift|modNoRepeat {
		t.Errorf("ctrl+shift+K = %#x %#x %v", mods, vk, err)
	}
	mods, _, _ = hotkeySpec(caps.Accelerator{Alt: true, Super: true, Key: "F5"})
	if mods != modAlt|modWin|modNoRepeat {
		t.Errorf("alt+win mods = %#x", mods)
	}
	if mods&modNoRepeat == 0 {
		t.Error("MOD_NOREPEAT must always be set")
	}
	if _, _, err := hotkeySpec(caps.Accelerator{Ctrl: true, Key: "Bogus"}); err == nil || !strings.Contains(err.Error(), "Bogus") {
		t.Errorf("unknown key error = %v", err)
	}
}

func TestHotkeyError(t *testing.T) {
	a := caps.Accelerator{Ctrl: true, Alt: true, Key: "T"}
	err := hotkeyError(a, errHotkeyDup)
	if !strings.Contains(err.Error(), "already registered by another app") || !strings.Contains(err.Error(), "Ctrl+Alt+T") {
		t.Errorf("dup error = %v", err)
	}
	if e := hotkeyError(a, 12345); !strings.Contains(e.Error(), "12345") {
		t.Errorf("generic error = %v", e)
	}
}

func TestHotkeyTable(t *testing.T) {
	tb := newHotkeyTable()
	a := caps.Accelerator{Ctrl: true, Key: "J"}
	id1, err := tb.add("one", a)
	if err != nil || id1 != 1 {
		t.Fatalf("add one = %d %v", id1, err)
	}
	if _, err := tb.add("one", a); err == nil {
		t.Error("duplicate handle accepted")
	}
	id2, _ := tb.add("two", a)
	if h, ok := tb.handle(id2); !ok || h != "two" {
		t.Errorf("handle(%d) = %q %v", id2, h, ok)
	}
	if got := tb.remove("one"); got != id1 {
		t.Errorf("remove one = %d", got)
	}
	if got := tb.remove("one"); got != 0 {
		t.Errorf("second remove = %d", got)
	}
	if _, ok := tb.handle(id1); ok {
		t.Error("released id still resolves")
	}
	id3, _ := tb.add("one", a)
	if id3 == id1 {
		t.Error("ids must not be reused")
	}
	if len(tb.ids()) != 2 {
		t.Errorf("ids = %v", tb.ids())
	}
}
