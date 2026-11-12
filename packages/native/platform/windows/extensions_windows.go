//go:build windows

package windows

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unsafe"


	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Browser extensions. WebView2 loads unpacked Chromium extensions into the
// profile with ICoreWebView2Profile7::AddBrowserExtension, provided the
// environment was created with AreBrowserExtensionsEnabled (see
// envoptions_windows.go). The interfaces are checked against WebView2.h by
// the vtable table test; the CI native job runs TestGUIExtension with a tiny
// content-script extension.
//
// Not run on a real Windows machine by the author.

var _ caps.Extensions = (*App)(nil)

// ExtensionsSupported reports whether the app asked for extensions; whether
// the installed runtime has the interface shows when one is loaded.
func (a *App) ExtensionsSupported() bool { return a.cfg.Extensions }

const extensionTimeout = 30 * time.Second

// withProfile7 calls fn on the main thread with the ICoreWebView2Profile7 of
// the first ready view. fn must release nothing: the pointer is released
// after it returns.
func (a *App) withProfile7(fn func(p7 unsafe.Pointer)) error {
	if !a.cfg.Extensions {
		return fmt.Errorf("%w: the app was not started with extensions enabled (native.extensions)", krate.ErrUnsupported)
	}
	var found *view
	a.callMain(func() {
		for _, w := range a.windows {
			if w.destroyed || w.released {
				continue
			}
			if w.pv != nil {
				found = w.pv
				return
			}
		}
	})
	if found == nil {
		return errors.New("windows: no window is open to load an extension into")
	}
	errc := make(chan error, 1)
	found.do(func() {
		wv13 := queryInterface(found.webview, iidWebView2_13)
		if wv13 == nil {
			errc <- fmt.Errorf("%w: this WebView2 runtime has no profile API (ICoreWebView2_13)", krate.ErrUnsupported)
			return
		}
		defer comRelease(wv13)
		var prof uintptr
		if hr := vcall(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&prof))); failed(hr) || prof == 0 {
			errc <- fmt.Errorf("windows: get_Profile: 0x%08x", uint32(hr))
			return
		}
		defer comRelease(ptr(prof))
		p7 := queryInterface(ptr(prof), iidProfile7)
		if p7 == nil {
			errc <- fmt.Errorf("%w: this WebView2 runtime has no extension support (ICoreWebView2Profile7)", krate.ErrUnsupported)
			return
		}
		defer comRelease(p7)
		fn(p7)
		errc <- nil
	})
	select {
	case err := <-errc:
		return err
	case <-time.After(extensionTimeout):
		return errors.New("windows: the WebView2 profile did not become available")
	}
}

// extInfo reads an ICoreWebView2BrowserExtension.
func extInfo(ext unsafe.Pointer) caps.ExtensionInfo {
	var id, name uintptr
	var enabled int32
	_ = vcall(ext, extGetID, uintptr(unsafe.Pointer(&id)))
	_ = vcall(ext, extGetName, uintptr(unsafe.Pointer(&name)))
	_ = vcall(ext, extGetIsEnabled, uintptr(unsafe.Pointer(&enabled)))
	return caps.ExtensionInfo{ID: takeString(id), Name: takeString(name), Enabled: enabled != 0}
}

// LoadExtension loads an unpacked extension directory.
func (a *App) LoadExtension(dir string) (caps.ExtensionInfo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return caps.ExtensionInfo{}, err
	}
	type result struct {
		info caps.ExtensionInfo
		err  error
	}
	done := make(chan result, 1)
	werr := a.withProfile7(func(p7 unsafe.Pointer) {
		h := newHandler(iidAddExtensionHandler, true, func(hr, ext uintptr) uintptr {
			if failed(hr) || ext == 0 {
				done <- result{err: fmt.Errorf("windows: AddBrowserExtension %s: 0x%08x", abs, uint32(hr))}
				return sOK
			}
			done <- result{info: extInfo(ptr(ext))}
			return sOK
		})
		if hr := vcall(p7, profile7AddExtension, uintptr(unsafe.Pointer(wstr(abs))), uintptr(h)); failed(hr) {
			done <- result{err: fmt.Errorf("windows: AddBrowserExtension %s: 0x%08x", abs, uint32(hr))}
		}
	})
	if werr != nil {
		return caps.ExtensionInfo{}, werr
	}
	select {
	case r := <-done:
		return r.info, r.err
	case <-time.After(extensionTimeout):
		return caps.ExtensionInfo{}, errors.New("windows: loading the extension timed out")
	}
}

// listExtensions runs visit for each extension (main thread); the pointer is
// valid only inside visit.
func (a *App) listExtensions(visit func(ext unsafe.Pointer) bool) error {
	done := make(chan error, 1)
	werr := a.withProfile7(func(p7 unsafe.Pointer) {
		h := newHandler(iidGetExtensionsHandler, true, func(hr, list uintptr) uintptr {
			if failed(hr) || list == 0 {
				done <- fmt.Errorf("windows: GetBrowserExtensions: 0x%08x", uint32(hr))
				return sOK
			}
			l := ptr(list)
			var n uint32
			_ = vcall(l, extListGetCount, uintptr(unsafe.Pointer(&n)))
			for i := uint32(0); i < n; i++ {
				var ext uintptr
				if failed(vcall(l, extListGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&ext)))) || ext == 0 {
					continue
				}
				cont := visit(ptr(ext))
				comRelease(ptr(ext))
				if !cont {
					break
				}
			}
			done <- nil
			return sOK
		})
		if hr := vcall(p7, profile7GetExtensions, uintptr(h)); failed(hr) {
			done <- fmt.Errorf("windows: GetBrowserExtensions: 0x%08x", uint32(hr))
		}
	})
	if werr != nil {
		return werr
	}
	select {
	case err := <-done:
		return err
	case <-time.After(extensionTimeout):
		return errors.New("windows: listing the extensions timed out")
	}
}

// ListExtensions returns the loaded extensions.
func (a *App) ListExtensions() ([]caps.ExtensionInfo, error) {
	out := []caps.ExtensionInfo{}
	err := a.listExtensions(func(ext unsafe.Pointer) bool {
		out = append(out, extInfo(ext))
		return true
	})
	return out, err
}

// RemoveExtension unloads an extension by id.
func (a *App) RemoveExtension(id string) error {
	removed := make(chan error, 1)
	found := false
	err := a.listExtensions(func(ext unsafe.Pointer) bool {
		if extInfo(ext).ID != id {
			return true
		}
		found = true
		h := newHandler(iidRemoveExtensionHandler, true, func(hr, _ uintptr) uintptr {
			if failed(hr) {
				removed <- fmt.Errorf("windows: removing extension %s: 0x%08x", id, uint32(hr))
			} else {
				removed <- nil
			}
			return sOK
		})
		if hr := vcall(ext, extRemove, uintptr(h)); failed(hr) {
			removed <- fmt.Errorf("windows: removing extension %s: 0x%08x", id, uint32(hr))
		}
		return false
	})
	if err != nil {
		return err
	}
	if !found {
		return krate.Errorf("not_found", "no extension with id %q", id)
	}
	select {
	case err := <-removed:
		return err
	case <-time.After(extensionTimeout):
		return errors.New("windows: removing the extension timed out")
	}
}

