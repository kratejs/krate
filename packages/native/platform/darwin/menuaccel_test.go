package darwin

import "testing"

func TestAccelKeyEquivalent(t *testing.T) {
	cases := []struct {
		in   string
		key  string
		mask uint
	}{
		{"", "", 0},
		{"CmdOrCtrl+N", "n", keyModCommand},
		{"CmdOrCtrl+Shift+S", "s", keyModCommand | keyModShift},
		{"Ctrl+Alt+Delete", "", keyModControl | keyModOption},
		{"Cmd+F5", "", keyModCommand},
		{"F1", "", 0},
		{"F12", "", 0},
		{"F20", "", 0},
		{"F21", "", 0},
		{"Cmd+Space", " ", keyModCommand},
		{"Cmd+Enter", "\r", keyModCommand},
		{"Cmd+Escape", "\x1b", keyModCommand},
		{"Cmd+Left", "", keyModCommand},
		{"Cmd++", "+", keyModCommand},
		{"Cmd+plus", "+", keyModCommand},
		{"Cmd+,", ",", keyModCommand},
		{"Super+Q", "q", keyModCommand},
		{"Cmd+Hyper", "", 0},
	}
	for _, c := range cases {
		key, mask := accelKeyEquivalent(c.in)
		if key != c.key || mask != c.mask {
			t.Errorf("accelKeyEquivalent(%q) = %q, %#x; want %q, %#x", c.in, key, mask, c.key, c.mask)
		}
	}
}

func TestMenuLabel(t *testing.T) {
	for in, want := range map[string]string{
		"&File": "File", "Save && Quit": "Save & Quit", "Plain": "Plain", "": "", "&": "",
	} {
		if got := menuLabel(in); got != want {
			t.Errorf("menuLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
