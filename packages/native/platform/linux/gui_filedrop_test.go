//go:build linux

package linux

import (
	"reflect"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// dropRig connects the drop watcher to a plain GtkBox, which has no drag
// handlers of its own, and drives the GTK drag signals by hand. Emitting them
// on a real WebKitWebView would run WebKit's own handlers with a context that
// does not exist, so the real view is covered by the XDND test.
type dropRig struct {
	t      *testing.T
	widget uintptr
	ref    caps.ViewRef
	got    chan caps.FileDrop
	asked  []uintptr // contexts the watcher requested data for
	ctxs   map[uintptr]uintptr
	newObj func(typ uintptr, first uintptr) uintptr
	ctxTyp func() uintptr

	emitData func(inst uintptr, sig string, ctx uintptr, x, y int32, sd uintptr, info, tm uint32)
	emitDrop func(inst uintptr, sig string, ctx uintptr, x, y int32, tm uint32, ret *int32)
	emitLeav func(inst uintptr, sig string, ctx uintptr, tm uint32)
}

func newDropRig(t *testing.T) *dropRig {
	t.Helper()
	needGUI(t)
	if guiApp.x.gtkSelGetUris == nil || guiApp.x.gtkSelSet == nil {
		t.Skip("GTK selection data functions are missing")
	}
	w := ext_newWin(t, "drop")
	r := &dropRig{t: t, ref: w.pv.ref, got: make(chan caps.FileDrop, 8), ctxs: map[uintptr]uintptr{}}
	gobj, err := purego.Dlopen("libgobject-2.0.so.0", rtldNow|rtldGlobal)
	if err != nil {
		t.Fatal(err)
	}
	purego.RegisterLibFunc(&r.emitData, gobj, "g_signal_emit_by_name")
	purego.RegisterLibFunc(&r.emitDrop, gobj, "g_signal_emit_by_name")
	purego.RegisterLibFunc(&r.emitLeav, gobj, "g_signal_emit_by_name")
	purego.RegisterLibFunc(&r.newObj, gobj, "g_object_new")
	gdk, err := purego.Dlopen("libgdk-3.so.0", rtldNow|rtldGlobal)
	if err != nil {
		t.Fatal(err)
	}
	purego.RegisterLibFunc(&r.ctxTyp, gdk, "gdk_drag_context_get_type")
	guiApp.OnFileDrop(func(d caps.FileDrop) { r.got <- d })
	t.Cleanup(func() { guiApp.OnFileDrop(nil) })
	guiApp.callMain(func() {
		r.widget = guiApp.x.gtkBoxNew(0, 0)
		guiApp.initDrops()
		guiApp.connectDrops(r.widget, r.ref)
		// The real gtk_drag_get_data needs a live drag context.
		old := drops.getter
		drops.getter = func(_, ctx, _ uintptr, _ uint32) { r.asked = append(r.asked, ctx) }
		t.Cleanup(func() { drops.getter = old })
	})
	t.Cleanup(func() {
		guiApp.callMain(func() {
			for _, o := range r.ctxs {
				guiApp.api.gObjectUnref(o)
			}
			delete(drops.stash, r.ref)
			delete(drops.wait, r.ref)
			guiApp.api.gtkWidgetDestroy(r.widget)
		})
	})
	return r
}

// ctx returns the GdkDragContext standing for drag n. Signal emission refs its
// object arguments, so they have to be real objects.
func (r *dropRig) ctx(n uintptr) uintptr {
	if o, ok := r.ctxs[n]; ok {
		return o
	}
	guiApp.callMain(func() { r.ctxs[n] = r.newObj(r.ctxTyp(), 0) })
	if r.ctxs[n] == 0 {
		r.t.Fatal("cannot create a GdkDragContext")
	}
	return r.ctxs[n]
}

// uriList builds a GtkSelectionData holding a text/uri-list. The memory is
// kept alive by the returned slice.
func (r *dropRig) uriList(body string) (sd uintptr, keep []byte) {
	keep = make([]byte, 64) // struct _GtkSelectionData is seven words
	sd = uintptr(unsafe.Pointer(&keep[0]))
	data := []byte(body)
	var p unsafe.Pointer
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	guiApp.x.gtkSelSet(sd, drops.atom, 8, p, int32(len(data)))
	// The display is the last field; GTK reads text through it.
	*(*uintptr)(unsafe.Pointer(&keep[48])) = guiApp.x.gdkDisplayDefault()
	runtime.KeepAlive(data)
	return sd, keep
}

func (r *dropRig) hover(n uintptr, x, y int32, body string) {
	ctx := r.ctx(n)
	sd, keep := r.uriList(body)
	guiApp.callMain(func() { r.emitData(r.widget, "drag-data-received", ctx, x, y, sd, 0, 0) })
	runtime.KeepAlive(keep)
}

func (r *dropRig) leave(n uintptr) {
	ctx := r.ctx(n)
	guiApp.callMain(func() { r.emitLeav(r.widget, "drag-leave", ctx, 0) })
}

func (r *dropRig) drop(n uintptr, x, y int32) (handled int32) {
	ctx := r.ctx(n)
	guiApp.callMain(func() { r.emitDrop(r.widget, "drag-drop", ctx, x, y, 0, &handled) })
	return handled
}

func (r *dropRig) expectDrop(want caps.FileDrop) {
	r.t.Helper()
	select {
	case d := <-r.got:
		if !reflect.DeepEqual(d, want) {
			r.t.Fatalf("drop = %+v, want %+v", d, want)
		}
	case <-time.After(5 * time.Second):
		r.t.Fatalf("no drop; wanted %+v", want)
	}
}

func (r *dropRig) expectNothing() {
	r.t.Helper()
	select {
	case d := <-r.got:
		r.t.Fatalf("unexpected drop %+v", d)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestGUIFileDropHoverThenDrop(t *testing.T) {
	r := newDropRig(t)
	win := guiApp.viewByRef(uintptr(r.ref)).w.Ref()
	// WebKit asks for the file list while the drag hovers; GTK emits
	// drag-leave and then drag-drop when the button is released.
	r.hover(0x1001, 3, 4, "file:///tmp/a.txt\r\nfile:///tmp/b%20c.md\r\n")
	r.expectNothing()
	c1 := r.ctx(0x1001)
	guiApp.callMain(func() {
		r.emitLeav(r.widget, "drag-leave", c1, 0)
		var handled int32
		r.emitDrop(r.widget, "drag-drop", c1, 30, 40, 0, &handled)
		if handled != 0 {
			t.Errorf("the watcher consumed drag-drop (returned %d); WebKit must still see it", handled)
		}
	})
	r.expectDrop(caps.FileDrop{View: r.ref, Window: win, Paths: []string{"/tmp/a.txt", "/tmp/b c.md"}, X: 30, Y: 40})
	if len(r.asked) != 0 {
		t.Errorf("asked the source for data it already had: %v", r.asked)
	}
	// The deferred forget of the leave must not break the next drag.
	waitFor(t, "an idle round", func() bool { guiApp.callMain(func() {}); return true })
	r.hover(0x1002, 1, 1, "file:///tmp/d.txt\r\n")
	r.drop(0x1002, 5, 6)
	r.expectDrop(caps.FileDrop{View: r.ref, Window: win, Paths: []string{"/tmp/d.txt"}, X: 5, Y: 6})
}

func TestGUIFileDropWithoutHoverData(t *testing.T) {
	r := newDropRig(t)
	win := guiApp.viewByRef(uintptr(r.ref)).w.Ref()
	// No data arrived while hovering: the watcher requests it at the drop and
	// reports the drop when the data comes.
	r.drop(0x2001, 7, 8)
	r.expectNothing()
	if !reflect.DeepEqual(r.asked, []uintptr{r.ctx(0x2001)}) {
		t.Fatalf("data requests = %v", r.asked)
	}
	r.hover(0x2001, 7, 8, "file:///tmp/late.txt")
	r.expectDrop(caps.FileDrop{View: r.ref, Window: win, Paths: []string{"/tmp/late.txt"}, X: 7, Y: 8})
}

func TestGUIFileDropForgetsAHoverThatLeft(t *testing.T) {
	r := newDropRig(t)
	r.hover(0x3001, 0, 0, "file:///tmp/old.txt")
	r.leave(0x3001)
	waitFor(t, "the deferred forget", func() bool {
		var gone bool
		guiApp.callMain(func() { gone = drops.stash[r.ref] == nil })
		return gone
	})
	// A different drag with the same context address must not report old files.
	r.drop(0x3001, 1, 1)
	r.expectNothing()
	if len(r.asked) != 1 {
		t.Fatalf("data requests = %v", r.asked)
	}
}

func TestGUIFileDropIgnoresWhatIsNotAFile(t *testing.T) {
	r := newDropRig(t)
	r.hover(0x4001, 0, 0, "https://example.com/a.png\r\nfile://otherhost/tmp/x\r\n")
	r.drop(0x4001, 1, 1)
	r.expectNothing()
	// An empty list (a text drag) is not a drop either.
	r.hover(0x4002, 0, 0, "")
	r.drop(0x4002, 1, 1)
	r.expectNothing()
}

func TestGUIFileDropWatchesTheRealView(t *testing.T) {
	needGUI(t)
	w := ext_newWin(t, "dropwatch")
	var connected bool
	guiApp.callMain(func() {
		gtk, _ := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal)
		gobj, _ := purego.Dlopen("libgobject-2.0.so.0", rtldNow|rtldGlobal)
		var widgetType func() uintptr
		var lookup func(name string, typ uintptr) uint32
		var pending func(inst uintptr, id uint32, detail uint32, mayBeBlocked int32) int32
		purego.RegisterLibFunc(&widgetType, gtk, "gtk_widget_get_type")
		purego.RegisterLibFunc(&lookup, gobj, "g_signal_lookup")
		purego.RegisterLibFunc(&pending, gobj, "g_signal_has_handler_pending")
		connected = true
		for _, sig := range []string{"drag-data-received", "drag-drop", "drag-leave"} {
			id := lookup(sig, widgetType())
			if id == 0 || pending(w.pv.widget, id, 0, 1) == 0 {
				connected = false
				t.Errorf("no handler connected to %s on the primary view", sig)
			}
		}
	})
	if !connected {
		t.Fatal("the file drop watcher is not connected to the primary view")
	}
}
