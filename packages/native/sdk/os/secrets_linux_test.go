//go:build linux

package sdkos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/dbus"
	"github.com/kratejs/krate/packages/native/internal/dbus/dbustest"
	"github.com/kratejs/krate/packages/native/krate"
)

// fakeKeyring implements enough of org.freedesktop.Secret to exercise the
// client: sessions, the default collection, items, locking and prompts.
type fakeKeyring struct {
	c *dbus.Conn

	mu      sync.Mutex
	items   map[dbus.ObjectPath]*fakeItem
	next    int
	locked  bool // the collection starts locked and needs a prompt
	dismiss bool
}

type fakeItem struct {
	attrs map[string]string
	value []byte
}

const (
	collPath   = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	promptPath = dbus.ObjectPath("/org/freedesktop/secrets/prompt/u1")
)

func startFakeKeyring(t *testing.T, addr string, locked bool) *fakeKeyring {
	t.Helper()
	ctx := context.Background()
	c, err := dbus.DialAddress(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f := &fakeKeyring{c: c, items: map[dbus.ObjectPath]*fakeItem{}, locked: locked}
	if err := c.RequestName(ctx, ssName, 0); err != nil {
		t.Fatal(err)
	}
	c.Export(ssPath, ssService, map[string]dbus.MethodFunc{
		"OpenSession": func(m *dbus.Message) (string, []any, error) {
			return "vo", []any{dbus.V("s", ""), dbus.ObjectPath("/org/freedesktop/secrets/session/s1")}, nil
		},
		"ReadAlias": func(m *dbus.Message) (string, []any, error) {
			return "o", []any{collPath}, nil
		},
		"Unlock": func(m *dbus.Message) (string, []any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.locked {
				return "aoo", []any{[]dbus.ObjectPath{}, promptPath}, nil
			}
			return "aoo", []any{m.Body[0], noObject}, nil
		},
		"SearchItems": func(m *dbus.Message) (string, []any, error) {
			want := m.Body[0].(map[string]any)
			f.mu.Lock()
			defer f.mu.Unlock()
			var found []dbus.ObjectPath
			for p, it := range f.items {
				match := true
				for k, v := range want {
					if it.attrs[k] != v.(string) {
						match = false
					}
				}
				if match {
					found = append(found, p)
				}
			}
			if found == nil {
				found = []dbus.ObjectPath{}
			}
			return "aoao", []any{found, []dbus.ObjectPath{}}, nil
		},
	})
	c.Export(collPath, ssColl, map[string]dbus.MethodFunc{
		"CreateItem": func(m *dbus.Message) (string, []any, error) {
			props := m.Body[0].(map[string]any)
			attrs := props[itemAttrsProp].(dbus.Variant).Value.(map[string]any)
			secret := m.Body[1].(dbus.Struct)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.locked {
				return "", nil, dbus.NewError("org.freedesktop.Secret.Error.IsLocked", "locked")
			}
			it := &fakeItem{attrs: map[string]string{}, value: secret[2].([]byte)}
			for k, v := range attrs {
				it.attrs[k] = v.(string)
			}
			for p, old := range f.items { // replace=true
				if old.attrs["service"] == it.attrs["service"] && old.attrs["username"] == it.attrs["username"] {
					delete(f.items, p)
				}
			}
			f.next++
			p := dbus.ObjectPath("/org/freedesktop/secrets/collection/login/" + string(rune('a'+f.next)))
			f.items[p] = it
			f.exportItem(p)
			return "oo", []any{p, noObject}, nil
		},
	})
	c.Export(promptPath, ssPrompt, map[string]dbus.MethodFunc{
		"Prompt": func(m *dbus.Message) (string, []any, error) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				f.mu.Lock()
				dismissed := f.dismiss
				if !dismissed {
					f.locked = false
				}
				f.mu.Unlock()
				// The bus may be gone if the test already ended.
				_ = c.Emit(promptPath, ssPrompt, "Completed", "bv", dismissed, dbus.V("s", ""))
			}()
			return "", nil, nil
		},
	})
	c.Export("/org/freedesktop/secrets/session/s1", ssSession, map[string]dbus.MethodFunc{
		"Close": func(m *dbus.Message) (string, []any, error) { return "", nil, nil },
	})
	return f
}

func (f *fakeKeyring) exportItem(p dbus.ObjectPath) {
	f.c.Export(p, ssItem, map[string]dbus.MethodFunc{
		"GetSecret": func(m *dbus.Message) (string, []any, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			it := f.items[p]
			if it == nil {
				return "", nil, dbus.NewError("org.freedesktop.Secret.Error.NoSuchObject", "gone")
			}
			return "(oayays)", []any{[]any{m.Body[0], []byte{}, it.value, "text/plain"}}, nil
		},
		"Delete": func(m *dbus.Message) (string, []any, error) {
			f.mu.Lock()
			delete(f.items, p)
			f.mu.Unlock()
			return "o", []any{noObject}, nil
		},
	})
}

func testService(addr string) *secretService {
	return &secretService{dial: func(ctx context.Context) (*dbus.Conn, error) { return dbus.DialAddress(ctx, addr) }}
}

func TestSecretServiceRoundTrip(t *testing.T) {
	addr := dbustest.Start(t)
	kr := startFakeKeyring(t, addr, false)
	s := testService(addr)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := s.get(ctx, "app", "k"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.set(ctx, "app", "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.get(ctx, "app", "k"); err != nil || got != "v1" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := s.set(ctx, "app", "k", "v2 \u00e9\x00bin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.get(ctx, "app", "k"); got != "v2 \u00e9\x00bin" {
		t.Fatalf("overwrite: %q", got)
	}
	kr.mu.Lock()
	n := len(kr.items)
	kr.mu.Unlock()
	if n != 1 {
		t.Fatalf("replace left %d items", n)
	}
	if _, err := s.get(ctx, "other-app", "k"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("secrets leak across apps: %v", err)
	}
	if err := s.remove(ctx, "app", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.get(ctx, "app", "k"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := s.remove(ctx, "app", "k"); err != nil {
		t.Fatalf("deleting a missing secret: %v", err)
	}
}

func TestSecretServicePrompt(t *testing.T) {
	addr := dbustest.Start(t)
	kr := startFakeKeyring(t, addr, true)
	s := testService(addr)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	kr.dismiss = true
	if err := s.set(ctx, "app", "k", "v"); err == nil {
		t.Fatal("dismissed prompt still stored the secret")
	}
	kr.mu.Lock()
	kr.dismiss = false
	kr.mu.Unlock()
	if err := s.set(ctx, "app", "k", "v"); err != nil {
		t.Fatalf("after the user unlocks: %v", err)
	}
	if got, err := s.get(ctx, "app", "k"); err != nil || got != "v" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestSecretServiceMissingProvider(t *testing.T) {
	addr := dbustest.Start(t) // a bus with nobody owning org.freedesktop.secrets
	s := testService(addr)
	err := s.set(context.Background(), "app", "k", "v")
	if !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
	s = &secretService{dial: func(context.Context) (*dbus.Conn, error) { return nil, dbus.ErrNoBus }}
	if _, err := s.get(context.Background(), "app", "k"); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("no bus: %v", err)
	}
}
