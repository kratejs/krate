// Package echo is a test plugin: generated ops, a manual op and an Init.
package echo

import (
	"strings"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/plugin"
	"github.com/kratejs/krate/packages/native/wire"
)

// Echo returns its argument.
func Echo(s string) string { return s }

// Shout returns its argument in capitals.
func Shout(s string) string { return strings.ToUpper(s) }

// Reset needs the admin permission.
func Reset() error { return nil }

// Free needs no permission.
func Free() string { return "free" }

var started bool

// Init registers the manual op and records the start.
func Init(c *plugin.Context) error {
	c.OnStart(func() error { started = true; return nil })
	return c.Func("hand", func(_ krate.Ctx, _ *wire.Decoder, out *wire.Encoder) error {
		if started {
			out.String("started")
		} else {
			out.String("not started")
		}
		return nil
	})
}
