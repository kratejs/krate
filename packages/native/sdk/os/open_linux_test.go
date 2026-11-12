//go:build linux

package sdkos

import (
	"errors"
	"testing"

	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

type recBus struct {
	dbusiface.Conn
	dest, iface, method string
	args                []any
	err                 error
}

func (r *recBus) Call(dest, path, iface, method string, args ...any) ([]any, error) {
	r.dest, r.iface, r.method, r.args = dest, iface, method, args
	return []any{dbusiface.ObjectPath("/r")}, r.err
}

func TestOpenURICallsThePortal(t *testing.T) {
	b := &recBus{}
	if err := openURI(b, "https://example.com/x"); err != nil {
		t.Fatal(err)
	}
	if b.dest != "org.freedesktop.portal.Desktop" || b.iface != "org.freedesktop.portal.OpenURI" || b.method != "OpenURI" {
		t.Errorf("call = %s %s.%s", b.dest, b.iface, b.method)
	}
	if len(b.args) != 3 || b.args[0] != "" || b.args[1] != "https://example.com/x" {
		t.Errorf("args = %#v", b.args)
	}
	if _, ok := b.args[2].(map[string]dbusiface.Variant); !ok {
		t.Errorf("options must be a{sv}, got %T", b.args[2])
	}
	b.err = errors.New("boom")
	if err := openURI(b, "https://example.com"); err == nil {
		t.Error("a portal error must surface")
	}
}
