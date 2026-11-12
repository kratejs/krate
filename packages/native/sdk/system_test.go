package sdk_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/crashreport"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/crash"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/screen"
)

func rigWith(t *testing.T, app platform.App, fa *fakeplat.App) *rig {
	t.Helper()
	h := sdk.NewHost()
	h.Attach(app)
	w, err := app.NewWindow(platform.WindowOptions{Title: "main", Width: 800, Height: 600, Resizable: true})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", w)
	t.Cleanup(h.Close)
	return &rig{app: fa, host: h, main: w.(*fakeplat.Window)}
}

type displaysApp struct {
	*fakeplat.App
	mu   sync.Mutex
	list []caps.Display
	fn   func()
}

func (a *displaysApp) Displays() ([]caps.Display, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]caps.Display(nil), a.list...), nil
}

func (a *displaysApp) OnDisplays(fn func()) { a.mu.Lock(); a.fn = fn; a.mu.Unlock() }

func (a *displaysApp) change(list []caps.Display) {
	a.mu.Lock()
	a.list = list
	fn := a.fn
	a.mu.Unlock()
	fn()
}

var twoDisplays = []caps.Display{
	{ID: "m0", Name: "Left", Primary: true, X: 0, Y: 0, Width: 1920, Height: 1080, WorkX: 0, WorkY: 30, WorkWidth: 1920, WorkHeight: 1050, Scale: 1, RefreshHz: 60},
	{ID: "m1", Name: "Right", X: 1920, Y: 0, Width: 2560, Height: 1440, WorkX: 1920, WorkY: 0, WorkWidth: 2560, WorkHeight: 1400, Scale: 2},
}

func TestScreenOps(t *testing.T) {
	fa := fakeplat.New()
	app := &displaysApp{App: fa, list: twoDisplays}
	r := rigWith(t, app, fa)

	l, err := screen.List()
	if err != nil || len(l) != 2 || l[0].ID != "m0" || !l[0].Primary || l[1].Scale != 2 || l[0].RefreshHz != 60 {
		t.Fatalf("List = %+v, %v", l, err)
	}
	if p, err := screen.Primary(); err != nil || p.ID != "m0" {
		t.Fatalf("Primary = %+v, %v", p, err)
	}
	r.main.SetPosition(2000, 100) // 800x600 window mostly on the right display
	if d, err := screen.OfWindow(""); err != nil || d.ID != "m1" {
		t.Fatalf("OfWindow = %+v, %v", d, err)
	}
	if err := screen.CenterWindow("", ""); err != nil {
		t.Fatal(err)
	}
	if st := r.main.State(); st.X != 1920+(2560-800)/2 || st.Y != (1400-600)/2 {
		t.Fatalf("centered on its own display: %+v", st)
	}
	if err := screen.CenterWindow("", "m0"); err != nil {
		t.Fatal(err)
	}
	if st := r.main.State(); st.X != (1920-800)/2 || st.Y != 30+(1050-600)/2 {
		t.Fatalf("centered on m0: %+v", st)
	}
	if err := screen.CenterWindow("", "nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown display: %v", err)
	}
	if err := screen.CenterWindow("ghost", ""); err == nil {
		t.Fatal("unknown window accepted")
	}

	events := make(chan screen.Event, 4)
	cancel, err := screen.OnChange(func(e screen.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "screen")
	app.change(twoDisplays[:1])
	select {
	case e := <-events:
		if len(e.Displays) != 1 || e.Displays[0].ID != "m0" {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no display change event")
	}
	if !r.host.SupportsForTest("screen") {
		t.Error("screen not supported with a Displays platform")
	}
}

func TestScreenPermissionAndUnsupported(t *testing.T) {
	fa := fakeplat.New()
	r := rigWith(t, &displaysApp{App: fa, list: twoDisplays}, fa)
	if got := uiCall(t, r.ui("window"), "krate/screen.list", nil); !strings.Contains(got, "permission_denied") || !strings.Contains(got, `"screen"`) {
		t.Fatalf("without the permission: %s", got)
	}
	got := uiCall(t, r.ui("screen"), "krate/screen.primary", nil)
	if !strings.Contains(got, `"id":"m0"`) || !strings.Contains(got, `"workY":30`) {
		t.Fatalf("with the permission: %s", got)
	}

	plain := rigWith(t, fakeplat.New(), fakeplat.New())
	if _, err := screen.List(); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("a platform without displays: %v", err)
	}
	if plain.host.SupportsForTest("screen") {
		t.Error("screen supported without the capability")
	}
}

