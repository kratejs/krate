package darwin

import (
	"errors"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestToCarbon(t *testing.T) {
	cases := []struct {
		name string
		in   caps.Accelerator
		want carbonHotKey
	}{
		{"cmd+shift+k", caps.Accelerator{Super: true, Shift: true, Key: "K"}, carbonHotKey{0x28, carbonCmdKey | carbonShiftKey}},
		{"ctrl+alt+space", caps.Accelerator{Ctrl: true, Alt: true, Key: "Space"}, carbonHotKey{0x31, carbonControlKey | carbonOptionKey}},
		{"cmd+f5", caps.Accelerator{Super: true, Key: "F5"}, carbonHotKey{0x60, carbonCmdKey}},
		{"esc alias", caps.Accelerator{Super: true, Key: "Esc"}, carbonHotKey{0x35, carbonCmdKey}},
		{"enter alias", caps.Accelerator{Super: true, Key: "Enter"}, carbonHotKey{0x24, carbonCmdKey}},
		{"arrow", caps.Accelerator{Ctrl: true, Key: "Left"}, carbonHotKey{0x7B, carbonControlKey}},
		{"digit", caps.Accelerator{Super: true, Key: "0"}, carbonHotKey{0x1D, carbonCmdKey}},
		{"minus word", caps.Accelerator{Super: true, Key: "minus"}, carbonHotKey{0x1B, carbonCmdKey}},
		{"punctuation", caps.Accelerator{Super: true, Key: ","}, carbonHotKey{0x2B, carbonCmdKey}},
		{"plus adds shift", caps.Accelerator{Super: true, Key: "plus"}, carbonHotKey{0x18, carbonCmdKey | carbonShiftKey}},
		{"plus keeps one shift", caps.Accelerator{Super: true, Shift: true, Key: "plus"}, carbonHotKey{0x18, carbonCmdKey | carbonShiftKey}},
		{"no modifiers", caps.Accelerator{Key: "F12"}, carbonHotKey{0x6F, 0}},
		{"keypad", caps.Accelerator{Ctrl: true, Key: "Num8"}, carbonHotKey{0x5B, carbonControlKey}},
	}
	for _, c := range cases {
		got, err := toCarbon(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
	if _, err := toCarbon(caps.Accelerator{Super: true, Key: "Hyper"}); err == nil || !strings.Contains(err.Error(), "Hyper") {
		t.Errorf("unknown key error = %v", err)
	}
}

// TestCarbonKeysDistinct checks the table has no two names on one key code
// other than the documented keypad-free layout (each code is used once).
func TestCarbonKeysDistinct(t *testing.T) {
	seen := map[uint32]string{}
	for name, code := range carbonKeys {
		if other, dup := seen[code]; dup {
			t.Errorf("key code 0x%02X is used by both %q and %q", code, other, name)
		}
		seen[code] = name
	}
	for alias, target := range keyAliases {
		if _, ok := carbonKeys[target]; !ok {
			t.Errorf("alias %q points at unknown key %q", alias, target)
		}
	}
}

func TestCarbonLetterAndDigitCoverage(t *testing.T) {
	for c := 'a'; c <= 'z'; c++ {
		if _, ok := carbonKeys[string(c)]; !ok {
			t.Errorf("letter %q missing", c)
		}
	}
	for c := '0'; c <= '9'; c++ {
		if _, ok := carbonKeys[string(c)]; !ok {
			t.Errorf("digit %q missing", c)
		}
	}
}

func TestCarbonStatusError(t *testing.T) {
	if err := carbonStatusError(0, "Cmd+K"); err != nil {
		t.Errorf("noErr -> %v", err)
	}
	err := carbonStatusError(carbonEventHotKeyExistsErr, "Cmd+K")
	if !errors.Is(err, errShortcutTaken) || !strings.Contains(err.Error(), "Cmd+K") {
		t.Errorf("exists err = %v", err)
	}
	if err := carbonStatusError(carbonEventHotKeyInvalidErr, "Cmd+K"); err == nil || errors.Is(err, errShortcutTaken) {
		t.Errorf("invalid err = %v", err)
	}
	if err := carbonStatusError(-50, "Cmd+K"); err == nil || !strings.Contains(err.Error(), "-50") {
		t.Errorf("other err = %v", err)
	}
}

func TestHotKeyTable(t *testing.T) {
	tb := newHotKeyTable()
	a, err := tb.add("a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := tb.add("b")
	if a == b || a == 0 || b == 0 {
		t.Fatalf("ids %d %d", a, b)
	}
	if _, err := tb.add("a"); err == nil {
		t.Error("duplicate handle accepted")
	}
	if h, ok := tb.handleOf(b); !ok || h != "b" {
		t.Errorf("handleOf = %q %v", h, ok)
	}
	if id, ok := tb.remove("a"); !ok || id != a {
		t.Errorf("remove = %d %v", id, ok)
	}
	if _, ok := tb.remove("a"); ok {
		t.Error("removed twice")
	}
	if _, ok := tb.handleOf(a); ok {
		t.Error("id survived remove")
	}
	if c, _ := tb.add("a"); c == a {
		t.Error("ids must not be reused")
	}
}

func TestAccelString(t *testing.T) {
	got := accelString(caps.Accelerator{Ctrl: true, Super: true, Key: "K"})
	if got != "Ctrl+Cmd+K" {
		t.Errorf("accelString = %q", got)
	}
}
