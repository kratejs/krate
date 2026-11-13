//go:build linux

package linux

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Page snapshots and offscreen windows. Everything the offscreen area needs
// from WebKitGTK is in this file: webkit_web_view_get_snapshot (async, returns
// a cairo image surface) and gtk_offscreen_window_new (a toplevel that renders
// into memory and is never mapped on screen).

var (
	_ caps.ViewSnapshots    = (*App)(nil)
	_ caps.OffscreenWindows = (*App)(nil)
)

const (
	snapshotRegionVisible      = 0 // WEBKIT_SNAPSHOT_REGION_VISIBLE
	snapshotRegionFullDocument = 1 // WEBKIT_SNAPSHOT_REGION_FULL_DOCUMENT
	snapshotOptionsNone        = 0 // WEBKIT_SNAPSHOT_OPTIONS_NONE

	cairoFormatARGB32 = 0
)

type snapAPI struct {
	getSnapshot       func(view uintptr, region, options int32, cancellable, cb, data uintptr)
	getSnapshotFinish func(view, res uintptr, gerr *uintptr) uintptr

	offscreenNew func() uintptr

	cairoWidth   func(s uintptr) int32
	cairoHeight  func(s uintptr) int32
	cairoStride  func(s uintptr) int32
	cairoData    func(s uintptr) uintptr
	cairoFormat  func(s uintptr) int32
	cairoFlush   func(s uintptr)
	cairoDestroy func(s uintptr)
	cairoStatus  func(s uintptr) int32
}

var (
	snapOnce sync.Once
	snapLib  *snapAPI
	snapErr  error

	snapCb uintptr

	snapMu   sync.Mutex
	snapNext uintptr
	snapReqs = map[uintptr]*snapReq{}
)

type snapReq struct {
	view uintptr
	done chan snapResult
}

type snapResult struct {
	snap caps.Snapshot
	err  error
}

func loadSnap(a *App) (*snapAPI, error) {
	snapOnce.Do(func() {
		wk, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
		if err != nil {
			snapErr = err
			return
		}
		cairo, err := purego.Dlopen("libcairo.so.2", rtldNow|rtldGlobal)
		if err != nil {
			snapErr = fmt.Errorf("krate: page snapshots need libcairo: %w", err)
			return
		}
		gtk, _ := purego.Dlopen("libgtk-3.so.0", rtldNow|rtldGlobal)
		s := &snapAPI{}
		b := &binder{}
		b.bind(&s.getSnapshot, wk, "webkit_web_view_get_snapshot")
		b.bind(&s.getSnapshotFinish, wk, "webkit_web_view_get_snapshot_finish")
		b.bind(&s.cairoWidth, cairo, "cairo_image_surface_get_width")
		b.bind(&s.cairoHeight, cairo, "cairo_image_surface_get_height")
		b.bind(&s.cairoStride, cairo, "cairo_image_surface_get_stride")
		b.bind(&s.cairoData, cairo, "cairo_image_surface_get_data")
		b.bind(&s.cairoFormat, cairo, "cairo_image_surface_get_format")
		b.bind(&s.cairoFlush, cairo, "cairo_surface_flush")
		b.bind(&s.cairoDestroy, cairo, "cairo_surface_destroy")
		b.bind(&s.cairoStatus, cairo, "cairo_surface_status")
		if len(b.missing) > 0 {
			snapErr = fmt.Errorf("krate: this WebKitGTK or cairo lacks %v", b.missing)
			return
		}
		if gtk != 0 {
			ob := &binder{}
			ob.bind(&s.offscreenNew, gtk, "gtk_offscreen_window_new")
		}
		snapCb = purego.NewCallback(snapshotDoneCallback)
		snapLib = s
	})
	return snapLib, snapErr
}

// OffscreenSupported reports whether NewWindow honors Offscreen.
func (a *App) OffscreenSupported() bool {
	s, err := loadSnap(a)
	return err == nil && s.offscreenNew != nil
}

