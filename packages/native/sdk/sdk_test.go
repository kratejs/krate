package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

type rig struct {
	app  *fakeplat.App
	host *sdk.Host
	main *fakeplat.Window
}

func newRig(t *testing.T) *rig {
	t.Helper()
	fa := fakeplat.New()
	h := sdk.NewHost()
	h.Attach(fa)
	w, err := fa.NewWindow(platform.WindowOptions{Title: "main", Width: 800, Height: 600, Resizable: true})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", w)
	t.Cleanup(h.Close)
	return &rig{app: fa, host: h, main: w.(*fakeplat.Window)}
}

// ui returns the page-facing handler with only the named permissions.
func (r *rig) ui(allowed ...string) http.Handler {
	reg := scheme.NewRegistry()
	r.host.RegisterUI(reg, func(p string) bool {
		for _, a := range allowed {
			if a == p {
				return true
			}
		}
		return false
	})
	return scheme.New(scheme.Options{Registry: reg})
}

func uiCall(t *testing.T, h http.Handler, fn string, args func(*wire.Encoder)) string {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__krate/call", bytes.NewReader(e.Frame()))
	req.Header.Set("x-krate-window", "ui")
	h.ServeHTTP(rec, req)
	d, err := wire.ReadFrame(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := d.RawValue()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDialogRoundTrip(t *testing.T) {
	r := newRig(t)
	r.app.FileResult = []string{"/a/b.png", "/c d/e.png"}
	got, err := dialog.OpenFile(krate.Background(), dialog.OpenOptions{
		Title: "Pick", Dir: "/home", Multiple: true, ShowHidden: true,
		Filters: []dialog.Filter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}},
	})
	if err != nil || !reflect.DeepEqual(got, r.app.FileResult) {
		t.Fatalf("OpenFile = %v, %v", got, err)
	}
	o := r.app.FileOpts[0]
	if o.Mode != caps.OpenFile || o.Title != "Pick" || o.Dir != "/home" || !o.Multiple || !o.ShowHidden ||
		!reflect.DeepEqual(o.Filters, []caps.FileFilter{{Name: "Images", Patterns: []string{"*.png", "*.jpg"}}}) {
		t.Fatalf("platform options %+v", o)
	}
	r.app.FileResult = nil
	p, err := dialog.SaveFile(krate.Background(), dialog.SaveOptions{Name: "x.txt", Overwrite: true})
	if err != nil || p != "" {
		t.Fatalf("cancelled SaveFile = %q, %v", p, err)
	}
	if o := r.app.FileOpts[1]; o.Mode != caps.SaveFile || o.Name != "x.txt" || !o.ConfirmWrite {
		t.Fatalf("save options %+v", o)
	}
	r.app.FileResult = []string{"/out.txt"}
	if p, _ = dialog.SaveFile(krate.Background(), dialog.SaveOptions{}); p != "/out.txt" {
		t.Fatalf("SaveFile = %q", p)
	}
	if _, err := dialog.OpenFolder(krate.Background(), dialog.OpenOptions{}); err != nil || r.app.FileOpts[3].Mode != caps.OpenFolder {
		t.Fatalf("OpenFolder %v %+v", err, r.app.FileOpts)
	}
	r.app.MessageReply = 1
	yes, err := dialog.Confirm(krate.Background(), "Sure?", "Delete?")
	if err != nil || !yes {
		t.Fatalf("Confirm = %v, %v", yes, err)
	}
	if m := r.app.MessageOpts[0]; m.Kind != caps.MessageQuestion || m.Title != "Sure?" || !reflect.DeepEqual(m.Buttons, []string{"No", "Yes"}) || m.Default != 1 {
		t.Fatalf("message options %+v", m)
	}
}

