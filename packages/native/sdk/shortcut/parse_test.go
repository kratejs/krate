package shortcut

import "testing"

func TestParseCmdOrCtrl(t *testing.T) {
	a, err := parse("CmdOrCtrl+Shift+K", "darwin")
	if err != nil || !a.Super || a.Ctrl || !a.Shift || a.Key != "K" {
		t.Errorf("darwin: %+v %v", a, err)
	}
	a, err = parse("CmdOrCtrl+Shift+K", "linux")
	if err != nil || a.Super || !a.Ctrl || !a.Shift {
		t.Errorf("linux: %+v %v", a, err)
	}
	a, _ = parse("Ctrl+Cmd+K", "darwin")
	if !a.Ctrl || !a.Super {
		t.Errorf("explicit Ctrl and Cmd: %+v", a)
	}
	if _, err := parse("K", "darwin"); err == nil {
		t.Error("a bare letter must be rejected")
	}
}
