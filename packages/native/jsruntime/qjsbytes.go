package jsruntime

// Raw byte transfer between Go and a QuickJS VM.
//
// modernc.org/quickjs converts only numbers, strings and JSON-shaped values,
// so binary data used to cross the boundary as base64 text, encoded and
// decoded by JS code in the VM, which is slow for large bodies. The C library
// underneath (modernc.org/libquickjs) has the ArrayBuffer API; calling it
// needs the JSContext and libc TLS of a VM and the raw JSValue inside a
// quickjs.Value, which the wrapper keeps in unexported fields. This file reads
// them through offsets found by reflection at init, checked by field name and
// type, so a renamed or retyped field disables the fast path instead of
// reading garbage.
//
// When the check fails (a future wrapper version), binaryTransfer is false
// and every crossing falls back to base64; TestBinaryTransferAvailable fails
// so the upgrade is noticed.
//
// Thread affinity: a VM is driven by one goroutine at a time, and these
// helpers run on that goroutine (inside host function calls or between
// evals) with the TLS of the wrapper's runtime, as the wrapper's own calls do.
// No Go pointer is passed into the C code: bytes are copied into and out of
// memory the VM allocated.

import (
	"fmt"
	"reflect"
	"unsafe"

	"modernc.org/libc"
	lib "modernc.org/libquickjs"
	"modernc.org/quickjs"
)

// valueLayout mirrors quickjs.Value ({vm *VM; v lib.TJSValue}).
type valueLayout struct {
	vm *quickjs.VM
	v  lib.TJSValue
}

var (
	// binaryTransfer reports whether the raw ArrayBuffer path is usable.
	binaryTransfer bool
	// binaryLayoutErr says why it is not.
	binaryLayoutErr error
	// forceBase64 makes new VMs use the base64 fallback (tests only).
	forceBase64 bool

	vmContextOff, vmRuntimeOff, runtimeTLSOff uintptr
)

func init() {
	binaryLayoutErr = checkQuickJSLayout()
	binaryTransfer = binaryLayoutErr == nil
}

func checkQuickJSLayout() error {
	vt := reflect.TypeFor[quickjs.VM]()
	c, ok := vt.FieldByName("cContext")
	if !ok || c.Type.Kind() != reflect.Uintptr {
		return fmt.Errorf("quickjs.VM has no cContext uintptr field")
	}
	rf, ok := vt.FieldByName("runtime")
	if !ok || rf.Type.Kind() != reflect.Pointer || rf.Type.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("quickjs.VM has no runtime pointer field")
	}
	tf, ok := rf.Type.Elem().FieldByName("tls")
	if !ok || tf.Type != reflect.TypeFor[*libc.TLS]() {
		return fmt.Errorf("the quickjs runtime has no tls *libc.TLS field")
	}
	val := reflect.TypeFor[quickjs.Value]()
	if val.NumField() != 2 || val.Size() != unsafe.Sizeof(valueLayout{}) {
		return fmt.Errorf("quickjs.Value layout changed")
	}
	f0, f1 := val.Field(0), val.Field(1)
	if f0.Name != "vm" || f0.Type != reflect.TypeFor[*quickjs.VM]() || f0.Offset != unsafe.Offsetof(valueLayout{}.vm) ||
		f1.Name != "v" || f1.Type != reflect.TypeFor[lib.TJSValue]() || f1.Offset != unsafe.Offsetof(valueLayout{}.v) {
		return fmt.Errorf("quickjs.Value layout changed")
	}
	vmContextOff, vmRuntimeOff, runtimeTLSOff = c.Offset, rf.Offset, tf.Offset
	return nil
}

// vmHandles returns the libc TLS and JSContext of vm.
func vmHandles(vm *quickjs.VM) (*libc.TLS, uintptr) {
	p := unsafe.Pointer(vm)
	ctx := *(*uintptr)(unsafe.Add(p, vmContextOff))
	rt := *(*unsafe.Pointer)(unsafe.Add(p, vmRuntimeOff))
	return *(**libc.TLS)(unsafe.Add(rt, runtimeTLSOff)), ctx
}

func rawValue(v *quickjs.Value) lib.TJSValue {
	return (*valueLayout)(unsafe.Pointer(v)).v
}