func TestClipboardRoundTrip(t *testing.T) {
	newRig(t)
	if err := clipboard.WriteText("héllo   \"q\""); err != nil {
		t.Fatal(err)
	}
	if s, _ := clipboard.ReadText(); s != "héllo   \"q\"" {
		t.Fatalf("text %q", s)
	}
	png := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 255}
	if err := clipboard.WriteImage(png); err != nil {
		t.Fatal(err)
	}
	if b, _ := clipboard.ReadImage(); !bytes.Equal(b, png) {
		t.Fatalf("image %v", b)
	}
	files := []string{"/x y/a.txt", "/b"}
	clipboard.WriteFiles(files)
	if f, _ := clipboard.ReadFiles(); !reflect.DeepEqual(f, files) {
		t.Fatalf("files %v", f)
	}
	clipboard.Clear()
	if s, _ := clipboard.ReadText(); s != "" {
		t.Fatal("not cleared")
	}
	if b, err := clipboard.ReadImage(); err != nil || b != nil {
		t.Fatalf("empty image = %v, %v", b, err)
	}
}

func TestPermissionGating(t *testing.T) {
	r := newRig(t)
	r.app.Text = "secret"
	// Write permission without read: ReadText is not even registered.
	h := r.ui("clipboard")
	if got := uiCall(t, h, "krate/clipboard.WriteText", func(e *wire.Encoder) { e.String("hi") }); !strings.Contains(got, `"ok":null`) {
		t.Fatalf("write: %s", got)
	}
	if r.app.Text != "hi" {
		t.Fatal("write did not reach the platform")
	}
	if got := uiCall(t, h, "krate/clipboard.ReadText", nil); !strings.Contains(got, "not_found") {
		t.Fatalf("read without clipboard:read: %s", got)
	}
	if got := uiCall(t, h, "krate/dialog.Message", func(e *wire.Encoder) { e.BeginObject(); e.EndObject() }); !strings.Contains(got, "not_found") {
		t.Fatalf("dialog without permission: %s", got)
	}
	h = r.ui("clipboard", "clipboard:read")
	if got := uiCall(t, h, "krate/clipboard.ReadText", nil); !strings.Contains(got, `"ok":"hi"`) {
		t.Fatalf("read: %s", got)
	}
	// Go-only operations never reach the page.
	h = r.ui("clipboard", "clipboard:read", "app", "window", "dialog", "menu", "tray", "notify", "shortcut")
	for _, id := range []string{"krate/app.SingleInstance", "krate/app.LaunchArgs"} {
		if got := uiCall(t, h, id, nil); !strings.Contains(got, "not_found") {
			t.Fatalf("%s reachable from the page: %s", id, got)
		}
	}
	// Malformed arguments are bad_args, not a crash.
	if got := uiCall(t, h, "krate/dialog.Message", func(e *wire.Encoder) { e.Int(3) }); !strings.Contains(got, "bad_args") {
		t.Fatalf("bad args: %s", got)
	}
}

