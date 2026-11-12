//go:build linux

package sdk_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/badge"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/login"
	"github.com/kratejs/krate/packages/native/sdk/power"
	"github.com/kratejs/krate/packages/native/sdk/theme"
	"github.com/kratejs/krate/packages/native/wire"
)

// hostFor attaches a Host to app and returns it with a cleanup.
func hostFor(t *testing.T, app platform.App) *sdk.Host {
	t.Helper()
	h := sdk.NewHost()
	h.Attach(app)
	t.Cleanup(h.Close)
	return h
}

type launcherUpdate struct {
	URI   string
	Props map[string]any
}

// A listener on the session bus sees the LauncherEntry signals the way a dock
// would.
func TestBadgeLauncherEntryOverDBus(t *testing.T) {
	addr := session(t)
	t.Setenv("KRATE_APP_ID", "com.acme.badge")
	listener := dial(t, addr)
	updates := make(chan launcherUpdate, 16)
	cancel, err := listener.Subscribe("type='signal',interface='com.canonical.Unity.LauncherEntry',member='Update'", func(s dbusiface.Signal) {
		u := launcherUpdate{Props: map[string]any{}}
		u.URI, _ = s.Body[0].(string)
		for k, v := range s.Body[1].(map[string]any) {
			u.Props[k] = v.(dbusiface.Variant).Value
		}
		updates <- u
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	hostFor(t, fakeplat.New())
	next := func() launcherUpdate {
		t.Helper()
		select {
		case u := <-updates:
			if u.URI != "application://com.acme.badge.desktop" {
				t.Fatalf("app uri %q", u.URI)
			}
			return u
		case <-time.After(3 * time.Second):
			t.Fatal("no LauncherEntry signal")
			return launcherUpdate{}
		}
	}

	if err := badge.Set("7"); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["count"] != int64(7) || u.Props["count-visible"] != true {
		t.Fatalf("badge 7: %+v", u)
	}
	if got, _ := badge.Get(); got != "7" {
		t.Fatalf("Get = %q", got)
	}
	if err := badge.SetProgress(badge.Normal, 0.25); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["progress"] != 0.25 || u.Props["progress-visible"] != true {
		t.Fatalf("progress: %+v", u)
	}
	if p, _ := badge.GetProgress(); p.State != badge.Normal || p.Value != 0.25 {
		t.Fatalf("GetProgress = %+v", p)
	}
	// A value outside 0..1 is clamped; indeterminate has no launcher form.
	if err := badge.SetProgress(badge.Error, 5); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["progress"] != 1.0 {
		t.Fatalf("clamped: %+v", u)
	}
	if err := badge.ClearProgress(); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["progress-visible"] != false {
		t.Fatalf("cleared: %+v", u)
	}
	if err := badge.SetUrgent(true); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["urgent"] != true {
		t.Fatalf("urgent: %+v", u)
	}
	if err := badge.SetCount(0); err != nil {
		t.Fatal(err)
	}
	if u := next(); u.Props["count-visible"] != false {
		t.Fatalf("removed: %+v", u)
	}
	if got, _ := badge.Get(); got != "" {
		t.Fatalf("Get after clear = %q", got)
	}
	// Text a launcher cannot show is refused, and the old badge stays.
	if err := badge.Set("3"); err != nil {
		t.Fatal(err)
	}
	next()
	if err := badge.Set("new"); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("text badge: %v", err)
	}
	if got, _ := badge.Get(); got != "3" {
		t.Fatalf("a refused badge must not replace the old one, Get = %q", got)
	}
	if err := badge.SetProgress("bogus", 0); err == nil {
		t.Fatal("unknown progress state accepted")
	}
}

func TestBadgeWithoutSessionBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/bus")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	hostFor(t, fakeplat.New())
	if err := badge.Set("1"); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

// themeApp is a platform that records the title bar theme it is given.
type themeApp struct {
	*fakeplat.App
	mu    sync.Mutex
	modes []string
}

func (a *themeApp) SetWindowTheme(mode string) error {
	a.mu.Lock()
	a.modes = append(a.modes, mode)
	a.mu.Unlock()
	return nil
}

func (a *themeApp) last() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.modes) == 0 {
		return ""
	}
	return a.modes[len(a.modes)-1]
}

func waitLast(t *testing.T, a *themeApp, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.last() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("window theme = %q, want %q (history %v)", a.last(), want, a.modes)
}

