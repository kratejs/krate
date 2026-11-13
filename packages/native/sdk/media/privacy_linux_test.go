//go:build linux

package media

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

// fakeBus is a dbusiface.Conn that plays the camera portal.
type fakeBus struct {
	mu       sync.Mutex
	response uint32
	callErr  error
	subs     map[string]func(dbusiface.Signal)
	calls    []string
	silent   bool
}

func (f *fakeBus) UniqueName() string { return ":1.42" }

func (f *fakeBus) Call(dest, path, iface, method string, args ...any) ([]any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, dest+" "+iface+"."+method)
	err, silent := f.callErr, f.silent
	var fn func(dbusiface.Signal)
	if opts, ok := args[0].(map[string]dbusiface.Variant); ok {
		tok, _ := opts["handle_token"].Value.(string)
		for m, cb := range f.subs {
			if strings.Contains(m, "/1_42/"+tok) {
				fn = cb
			}
		}
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if fn != nil && !silent {
		go fn(dbusiface.Signal{Interface: "org.freedesktop.portal.Request", Name: "Response", Body: []any{f.response, map[string]any{}}})
	}
	return []any{dbusiface.ObjectPath("/request")}, nil
}

func (f *fakeBus) Subscribe(match string, fn func(dbusiface.Signal)) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = map[string]func(dbusiface.Signal){}
	}
	f.subs[match] = fn
	return func() { f.mu.Lock(); delete(f.subs, match); f.mu.Unlock() }, nil
}

func (f *fakeBus) Export(path, iface string, methods map[string]dbusiface.Method) {}
func (f *fakeBus) ExportProperties(path, iface string, get func() map[string]dbusiface.Variant) {
}
func (f *fakeBus) Emit(path, iface, name string, args ...any) error { return nil }
func (f *fakeBus) RequestName(name string) error                    { return nil }
func (f *fakeBus) Close() error                                     { return nil }

func newPortal(bus *fakeBus, sandboxed bool) *portalPrivacy {
	return &portalPrivacy{conn: func() (dbusiface.Conn, error) { return bus, nil }, sandboxed: func() bool { return sandboxed }, timeout: 200 * time.Millisecond}
}

func TestPortalCameraGrantAndDeny(t *testing.T) {
	bus := &fakeBus{response: 0}
	p := newPortal(bus, true)
	if st, _ := p.PrivacyStatus(caps.PermCamera); st != caps.PrivacyNotDetermined {
		t.Fatalf("sandboxed camera state %s", st)
	}
	st, err := p.RequestPrivacy(caps.PermCamera)
	if err != nil || st != caps.PrivacyGranted {
		t.Fatalf("grant: %s %v", st, err)
	}
	if len(bus.calls) != 1 || !strings.Contains(bus.calls[0], "org.freedesktop.portal.Camera.AccessCamera") {
		t.Fatalf("calls %v", bus.calls)
	}
	if len(bus.subs) != 0 {
		t.Fatal("the Response subscription leaked")
	}
	bus.response = 1
	if st, _ := p.RequestPrivacy(caps.PermCamera); st != caps.PrivacyDenied {
		t.Fatalf("cancelled prompt: %s", st)
	}
}

func TestPortalOutsideASandboxHasNoSwitch(t *testing.T) {
	bus := &fakeBus{}
	p := newPortal(bus, false)
	for _, k := range []caps.PermissionKind{caps.PermCamera, caps.PermMicrophone, caps.PermScreen, caps.PermLocation} {
		if st, err := p.PrivacyStatus(k); st != caps.PrivacyUnknown || err != nil {
			t.Errorf("%s status %s %v", k, st, err)
		}
		if st, err := p.RequestPrivacy(k); st != caps.PrivacyUnknown || err != nil {
			t.Errorf("%s request %s %v", k, st, err)
		}
	}
	if len(bus.calls) != 0 {
		t.Fatalf("no portal call expected, got %v", bus.calls)
	}
}

func TestPortalErrorsAndTimeout(t *testing.T) {
	bus := &fakeBus{callErr: errors.New("org.freedesktop.DBus.Error.ServiceUnknown: no portal")}
	if _, err := newPortal(bus, true).RequestPrivacy(caps.PermCamera); err == nil || !strings.Contains(err.Error(), "portal") {
		t.Fatalf("missing portal: %v", err)
	}
	bus = &fakeBus{silent: true}
	st, err := newPortal(bus, true).RequestPrivacy(caps.PermCamera)
	if err != nil || st != caps.PrivacyNotDetermined {
		t.Fatalf("an unanswered prompt stays undetermined: %s %v", st, err)
	}
}
