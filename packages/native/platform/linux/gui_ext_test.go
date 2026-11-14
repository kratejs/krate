//go:build linux

package linux

import (
	"bytes"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func ext_newWin(t *testing.T, title string) *Window {
	t.Helper()
	loaded := make(chan struct{}, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: title, Width: 400, Height: 300, Resizable: true, Path: "/blank?" + title}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
		t.Fatal("window did not load")
	}
	t.Cleanup(w.Close)
	return w
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestGUIClipboardRich(t *testing.T) {
	needGUI(t)
	a := guiApp
	d := caps.ClipboardData{Text: "h\u00e9llo plain", HTML: "<p>h\u00e9llo <b>rich</b></p>", RTF: "{\\rtf1\\ansi hello}"}
	if err := a.WriteRich(d); err != nil {
		t.Fatal(err)
	}
	if s, err := a.ReadText(); err != nil || s != d.Text {
		t.Fatalf("text %q %v", s, err)
	}
	if h, err := a.ReadHTML(); err != nil || h != d.HTML {
		t.Fatalf("html %q %v", h, err)
	}
	if r, err := a.ReadRTF(); err != nil || r != d.RTF {
		t.Fatalf("rtf %q %v", r, err)
	}
	fm, err := a.ClipboardFormats()
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range fm {
		has[f] = true
	}
	if !has["text/plain"] || !has["text/html"] || !has["text/rtf"] {
		t.Fatalf("formats %v", fm)
	}
	if b, err := a.ReadFormat("text/html"); err != nil || string(b) != d.HTML {
		t.Fatalf("format html %q %v", b, err)
	}
	payload := []byte{0, 1, 2, 250, 'x'}
	if err := a.WriteFormat("application/x-krate-test", payload); err != nil {
		t.Fatal(err)
	}
	if b, err := a.ReadFormat("application/x-krate-test"); err != nil || !bytes.Equal(b, payload) {
		t.Fatalf("custom %v %v", b, err)
	}
	if h, _ := a.ReadHTML(); h != "" {
		t.Fatalf("html survived a replacing write: %q", h)
	}
	if err := a.WriteFormat("text/html", []byte("<i>only</i>")); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.ReadHTML(); h != "<i>only</i>" {
		t.Fatalf("html %q", h)
	}
	if err := a.Clear(); err != nil {
		t.Fatal(err)
	}
	if fm, _ := a.ClipboardFormats(); len(fm) != 0 {
		t.Fatalf("formats after clear: %v", fm)
	}
}

func TestGUIMenus(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "menu")
	events := make(chan caps.MenuEvent, 8)
	guiApp.OnMenu(func(e caps.MenuEvent) { events <- e })
	items := []caps.MenuItem{
		{Label: "&File", Kind: caps.MenuItemSubmenu, Submenu: []caps.MenuItem{
			{ID: "new", Label: "New", Accelerator: "CmdOrCtrl+N"},
			{Kind: caps.MenuItemSeparator},
			{ID: "auto", Label: "Autosave", Kind: caps.MenuItemCheck, Checked: true},
			{ID: "off", Label: "Disabled", Disabled: true},
		}},
	}
	if err := guiApp.SetAppMenu(items); err != nil {
		t.Fatal(err)
	}
	if !guiApp.ActivateMenuItem(w.Ref(), "new") {
		t.Fatal("item new not found")
	}
	select {
	case e := <-events:
		if e.ID != "new" || e.Window != w.Ref() {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no menu event")
	}
	guiApp.ActivateMenuItem(w.Ref(), "auto")
	if e := <-events; e.ID != "auto" || e.Checked {
		t.Fatalf("check event %+v (toggled from true)", e)
	}
	// A window opened later gets the menu bar too.
	w2 := ext_newWin(t, "menu2")
	if !guiApp.ActivateMenuItem(w2.Ref(), "new") {
		t.Fatal("second window has no menu")
	}
	if e := <-events; e.Window != w2.Ref() {
		t.Fatalf("event window %v", e.Window)
	}
	if err := guiApp.PopupMenu(w.Ref(), []caps.MenuItem{{ID: "ctx", Label: "Copy"}}, -1, -1); err != nil {
		t.Fatal(err)
	}
	if !guiApp.ActivateMenuItem(w.Ref(), "ctx") {
		t.Fatal("context item missing")
	}
	if e := <-events; e.ID != "ctx" {
		t.Fatalf("ctx event %+v", e)
	}
	if err := guiApp.SetAppMenu(nil); err != nil {
		t.Fatal(err)
	}
	if guiApp.ActivateMenuItem(w.Ref(), "new") {
		t.Fatal("menu not removed")
	}
}
