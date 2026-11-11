//go:build windows

package windows

import (
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

// envOptions is a Go implementation of ICoreWebView2EnvironmentOptions
// (WebView2.h line 48628): four properties with get/put methods. Later
// options interfaces (ICoreWebView2EnvironmentOptions2..8) are not
// implemented; QueryInterface answers E_NOINTERFACE for them and the runtime
// then uses their defaults. This follows the COM contract but has not been run.
//
// TargetCompatibleBrowserVersion defaults to defaultTargetVersion: the SDK
// implementation always reports a version there, and the runtime rejects the
// whole options object with E_INVALIDARG when it is null.
//
// String getters return nil when the property was never set, like the SDK
// implementation in WebView2EnvironmentOptions.h; non-empty values are copied
// with CoTaskMemAlloc because the caller frees them with CoTaskMemFree.
type envOptions struct {
	mu                       sync.Mutex
	arguments, lang, version string
	sso                      bool
}

// defaultTargetVersion is the oldest runtime that has the options interface
// we implement; any installed runtime is at least this new.
const defaultTargetVersion = "86.0.616.0"

var (
	optOnce sync.Once
	vtOpts  unsafe.Pointer
)

func initOptionsVtable() {
	optOnce.Do(func() {
		qi := syscall.NewCallback(cbQueryInterface)
		addRef := syscall.NewCallback(cbAddRef)
		release := syscall.NewCallback(cbRelease)
		n := len(ifaceMethods["ICoreWebView2EnvironmentOptions"])
		t := make([]uintptr, n)
		t[iunknownSlotQI], t[iunknownSlotAddRef], t[iunknownSlotRelease] = qi, addRef, release
		const iface = "ICoreWebView2EnvironmentOptions"
		str := func(prop string, field func(o *envOptions) *string) {
			t[vtIndex(iface, "get_"+prop)] = syscall.NewCallback(func(this, out uintptr) uintptr {
				return optGetString(this, out, field)
			})
			t[vtIndex(iface, "put_"+prop)] = syscall.NewCallback(func(this, value uintptr) uintptr {
				return optPutString(this, value, field)
			})
		}
		str("AdditionalBrowserArguments", func(o *envOptions) *string { return &o.arguments })
		str("Language", func(o *envOptions) *string { return &o.lang })
		str("TargetCompatibleBrowserVersion", func(o *envOptions) *string { return &o.version })
		t[vtIndex(iface, "get_AllowSingleSignOnUsingOSPrimaryAccount")] = syscall.NewCallback(func(this, out uintptr) uintptr {
			o := optsOf(this)
			if o == nil || out == 0 {
				return eInvalidArg
			}
			o.mu.Lock()
			defer o.mu.Unlock()
			v := uint32(0)
			if o.sso {
				v = 1
			}
			*(*uint32)(ptr(out)) = v
			return sOK
		})
		t[vtIndex(iface, "put_AllowSingleSignOnUsingOSPrimaryAccount")] = syscall.NewCallback(func(this, v uintptr) uintptr {
			o := optsOf(this)
			if o == nil {
				return eInvalidArg
			}
			o.mu.Lock()
			o.sso = uint32(v) != 0
			o.mu.Unlock()
			return sOK
		})
		vtOpts = unsafe.Pointer(&t[0])
	})
}

func optsOf(this uintptr) *envOptions {
	if o := lookup(this); o != nil {
		return o.opts
	}
	return nil
}

func optGetString(this, out uintptr, field func(*envOptions) *string) uintptr {
	o := optsOf(this)
	if o == nil || out == 0 {
		return eInvalidArg
	}
	*(*uintptr)(ptr(out)) = 0
	o.mu.Lock()
	s := *field(o)
	o.mu.Unlock()
	if s == "" {
		return sOK
	}
	u := utf16.Encode([]rune(s))
	u = append(u, 0)
	p, _, _ := pCoTaskMemAlloc.Call(uintptr(len(u)) * 2)
	if p == 0 {
		return eOutOfMemory
	}
	copy(unsafe.Slice((*uint16)(ptr(p)), len(u)), u)
	*(*uintptr)(ptr(out)) = p
	return sOK
}

func optPutString(this, value uintptr, field func(*envOptions) *string) uintptr {
	o := optsOf(this)
	if o == nil {
		return eInvalidArg
	}
	s := ""
	if value != 0 {
		s = xw.UTF16PtrToString((*uint16)(ptr(value)))
	}
	o.mu.Lock()
	*field(o) = s
	o.mu.Unlock()
	return sOK
}

// newEnvironmentOptions returns an ICoreWebView2EnvironmentOptions carrying
// the given AdditionalBrowserArguments, or nil when args is empty (the loader
// then receives no options, exactly as before). The caller owns one
// reference.
func newEnvironmentOptions(args string) unsafe.Pointer {
	if args == "" {
		return nil
	}
	initOptionsVtable()
	o := &comObj{
		vtbl: vtOpts,
		ids:  []guid{mustGUID(iidIUnknown), mustGUID(iidEnvironmentOptions)},
		opts: &envOptions{arguments: args, version: defaultTargetVersion},
	}
	return register(o)
}
