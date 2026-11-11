//go:build windows

package windows

// Global shortcuts through RegisterHotKey on the hidden utility window. WM_HOTKEY
// arrives on the main thread. UNTESTED beyond compilation and the pure logic in
// hotkey.go; the CI GUI test registers and releases real hotkeys.

import (
	"syscall"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	pRegisterHotKey   = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey = user32.NewProc("UnregisterHotKey")

	_ caps.Shortcuts = (*App)(nil)

	hotFn func(string)
)

// OnShortcut sets the handler for shortcut presses.
func (a *App) OnShortcut(fn func(handle string)) {
	extMu.Lock()
	hotFn = fn
	extMu.Unlock()
}

// RegisterShortcut registers a system-wide hotkey under handle. A combination
// another app (or Windows itself) holds fails with an error saying so.
func (a *App) RegisterShortcut(handle string, acc caps.Accelerator) (err error) {
	mods, vk, err := hotkeySpec(acc)
	if err != nil {
		return err
	}
	a.callMain(func() {
		id, e := a.hot.add(handle, acc)
		if e != nil {
			err = e
			return
		}
		r, _, le := pRegisterHotKey.Call(a.utilWnd, uintptr(id), uintptr(mods), uintptr(vk))
		if r == 0 {
			a.hot.remove(handle)
			errno, _ := le.(syscall.Errno)
			err = hotkeyError(acc, uint32(errno))
		}
	})
	return err
}

// UnregisterShortcut releases handle's hotkey. An unknown handle is not an
// error.
func (a *App) UnregisterShortcut(handle string) error {
	a.callMain(func() {
		if id := a.hot.remove(handle); id != 0 {
			_, _, _ = pUnregisterHotKey.Call(a.utilWnd, uintptr(id))
		}
	})
	return nil
}

// hotkeyMessage handles WM_HOTKEY.
func (a *App) hotkeyMessage(id int32) {
	h, ok := a.hot.handle(id)
	if !ok {
		return
	}
	extMu.Lock()
	fn := hotFn
	extMu.Unlock()
	if fn != nil {
		fn(h)
	}
}

// releaseHotkeys unregisters everything (shutdown).
func (a *App) releaseHotkeys() {
	for _, id := range a.hot.ids() {
		_, _, _ = pUnregisterHotKey.Call(a.utilWnd, uintptr(id))
	}
	a.hot = newHotkeyTable()
}
