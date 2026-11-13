//go:build krate_no_hostjs

package shell

import (
	"errors"

	"github.com/kratejs/krate/packages/native/scheme"
)

type jsHost interface {
	Load(bundle, srcMap []byte, name string) error
	Reload(bundle, srcMap []byte, name string) error
	Register(r *scheme.Registry)
	GC()
	Close() error
}

func newJSHost(bool, int, map[string]string, func(string, ...any)) (jsHost, error) {
	return nil, errors.New("shell: this binary was built without TypeScript host support (tag krate_no_hostjs) but the app has host code")
}

const hasJSHost = false
