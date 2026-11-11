//go:build windows

package windows

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// comObj is a Go implementation of a COM object. The first field must be the
// vtable pointer: a COM interface pointer is the address of a struct whose
// first word points to the vtable.
//
// Objects are kept in objs (keyed by address) from creation until their
// reference count drops to zero, so the GC cannot collect an object that
// foreign code still points at. The Go heap does not move objects.
type comObj struct {
	vtbl    unsafe.Pointer
	refs    atomic.Int32
	ids     []guid
	oneShot bool // drop the creator's reference after the first Invoke

	invoke func(a, b uintptr) uintptr // completion and event handlers
	stream *goStream                  // IStream objects
	opts   *envOptions                // ICoreWebView2EnvironmentOptions objects
	app    *App
}

var (
	objMu sync.RWMutex
	objs  = map[uintptr]*comObj{}

	cbOnce sync.Once
	// Shared vtables. Every callback below is created once per process;
	// syscall.NewCallback slots are a small fixed resource.
	vtHandler unsafe.Pointer
	vtStream  unsafe.Pointer
)

func lookup(this uintptr) *comObj {
	objMu.RLock()
	o := objs[this]
	objMu.RUnlock()
	return o
}

func register(o *comObj) unsafe.Pointer {
	o.refs.Store(1)
	objMu.Lock()
	objs[uintptr(unsafe.Pointer(o))] = o
	objMu.Unlock()
	return unsafe.Pointer(o)
}

func unregister(o *comObj) {
	objMu.Lock()
	delete(objs, uintptr(unsafe.Pointer(o)))
	objMu.Unlock()
}

func initCallbacks() {
	cbOnce.Do(func() {
		qi := syscall.NewCallback(cbQueryInterface)
		addRef := syscall.NewCallback(cbAddRef)
		release := syscall.NewCallback(cbRelease)
		invoke := syscall.NewCallback(cbInvoke)
		notImpl := syscall.NewCallback(cbNotImpl)

		h := make([]uintptr, len(ifaceMethods["ICoreWebView2WebResourceRequestedEventHandler"]))
		h[iunknownSlotQI], h[iunknownSlotAddRef], h[iunknownSlotRelease] = qi, addRef, release
		h[handlerInvoke] = invoke
		vtHandler = unsafe.Pointer(&h[0])

		s := make([]uintptr, len(ifaceMethods["IStream"]))
		for i := range s {
			s[i] = notImpl
		}
		s[iunknownSlotQI], s[iunknownSlotAddRef], s[iunknownSlotRelease] = qi, addRef, release
		s[vtIndex("IStream", "Read")] = syscall.NewCallback(cbStreamRead)
		s[vtIndex("IStream", "Write")] = syscall.NewCallback(cbStreamWrite)
		s[vtIndex("IStream", "Seek")] = syscall.NewCallback(cbStreamSeek)
		vtStream = unsafe.Pointer(&s[0])
	})
}

const (
	iunknownSlotQI      = 0
	iunknownSlotAddRef  = 1
	iunknownSlotRelease = 2
)

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "krate native: "+format+"\n", a...)
}

func cbQueryInterface(this, riid, out uintptr) uintptr {
	if out == 0 {
		return eInvalidArg
	}
	*(*uintptr)(ptr(out)) = 0
	o := lookup(this)
	if o == nil || riid == 0 {
		return eNoInterface
	}
	g := *(*guid)(ptr(riid))
	for _, id := range o.ids {
		if id == g {
			o.refs.Add(1)
			*(*uintptr)(ptr(out)) = this
			return sOK
		}
	}
	return eNoInterface
}

func cbAddRef(this uintptr) uintptr {
	if o := lookup(this); o != nil {
		return uintptr(o.refs.Add(1))
	}
	return 1
}

func cbRelease(this uintptr) uintptr {
	o := lookup(this)
	if o == nil {
		return 0
	}
	n := o.refs.Add(-1)
	if n == 0 {
		unregister(o)
		if o.stream != nil {
			o.stream.release()
		}
	}
	if n < 0 {
		return 0
	}
	return uintptr(n)
}

// cbInvoke serves the Invoke method of every handler interface; all of them
// take two arguments after this.
func cbInvoke(this, a, b uintptr) (ret uintptr) {
	o := lookup(this)
	if o == nil || o.invoke == nil {
		return eFail
	}
	defer func() {
		if r := recover(); r != nil {
			logf("panic in COM callback: %v", r)
			ret = eFail
		}
		if o.oneShot {
			cbRelease(this)
		}
	}()
	return o.invoke(a, b)
}

func cbNotImpl(this, a, b, c uintptr) uintptr { return eNotImpl }

// newHandler creates a handler object for the interface with the given id.
func newHandler(iid string, oneShot bool, fn func(a, b uintptr) uintptr) unsafe.Pointer {
	o := &comObj{
		vtbl:    vtHandler,
		ids:     []guid{mustGUID(iidIUnknown), mustGUID(iid)},
		oneShot: oneShot,
		invoke:  fn,
	}
	return register(o)
}

// goStream is a read-only IStream fed by a blockBuf. WebView2 pulls the
// response body from it. Reads are served from whatever thread WebView2
// uses: a blocking wait is fine on a background thread; on the UI thread the
// wait pumps messages instead (see Read).
type goStream struct {
	app *App
	buf *blockBuf
}

func (s *goStream) release() { s.buf.Cancel() }

func newGoStream(app *App, buf *blockBuf) unsafe.Pointer {
	o := &comObj{
		vtbl:   vtStream,
		ids:    []guid{mustGUID(iidIUnknown), mustGUID(iidISequentialStream), mustGUID(iidIStream)},
		stream: &goStream{app: app, buf: buf},
	}
	return register(o)
}

// Read implements ISequentialStream::Read: it returns as soon as any data is
// available (a short read with S_OK) and signals the end with zero bytes.
func cbStreamRead(this, pv, cb, pcbRead uintptr) uintptr {
	o := lookup(this)
	if o == nil || o.stream == nil {
		return eFail
	}
	if pcbRead != 0 {
		*(*uint32)(ptr(pcbRead)) = 0
	}
	if cb == 0 {
		return sOK
	}
	if cb > 1<<20 {
		cb = 1 << 20
	}
	p := unsafe.Slice((*byte)(ptr(pv)), int(cb))
	n, err := o.stream.read(p)
	if pcbRead != 0 {
		*(*uint32)(ptr(pcbRead)) = uint32(n)
	}
	if err == nil || err == io.EOF {
		return sOK
	}
	return eFail
}

func (s *goStream) read(p []byte) (int, error) {
	if s.app == nil || !s.app.onMain() {
		return s.buf.Read(p)
	}
	for {
		n, err := s.buf.TryRead(p)
		if err != errWouldBlock {
			return n, err
		}
		if !s.app.pumpOnce(10) {
			return 0, io.ErrClosedPipe
		}
	}
}

// Write: the stream is read-only.
func cbStreamWrite(this, pv, cb, pcbWritten uintptr) uintptr {
	if pcbWritten != 0 {
		*(*uint32)(ptr(pcbWritten)) = 0
	}
	return stgEAccessDenied
}

// Seek: the stream is not seekable (its length is unknown while streaming).
func cbStreamSeek(this, move, origin, newPos uintptr) uintptr {
	if newPos != 0 {
		*(*uint64)(ptr(newPos)) = 0
	}
	return stgEInvalidFunction
}
