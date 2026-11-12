//go:build !linux

package badge

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func newDefaultBackend(env *bridge.Env) (backend, error) {
	return nil, fmt.Errorf("%w: app icon badges are not available on this platform build", krate.ErrUnsupported)
}

func defaultAvailable() bool { return false }
