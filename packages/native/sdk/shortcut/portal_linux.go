//go:build linux

package shortcut

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	portalDest  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	portalIface = "org.freedesktop.portal.GlobalShortcuts"
	reqIface    = "org.freedesktop.portal.Request"
)

type shortcutSpec struct {
	ID    string
	Props map[string]dbusiface.Variant
}

// portal drives xdg-desktop-portal GlobalShortcuts.
type portal struct {
	conn    dbusiface.Conn
	publish func(string)
	token   atomic.Int64

	mu      sync.Mutex
	session dbusiface.ObjectPath
	accels  map[string]caps.Accelerator
}

func newPortalBackend(publish func(string)) (backend, error) {
	conn, err := sessionbus.Get()
	if err != nil {
		return nil, err
	}
	if _, err := conn.Call(portalDest, portalPath, "org.freedesktop.DBus.Properties", "Get", portalIface, "version"); err != nil {
		return nil, fmt.Errorf("no GlobalShortcuts portal: %w", err)
	}
	p := &portal{conn: conn, publish: publish, accels: map[string]caps.Accelerator{}}
	if _, err := conn.Subscribe("type='signal',interface='"+portalIface+"',member='Activated'", p.onActivated); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *portal) onActivated(s dbusiface.Signal) {
	if len(s.Body) < 2 {
		return
	}
	id, _ := s.Body[1].(string)
	p.mu.Lock()
	_, ok := p.accels[id]
	p.mu.Unlock()
	if ok {
		p.publish(id)
	}
}

// request calls a portal method that answers through a Request object and
// waits for the Response signal. build receives the options dictionary to
// extend and returns the method arguments.
func (p *portal) request(method string, timeout time.Duration, build func(opts map[string]dbusiface.Variant) []any) (map[string]any, error) {
	token := fmt.Sprintf("krate%d_%d", os.Getpid(), p.token.Add(1))
	unique := ""
	if un, ok := p.conn.(dbusiface.UniqueNamer); ok {
		unique = un.UniqueName()
	}
	reqPath := portalPath + "/request/" + strings.ReplaceAll(strings.TrimPrefix(unique, ":"), ".", "_") + "/" + token
	type resp struct {
		code    uint32
		results map[string]any
	}
	ch := make(chan resp, 1)
	cancel, err := p.conn.Subscribe("type='signal',interface='"+reqIface+"',member='Response',path='"+reqPath+"'", func(s dbusiface.Signal) {
		if len(s.Body) < 2 {
			return
		}
		code, _ := s.Body[0].(uint32)
		res, _ := s.Body[1].(map[string]any)
		select {
		case ch <- resp{code, res}:
		default:
		}
	})
	if err != nil {
		return nil, err
	}
	defer cancel()
	opts := map[string]dbusiface.Variant{"handle_token": {Value: token}}
	args := build(opts)
	if _, err := p.conn.Call(portalDest, portalPath, portalIface, method, args...); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.code != 0 {
			return nil, fmt.Errorf("the portal refused %s (response %d)", method, r.code)
		}
		return r.results, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("the portal did not answer %s in %s", method, timeout)
	}
}

func (p *portal) ensureSession() error {
	if p.session != "" {
		return nil
	}
	res, err := p.request("CreateSession", 10*time.Second, func(opts map[string]dbusiface.Variant) []any {
		opts["session_handle_token"] = dbusiface.Variant{Value: fmt.Sprintf("krate_session_%d", os.Getpid())}
		return []any{opts}
	})
	if err != nil {
		return err
	}
	v, _ := res["session_handle"].(dbusiface.Variant)
	s, _ := v.Value.(string)
	if s == "" {
		return errors.New("the portal returned no session")
	}
	p.session = dbusiface.ObjectPath(s)
	return nil
}

// portalTrigger renders an accelerator in the shortcuts specification form,
// e.g. "CTRL+SHIFT+k".
func portalTrigger(a caps.Accelerator) string {
	var parts []string
	if a.Ctrl {
		parts = append(parts, "CTRL")
	}
	if a.Alt {
		parts = append(parts, "ALT")
	}
	if a.Shift {
		parts = append(parts, "SHIFT")
	}
	if a.Super {
		parts = append(parts, "LOGO")
	}
	key := a.Key
	if len(key) == 1 {
		key = strings.ToLower(key)
	}
	return strings.Join(append(parts, key), "+")
}

func (p *portal) bindLocked() error {
	if err := p.ensureSession(); err != nil {
		return err
	}
	specs := []shortcutSpec{}
	for id, a := range p.accels {
		specs = append(specs, shortcutSpec{ID: id, Props: map[string]dbusiface.Variant{
			"description":       {Value: "Krate shortcut " + portalTrigger(a)},
			"preferred_trigger": {Value: portalTrigger(a)},
		}})
	}
	_, err := p.request("BindShortcuts", 2*time.Minute, func(opts map[string]dbusiface.Variant) []any {
		return []any{p.session, specs, "", opts}
	})
	return err
}

func (p *portal) register(handle string, a caps.Accelerator) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.accels[handle] = a
	if err := p.bindLocked(); err != nil {
		delete(p.accels, handle)
		return err
	}
	return nil
}

func (p *portal) unregister(handle string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.accels, handle)
	if len(p.accels) == 0 {
		return nil
	}
	return p.bindLocked()
}

// Available reports whether global shortcuts can work in this session.
func Available(env *bridge.Env) bool {
	if os.Getenv("WAYLAND_DISPLAY") == "" || os.Getenv("KRATE_FORCE_X11_SHORTCUTS") != "" {
		_, ok := env.App.(caps.Shortcuts)
		return ok
	}
	conn, err := sessionbus.Get()
	if err != nil {
		return false
	}
	_, err = conn.Call(portalDest, portalPath, "org.freedesktop.DBus.Properties", "Get", portalIface, "version")
	return err == nil
}