// The portal says dark; the SDK reports it, applies it to the title bars and
// follows SettingChanged.
func TestThemeFollowsThePortal(t *testing.T) {
	addr := session(t)
	svc := dial(t, addr)
	if err := svc.RequestName("org.freedesktop.portal.Desktop"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	vals := map[string]uint32{"color-scheme": 1, "contrast": 0}
	svc.Export("/org/freedesktop/portal/desktop", "org.freedesktop.portal.Settings", map[string]dbusiface.Method{
		"Read": func(a []any) ([]any, error) {
			mu.Lock()
			defer mu.Unlock()
			if a[0].(string) != "org.freedesktop.appearance" {
				return nil, &dbusiface.Error{Name: "org.freedesktop.portal.Error.NotFound", Message: "no such namespace"}
			}
			v, ok := vals[a[1].(string)]
			if !ok {
				return nil, &dbusiface.Error{Name: "org.freedesktop.portal.Error.NotFound", Message: "no such key"}
			}
			return []any{dbusiface.Variant{Value: dbusiface.Variant{Value: v}}}, nil
		},
	})
	change := func(key string, v uint32) {
		mu.Lock()
		vals[key] = v
		mu.Unlock()
		if err := svc.Emit("/org/freedesktop/portal/desktop", "org.freedesktop.portal.Settings", "SettingChanged",
			"org.freedesktop.appearance", key, dbusiface.Variant{Value: v}); err != nil {
			t.Fatal(err)
		}
	}

	app := &themeApp{App: fakeplat.New()}
	h := hostFor(t, app)
	waitLast(t, app, "dark")

	got, err := theme.Get()
	if err != nil || got.Scheme != "dark" || got.System != "dark" || got.Source != theme.System || got.HighContrast {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	events := make(chan theme.Theme, 8)
	cancel, err := theme.OnChange(func(th theme.Theme) { events <- th })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	waitSubs(t, &rig{host: h}, "theme")

	change("color-scheme", 2) // prefer light
	select {
	case th := <-events:
		if th.Scheme != "light" || th.System != "light" {
			t.Fatalf("event %+v", th)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SettingChanged did not reach the page")
	}
	waitLast(t, app, "light")

	change("contrast", 1)
	select {
	case th := <-events:
		if !th.HighContrast {
			t.Fatalf("event %+v", th)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("contrast change did not reach the page")
	}

	// An override wins over the system until it goes back to "system".
	if err := theme.Set(theme.Dark); err != nil {
		t.Fatal(err)
	}
	waitLast(t, app, "dark")
	if got, _ := theme.Get(); got.Scheme != "dark" || got.System != "light" || got.Source != theme.Dark {
		t.Fatalf("after Set(dark) = %+v", got)
	}
	if err := theme.Set("neon"); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err := theme.Set(theme.System); err != nil {
		t.Fatal(err)
	}
	waitLast(t, app, "light")
}

// Without a portal the GTK settings file decides.
func TestThemeFallsBackToGTKSettings(t *testing.T) {
	session(t) // a bus without a portal on it
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("GTK_THEME", "")
	mustWrite(t, cfg+"/gtk-3.0/settings.ini", "[Settings]\ngtk-theme-name=Adwaita\ngtk-application-prefer-dark-theme=1\n")
	app := &themeApp{App: fakeplat.New()}
	hostFor(t, app)
	waitLast(t, app, "dark")
	if got, _ := theme.Get(); got.Scheme != "dark" {
		t.Fatalf("Get = %+v", got)
	}
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoginThroughTheHost(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("KRATE_APP_ID", "com.acme.login")
	r := newRig(t)
	if it, err := login.Get(); err != nil || it.Enabled {
		t.Fatalf("fresh: %+v, %v", it, err)
	}
	if err := login.Set(true, login.Options{Args: []string{"--background"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg, "autostart", "com.acme.login.desktop"))
	if err != nil || !strings.Contains(string(b), " --background\n") {
		t.Fatalf("entry %q, %v", b, err)
	}
	if it, _ := login.Get(); !it.Enabled || it.Method != "autostart" {
		t.Fatalf("after Set: %+v", it)
	}
	// From the page: reading needs the permission, and so does changing.
	if got := uiCall(t, r.ui("window"), "krate/login.get", nil); !strings.Contains(got, "permission_denied") {
		t.Fatalf("without the permission: %s", got)
	}
	if got := uiCall(t, r.ui("login"), "krate/login.get", nil); !strings.Contains(got, `"enabled":true`) {
		t.Fatalf("with the permission: %s", got)
	}
	if err := login.Set(false, login.Options{}); err != nil {
		t.Fatal(err)
	}
	if it, _ := login.Get(); it.Enabled {
		t.Fatalf("after removal: %+v", it)
	}
}

func TestPowerThroughTheHost(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/bus")
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/nonexistent/system")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	r := newRig(t)
	// With no services the state still answers; idle is unknown.
	st, err := power.Get()
	if err != nil || st.IdleSeconds != -1 || st.IdleState != "unknown" {
		t.Fatalf("Get = %+v, %v", st, err)
	}
	if err := power.SetIdleThreshold(30); err != nil {
		t.Fatal(err)
	}
	if _, err := power.PreventSleep("x", false); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("PreventSleep without a session bus: %v", err)
	}
	// Sleep blockers need their own permission.
	if got := uiCall(t, r.ui("power"), "krate/power.state", nil); !strings.Contains(got, `"idleState":"unknown"`) {
		t.Fatalf("state from the page: %s", got)
	}
	got := uiCall(t, r.ui("power"), "krate/power.preventSleep", func(e *wire.Encoder) { e.String("why") })
	if !strings.Contains(got, "permission_denied") || !strings.Contains(got, "power:inhibit") {
		t.Fatalf("preventSleep with only power: %s", got)
	}
}
