//go:build linux

package power

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const sessionObj = "/org/freedesktop/login1/session/_342"

// fakes is a pair of private buses with the services the provider talks to.
type fakes struct {
	sysAddr, sessAddr string
	sys, sess         *dbusiface.Client

	mu        sync.Mutex
	onBattery bool
	locked    bool
	idleHint  bool
	idleSince uint64
	idleMs    uint32
	cookies   map[string]uint32 // service -> live cookie, 0 when none
	nextC     uint32
	calls     []string
}

func connect(t *testing.T, addr string) *dbusiface.Client {
	t.Helper()
	c, err := dbusiface.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func newFakes(t *testing.T) *fakes {
	t.Helper()
	f := &fakes{cookies: map[string]uint32{}}
	f.sysAddr = dbustest.Start(t)
	f.sessAddr = dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", f.sessAddr)
	t.Setenv("XDG_SESSION_ID", "342")
	t.Setenv("KRATE_APP_ID", "com.acme.power")
	sessionbus.Reset()
	t.Cleanup(sessionbus.Reset)
	f.sys, f.sess = connect(t, f.sysAddr), connect(t, f.sessAddr)

	for _, n := range []string{"org.freedesktop.login1", "org.freedesktop.UPower"} {
		if err := f.sys.RequestName(n); err != nil {
			t.Fatal(err)
		}
	}
	f.sys.Export("/org/freedesktop/login1", "org.freedesktop.login1.Manager", map[string]dbusiface.Method{
		"GetSession": func(a []any) ([]any, error) {
			if a[0].(string) != "342" {
				return nil, &dbusiface.Error{Name: "org.freedesktop.login1.NoSuchSession"}
			}
			return []any{dbusiface.ObjectPath(sessionObj)}, nil
		},
	})
	f.sys.ExportProperties(sessionObj, "org.freedesktop.login1.Session", func() map[string]dbusiface.Variant {
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]dbusiface.Variant{
			"LockedHint":    {Value: f.locked},
			"IdleHint":      {Value: f.idleHint},
			"IdleSinceHint": {Value: f.idleSince},
		}
	})
	f.sys.ExportProperties("/org/freedesktop/UPower", "org.freedesktop.UPower", func() map[string]dbusiface.Variant {
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]dbusiface.Variant{"OnBattery": {Value: f.onBattery}}
	})
	return f
}

func (f *fakes) provider(t *testing.T) *linuxProvider {
	t.Helper()
	p := &linuxProvider{sysAddr: func() string { return f.sysAddr }, supplyRoot: filepath.Join(t.TempDir(), "none")}
	return p
}

// idleService serves org.freedesktop.ScreenSaver idle time on the session bus.
func (f *fakes) idleService(t *testing.T) {
	t.Helper()
	if err := f.sess.RequestName("org.freedesktop.ScreenSaver"); err != nil {
		t.Fatal(err)
	}
	f.sess.Export("/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver", map[string]dbusiface.Method{
		"GetSessionIdleTime": func([]any) ([]any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return []any{f.idleMs}, nil
		},
	})
}

// cookieService exports Inhibit and the matching release method.
func (f *fakes) cookieService(t *testing.T, name, path, iface, inhibit, release string, nargs int) {
	t.Helper()
	if err := f.sess.RequestName(name); err != nil {
		t.Fatal(err)
	}
	f.sess.Export(path, iface, map[string]dbusiface.Method{
		inhibit: func(a []any) ([]any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(a) != nargs {
				return nil, &dbusiface.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs"}
			}
			f.nextC++
			f.cookies[name+path] = f.nextC
			f.calls = append(f.calls, name+" "+inhibit)
			return []any{f.nextC}, nil
		},
		release: func(a []any) ([]any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if a[0].(uint32) == f.cookies[name+path] {
				f.cookies[name+path] = 0
			}
			f.calls = append(f.calls, name+" "+release)
			return nil, nil
		},
	})
}

func (f *fakes) live(key string) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cookies[key]
}

func TestSampleFromServices(t *testing.T) {
	f := newFakes(t)
	f.idleService(t)
	p := f.provider(t)

	f.mu.Lock()
	f.onBattery, f.locked, f.idleMs = true, true, 12500
	f.mu.Unlock()
	s, err := p.sample()
	if err != nil {
		t.Fatal(err)
	}
	if !s.OnBattery || !s.Locked || s.IdleSeconds != 12.5 {
		t.Fatalf("sample = %+v", s)
	}
	f.mu.Lock()
	f.onBattery, f.locked = false, false
	f.mu.Unlock()
	if s, _ = p.sample(); s.OnBattery || s.Locked {
		t.Fatalf("sample = %+v", s)
	}
}

func TestIdleFallsBackToLogind(t *testing.T) {
	f := newFakes(t) // no screensaver service on the session bus
	p := f.provider(t)
	if s, _ := p.sample(); s.IdleSeconds != 0 {
		t.Fatalf("an active session is 0 seconds idle, got %v", s.IdleSeconds)
	}
	f.mu.Lock()
	f.idleHint = true
	f.idleSince = uint64(time.Now().Add(-90 * time.Second).UnixMicro())
	f.mu.Unlock()
	s, _ := p.sample()
	if s.IdleSeconds < 89 || s.IdleSeconds > 95 {
		t.Fatalf("logind idle = %v", s.IdleSeconds)
	}
}