// newToplevel creates the GtkWindow of a window: a normal toplevel, or an
// offscreen one.
func (a *App) newToplevel(offscreen bool) uintptr {
	if offscreen {
		if s, err := loadSnap(a); err == nil && s.offscreenNew != nil {
			return s.offscreenNew()
		}
	}
	return a.api.gtkWindowNew(gtkWindowToplevel)
}

// SnapshotView renders a view to BGRA pixels. It blocks until WebKit delivers
// the frame (up to ten seconds) and may be called from any goroutine.
func (a *App) SnapshotView(ref caps.ViewRef, o caps.SnapshotOptions) (caps.Snapshot, error) {
	s, err := loadSnap(a)
	if err != nil {
		return caps.Snapshot{}, fmt.Errorf("%w: %v", krate.ErrUnsupported, err)
	}
	req := &snapReq{done: make(chan snapResult, 1)}
	var startErr error
	if !a.callMain(func() {
		v := a.viewByRef(uintptr(ref))
		switch {
		case v == nil:
			startErr = fmt.Errorf("krate: view %d does not exist", ref)
		case v.widget == 0:
			startErr = errReleased
		default:
			req.view = v.widget
			snapMu.Lock()
			snapNext++
			id := snapNext
			snapReqs[id] = req
			snapMu.Unlock()
			region := int32(snapshotRegionVisible)
			if o.FullDocument {
				region = snapshotRegionFullDocument
			}
			a.api.gObjectRef(v.widget)
			s.getSnapshot(v.widget, region, snapshotOptionsNone, 0, snapCb, id)
		}
	}) {
		return caps.Snapshot{}, errors.New("krate: application is not running")
	}
	if startErr != nil {
		return caps.Snapshot{}, startErr
	}
	select {
	case r := <-req.done:
		return r.snap, r.err
	case <-time.After(10 * time.Second):
		return caps.Snapshot{}, errors.New("krate: the page snapshot did not arrive within 10 seconds")
	}
}

// snapshotDoneCallback is the GAsyncReadyCallback: (source, result, data).
// It runs on the main thread.
func snapshotDoneCallback(source, res, data uintptr) uintptr {
	snapMu.Lock()
	req := snapReqs[data]
	delete(snapReqs, data)
	snapMu.Unlock()
	if req == nil {
		return 0
	}
	a, s := theApp, snapLib
	defer a.api.gObjectUnref(req.view)
	var gerr uintptr
	surf := s.getSnapshotFinish(source, res, &gerr)
	if surf == 0 {
		msg := "WebKit returned no snapshot"
		if gerr != 0 {
			// GError { GQuark domain; gint code; gchar *message }: the message
			// pointer sits after the 4-byte domain, 4-byte code.
			msg = goString(*(*uintptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&gerr)), 8)))
			a.api.gErrorFree(gerr)
		}
		req.done <- snapResult{err: errors.New("krate: page snapshot failed: " + msg)}
		return 0
	}
	defer s.cairoDestroy(surf)
	if s.cairoStatus(surf) != 0 || s.cairoFormat(surf) != cairoFormatARGB32 {
		req.done <- snapResult{err: errors.New("krate: the page snapshot has an unexpected pixel format")}
		return 0
	}
	s.cairoFlush(surf)
	w, h, stride := int(s.cairoWidth(surf)), int(s.cairoHeight(surf)), int(s.cairoStride(surf))
	p := s.cairoData(surf)
	if p == 0 || w <= 0 || h <= 0 {
		req.done <- snapResult{err: errors.New("krate: the page snapshot is empty")}
		return 0
	}
	pix := make([]byte, stride*h)
	copy(pix, unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), stride*h))
	req.done <- snapResult{snap: caps.Snapshot{Width: w, Height: h, Stride: stride, Pix: pix}}
	return 0
}
