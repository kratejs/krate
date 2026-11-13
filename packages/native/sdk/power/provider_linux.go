//go:build linux

package power

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	loginDest     = "org.freedesktop.login1"
	loginPath     = "/org/freedesktop/login1"
	loginManager  = "org.freedesktop.login1.Manager"
	loginSession  = "org.freedesktop.login1.Session"
	propsIface    = "org.freedesktop.DBus.Properties"
	upowerDest    = "org.freedesktop.UPower"
	upowerPath    = "/org/freedesktop/UPower"
	upowerIface   = "org.freedesktop.UPower"
	defaultSystem = "unix:path=/var/run/dbus/system_bus_socket"
)

// linuxProvider talks to logind and UPower on the system bus and to the
// idle and inhibit services on the session bus.
type linuxProvider struct {
	// sysAddr and supplyRoot are fields so tests can point at fakes.
	sysAddr    func() string
	supplyRoot string

	mu      sync.Mutex
	sys     *dbusiface.Client
	sysAt   string
	session string // logind session object path
}

func newDefaultProvider() provider {
	return &linuxProvider{sysAddr: systemAddress, supplyRoot: "/sys/class/power_supply"}
}

func defaultAvailable() bool { return true }

func systemAddress() string {
	if a := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS"); a != "" {
		return a
	}
	return defaultSystem
}

func (p *linuxProvider) system() (dbusiface.Conn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	addr := p.sysAddr()
	if p.sys != nil {
		select {
		case <-p.sys.Done():
			p.sys = nil
		default:
			if p.sysAt == addr {
				return p.sys, nil
			}
			p.sys.Close()
			p.sys = nil
		}
	}
	c, err := dbusiface.Connect(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: no system bus: %v", krate.ErrUnsupported, err)
	}
	p.sys, p.sysAt = c, addr
	return c, nil
}

func getProp(c dbusiface.Conn, dest, path, iface, prop string) (any, error) {
	out, err := c.Call(dest, path, propsIface, "Get", iface, prop)
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, errors.New("power: unexpected property reply")
	}
	if v, ok := out[0].(dbusiface.Variant); ok {
		return v.Value, nil
	}
	return out[0], nil
}

// sessionPath finds the logind object of this login session.
func (p *linuxProvider) sessionPath(c dbusiface.Conn) string {
	p.mu.Lock()
	if p.session != "" {
		s := p.session
		p.mu.Unlock()
		return s
	}
	p.mu.Unlock()
	var path string
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		if out, err := c.Call(loginDest, loginPath, loginManager, "GetSession", id); err == nil && len(out) == 1 {
			path = objPath(out[0])
		}
	}
	if path == "" {
		if out, err := c.Call(loginDest, loginPath, loginManager, "GetSessionByPID", uint32(os.Getpid())); err == nil && len(out) == 1 {
			path = objPath(out[0])
		}
	}
	if path == "" {
		// systemd 243 and later resolve "auto" to the caller's session.
		path = loginPath + "/session/auto"
	}
	p.mu.Lock()
	p.session = path
	p.mu.Unlock()
	return path
}

func objPath(v any) string {
	switch x := v.(type) {
	case dbusiface.ObjectPath:
		return string(x)
	case string:
		return x
	}
	return ""
}

func (p *linuxProvider) sample() (caps.PowerSample, error) {
	s := caps.PowerSample{IdleSeconds: -1}
	s.OnBattery = p.onBattery()
	if c, err := p.system(); err == nil {
		if v, err := getProp(c, loginDest, p.sessionPath(c), loginSession, "LockedHint"); err == nil {
			s.Locked, _ = v.(bool)
		}
	}
	if idle, ok := p.idle(); ok {
		s.IdleSeconds = idle
	}
	return s, nil
}

