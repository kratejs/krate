//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Global shortcuts through Carbon's RegisterEventHotKey. It needs no
// Accessibility or Input Monitoring permission and fires while the app is in
// the background. Carbon is deprecated as a whole but this part has no
// replacement and is what Electron, Tauri and others use.

var _ caps.Shortcuts = (*App)(nil)

const (
	carbonSignature      uint32 = 0x4B525433 // 'KRT3'
	carbonEventClassKeyb uint32 = 0x6B657962 // 'keyb'
	carbonHotKeyPressed  uint32 = 5          // kEventHotKeyPressed
	carbonParamDirect    uint32 = 0x2D2D2D2D // kEventParamDirectObject '----'
	carbonTypeHotKeyID   uint32 = 0x686B6964 // typeEventHotKeyID 'hkid'
)

var carbon struct {
	once sync.Once
	err  error

	getApplicationEventTarget func() uintptr
	installEventHandler       func(target, upp uintptr, numTypes uint32, specs unsafe.Pointer, userData uintptr, out *uintptr) int32
	registerEventHotKey       func(code, mods uint32, id uint64, target uintptr, options uint32, out *uintptr) int32
	unregisterEventHotKey     func(ref uintptr) int32
	getEventParameter         func(event uintptr, name, typ uint32, actualType *uint32, size uint32, actualSize *uint32, out unsafe.Pointer) int32

	handler uintptr // EventHandlerRef
	target  uintptr
}

var hot struct {
	mu     sync.Mutex
	fn     func(handle string)
	table  *hotKeyTable
	refs   map[uint32]uintptr
	combos map[carbonHotKey]string // combination -> handle
	keys   map[string]carbonHotKey // handle -> combination
}

// hotKeyID packs EventHotKeyID {OSType signature; UInt32 id} into the single
// 64-bit register both the arm64 and the amd64 calling conventions pass an
// 8-byte struct of two 32-bit integers in.
func hotKeyID(id uint32) uint64 { return uint64(carbonSignature) | uint64(id)<<32 }

func loadCarbon() error {
	carbon.once.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			carbon.err = fmt.Errorf("darwin: load Carbon: %w", err)
			return
		}
		purego.RegisterLibFunc(&carbon.getApplicationEventTarget, lib, "GetApplicationEventTarget")
		purego.RegisterLibFunc(&carbon.installEventHandler, lib, "InstallEventHandler")
		purego.RegisterLibFunc(&carbon.registerEventHotKey, lib, "RegisterEventHotKey")
		purego.RegisterLibFunc(&carbon.unregisterEventHotKey, lib, "UnregisterEventHotKey")
		purego.RegisterLibFunc(&carbon.getEventParameter, lib, "GetEventParameter")
		hot.table = newHotKeyTable()
		hot.refs = map[uint32]uintptr{}
		hot.combos = map[carbonHotKey]string{}
		hot.keys = map[string]carbonHotKey{}
	})
	return carbon.err
}

// installHotKeyHandler installs the one application event handler (main
// thread). Calling it again does nothing.
func installHotKeyHandler() error {
	if carbon.handler != 0 {
		return nil
	}
	carbon.target = carbon.getApplicationEventTarget()
	if carbon.target == 0 {
		return errors.New("darwin: Carbon has no application event target")
	}
	cb := purego.NewCallback(hotKeyEvent)
	spec := [2]uint32{carbonEventClassKeyb, carbonHotKeyPressed}
	var ref uintptr
	if st := carbon.installEventHandler(carbon.target, cb, 1, unsafe.Pointer(&spec[0]), 0, &ref); st != 0 {
		return fmt.Errorf("darwin: InstallEventHandler failed with OSStatus %d", st)
	}
	carbon.handler = ref
	return nil
}

// hotKeyEvent is the EventHandlerUPP: OSStatus (*)(EventHandlerCallRef,
// EventRef, void*). It runs on the main thread from the AppKit event loop.
func hotKeyEvent(next, event, userData uintptr) uintptr {
	var id uint64
	st := carbon.getEventParameter(event, carbonParamDirect, carbonTypeHotKeyID, nil, 8, nil, unsafe.Pointer(&id))
	if st != 0 || uint32(id) != carbonSignature {
		return 0
	}
	hot.mu.Lock()
	handle, ok := hot.table.handleOf(uint32(id >> 32))
	fn := hot.fn
	hot.mu.Unlock()
	if ok && fn != nil {
		fn(handle)
	}
	return 0
}

// OnShortcut sets the handler for presses; it runs on the main thread.
func (a *App) OnShortcut(fn func(handle string)) {
	if err := loadCarbon(); err != nil {
		return
	}
	hot.mu.Lock()
	hot.fn = fn
	hot.mu.Unlock()
}

// RegisterShortcut registers the combination system-wide. A combination that
// is already registered, by this app or by the system, is an error that says
// so.
func (a *App) RegisterShortcut(handle string, acc caps.Accelerator) error {
	ck, err := toCarbon(acc)
	if err != nil {
		return err
	}
	if err := loadCarbon(); err != nil {
		return err
	}
	return a.mainCall(func() error {
		if err := installHotKeyHandler(); err != nil {
			return err
		}
		hot.mu.Lock()
		defer hot.mu.Unlock()
		name := accelString(acc)
		if other, dup := hot.combos[ck]; dup {
			return fmt.Errorf("%w: %s is already used by shortcut %q", errShortcutTaken, name, other)
		}
		id, err := hot.table.add(handle)
		if err != nil {
			return err
		}
		var ref uintptr
		if st := carbon.registerEventHotKey(ck.Code, ck.Mods, hotKeyID(id), carbon.target, 0, &ref); st != 0 {
			hot.table.remove(handle)
			return carbonStatusError(st, name)
		}
		hot.refs[id] = ref
		hot.combos[ck] = handle
		hot.keys[handle] = ck
		return nil
	})
}

// UnregisterShortcut releases the shortcut; an unknown handle is not an error.
func (a *App) UnregisterShortcut(handle string) error {
	if err := loadCarbon(); err != nil {
		return err
	}
	return a.mainCall(func() error {
		hot.mu.Lock()
		defer hot.mu.Unlock()
		id, ok := hot.table.remove(handle)
		if !ok {
			return nil
		}
		ref := hot.refs[id]
		delete(hot.refs, id)
		if ck, ok := hot.keys[handle]; ok {
			delete(hot.combos, ck)
			delete(hot.keys, handle)
		}
		if ref != 0 {
			if st := carbon.unregisterEventHotKey(ref); st != 0 {
				return fmt.Errorf("darwin: UnregisterEventHotKey failed with OSStatus %d", st)
			}
		}
		return nil
	})
}