func TestBatteryFromSysfsWhenNoUPower(t *testing.T) {
	f := newFakes(t)
	f.sys.Close() // UPower is gone
	root := t.TempDir()
	put := func(dir string, kv map[string]string) {
		for k, v := range kv {
			p := filepath.Join(root, dir, k)
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(v+"\n"), 0o644)
		}
	}
	put("BAT0", map[string]string{"type": "Battery", "status": "Discharging"})
	put("AC", map[string]string{"type": "Mains", "online": "0"})
	p := &linuxProvider{sysAddr: func() string { return "unix:path=/nonexistent" }, supplyRoot: root}
	if s, _ := p.sample(); !s.OnBattery {
		t.Error("discharging with the charger offline is on battery")
	}
	put("AC", map[string]string{"online": "1"})
	if s, _ := p.sample(); s.OnBattery {
		t.Error("charger online is not on battery")
	}
	if b, ok := batteryFromSysfs(filepath.Join(root, "missing")); ok || b {
		t.Error("no power_supply directory means no answer")
	}
}

func collect(m *manager) (get func() []string) {
	var mu sync.Mutex
	var seen []string
	m.pub = func(e Event) { mu.Lock(); seen = append(seen, e.Kind); mu.Unlock() }
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func has(list []string, k string) bool {
	for _, e := range list {
		if e == k {
			return true
		}
	}
	return false
}

func TestEventsFromLogindAndPolling(t *testing.T) {
	old := pollEvery
	pollEvery = 20 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	f := newFakes(t)
	f.idleService(t)
	m := &manager{prov: f.provider(t)}
	seen := collect(m)
	m.tr.setThreshold(60)
	m.open()
	defer m.close()
	time.Sleep(60 * time.Millisecond) // the baseline sample

	emit := func(path, iface, name string, args ...any) {
		t.Helper()
		if err := f.sys.Emit(path, iface, name, args...); err != nil {
			t.Fatal(err)
		}
	}
	emit("/org/freedesktop/login1", "org.freedesktop.login1.Manager", "PrepareForSleep", true)
	waitFor(t, "suspend", func() bool { return has(seen(), "suspend") })
	emit("/org/freedesktop/login1", "org.freedesktop.login1.Manager", "PrepareForSleep", false)
	waitFor(t, "resume", func() bool { return has(seen(), "resume") })

	// Lock and Unlock count only from this login session.
	emit("/org/freedesktop/login1/session/_999", "org.freedesktop.login1.Session", "Lock")
	emit(sessionObj, "org.freedesktop.login1.Session", "Lock")
	waitFor(t, "lock", func() bool { return has(seen(), "lock") })
	if st, _ := m.state(); !st.Locked || st.IdleState != "locked" {
		t.Errorf("state after lock: %+v", st)
	}
	emit(sessionObj, "org.freedesktop.login1.Session", "Unlock")
	waitFor(t, "unlock", func() bool { return has(seen(), "unlock") })
	locks := 0
	for _, k := range seen() {
		if k == "lock" {
			locks++
		}
	}
	if locks != 1 {
		t.Errorf("a lock of another session was reported: %v", seen())
	}

	f.mu.Lock()
	f.onBattery, f.idleMs = true, 61000
	f.mu.Unlock()
	waitFor(t, "battery", func() bool { return has(seen(), "battery") })
	waitFor(t, "idle", func() bool { return has(seen(), "idle") })
	f.mu.Lock()
	f.onBattery, f.idleMs = false, 1000
	f.mu.Unlock()
	waitFor(t, "ac", func() bool { return has(seen(), "ac") })
	waitFor(t, "active", func() bool { return has(seen(), "active") })
}

func TestPreventSleepGnome(t *testing.T) {
	f := newFakes(t)
	f.cookieService(t, "org.gnome.SessionManager", "/org/gnome/SessionManager", "org.gnome.SessionManager", "Inhibit", "Uninhibit", 4)
	m := &manager{prov: f.provider(t)}
	id, err := m.prevent("exporting", true)
	if err != nil {
		t.Fatal(err)
	}
	key := "org.gnome.SessionManager/org/gnome/SessionManager"
	if f.live(key) == 0 {
		t.Fatal("no inhibit cookie held")
	}
	m.allow(id)
	if f.live(key) != 0 {
		t.Fatal("the cookie was not released")
	}
	m.allow(id) // twice is fine
}

func TestPreventSleepKDE(t *testing.T) {
	f := newFakes(t)
	f.cookieService(t, "org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit", "org.freedesktop.PowerManagement.Inhibit", "Inhibit", "UnInhibit", 2)
	f.cookieService(t, "org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver", "Inhibit", "UnInhibit", 2)
	m := &manager{prov: f.provider(t)}
	pm := "org.freedesktop.PowerManagement/org/freedesktop/PowerManagement/Inhibit"
	ss := "org.freedesktop.ScreenSaver/org/freedesktop/ScreenSaver"

	id, err := m.prevent("render", false)
	if err != nil {
		t.Fatal(err)
	}
	if f.live(pm) == 0 || f.live(ss) != 0 {
		t.Fatalf("sleep only must not inhibit the screensaver: pm=%d ss=%d", f.live(pm), f.live(ss))
	}
	m.allow(id)
	if f.live(pm) != 0 {
		t.Fatal("not released")
	}
	id, err = m.prevent("video", true)
	if err != nil {
		t.Fatal(err)
	}
	if f.live(pm) == 0 || f.live(ss) == 0 {
		t.Fatalf("display + sleep must hold both: pm=%d ss=%d", f.live(pm), f.live(ss))
	}
	m.allow(id)
	if f.live(pm) != 0 || f.live(ss) != 0 {
		t.Fatal("not released")
	}
}

func TestPreventSleepWithoutAnyService(t *testing.T) {
	f := newFakes(t)
	m := &manager{prov: f.provider(t)}
	if _, err := m.prevent("x", false); err == nil {
		t.Fatal("expected an error when no session service takes a blocker")
	}
}
