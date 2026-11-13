//go:build windows

package login

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

// runKeyPath is a variable so tests can use a scratch key under HKCU.
var runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

type registryStore struct{}

func newStore(env *bridge.Env) store { return registryStore{} }

func appID() (string, error) {
	id := os.Getenv("KRATE_APP_ID")
	if id == "" {
		return "", fmt.Errorf("%w: the app has no id to name its Run value", krate.ErrUnsupported)
	}
	return id, nil
}

// packaged reports whether the process has an MSIX package identity.
func packaged() bool {
	var n uint32
	r, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentPackageFullName").Call(uintptr(unsafe.Pointer(&n)), 0)
	const appModelErrorNoPackage = 15700
	return r != appModelErrorNoPackage
}

func errPackaged() error {
	return fmt.Errorf("%w: a packaged (MSIX) app starts at login through a windows.startupTask extension in its package manifest; the Run key is not used for packaged apps", krate.ErrUnsupported)
}

func (registryStore) get() (Item, error) {
	if packaged() {
		return Item{}, errPackaged()
	}
	id, err := appID()
	if err != nil {
		return Item{}, err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return Item{}, nil
	}
	if err != nil {
		return Item{}, err
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(id); errors.Is(err, registry.ErrNotExist) {
		return Item{}, nil
	} else if err != nil {
		return Item{}, err
	}
	return Item{Enabled: true, Method: "registry"}, nil
}

func (registryStore) set(enabled bool, args []string) error {
	if packaged() {
		return errPackaged()
	}
	id, err := appID()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(id); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(id, windowsCommandLine(exe, args))
}
