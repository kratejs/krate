//go:build linux

package a11y

import (
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/atspi"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/linux"
)

type dialogResult struct {
	button int
	err    error
}

func showMessage(parent caps.WindowRef, o caps.MessageDialogOptions) chan dialogResult {
	ch := make(chan dialogResult, 1)
	go func() {
		b, err := app.MessageDialog(parent, o)
		ch <- dialogResult{b, err}
	}()
	return ch
}

func wait(t *testing.T, ch chan dialogResult) dialogResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("the dialog did not return\n%s", treeDump())
	}
	return dialogResult{}
}

func TestMessageDialogTree(t *testing.T) {
	needWM(t)
	w := open(t, linux.WindowOptions{Title: "Dialog parent"})
	focusWindow(t, w, "Dialog parent")
	res := showMessage(w.Ref(), caps.MessageDialogOptions{
		Kind: caps.MessageQuestion, Title: "Unsaved changes", Message: "Save changes?", Detail: "They are lost otherwise.",
		Buttons: []string{"Save", "Discard", "Cancel"}, Default: 0,
	})
	dlg := waitTopLevel(t, "Unsaved changes")
	if dlg.Role() != atspi.RoleAlert && dlg.Role() != atspi.RoleDialog {
		t.Errorf("dialog role %d, want alert or dialog", dlg.Role())
	}
	for _, text := range []string{"Save changes?", "They are lost otherwise."} {
		if _, ok := dlg.Find(named(atspi.RoleLabel, text)); !ok {
			t.Errorf("message text %q is not in the dialog:\n%s", text, dlg.Dump())
		}
	}
	var buttons []string
	for _, b := range dlg.FindAll(func(n atspi.Node) bool { return n.Role() == atspi.RolePushButton }) {
		buttons = append(buttons, b.Name())
		if !b.HasState(atspi.StateFocusable) {
			t.Errorf("button %q is not focusable", b.Name())
		}
	}
	if len(buttons) != 3 || buttons[0] != "Save" || buttons[1] != "Discard" || buttons[2] != "Cancel" {
		t.Errorf("buttons = %v", buttons)
	}
	// The dialog is modal and holds the focus.
	waitFor(t, "the dialog to be active", func() bool { d, _ := topLevel("Unsaved changes"); return d.HasState(atspi.StateActive) })
	// Enter presses the default button.
	if err := client.KeySym("Return"); err != nil {
		t.Fatal(err)
	}
	if r := wait(t, res); r.err != nil || r.button != 0 {
		t.Errorf("Enter answered %+v, want button 0", r)
	}
	// Focus comes back to the window that opened the dialog.
	waitFor(t, "the parent to be active again", func() bool {
		p, ok := topLevel("Dialog parent")
		return ok && p.HasState(atspi.StateActive)
	})
}

func TestMessageDialogEscapeAndTabOrder(t *testing.T) {
	needWM(t)
	w := open(t, linux.WindowOptions{Title: "Dialog parent 2"})
	focusWindow(t, w, "Dialog parent 2")
	res := showMessage(w.Ref(), caps.MessageDialogOptions{
		Kind: caps.MessageWarning, Title: "Choose", Message: "Pick one", Buttons: []string{"One", "Two"}, Default: 1,
	})
	dlg := waitTopLevel(t, "Choose")
	waitFor(t, "the dialog to be active", func() bool { d, _ := topLevel("Choose"); return d.HasState(atspi.StateActive) })
	// The default button holds the focus; Tab reaches the other one.
	focused := func() string {
		d, _ := topLevel("Choose")
		if f, ok := d.Find(func(n atspi.Node) bool { return n.Role() == atspi.RolePushButton && n.HasState(atspi.StateFocused) }); ok {
			return f.Name()
		}
		return ""
	}
	waitFor(t, "the default button to be focused", func() bool { return focused() == "Two" })
	_ = client.KeySym("Tab")
	waitFor(t, "Tab to move focus to the other button", func() bool { f := focused(); return f != "" && f != "Two" })
	_ = dlg
	// Escape closes the dialog without a choice.
	if err := client.KeySym("Escape"); err != nil {
		t.Fatal(err)
	}
	if r := wait(t, res); r.err != nil || r.button != -1 {
		t.Errorf("Escape answered %+v, want -1", r)
	}
}

func TestMessageDialogWithoutTitleIsStillNamed(t *testing.T) {
	need(t)
	res := showMessage(0, caps.MessageDialogOptions{Kind: caps.MessageError, Message: "Disk full", Buttons: []string{"OK"}})
	var dlg atspi.Node
	waitFor(t, "an error dialog", func() bool {
		a, _ := appNode()
		for _, c := range a.Children() {
			if c.Role() == atspi.RoleAlert && c.Name() != "" {
				if _, ok := c.Find(named(atspi.RoleLabel, "Disk full")); ok {
					dlg = c
					return true
				}
			}
		}
		return false
	})
	if dlg.Name() == "" {
		t.Error("a dialog without a title has no accessible name")
	}
	// Answer it the way an assistive technology does, by its button's action.
	btn, ok := dlg.Find(named(atspi.RolePushButton, "OK"))
	if !ok || !btn.DoAction(0) {
		t.Fatalf("cannot press OK:\n%s", dlg.Dump())
	}
	if r := wait(t, res); r.err != nil || r.button != 0 {
		t.Errorf("OK answered %+v", r)
	}
}

func TestFileDialogIsNamedAndClosesWithEscape(t *testing.T) {
	needWM(t)
	w := open(t, linux.WindowOptions{Title: "File parent"})
	focusWindow(t, w, "File parent")
	done := make(chan error, 1)
	go func() {
		_, err := app.FileDialog(w.Ref(), caps.FileDialogOptions{Mode: caps.OpenFile, Title: "Pick a document"})
		done <- err
	}()
	dlg := waitTopLevel(t, "Pick a document")
	if dlg.Role() != atspi.RoleFileChooser && dlg.Role() != atspi.RoleDialog {
		t.Errorf("file dialog role %d", dlg.Role())
	}
	if _, ok := dlg.Find(func(n atspi.Node) bool { return n.Role() == atspi.RolePushButton && n.Name() == "Cancel" }); !ok {
		t.Errorf("the file dialog has no Cancel button:\n%s", dlg.Dump())
	}
	waitFor(t, "the file dialog to be active", func() bool { d, _ := topLevel("Pick a document"); return d.HasState(atspi.StateActive) })
	_ = client.KeySym("Escape")
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Escape did not close the file dialog\n%s", treeDump())
	}
	waitFor(t, "the parent to be active again", func() bool {
		p, ok := topLevel("File parent")
		return ok && p.HasState(atspi.StateActive)
	})
}
