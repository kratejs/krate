package kv

import (
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/db"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

func store(t *testing.T) *Store {
	t.Helper()
	d, err := db.OpenPath(filepath.Join(t.TempDir(), "kv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d)
}

func caller(t *testing.T, s *Store, perms ...string) *sdktest.Caller {
	t.Helper()
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterWith(reg, set, func() (*Store, error) { return s, nil })
	return sdktest.New(t, reg)
}

func str(s string) func(*wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func TestUIDenied(t *testing.T) {
	c := caller(t, store(t), "db")
	r := c.Call("krate/kv.get", str("a"))
	if r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"kv"}` {
		t.Fatalf("%+v", r)
	}
}
