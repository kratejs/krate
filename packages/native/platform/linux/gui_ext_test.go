//go:build linux

package linux

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

func TestGUIWindowControls(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "ctl")
	if st := w.State(); !st.Visible || !st.Decorated || st.Fullscreen {
		t.Fatalf("initial state %+v", st)
	}
	w.SetDecorated(false)
	waitFor(t, "frameless", func() bool { return !w.State().Decorated })
	w.SetDecorated(true)
	w.SetAlwaysOnTop(true)
	waitFor(t, "above", func() bool { return w.State().AlwaysOnTop })
	w.SetAlwaysOnTop(false)
	waitFor(t, "not above", func() bool { return !w.State().AlwaysOnTop })
	w.SetPosition(120, 90)
	w.SetSize(500, 350)
	waitFor(t, "size", func() bool { s := w.State(); return s.Width == 500 && s.Height == 350 })
	w.Hide()
	waitFor(t, "hidden", func() bool { return !w.State().Visible })
	w.Show()
	waitFor(t, "shown", func() bool { return w.State().Visible })
	// Maximize, fullscreen and iconify need a window manager; the GUI
	// environment with a WM (display :98) exercises them for real.
	if os.Getenv("KRATE_NATIVE_WM") == "1" {
		w.SetFullscreen(true)
		waitFor(t, "fullscreen", func() bool { return w.State().Fullscreen })
		if w.GDKState()&winFullscreen == 0 {
			t.Fatal("GDK window state lacks FULLSCREEN")
		}
		w.SetFullscreen(false)
		waitFor(t, "not fullscreen", func() bool { return !w.State().Fullscreen })
		w.Maximize()
		waitFor(t, "maximized", func() bool { return w.State().Maximized })
		w.Restore()
		waitFor(t, "restored", func() bool { return !w.State().Maximized })
		w.Minimize()
		waitFor(t, "minimized", func() bool { return w.State().Minimized })
	}
}

func TestGUICloseRequested(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "closereq")
	var mu sync.Mutex
	var got []caps.WindowEventKind
	guiApp.OnWindowEvent(func(e caps.WindowEvent) {
		if e.Window == w.Ref() {
			mu.Lock()
			got = append(got, e.Kind)
			mu.Unlock()
		}
	})
	defer guiApp.OnWindowEvent(nil)
	w.SetPreventClose(true)
	time.Sleep(100 * time.Millisecond)
	// A close request as the window manager would send it.
	guiApp.callMain(func() {
		if deleteCallback(w.win, 0, w.id) != 1 {
			t.Error("close was not prevented")
		}
	})
	waitFor(t, "close-requested", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	if !w.State().Visible {
		t.Fatal("window closed despite prevent")
	}
	w.SetPreventClose(false)
	w.Close()
	waitFor(t, "closed", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 })
	if got[0] != caps.WindowCloseRequested || got[1] != caps.WindowClosed {
		t.Fatalf("events %v", got)
	}
}

func TestGUIClipboard(t *testing.T) {
	needGUI(t)
	a := guiApp
	if err := a.WriteText("héllo clip"); err != nil {
		t.Fatal(err)
	}
	if s, err := a.ReadText(); err != nil || s != "héllo clip" {
		t.Fatalf("text %q %v", s, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 60), uint8(y * 80), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteImage(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	back, err := a.ReadImage()
	if err != nil || len(back) == 0 {
		t.Fatalf("image %d bytes, %v", len(back), err)
	}
	dec, err := png.Decode(bytes.NewReader(back))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds() != img.Bounds() {
		t.Fatalf("bounds %v", dec.Bounds())
	}
	r, g, b, _ := dec.At(3, 2).RGBA()
	if r>>8 != 180 || g>>8 != 160 || b>>8 != 200 {
		t.Fatalf("pixel %d %d %d", r>>8, g>>8, b>>8)
	}
	files := []string{"/tmp/a b.txt", "/var/x#y.png"}
	if err := a.WriteFiles(files); err != nil {
		t.Fatal(err)
	}
	got, err := a.ReadFiles()
	if err != nil || !reflect.DeepEqual(got, files) {
		t.Fatalf("files %v %v", got, err)
	}
	if err := a.Clear(); err != nil {
		t.Fatal(err)
	}
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

func TestGUIDialogs(t *testing.T) {
	needGUI(t)
	type seen struct {
		title    string
		action   int
		multiple bool
		filters  []string
	}
	got := make(chan seen, 1)
	DialogHook = func(p *DialogProbe) bool {
		if p.Kind == "file" {
			got <- seen{p.Title(), p.Action(), p.Multiple(), p.FilterNames()}
			p.Cancel()
		}
		return true
	}
	defer func() { DialogHook = nil }()
	paths, err := guiApp.FileDialog(0, caps.FileDialogOptions{
		Mode: caps.OpenFile, Title: "Pick images", Multiple: true,
		Filters: []caps.FileFilter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}, {Name: "All", Patterns: []string{"*"}}},
	})
	if err != nil || len(paths) != 0 {
		t.Fatalf("cancelled dialog: %v %v", paths, err)
	}
	s := <-got
	if s.title != "Pick images" || s.action != 0 || !s.multiple || !reflect.DeepEqual(s.filters, []string{"Images", "All"}) {
		t.Fatalf("dialog options %+v", s)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "chosen.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	DialogHook = func(p *DialogProbe) bool {
		p.Show()
		go func() {
			time.Sleep(700 * time.Millisecond)
			guiApp.callMain(func() { p.Accept(file) })
		}()
		return true
	}
	paths, err = guiApp.FileDialog(0, caps.FileDialogOptions{Mode: caps.OpenFile, Dir: dir})
	if err != nil || len(paths) != 1 || paths[0] != file {
		t.Fatalf("accepted dialog: %v %v", paths, err)
	}

	DialogHook = func(p *DialogProbe) bool {
		if p.Kind != "message" || p.Title() != "Sure?" {
			t.Errorf("message dialog %s %q", p.Kind, p.Title())
		}
		p.Press(1)
		return true
	}
	n, err := guiApp.MessageDialog(0, caps.MessageDialogOptions{Kind: caps.MessageQuestion, Title: "Sure?", Message: "Delete it?", Detail: "No undo.", Buttons: []string{"Cancel", "Delete"}})
	if err != nil || n != 1 {
		t.Fatalf("message dialog %d %v", n, err)
	}
	DialogHook = func(p *DialogProbe) bool { p.Cancel(); return true }
	if n, _ = guiApp.MessageDialog(0, caps.MessageDialogOptions{Message: "x"}); n != -1 {
		t.Fatalf("closed message dialog returned %d", n)
	}
}