func wrapValue(vm *quickjs.VM, v lib.TJSValue) quickjs.Value {
	m := valueLayout{vm: vm, v: v}
	return *(*quickjs.Value)(unsafe.Pointer(&m))
}

// cMemory views n bytes of C memory at p. Loading the pointer through memory
// keeps go vet's unsafeptr check quiet; p is never Go memory.
func cMemory(p uintptr, n int) []byte {
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), n)
}

func readSize(p uintptr) uint64 {
	return uint64(*(*lib.Tsize_t)(*(*unsafe.Pointer)(unsafe.Pointer(&p))))
}

// clearException drops the exception a failed C call left pending.
func clearException(tls *libc.TLS, ctx uintptr) {
	lib.XFreeValue(tls, ctx, lib.XJS_GetException(tls, ctx))
}

// arrayBufferData returns the data pointer and length of an ArrayBuffer (or
// SharedArrayBuffer), or ok false (with no exception left pending).
func arrayBufferData(tls *libc.TLS, ctx uintptr, v lib.TJSValue) (p uintptr, n uint64, ok bool) {
	ps := tls.Alloc(int(unsafe.Sizeof(lib.Tsize_t(0))))
	defer tls.Free(int(unsafe.Sizeof(lib.Tsize_t(0))))
	// The data of a live buffer is never NULL: QuickJS allocates at least one
	// byte, even for an empty buffer.
	p = lib.XJS_GetArrayBuffer(tls, ctx, ps, v)
	if p == 0 {
		clearException(tls, ctx)
		return 0, 0, false
	}
	return p, readSize(ps), true
}

// newArrayBuffer returns a new ArrayBuffer holding a copy of b. The caller
// owns the returned Value (Free it, or hand its reference to JS).
func newArrayBuffer(vm *quickjs.VM, b []byte) (quickjs.Value, bool) {
	tls, ctx := vmHandles(vm)
	// A NULL source makes QuickJS allocate a zeroed buffer from the VM's
	// allocator (counted against its memory limit) without copying.
	v := lib.XJS_NewArrayBufferCopy(tls, ctx, 0, lib.Tsize_t(len(b)))
	p, n, ok := arrayBufferData(tls, ctx, v)
	if !ok || n != uint64(len(b)) {
		lib.XFreeValue(tls, ctx, v)
		clearException(tls, ctx)
		return quickjs.Value{}, false
	}
	copy(cMemory(p, len(b)), b)
	return wrapValue(vm, v), true
}

// valueBytes copies the bytes of a typed array (honoring its offset and
// length) or of an ArrayBuffer. It reports false for any other value. v stays
// owned by the caller.
func valueBytes(vm *quickjs.VM, v quickjs.Value) ([]byte, bool) {
	tls, ctx := vmHandles(vm)
	raw := rawValue(&v)
	sz := int(unsafe.Sizeof(lib.Tsize_t(0)))
	pv := tls.Alloc(2 * sz)
	defer tls.Free(2 * sz)
	var p uintptr
	var n, off, length uint64
	var ok bool
	buf := lib.XJS_GetTypedArrayBuffer(tls, ctx, raw, pv, pv+uintptr(sz), 0)
	if lib.XJS_HasException(tls, ctx) != 0 {
		// Not a typed array: try a plain ArrayBuffer.
		clearException(tls, ctx)
		p, n, ok = arrayBufferData(tls, ctx, raw)
		length = n
	} else {
		// buf is a new reference to the view's buffer.
		p, n, ok = arrayBufferData(tls, ctx, buf)
		lib.XFreeValue(tls, ctx, buf)
		off, length = readSize(pv), readSize(pv+uintptr(sz))
	}
	if !ok || off > n || length > n-off {
		return nil, false
	}
	out := make([]byte, length)
	copy(out, cMemory(p+uintptr(off), int(length)))
	return out, true
}

// runGC runs a full QuickJS garbage collection, cycles included. The wrapper
// does not expose it; without binary transfer (layout unknown) it does nothing.
func runGC(vm *quickjs.VM) {
	if !binaryTransfer {
		return
	}
	tls, ctx := vmHandles(vm)
	lib.XJS_RunGC(tls, lib.XJS_GetRuntime(tls, ctx))
}