func TestEveryUIIDHasAKnownPermission(t *testing.T) {
	r := newRig(t)
	ids := r.host.IDs()
	if len(ids) < 40 {
		t.Fatalf("only %d ids registered", len(ids))
	}
	var bad []string
	for id, perm := range ids {
		if !strings.HasPrefix(id, "krate/") {
			bad = append(bad, id)
		}
		if perm != "" && !sdk.IsUIPermission(perm) {
			bad = append(bad, id+" needs unknown permission "+perm)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("%v", bad)
	}
}

func TestMenuEvents(t *testing.T) {
	r := newRig(t)
	m := menu.New(menu.Sub("&File", menu.Action("new", "New", "CmdOrCtrl+N"), menu.Separator(), menu.Check("auto", "Autosave", true)))
	if err := menu.SetApp(m); err != nil {
		t.Fatal(err)
	}
	if len(r.app.AppMenu) != 1 || r.app.AppMenu[0].Label != "&File" || len(r.app.AppMenu[0].Submenu) != 3 ||
		r.app.AppMenu[0].Submenu[0].Accelerator != "CmdOrCtrl+N" || r.app.AppMenu[0].Submenu[2].Kind != caps.MenuItemCheck || !r.app.AppMenu[0].Submenu[2].Checked {
		t.Fatalf("platform menu %+v", r.app.AppMenu)
	}
	got := make(chan menu.Event, 4)
	cancel, err := menu.OnItem("new", func(e menu.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "menu")
	ref := r.main.Ref()
	r.app.EmitMenu(caps.MenuEvent{ID: "other", Window: ref})
	r.app.EmitMenu(caps.MenuEvent{ID: "new", Window: ref})
	select {
	case e := <-got:
		if e.ID != "new" || e.Window != "main" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	if err := menu.Popup(menu.New(menu.Action("c", "Copy", "")), "", -1, -1); err != nil || len(r.app.Popups) != 1 {
		t.Fatalf("popup %v %v", err, r.app.Popups)
	}
	if err := menu.SetApp(nil); err != nil || len(r.app.AppMenu) != 0 {
		t.Fatalf("clear %v %v", err, r.app.AppMenu)
	}
}

// waitSubs waits until a topic has a subscriber, so an event is not lost.
func waitSubs(t *testing.T, r *rig, topic string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.host.Subscribers(topic) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nobody subscribed to %s", topic)
}

func TestMenuEventsReachThePage(t *testing.T) {
	r := newRig(t)
	srv := httptest.NewServer(r.ui("menu"))
	defer srv.Close()
	// The page opens the event stream with a call, then reads it.
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String("krate/menu.events")
	e.Key("args")
	e.BeginArray()
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(srv.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.ReadFrame(resp.Body)
	raw, _ := d.RawValue()
	resp.Body.Close()
	var res struct{ Stream string }
	json.Unmarshal([]byte(strings.ReplaceAll(string(raw), `"stream"`, `"Stream"`)), &res)
	if res.Stream == "" {
		t.Fatalf("no stream: %s", raw)
	}
	sresp, err := http.Get(srv.URL + "/__krate/stream/" + res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	waitSubs(t, r, "menu")
	r.app.EmitMenu(caps.MenuEvent{ID: "save", Window: r.main.Ref(), Checked: true})
	fd, err := wire.ReadFrame(sresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = fd.RawValue()
	if string(raw) != `{"v":{"id":"save","window":"main","checked":true}}` {
		t.Fatalf("stream frame %s", raw)
	}
}

func TestWindowOps(t *testing.T) {
	r := newRig(t)
	w, err := window.Open(window.Options{Title: "Settings", Path: "/settings", Width: 500, Height: 400, Frameless: true, AlwaysOnTop: true, HasPosition: true, X: 10, Y: 20, PreventClose: true})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == "" || w.ID == "main" {
		t.Fatalf("id %q", w.ID)
	}
	nw := r.app.Windows[1]
	if nw.Opts.Path != "/settings" || nw.Opts.Width != 500 || nw.Opts.Title != "Settings" {
		t.Fatalf("opts %+v", nw.Opts)
	}
	st, err := w.State()
	if err != nil || st.Decorated || !st.AlwaysOnTop || st.X != 10 || st.Y != 20 || st.Width != 500 {
		t.Fatalf("state %+v %v", st, err)
	}
	if err := w.SetFullscreen(true); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.State(); !st.Fullscreen {
		t.Fatal("fullscreen not applied")
	}
	w.Restore()
	if st, _ = w.State(); st.Fullscreen {
		t.Fatal("restore ignored")
	}
	w.SetTitle("T2")
	w.SetSize(300, 200)
	w.Navigate("/x")
	if st, _ = w.State(); st.Title != "T2" || st.Width != 300 {
		t.Fatalf("state %+v", st)
	}
	ids, _ := window.List()
	sort.Strings(ids)
	if len(ids) != 2 {
		t.Fatalf("list %v", ids)
	}
	if err := window.Get("nope").Maximize(); err == nil || !strings.Contains(err.Error(), "no window") {
		t.Fatalf("unknown window: %v", err)
	}

	events := make(chan window.Event, 4)
	cancel, err := window.OnEvent(func(e window.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "window")
	r.app.EmitWindow(nw, caps.WindowCloseRequested)
	if e := <-events; e.Window != w.ID || e.Kind != window.CloseRequested {
		t.Fatalf("event %+v", e)
	}
	r.app.EmitWindow(nw, caps.WindowClosed)
	if e := <-events; e.Kind != window.Closed {
		t.Fatalf("event %+v", e)
	}
	if ids, _ = window.List(); len(ids) != 1 {
		t.Fatalf("closed window still listed: %v", ids)
	}
	// Saved state round trip.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KRATE_APP_ID", "test.app")
	main := window.Main()
	main.SetSize(700, 500)
	main.SetPosition(30, 40)
	if err := window.SaveState(main, "main"); err != nil {
		t.Fatal(err)
	}
	main.SetSize(100, 100)
	main.SetPosition(0, 0)
	if ok, err := window.RestoreState(main, "main"); err != nil || !ok {
		t.Fatalf("restore %v %v", ok, err)
	}
	if st, _ = main.State(); st.Width != 700 || st.Height != 500 || st.X != 30 || st.Y != 40 {
		t.Fatalf("restored %+v", st)
	}
	if ok, _ := window.RestoreState(main, "never-saved"); ok {
		t.Fatal("restored nothing")
	}
	if err := window.SaveState(main, "../evil"); err == nil {
		t.Fatal("path in state name accepted")
	}
}

func TestShortcutAndSupports(t *testing.T) {
	r := newRig(t)
	if _, err := shortcut.Register("K", func() {}); err == nil {
		t.Fatal("bare letter accepted")
	}
	if _, err := shortcut.Register("Ctrl+Hyper+K", func() {}); err == nil {
		t.Fatal("unknown modifier accepted")
	}
	var mu sync.Mutex
	n := 0
	unreg, err := shortcut.Register("Ctrl+Shift+K", func() { mu.Lock(); n++; mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	if len(r.app.Shortcuts) != 1 {
		t.Fatal("not registered on the platform")
	}
	var handle string
	for h, a := range r.app.Shortcuts {
		handle = h
		if !a.Ctrl || !a.Shift || a.Key != "K" {
			t.Fatalf("accelerator %+v", a)
		}
	}
	waitSubs(t, r, "shortcut")
	r.app.EmitShortcut(handle)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := n
		mu.Unlock()
		if c == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if n != 1 {
		t.Fatalf("callback ran %d times", n)
	}
	mu.Unlock()
	if err := unreg(); err != nil || len(r.app.Shortcuts) != 0 {
		t.Fatalf("unregister %v %v", err, r.app.Shortcuts)
	}
	if !app.Supports("dialog") || !app.Supports("clipboard") || !app.Supports("window:alwaysOnTop") && app.Supports("nonsense") {
		t.Fatal("supports")
	}
	if app.Supports("nonsense") {
		t.Fatal("unknown capability supported")
	}
	if !krate.Supports("menu") {
		t.Fatal("krate.Supports does not see the SDK")
	}
}

func TestQuit(t *testing.T) {
	r := newRig(t)
	if err := app.Quit(); err != nil || r.app.QuitCalls() != 1 {
		t.Fatalf("quit %v %d", err, r.app.QuitCalls())
	}
}

func TestNoPlatformIsUnsupported(t *testing.T) {
	t.Setenv("KRATE_GOHOST_PARENT", "")
	_, err := clipboard.ReadText()
	if err == nil || !krate.IsUnsupported(err) || !strings.Contains(err.Error(), "KRATE_GOHOST_PARENT") {
		t.Fatalf("err = %v", err)
	}
	if !errorsIs(err, krate.ErrUnsupported) {
		t.Fatal("errors.Is(ErrUnsupported) is false")
	}
}

func TestBrowserModeIsUnsupported(t *testing.T) {
	h := sdk.NewHost()
	h.Attach(nil)
	defer h.Close()
	if _, err := clipboard.ReadText(); !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := window.Open(window.Options{}); !krate.IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	if app.Supports("dialog") {
		t.Fatal("dialogs supported without a platform")
	}
}

var _ = context.Background

func errorsIs(err, target error) bool {
	type is interface{ Is(error) bool }
	for e := err; e != nil; {
		if e == target {
			return true
		}
		if i, ok := e.(is); ok && i.Is(target) {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
