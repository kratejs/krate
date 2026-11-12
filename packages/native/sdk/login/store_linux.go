//go:build linux

package login

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

type autostartStore struct{}

func newStore(env *bridge.Env) store { return autostartStore{} }

func appID() (string, error) {
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		return "", fmt.Errorf("%w: the app has no id to name its autostart entry", krate.ErrUnsupported)
	}
	return id, nil
}

// autostartPath is where the entry lives; XDG_CONFIG_HOME wins.
func autostartPath(id string) (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", id+".desktop"), nil
}

func (autostartStore) get() (Item, error) {
	id, err := appID()
	if err != nil {
		return Item{}, err
	}
	p, err := autostartPath(id)
	if err != nil {
		return Item{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return Item{}, nil
	}
	if err != nil {
		return Item{}, err
	}
	if !autostartEnabled(string(b)) {
		return Item{}, nil
	}
	return Item{Enabled: true, Method: "autostart"}, nil
}

func (autostartStore) set(enabled bool, args []string) error {
	id, err := appID()
	if err != nil {
		return err
	}
	p, err := autostartPath(id)
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	name := os.Getenv("KRATE_APP_NAME")
	if name == "" {
		name = id
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(autostartEntry(name, exe, args)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
