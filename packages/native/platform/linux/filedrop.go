//go:build linux

package linux

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.FileDrops = (*App)(nil)

// OnFileDrop sets the handler for files dropped onto a view. The page keeps
// its own HTML5 drop events: the handlers below only watch the GTK drag
// signals and never stop them.
func (a *App) OnFileDrop(fn func(caps.FileDrop)) {
	a.mu.Lock()
	a.dropFn = fn
	a.mu.Unlock()
}

// dropStash is the file list WebKit asked the source for while the drag was
// over the view. WebKitGTK requests the data of a drag when it enters the
// view, so by the time of the drop the list is usually here already.
type dropStash struct {
	ctx   uintptr // the GdkDragContext of the drag
	paths []string
	x, y  int32
}

// dropState is the per-app state of the drop watcher (main thread only).
type dropState struct {
	once   sync.Once
	stash  map[caps.ViewRef]*dropStash
	wait   map[caps.ViewRef]uintptr // views whose drop waits for data, by context
	atom   uintptr                  // GdkAtom text/uri-list
	getter func(widget, ctx, target uintptr, time uint32)
}

var (
	drops                                  dropState
	cbDragData, cbDragDrop, cbDragLeaveCbk uintptr
)

func (a *App) initDrops() {
	drops.once.Do(func() {
		drops.stash = map[caps.ViewRef]*dropStash{}
		drops.wait = map[caps.ViewRef]uintptr{}
		cbDragData = purego.NewCallback(dragDataReceivedCallback)
		cbDragDrop = purego.NewCallback(dragDropCallback)
		cbDragLeaveCbk = purego.NewCallback(dragLeaveCallback)
		if a.x != nil && a.x.gdkAtomIntern != nil {
			drops.atom = a.x.gdkAtomIntern("text/uri-list", 0)
		}
		if h, err := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal); err == nil {
			if _, err := purego.Dlsym(h, "gtk_drag_get_data"); err == nil {
				purego.RegisterLibFunc(&drops.getter, h, "gtk_drag_get_data")
			}
		}
	})
}

// connectDrops watches the drag signals of a view's widget. The user data is
// the view ref. It may be called for any GtkWidget; the handlers never
// consume a signal.
func (a *App) connectDrops(widget uintptr, ref caps.ViewRef) {
	if a.x == nil || a.x.gtkSelGetUris == nil || widget == 0 {
		return
	}
	a.initDrops()
	if drops.atom == 0 {
		return
	}
	a.api.gSignalConnectData(widget, "drag-data-received", cbDragData, uintptr(ref), 0, 0)
	a.api.gSignalConnectData(widget, "drag-drop", cbDragDrop, uintptr(ref), 0, 0)
	a.api.gSignalConnectData(widget, "drag-leave", cbDragLeaveCbk, uintptr(ref), 0, 0)
}

// selectionPaths returns the local files in a GtkSelectionData that holds a
// text/uri-list.
func (a *App) selectionPaths(sd uintptr) []string {
	if sd == 0 {
		return nil
	}
	arr := a.x.gtkSelGetUris(sd)
	if arr == 0 {
		return nil
	}
	var uris []string
	for p := arr; ; p += unsafe.Sizeof(uintptr(0)) {
		s := *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&p)))
		if s == 0 {
			break
		}
		uris = append(uris, goString(s))
	}
	a.x.gStrfreev(arr)
	return caps.PathsFromURIs(uris)
}

func (a *App) emitDrop(ref caps.ViewRef, paths []string, x, y int32) {
	v := a.viewByRef(uintptr(ref))
	if v == nil || len(paths) == 0 {
		return
	}
	a.mu.Lock()
	fn := a.dropFn
	a.mu.Unlock()
	if fn != nil {
		fn(caps.FileDrop{View: v.ref, Window: v.w.Ref(), Paths: paths, X: int(x), Y: int(y)})
	}
}

// dragDataReceivedCallback: drag-data-received(widget, ctx, x, y, data, info, time).
func dragDataReceivedCallback(_, ctx, x, y, sd, _, _, ref uintptr) uintptr {
	a := theApp
	paths := a.selectionPaths(sd)
	if len(paths) == 0 {
		return 0
	}
	r := caps.ViewRef(ref)
	if waiting, ok := drops.wait[r]; ok && waiting == ctx {
		delete(drops.wait, r)
		a.emitDrop(r, paths, int32(x), int32(y))
		return 0
	}
	drops.stash[r] = &dropStash{ctx: ctx, paths: paths, x: int32(x), y: int32(y)}
	return 0
}

// dragDropCallback: drag-drop(widget, ctx, x, y, time). It returns FALSE so
// that WebKit handles the drop as usual.
func dragDropCallback(widget, ctx, x, y, tm, ref uintptr) uintptr {
	a := theApp
	r := caps.ViewRef(ref)
	if s := drops.stash[r]; s != nil && s.ctx == ctx {
		delete(drops.stash, r)
		a.emitDrop(r, s.paths, int32(x), int32(y))
		return 0
	}
	delete(drops.stash, r)
	// WebKit did not ask for the file list while the drag hovered, or this is
	// a different drag: ask for it, the answer arrives as drag-data-received.
	if drops.getter != nil {
		drops.wait[r] = ctx
		drops.getter(widget, ctx, drops.atom, uint32(tm))
	}
	return 0
}

// dragLeaveCallback: drag-leave(widget, ctx, time). GTK also emits it just
// before drag-drop, so the stash is forgotten once the main loop is idle.
func dragLeaveCallback(_, ctx, _, ref uintptr) uintptr {
	a := theApp
	r := caps.ViewRef(ref)
	a.Dispatch(func() {
		if s := drops.stash[r]; s != nil && s.ctx == ctx {
			delete(drops.stash, r)
		}
		if drops.wait[r] == ctx {
			delete(drops.wait, r)
		}
	})
	return 0
}
