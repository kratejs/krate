//go:build !linux && !darwin && !windows

package login

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

type noStore struct{}

func newStore(env *bridge.Env) store { return noStore{} }

func (noStore) get() (Item, error)       { return Item{}, errNo() }
func (noStore) set(bool, []string) error { return errNo() }

func errNo() error {
	return fmt.Errorf("%w: starting at login is not supported on this platform", krate.ErrUnsupported)
}
