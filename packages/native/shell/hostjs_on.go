//go:build !krate_no_hostjs

package shell

import (
	"github.com/kratejs/krate/packages/native/hostjs"
	"github.com/kratejs/krate/packages/native/scheme"
)

// jsHost is the TypeScript host runtime. Building with the krate_no_hostjs
// tag leaves it (and QuickJS) out of the binary.
type jsHost interface {
	Load(bundle, srcMap []byte, name string) error
	Reload(bundle, srcMap []byte, name string) error
	Register(r *scheme.Registry)
	Close() error
}

func newJSHost(dev bool, env map[string]string, logf func(string, ...any)) (jsHost, error) {
	return hostjs.New(hostjs.Options{
		Dev:       dev,
		Env:       env,
		LogPrefix: "[host] ",
		Log:       func(msg string) { logf("%s", msg) },
	})
}

// hasJSHost reports whether this binary can run TypeScript host code.
const hasJSHost = true
