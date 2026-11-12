//go:build windows

package windows

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.FileDrops = (*App)(nil)

var dropState struct {
	mu sync.Mutex
	fn func(caps.FileDrop)
}

// dropSeen counts what the host received, for tests: drop messages, and the
// additional objects that came with them.
var dropSeen struct{ messages, objects atomic.Int64 }

// OnFileDrop sets the handler for files dropped onto a view. The page keeps
// its own drop events; see dropShim.
func (a *App) OnFileDrop(fn func(caps.FileDrop)) {
	dropState.mu.Lock()
	dropState.fn = fn
	dropState.mu.Unlock()
}

// onDropMessage handles a drop message. args is the borrowed
// ICoreWebView2WebMessageReceivedEventArgs of the message. Main thread.
func (a *App) onDropMessage(v *view, args unsafe.Pointer, m dropMsg) {
	paths, objects := droppedFiles(args)
	dropSeen.messages.Add(1)
	dropSeen.objects.Add(int64(objects))
	if len(paths) == 0 {
		return
	}
	dropState.mu.Lock()
	fn := dropState.fn
	dropState.mu.Unlock()
	if fn != nil {
		fn(caps.FileDrop{View: v.ref, Window: v.w.Ref(), Paths: paths, X: m.X, Y: m.Y})
	}
}

// droppedFiles returns the paths of the File objects that came with a web
// message: the args' additional objects, each an ICoreWebView2File. A File that
// has no path (made by script, not by the system) is skipped. objects is how
// many additional objects there were.
func droppedFiles(args unsafe.Pointer) (paths []string, objects int) {
	args2 := queryInterface(args, iidWebMessageArgs2)
	if args2 == nil {
		logOnce("dropargs2", "file drops: the WebView2 runtime has no ICoreWebView2WebMessageReceivedEventArgs2; dropped file paths are unavailable")
		return nil, 0
	}
	defer comRelease(args2)
	var coll uintptr
	if failed(vcall(args2, msgArgs2GetAdditionalObjects, uintptr(unsafe.Pointer(&coll)))) || coll == 0 {
		return nil, 0
	}
	view := ptr(coll)
	defer comRelease(view)
	var n uint32
	if failed(vcall(view, objViewGetCount, uintptr(unsafe.Pointer(&n)))) {
		return nil, 0
	}
	const maxDropped = 4096
	if n > maxDropped {
		n = maxDropped
	}
	objects = int(n)
	for i := uint32(0); i < n; i++ {
		var unk uintptr
		if failed(vcall(view, objViewGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&unk)))) || unk == 0 {
			continue
		}
		obj := ptr(unk)
		if f := queryInterface(obj, iidWebView2File); f != nil {
			if p := comString(f, fileGetPath); p != "" {
				paths = append(paths, p)
			}
			comRelease(f)
		}
		comRelease(obj)
	}
	return paths, objects
}
