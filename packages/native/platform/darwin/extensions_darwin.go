//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Browser extensions through WKWebExtension and WKWebExtensionController,
// macOS 15.4 and later. Every class and selector is looked up at run time, so
// an older macOS reports krate.ErrUnsupported instead of crashing.
//
// Written and compiled, not run on a real Mac by the author. Two limits are
// from the API, not from this code: an extension's match patterns cover
// http, https and file pages, so a content script does not run on the app's
// own krate:// pages; and the controller must be attached to a web view's
// configuration before the web view is created, which newWebView does when
// Options.Extensions is set.

var _ caps.Extensions = (*App)(nil)

const (
	extPermissionGrantedExplicitly = 3 // WKWebExtensionContextPermissionStatusGrantedExplicitly
	extensionTimeout               = 30 * time.Second
)

// ExtensionsSupported reports whether the app asked for extensions and this
// macOS has WKWebExtension.
func (a *App) ExtensionsSupported() bool {
	return a.opts.Extensions && cls("WKWebExtension") != 0 && cls("WKWebExtensionController") != 0 &&
		cls("WKWebExtensionContext") != 0
}

// extensionController returns the app's controller, creating it on first use.
// Main thread.
func (a *App) extensionController() objc.ID {
	if a.extCtl == 0 && a.ExtensionsSupported() {
		a.extCtl = cls("WKWebExtensionController").Send(sel("new"))
	}
	return a.extCtl
}

// applyExtensionConfig attaches the controller to a web view configuration.
func (a *App) applyExtensionConfig(config objc.ID) {
	if !a.ExtensionsSupported() {
		return
	}
	if c := a.extensionController(); c != 0 && respondsTo(config, sel("setWebExtensionController:")) {
		config.Send(sel("setWebExtensionController:"), c)
	}
}

func (a *App) extensionInfo(ctx objc.ID) caps.ExtensionInfo {
	info := caps.ExtensionInfo{Enabled: true}
	if respondsTo(ctx, sel("uniqueIdentifier")) {
		info.ID = goString(ctx.Send(sel("uniqueIdentifier")))
	}
	if ext := ctx.Send(sel("webExtension")); ext != 0 && respondsTo(ext, sel("displayName")) {
		info.Name = goString(ext.Send(sel("displayName")))
	}
	return info
}

func nsErrorText(e objc.ID) string {
	if e == 0 {
		return "unknown error"
	}
	return goString(e.Send(sel("localizedDescription")))
}

// LoadExtension loads an unpacked extension directory.
func (a *App) LoadExtension(dir string) (caps.ExtensionInfo, error) {
	if !a.ExtensionsSupported() {
		return caps.ExtensionInfo{}, fmt.Errorf("%w: browser extensions need macOS 15.4 or later", krate.ErrUnsupported)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return caps.ExtensionInfo{}, err
	}
	type result struct {
		info caps.ExtensionInfo
		err  error
	}
	done := make(chan result, 1)
	finish := func(ext objc.ID, nsErr objc.ID) {
		if ext == 0 {
			done <- result{err: errors.New("darwin: loading the extension: " + nsErrorText(nsErr))}
			return
		}
		ctx := cls("WKWebExtensionContext").Send(sel("contextForExtension:"), ext)
		if ctx == 0 {
			done <- result{err: errors.New("darwin: no extension context")}
			return
		}
		// Content scripts and host access need an explicit grant.
		if pat := cls("WKWebExtensionMatchPattern").Send(sel("allURLsMatchPattern")); pat != 0 &&
			respondsTo(ctx, sel("setPermissionStatus:forMatchPattern:")) {
			ctx.Send(sel("setPermissionStatus:forMatchPattern:"), int(extPermissionGrantedExplicitly), pat)
		}
		var loadErr objc.ID
		ok := objc.Send[bool](a.extensionController(), sel("loadExtensionContext:error:"), ctx, uintptr(unsafe.Pointer(&loadErr)))
		if !ok {
			done <- result{err: errors.New("darwin: loading the extension: " + nsErrorText(loadErr))}
			return
		}
		retain(ctx)
		done <- result{info: a.extensionInfo(ctx)}
	}
	runOnMain(func() {
		withPool(func() {
			url := cls("NSURL").Send(sel("fileURLWithPath:isDirectory:"), nsString(abs), true)
			if s := sel("extensionWithResourceBaseURL:completionHandler:"); respondsTo(objc.ID(objc.GetClass("WKWebExtension")), s) {
				block := objc.NewBlock(func(_ objc.Block, ext, nsErr objc.ID) { finish(ext, nsErr) })
				cls("WKWebExtension").Send(s, url, objc.ID(block))
				return
			}
			var initErr objc.ID
			ext := cls("WKWebExtension").Send(sel("alloc")).Send(sel("initWithResourceBaseURL:error:"), url, uintptr(unsafe.Pointer(&initErr)))
			finish(ext, initErr)
		})
	})
	select {
	case r := <-done:
		return r.info, r.err
	case <-time.After(extensionTimeout):
		return caps.ExtensionInfo{}, errors.New("darwin: loading the extension timed out")
	}
}

// contexts returns the loaded extension contexts. Main thread.
func (a *App) contexts() []objc.ID {
	ctl := a.extensionController()
	if ctl == 0 {
		return nil
	}
	set := ctl.Send(sel("extensionContexts"))
	if set == 0 {
		return nil
	}
	arr := set.Send(sel("allObjects"))
	n := objc.Send[uint](arr, sel("count"))
	out := make([]objc.ID, 0, n)
	for i := uint(0); i < n; i++ {
		out = append(out, arr.Send(sel("objectAtIndex:"), i))
	}
	return out
}

// ListExtensions returns the loaded extensions.
func (a *App) ListExtensions() ([]caps.ExtensionInfo, error) {
	if !a.ExtensionsSupported() {
		return nil, fmt.Errorf("%w: browser extensions need macOS 15.4 or later", krate.ErrUnsupported)
	}
	out := []caps.ExtensionInfo{}
	runOnMain(func() {
		withPool(func() {
			for _, c := range a.contexts() {
				out = append(out, a.extensionInfo(c))
			}
		})
	})
	return out, nil
}

// RemoveExtension unloads an extension by id.
func (a *App) RemoveExtension(id string) error {
	if !a.ExtensionsSupported() {
		return fmt.Errorf("%w: browser extensions need macOS 15.4 or later", krate.ErrUnsupported)
	}
	var err error
	found := false
	runOnMain(func() {
		withPool(func() {
			for _, c := range a.contexts() {
				if a.extensionInfo(c).ID != id {
					continue
				}
				found = true
				var unloadErr objc.ID
				if !objc.Send[bool](a.extensionController(), sel("unloadExtensionContext:error:"), c, uintptr(unsafe.Pointer(&unloadErr))) {
					err = errors.New("darwin: unloading the extension: " + nsErrorText(unloadErr))
					return
				}
				release(c)
				return
			}
		})
	})
	if err == nil && !found {
		return krate.Errorf("not_found", "no extension with id %q", id)
	}
	return err
}