type crashApp struct {
	*fakeplat.App
	mu       sync.Mutex
	fn       func(caps.ViewCrash)
	reloaded []caps.ViewRef
}

func (a *crashApp) OnViewCrash(fn func(caps.ViewCrash)) { a.mu.Lock(); a.fn = fn; a.mu.Unlock() }
func (a *crashApp) ReloadView(v caps.ViewRef) error {
	a.mu.Lock()
	a.reloaded = append(a.reloaded, v)
	a.mu.Unlock()
	return nil
}
func (a *crashApp) crash(c caps.ViewCrash) { a.mu.Lock(); fn := a.fn; a.mu.Unlock(); fn(c) }
func (a *crashApp) reloads() int           { a.mu.Lock(); defer a.mu.Unlock(); return len(a.reloaded) }

func TestWebviewCrashEventsReportsAndAutoReload(t *testing.T) {
	dir := t.TempDir() + "/crashes"
	crashreport.Configure(crashreport.Config{Dir: dir, AppID: "com.acme.crash", AppVersion: "9", Process: "shell"})
	t.Cleanup(func() { crashreport.Configure(crashreport.Config{}) })

	fa := fakeplat.New()
	app := &crashApp{App: fa}
	r := rigWith(t, app, fa)
	view := r.main.PrimaryView()

	events := make(chan crash.Event, 8)
	cancel, err := crash.OnCrash(func(e crash.Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, r, "crash")
	next := func() crash.Event {
		t.Helper()
		select {
		case e := <-events:
			return e
		case <-time.After(3 * time.Second):
			t.Fatal("no crash event")
			return crash.Event{}
		}
	}

	// Without auto reload the crash is reported and the view left alone.
	app.crash(caps.ViewCrash{View: view, Reason: "crashed"})
	e := next()
	if e.Kind != "webview" || e.Reason != "crashed" || e.Window != "main" || e.View != "main" || e.Reloaded || e.ReportID == "" {
		t.Fatalf("event %+v", e)
	}
	if app.reloads() != 0 {
		t.Fatal("reloaded without being asked")
	}
	rep, err := crash.Read(e.ReportID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Kind != "webview" || rep.Process != "webview" || rep.Reason != "crashed" || rep.Window != "main" || rep.AppID != "com.acme.crash" || rep.AppVersion != "9" || rep.OS == "" {
		t.Fatalf("report %+v", rep)
	}

	// With auto reload, the first reloads are done and the loop is cut.
	if err := crash.SetAutoReload(true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		app.crash(caps.ViewCrash{View: view, Reason: "killed", ExitCode: 9})
		if e := next(); !e.Reloaded {
			t.Fatalf("crash %d was not reloaded: %+v", i, e)
		}
	}
	app.crash(caps.ViewCrash{View: view, Reason: "crashed"})
	if e := next(); e.Reloaded {
		t.Fatalf("a crash loop must not reload forever: %+v", e)
	}
	if app.reloads() != 3 {
		t.Fatalf("reloads = %d", app.reloads())
	}

	list, err := crash.List()
	if err != nil || len(list) != 5 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	if err := crash.Delete(e.ReportID); err != nil {
		t.Fatal(err)
	}
	if _, err := crash.Read(e.ReportID); err == nil {
		t.Fatal("deleted report still readable")
	}
	if _, err := crash.Read("../../etc/passwd"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if list, _ = crash.List(); len(list) != 4 {
		t.Fatalf("after delete: %d", len(list))
	}
}

func TestCrashWithoutPlatformSupport(t *testing.T) {
	crashreport.Configure(crashreport.Config{Dir: t.TempDir()})
	t.Cleanup(func() { crashreport.Configure(crashreport.Config{}) })
	r := rigWith(t, fakeplat.New(), fakeplat.New())
	if err := crash.SetAutoReload(true); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("SetAutoReload = %v", err)
	}
	// Reports of Go crashes work without webview support.
	if l, err := crash.List(); err != nil || len(l) != 0 {
		t.Fatalf("List = %v, %v", l, err)
	}
	if r.host.SupportsForTest("crash") {
		t.Error("crash supported without webview crash events")
	}
	crashreport.Configure(crashreport.Config{})
	t.Setenv(crashreport.DirEnv, "")
	if _, err := crash.List(); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("without a directory: %v", err)
	}
}
