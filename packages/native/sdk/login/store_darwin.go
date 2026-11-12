//go:build darwin

package login

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

// macStore prefers SMAppService (through the platform layer) and keeps a
// LaunchAgent as the fallback for apps outside a signed bundle. Arguments can
// only be given to the LaunchAgent: SMAppService starts the bundle plainly.
type macStore struct {
	sm caps.LoginItems
}

func newStore(env *bridge.Env) store {
	sm, _ := env.App.(caps.LoginItems)
	return macStore{sm: sm}
}

func appID() (string, error) {
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		return "", fmt.Errorf("%w: the app has no id to name its LaunchAgent", krate.ErrUnsupported)
	}
	return id, nil
}

func agentPath(id string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", id+".plist"), nil
}

func (s macStore) get() (Item, error) {
	if s.sm != nil {
		if on, _, err := s.sm.LoginItem(); err == nil && on {
			return Item{Enabled: true, Method: "smappservice"}, nil
		}
	}
	id, err := appID()
	if err != nil {
		return Item{}, err
	}
	p, err := agentPath(id)
	if err != nil {
		return Item{}, err
	}
	if _, err := os.Stat(p); err == nil {
		return Item{Enabled: true, Method: "launchagent"}, nil
	}
	return Item{}, nil
}

func (s macStore) set(enabled bool, args []string) error {
	id, err := appID()
	if err != nil {
		return err
	}
	p, err := agentPath(id)
	if err != nil {
		return err
	}
	if !enabled {
		var first error
		if s.sm != nil {
			if on, _, err := s.sm.LoginItem(); err == nil && on {
				first = s.sm.SetLoginItem(false)
			}
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
		return first
	}
	// A plain bundle launch is what SMAppService does; use it when there are
	// no arguments to pass.
	if s.sm != nil && len(args) == 0 {
		if err := s.sm.SetLoginItem(true); err == nil {
			_ = os.Remove(p) // never register twice
			return nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(launchAgentPlist(id, exe, args)), 0o644)
}
