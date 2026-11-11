//go:build darwin

package darwin

import (
	"fmt"
	"log"
	"strings"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Geometry structs passed by value to AppKit. purego marshals them per the
// platform ABI (HFA in float registers on arm64, SSE/memory on amd64).
type nsPoint struct {
	_    structs.HostLayout
	X, Y float64
}

type nsSize struct {
	_             structs.HostLayout
	Width, Height float64
}

type nsRect struct {
	_      structs.HostLayout
	Origin nsPoint
	Size   nsSize
}

var selCache sync.Map

// sel returns the cached selector for name.
func sel(name string) objc.SEL {
	if v, ok := selCache.Load(name); ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

// cls returns the class object as an ID, or 0 when the class is missing.
func cls(name string) objc.ID {
	return objc.ID(objc.GetClass(name))
}

func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// ptrFromID converts an object or block address to a pointer without tripping
// go vet's uintptr conversion check.
func ptrFromID(id objc.ID) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&id))
}

// lib holds process-wide handles resolved once by initRuntime.
var lib struct {
	once sync.Once
	err  error

	poolPush func() uintptr
	poolPop  func(uintptr)

	dispatchAsyncF func(queue, ctx, work uintptr)
	mainQueue      uintptr
	workCallback   uintptr
}

// mainWork is the Go-side queue behind Dispatch.
var mainWork = newWorkQueue()

func initRuntime() error {
	lib.once.Do(func() { lib.err = loadRuntime() })
	return lib.err
}

func loadRuntime() error {
	for _, f := range []string{
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/AppKit.framework/AppKit",
		"/System/Library/Frameworks/WebKit.framework/WebKit",
	} {
		if _, err := purego.Dlopen(f, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return fmt.Errorf("darwin: load %s: %w", f, err)
		}
	}
	libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&lib.poolPush, libobjc, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&lib.poolPop, libobjc, "objc_autoreleasePoolPop")

	libSystem, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return err
	}
	// dispatch_get_main_queue() is a macro for &_dispatch_main_q.
	lib.mainQueue, err = purego.Dlsym(libSystem, "_dispatch_main_q")
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&lib.dispatchAsyncF, libSystem, "dispatch_async_f")
	// One callback for the life of the process; the context carries a queue key.
	lib.workCallback = purego.NewCallback(runWork)
	return nil
}

// runWork is the dispatch_function_t behind every Dispatch call.
func runWork(ctx uintptr) {
	fn := mainWork.take(ctx)
	if fn == nil {
		return
	}
	pool := lib.poolPush()
	defer lib.poolPop(pool)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("krate/native: panic on main thread: %v", r)
		}
	}()
	fn()
}

// dispatch queues fn on the main queue and returns immediately.
func dispatch(fn func()) {
	key := mainWork.add(fn)
	lib.dispatchAsyncF(lib.mainQueue, key, lib.workCallback)
}

func isMainThread() bool {
	return objc.Send[bool](cls("NSThread"), sel("isMainThread"))
}

// runOnMain runs fn on the main thread and waits for it. Calling it from the
// main thread runs fn inline, so it is safe inside callbacks.
func runOnMain(fn func()) {
	if isMainThread() {
		fn()
		return
	}
	done := make(chan struct{})
	dispatch(func() {
		defer close(done)
		fn()
	})
	<-done
}

// withPool runs fn inside an autorelease pool. Main-thread only work that is
// not already inside dispatch or a callback from AppKit needs this.
func withPool(fn func()) {
	pool := lib.poolPush()
	defer lib.poolPop(pool)
	fn()
}

// nsString returns an autoreleased NSString. Invalid UTF-8 is replaced and NUL
// bytes dropped because stringWithUTF8String: would return nil or truncate.
func nsString(s string) objc.ID {
	s = strings.ToValidUTF8(s, "�")
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return cls("NSString").Send(sel("stringWithUTF8String:"), s)
}

// goString reads an NSString.
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	return objc.Send[string](s, sel("UTF8String"))
}

func respondsTo(obj objc.ID, s objc.SEL) bool {
	return objc.Send[bool](obj, sel("respondsToSelector:"), s)
}

func retain(obj objc.ID)  { obj.Send(sel("retain")) }
func release(obj objc.ID) { obj.Send(sel("release")) }

// nsDictionary builds an autoreleased NSDictionary of strings.
func nsDictionary(m map[string]string) objc.ID {
	d := cls("NSMutableDictionary").Send(sel("dictionaryWithCapacity:"), len(m))
	set := sel("setObject:forKey:")
	for k, v := range m {
		d.Send(set, nsString(v), nsString(k))
	}
	return d
}

// goStringMap reads an NSDictionary whose keys and values are NSStrings.
func goStringMap(d objc.ID) map[string]string {
	out := map[string]string{}
	if d == 0 {
		return out
	}
	keys := d.Send(sel("allKeys"))
	n := objc.Send[uint](keys, sel("count"))
	for i := uint(0); i < n; i++ {
		k := keys.Send(sel("objectAtIndex:"), i)
		v := d.Send(sel("objectForKey:"), k)
		out[goString(k)] = goString(v)
	}
	return out
}

// dataBytes copies the contents of an NSData.
func dataBytes(d objc.ID) []byte {
	if d == 0 {
		return nil
	}
	n := objc.Send[uint](d, sel("length"))
	if n == 0 {
		return nil
	}
	p := objc.Send[uintptr](d, sel("bytes"))
	return append([]byte(nil), unsafe.Slice((*byte)(ptrFromUintptr(p)), n)...)
}

// invokeBlock1 calls a foreign block of type void (^)(NSInteger). It reads the
// invoke pointer from the block literal (isa, flags, reserved, invoke) and calls
// it with the block itself as first argument. objc.Block.Invoke only works for
// blocks created by objc.NewBlock, so it cannot be used for blocks WebKit hands us.
func invokeBlock1(block objc.ID, arg uintptr) {
	if block == 0 {
		return
	}
	invoke := *(*uintptr)(unsafe.Add(ptrFromID(block), 16))
	purego.SyscallN(invoke, uintptr(block), arg)
}