func (p *linuxProvider) onBattery() bool {
	if c, err := p.system(); err == nil {
		if v, err := getProp(c, upowerDest, upowerPath, upowerIface, "OnBattery"); err == nil {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	if b, ok := batteryFromSysfs(p.supplyRoot); ok {
		return b
	}
	return false
}

// batteryFromSysfs decides from /sys/class/power_supply whether the machine
// runs on battery: some mains supply online means no; otherwise a battery that
// is discharging means yes. ok is false when there is nothing to go on.
func batteryFromSysfs(root string) (onBattery, ok bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, false
	}
	var haveMains, mainsOnline, discharging bool
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		typ := readTrim(filepath.Join(dir, "type"))
		switch strings.ToLower(typ) {
		case "mains", "usb", "usb_c", "usb_pd", "usb_dcp", "usb_cdp", "wireless":
			haveMains = true
			if readTrim(filepath.Join(dir, "online")) == "1" {
				mainsOnline = true
			}
		case "battery":
			if strings.EqualFold(readTrim(filepath.Join(dir, "status")), "Discharging") {
				discharging = true
			}
		}
	}
	switch {
	case mainsOnline:
		return false, true
	case discharging:
		return true, true
	case haveMains:
		return false, true
	}
	return false, false
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// idle asks the session's idle services in turn.
func (p *linuxProvider) idle() (float64, bool) {
	if sc, err := sessionbus.Get(); err == nil {
		if out, err := sc.Call("org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core", "org.gnome.Mutter.IdleMonitor", "GetIdletime"); err == nil && len(out) == 1 {
			if ms, ok := out[0].(uint64); ok {
				return float64(ms) / 1000, true
			}
		}
		for _, path := range []string{"/org/freedesktop/ScreenSaver", "/ScreenSaver"} {
			if out, err := sc.Call("org.freedesktop.ScreenSaver", path, "org.freedesktop.ScreenSaver", "GetSessionIdleTime"); err == nil && len(out) == 1 {
				if ms, ok := out[0].(uint32); ok {
					return float64(ms) / 1000, true
				}
			}
		}
	}
	if c, err := p.system(); err == nil {
		path := p.sessionPath(c)
		hint, err := getProp(c, loginDest, path, loginSession, "IdleHint")
		if err == nil {
			if idle, _ := hint.(bool); !idle {
				return 0, true
			}
			if since, err := getProp(c, loginDest, path, loginSession, "IdleSinceHint"); err == nil {
				if us, ok := since.(uint64); ok && us > 0 {
					return time.Since(time.UnixMicro(int64(us))).Seconds(), true
				}
			}
		}
	}
	return 0, false
}

// watch subscribes to PrepareForSleep and the session's Lock and Unlock.
func (p *linuxProvider) watch(fn func(caps.PowerEventKind)) func() {
	c, err := p.system()
	if err != nil {
		return func() {}
	}
	var cancels []func()
	add := func(match string, h func(dbusiface.Signal)) {
		if cancel, err := c.Subscribe(match, h); err == nil {
			cancels = append(cancels, cancel)
		}
	}
	add("type='signal',interface='"+loginManager+"',member='PrepareForSleep'", func(s dbusiface.Signal) {
		if len(s.Body) >= 1 {
			if going, ok := s.Body[0].(bool); ok {
				if going {
					fn(caps.PowerSuspend)
				} else {
					fn(caps.PowerResume)
				}
			}
		}
	})
	path := p.sessionPath(c)
	filter := ""
	if !strings.HasSuffix(path, "/session/auto") {
		filter = ",path='" + path + "'"
	}
	add("type='signal',interface='"+loginSession+"',member='Lock'"+filter, func(dbusiface.Signal) { fn(caps.PowerLock) })
	add("type='signal',interface='"+loginSession+"',member='Unlock'"+filter, func(dbusiface.Signal) { fn(caps.PowerUnlock) })
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// inhibit blocks sleep (and idle, with display) through whichever session
// service answers.
func (p *linuxProvider) inhibit(reason string, display bool) (func(), error) {
	sc, err := sessionbus.Get()
	if err != nil {
		return nil, err
	}
	app := os.Getenv("KRATE_APP_ID")
	if app == "" {
		app = "krate"
	}
	// GNOME session manager: flag 4 inhibits suspend, 8 inhibits idle.
	flags := uint32(4)
	if display {
		flags |= 8
	}
	if out, err := sc.Call("org.gnome.SessionManager", "/org/gnome/SessionManager", "org.gnome.SessionManager", "Inhibit", app, uint32(0), reason, flags); err == nil && len(out) == 1 {
		if cookie, ok := out[0].(uint32); ok {
			return func() {
				_, _ = sc.Call("org.gnome.SessionManager", "/org/gnome/SessionManager", "org.gnome.SessionManager", "Uninhibit", cookie)
			}, nil
		}
	}
	// KDE and others: PowerManagement blocks sleep, ScreenSaver the screen.
	var releases []func()
	if out, err := sc.Call("org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit", "org.freedesktop.PowerManagement.Inhibit", "Inhibit", app, reason); err == nil && len(out) == 1 {
		if cookie, ok := out[0].(uint32); ok {
			releases = append(releases, func() {
				_, _ = sc.Call("org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit", "org.freedesktop.PowerManagement.Inhibit", "UnInhibit", cookie)
			})
		}
	}
	if display || len(releases) == 0 {
		for _, path := range []string{"/org/freedesktop/ScreenSaver", "/ScreenSaver"} {
			out, err := sc.Call("org.freedesktop.ScreenSaver", path, "org.freedesktop.ScreenSaver", "Inhibit", app, reason)
			if err != nil || len(out) != 1 {
				continue
			}
			if cookie, ok := out[0].(uint32); ok {
				path := path
				releases = append(releases, func() {
					_, _ = sc.Call("org.freedesktop.ScreenSaver", path, "org.freedesktop.ScreenSaver", "UnInhibit", cookie)
				})
				break
			}
		}
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("%w: no session service (GNOME session manager, org.freedesktop.PowerManagement or org.freedesktop.ScreenSaver) accepted a sleep blocker", krate.ErrUnsupported)
	}
	return func() {
		for _, r := range releases {
			r()
		}
	}, nil
}
