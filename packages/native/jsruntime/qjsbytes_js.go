//go:build js

package jsruntime

import (
	"syscall/js"

	quickjs "github.com/kratejs/krate/packages/native/jsruntime/internal/qjs"
)

// Raw byte transfer on js/wasm: bytes cross as Uint8Array/ArrayBuffer values
// of the host engine, copied with syscall/js.

var (
	// binaryTransfer reports whether the raw ArrayBuffer path is usable.
	binaryTransfer = true
	// binaryLayoutErr says why it is not (never, on js/wasm).
	binaryLayoutErr error
	// forceBase64 makes new VMs use the base64 fallback (tests only).
	forceBase64 bool
)

// newArrayBuffer returns a new ArrayBuffer holding a copy of b.
func newArrayBuffer(vm *quickjs.VM, b []byte) (quickjs.Value, bool) {
	u8 := js.Global().Get("Uint8Array").New(len(b))
	if len(b) > 0 {
		js.CopyBytesToJS(u8, b)
	}
	return quickjs.ValueOf(vm, u8.Get("buffer")), true
}

// valueBytes copies the bytes of a typed array (honoring its offset and
// length) or of an ArrayBuffer. It reports false for any other value.
func valueBytes(vm *quickjs.VM, v quickjs.Value) ([]byte, bool) {
	jv := v.JS()
	if jv.Type() != js.TypeObject {
		return nil, false
	}
	g := js.Global()
	var u8 js.Value
	switch {
	case g.Get("ArrayBuffer").Call("isView", jv).Bool():
		u8 = g.Get("Uint8Array").New(jv.Get("buffer"), jv.Get("byteOffset"), jv.Get("byteLength"))
	case jv.InstanceOf(g.Get("ArrayBuffer")):
		u8 = g.Get("Uint8Array").New(jv)
	case g.Get("SharedArrayBuffer").Truthy() && jv.InstanceOf(g.Get("SharedArrayBuffer")):
		u8 = g.Get("Uint8Array").New(jv)
	default:
		return nil, false
	}
	out := make([]byte, u8.Get("length").Int())
	if len(out) > 0 {
		js.CopyBytesToGo(out, u8)
	}
	return out, true
}

// runGC does nothing: the host engine collects garbage itself.
func runGC(vm *quickjs.VM) {}

// GC does nothing on js/wasm.
func (r *Runtime) GC() {}
