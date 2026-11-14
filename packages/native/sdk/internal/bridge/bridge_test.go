package bridge

import (
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestRegisterUIFiltersByPermissionAndUIFlag(t *testing.T) {
	tb := NewTable()
	f := func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return nil }
	tb.Func("ok", "p1", true, f)
	tb.Func("denied", "p2", true, f)
	tb.Func("goonly", "p1", false, f)
	reg := scheme.NewRegistry()
	tb.RegisterUI(reg, func(p string) bool { return p == "p1" }, func(p string) error { return krate.Errorf("permission_denied", "needs %s", p) })
	// "denied" is a stub (registered), "goonly" is absent.
	reg.Func("goonly", f)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("denied stub was not registered")
			}
		}()
		reg.Func("denied", f)
	}()
	defer func() {
		if recover() == nil {
			t.Fatal("ok was not registered")
		}
	}()
	reg.Func("ok", f)
}
