//go:build linux

package linux

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.PageControls = (*Window)(nil)
	_ caps.PageEvents   = (*App)(nil)
)

// pageAPI holds the optional bindings behind print, find, zoom and capture.
// A symbol that is missing leaves its field nil and the operation answers
// krate.ErrUnsupported.
type pageAPI struct {
	// zoom
	getZoom func(v uintptr) float64
	setZoom func(v uintptr, z float64)

	// find
	findController func(v uintptr) uintptr
	findSearch     func(fc uintptr, text string, opts, max uint32)
	findNext       func(fc uintptr)
	findPrev       func(fc uintptr)
	findFinish     func(fc uintptr)

	// snapshot
	snapshot       func(v uintptr, region, opts int32, cancel, cb, data uintptr)
	snapshotFinish func(v, res uintptr, gerr *uintptr) uintptr
	cairoMap       func(surface uintptr, extents uintptr) uintptr
	cairoUnmap     func(surface, image uintptr)
	cairoDestroy   func(surface uintptr)
	cairoWidth     func(s uintptr) int32
	cairoHeight    func(s uintptr) int32
	cairoStride    func(s uintptr) int32
	cairoData      func(s uintptr) uintptr
	cairoFormat    func(s uintptr) int32
	cairoFlush     func(s uintptr)

	// print
	printNew       func(v uintptr) uintptr
	printSetting   func(op, settings uintptr)
	printSetSetup  func(op, setup uintptr)
	printPrint     func(op uintptr)
	printDialog    func(op, parent uintptr) int32
	printSettings  func() uintptr
	printSet       func(s uintptr, key, value string)
	settingsPrintB func(s uintptr, on int32)
	pageSetupNew   func() uintptr
	pageSetOrient  func(s uintptr, o int32)
	pageSetPaper   func(s, paper uintptr)
	pageSetTop     func(s uintptr, v float64, unit int32)
	pageSetBottom  func(s uintptr, v float64, unit int32)
	pageSetLeft    func(s uintptr, v float64, unit int32)
	pageSetRight   func(s uintptr, v float64, unit int32)
	paperNewCustom func(name, display string, w, h float64, unit int32) uintptr
	paperFree      func(p uintptr)
	unref          func(p uintptr)
	toplevels      func() uintptr
	glistFree      func(l uintptr)
	wkSettingsPB   func(s uintptr, on int32)
	wkSettingsGPB  func(s uintptr) int32
}

func loadPageAPI() *pageAPI {
	p := &pageAPI{}
	b := &binder{}
	open := func(names ...string) uintptr {
		for _, n := range names {
			if h, err := purego.Dlopen(n, rtldNow|rtldGlobal); err == nil {
				return h
			}
		}
		return 0
	}
	bind := func(lib uintptr, fn any, sym string) {
		if lib != 0 {
			b.bind(fn, lib, sym)
		}
	}
	wk := open("libwebkit2gtk-4.1.so.0")
	gtk := open("libgtk-3.so.0")
	cairo := open("libcairo.so.2")
	glib := open("libglib-2.0.so.0")
	gobj := open("libgobject-2.0.so.0")

	bind(wk, &p.getZoom, "webkit_web_view_get_zoom_level")
	bind(wk, &p.setZoom, "webkit_web_view_set_zoom_level")
	bind(wk, &p.findController, "webkit_web_view_get_find_controller")
	bind(wk, &p.findSearch, "webkit_find_controller_search")
	bind(wk, &p.findNext, "webkit_find_controller_search_next")
	bind(wk, &p.findPrev, "webkit_find_controller_search_previous")
	bind(wk, &p.findFinish, "webkit_find_controller_search_finish")
	bind(wk, &p.snapshot, "webkit_web_view_get_snapshot")
	bind(wk, &p.snapshotFinish, "webkit_web_view_get_snapshot_finish")
	bind(wk, &p.printNew, "webkit_print_operation_new")
	bind(wk, &p.printSetting, "webkit_print_operation_set_print_settings")
	bind(wk, &p.printSetSetup, "webkit_print_operation_set_page_setup")
	bind(wk, &p.printPrint, "webkit_print_operation_print")
	bind(wk, &p.printDialog, "webkit_print_operation_run_dialog")
	bind(wk, &p.wkSettingsPB, "webkit_settings_set_print_backgrounds")
	bind(wk, &p.wkSettingsGPB, "webkit_settings_get_print_backgrounds")
	bind(gtk, &p.printSettings, "gtk_print_settings_new")
	bind(gtk, &p.printSet, "gtk_print_settings_set")
	bind(gtk, &p.pageSetupNew, "gtk_page_setup_new")
	bind(gtk, &p.pageSetOrient, "gtk_page_setup_set_orientation")
	bind(gtk, &p.pageSetPaper, "gtk_page_setup_set_paper_size")
	bind(gtk, &p.pageSetTop, "gtk_page_setup_set_top_margin")
	bind(gtk, &p.pageSetBottom, "gtk_page_setup_set_bottom_margin")
	bind(gtk, &p.pageSetLeft, "gtk_page_setup_set_left_margin")
	bind(gtk, &p.pageSetRight, "gtk_page_setup_set_right_margin")
	bind(gtk, &p.paperNewCustom, "gtk_paper_size_new_custom")
	bind(gtk, &p.paperFree, "gtk_paper_size_free")
	bind(gtk, &p.toplevels, "gtk_window_list_toplevels")
	bind(glib, &p.glistFree, "g_list_free")
	bind(gobj, &p.unref, "g_object_unref")
	bind(cairo, &p.cairoMap, "cairo_surface_map_to_image")
	bind(cairo, &p.cairoUnmap, "cairo_surface_unmap_image")
	bind(cairo, &p.cairoDestroy, "cairo_surface_destroy")
	bind(cairo, &p.cairoWidth, "cairo_image_surface_get_width")
	bind(cairo, &p.cairoHeight, "cairo_image_surface_get_height")
	bind(cairo, &p.cairoStride, "cairo_image_surface_get_stride")
	bind(cairo, &p.cairoData, "cairo_image_surface_get_data")
	bind(cairo, &p.cairoFormat, "cairo_image_surface_get_format")
	bind(cairo, &p.cairoFlush, "cairo_surface_flush")
	return p
}

// Callbacks of the page features; created once in initPage.
var (
	cbFound, cbFindFailed, cbKey, cbAsync, cbPrintDone, cbPrintFail uintptr
)

func (a *App) initPage() {
	a.pg = loadPageAPI()
	a.asyncCbs = map[uintptr]func(src, res uintptr){}
	a.printJobs = map[uintptr]*printJob{}
	cbFound = purego.NewCallback(foundCallback)
	cbFindFailed = purego.NewCallback(findFailedCallback)
	cbKey = purego.NewCallback(keyCallback)
	cbAsync = purego.NewCallback(asyncCallback)
	cbPrintDone = purego.NewCallback(printDoneCallback)
	cbPrintFail = purego.NewCallback(printFailCallback)
}

func pageUnsupported(what string) error {
	return fmt.Errorf("%w: %s is not available in this WebKitGTK", krate.ErrUnsupported, what)
}

// asyncCallback completes a GAsyncReadyCallback registered with async.
func asyncCallback(src, res, tok uintptr) uintptr {
	a := theApp
	fn := a.asyncCbs[tok]
	delete(a.asyncCbs, tok)
	if fn != nil {
		runGuarded(func() { fn(src, res) })
	}
	return 0
}

// async registers fn for a GAsyncReadyCallback and returns the callback and
// user data to pass. Main thread.
func (a *App) async(fn func(src, res uintptr)) (cb, data uintptr) {
	a.nextTok++
	a.asyncCbs[a.nextTok] = fn
	return cbAsync, a.nextTok
}

// liveView returns the view with a webview, or an error. Main thread.
func (w *Window) liveView(ref caps.ViewRef) (*view, error) {
	v := w.viewByRef(ref)
	switch {
	case !w.alive || v == nil:
		return nil, fmt.Errorf("krate: view %d is not in this window", ref)
	case v.widget == 0:
		return nil, errReleased
	}
	return v, nil
}

// ZoomFactor returns the zoom of a view.
func (w *Window) ZoomFactor(ref caps.ViewRef) (z float64, err error) {
	if !w.app.callMain(func() {
		var v *view
		if v, err = w.liveView(ref); err != nil {
			return
		}
		if w.app.pg.getZoom == nil {
			err = pageUnsupported("page zoom")
			return
		}
		z = w.app.pg.getZoom(v.widget)
	}) {
		return 0, errors.New("krate: application is not running")
	}
	return
}

// SetZoomFactor sets the zoom of a view.
func (w *Window) SetZoomFactor(ref caps.ViewRef, f float64) (err error) {
	if f != f || f < caps.MinZoom || f > caps.MaxZoom {
		return krate.Errorf("bad_args", "zoom factor %v must be between %v and %v", f, caps.MinZoom, caps.MaxZoom)
	}
	if !w.app.callMain(func() {
		var v *view
		if v, err = w.liveView(ref); err != nil {
			return
		}
		if w.app.pg.setZoom == nil {
			err = pageUnsupported("page zoom")
			return
		}
		v.zoom = f
		w.app.pg.setZoom(v.widget, f)
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// keyCallback implements Ctrl plus, minus and 0 for a view.
func keyCallback(widget, ev, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil || v.widget == 0 || ev == 0 || a.pg.setZoom == nil {
		return 0
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&ev))
	state := *(*uint32)(unsafe.Add(base, 24))
	keyval := *(*uint32)(unsafe.Add(base, 28))
	const (
		shift   = 1 << 0
		control = 1 << 2
		mod1    = 1 << 3
		super   = 1 << 26
		meta    = 1 << 28
	)
	if state&control == 0 || state&(mod1|super|meta) != 0 {
		return 0
	}
	cur := a.pg.getZoom(v.widget)
	var next float64
	switch keyval {
	case 0x2b, 0x3d, 0xffab: // plus, equal, KP_Add
		next = caps.NextZoom(cur, true)
	case 0x2d, 0x5f, 0xffad: // minus, underscore, KP_Subtract
		next = caps.NextZoom(cur, false)
	case 0x30, 0xffb0, 0xff9e: // 0, KP_0, KP_Insert (KP_0 without NumLock)
		next = 1
	default:
		return 0
	}
	_ = shift
	v.zoom = next
	a.pg.setZoom(v.widget, next)
	return 1
}

// Find state and callbacks.

func (a *App) connectPage(v *view) {
	a.api.gSignalConnectData(v.widget, "key-press-event", cbKey, uintptr(v.ref), 0, 0)
	v.findConnected = false
}

// findControllerOf returns the find controller of a view, connecting its
// signals on first use. Creating it earlier, while the view is made, changes
// when the first page loads relative to the pane layout, so it waits for the
// first search. Main thread.
func (a *App) findControllerOf(v *view) uintptr {
	p := a.pg
	if p.findController == nil || p.findSearch == nil {
		return 0
	}
	fc := p.findController(v.widget)
	if fc != 0 && !v.findConnected {
		a.api.gSignalConnectData(fc, "found-text", cbFound, uintptr(v.ref), 0, 0)
		a.api.gSignalConnectData(fc, "failed-to-find-text", cbFindFailed, uintptr(v.ref), 0, 0)
		v.findConnected = true
	}
	return fc
}

// OnFind sets the handler for find results.
func (a *App) OnFind(fn func(caps.FindEvent)) {
	a.mu.Lock()
	a.findHandler = fn
	a.mu.Unlock()
}

func (a *App) emitFind(e caps.FindEvent) {
	a.mu.Lock()
	fn := a.findHandler
	a.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

func foundCallback(fc, count, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil {
		return 0
	}
	active := v.find.Report(int(uint32(count)))
	a.emitFind(caps.FindEvent{View: v.ref, Text: v.find.Text(), Matches: v.find.Total(), Active: active, Final: true})
	return 0
}

func findFailedCallback(fc, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil {
		return 0
	}
	text := v.find.Text()
	v.find.Report(0)
	a.emitFind(caps.FindEvent{View: v.ref, Text: text, Matches: 0, Final: true})
	return 0
}

const (
	findCaseInsensitive = 1 << 0
	findBackwards       = 1 << 3
	findWrap            = 1 << 4
)

// Find searches for text in a view.
func (w *Window) Find(ref caps.ViewRef, text string, o caps.FindOptions) (err error) {
	if text == "" {
		return w.StopFind(ref)
	}
	if !w.app.callMain(func() {
		var v *view
		if v, err = w.liveView(ref); err != nil {
			return
		}
		p := w.app.pg
		fc := w.app.findControllerOf(v)
		if fc == 0 {
			err = pageUnsupported("find in page")
			return
		}
		var flags uint32
		if !o.MatchCase {
			flags |= findCaseInsensitive
		}
		if o.Backward {
			flags |= findBackwards
		}
		if !o.NoWrap {
			flags |= findWrap
		}
		if v.find.Start(text, o) {
			if o.Backward {
				p.findPrev(fc)
			} else {
				p.findNext(fc)
			}
			// WebKit answers a continued search with found-text too.
			return
		}
		p.findSearch(fc, text, flags, 0xffffffff)
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// StopFind clears the highlight.
func (w *Window) StopFind(ref caps.ViewRef) (err error) {
	if !w.app.callMain(func() {
		var v *view
		if v, err = w.liveView(ref); err != nil {
			return
		}
		p := w.app.pg
		fc := w.app.findControllerOf(v)
		if fc == 0 {
			err = pageUnsupported("find in page")
			return
		}
		p.findFinish(fc)
		v.find.Reset()
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// Capture takes a PNG of the visible area of a view.
func (w *Window) Capture(ref caps.ViewRef, _ caps.CaptureOptions) ([]byte, error) {
	type result struct {
		png []byte
		err error
	}
	ch := make(chan result, 1)
	var startErr error
	if !w.app.callMain(func() {
		v, err := w.liveView(ref)
		if err != nil {
			startErr = err
			return
		}
		p := w.app.pg
		if p.snapshot == nil || p.cairoMap == nil || p.cairoData == nil {
			startErr = pageUnsupported("view capture")
			return
		}
		cb, data := w.app.async(func(src, res uintptr) {
			png, err := w.app.snapshotPNG(src, res)
			ch <- result{png, err}
		})
		// WEBKIT_SNAPSHOT_REGION_VISIBLE, WEBKIT_SNAPSHOT_OPTIONS_NONE
		p.snapshot(v.widget, 0, 0, 0, cb, data)
	}) {
		return nil, errors.New("krate: application is not running")
	}
	if startErr != nil {
		return nil, startErr
	}
	select {
	case r := <-ch:
		return r.png, r.err
	case <-time.After(20 * time.Second):
		return nil, errors.New("krate: the view did not produce a snapshot in time")
	}
}

func (a *App) snapshotPNG(view, res uintptr) ([]byte, error) {
	p := a.pg
	var gerr uintptr
	surface := p.snapshotFinish(view, res, &gerr)
	if surface == 0 {
		msg := gerrMessage(gerr)
		if gerr != 0 {
			a.api.gErrorFree(gerr)
		}
		return nil, fmt.Errorf("krate: snapshot failed: %s", msg)
	}
	defer p.cairoDestroy(surface)
	img := p.cairoMap(surface, 0)
	if img == 0 {
		return nil, errors.New("krate: the snapshot surface cannot be read")
	}
	defer p.cairoUnmap(surface, img)
	p.cairoFlush(img)
	w, h, stride := int(p.cairoWidth(img)), int(p.cairoHeight(img)), int(p.cairoStride(img))
	dp := p.cairoData(img)
	if w <= 0 || h <= 0 || dp == 0 {
		return nil, errors.New("krate: the snapshot is empty")
	}
	data := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&dp))), stride*h)
	// CAIRO_FORMAT_ARGB32 is 0, CAIRO_FORMAT_RGB24 is 1.
	format := p.cairoFormat(img)
	return caps.EncodePNGFromBGRA(w, h, stride, data, format == 0, format == 1)
}

// Printing.

type printJob struct {
	done chan error
	path string
}

func printDoneCallback(op, id uintptr) uintptr {
	a := theApp
	if j := a.printJobs[id]; j != nil {
		delete(a.printJobs, id)
		j.done <- nil
	}
	return 0
}

func printFailCallback(op, gerr, id uintptr) uintptr {
	a := theApp
	if j := a.printJobs[id]; j != nil {
		delete(a.printJobs, id)
		msg := gerrMessage(gerr)
		if strings.Contains(msg, "Printer not found") {
			msg += " (GTK needs its file print backend, libprintbackend-file, to print to a PDF)"
		}
		j.done <- fmt.Errorf("krate: printing failed: %s", msg)
	}
	return 0
}

// PrintPDF renders a view to a PDF.
func (w *Window) PrintPDF(ref caps.ViewRef, o caps.PDFOptions) ([]byte, error) {
	o, err := o.Resolve()
	if err != nil {
		return nil, krate.Errorf("bad_args", "%v", err)
	}
	f, err := os.CreateTemp("", "krate-print-*.pdf")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	job := &printJob{done: make(chan error, 1), path: path}
	var startErr error
	var restore func()
	if !w.app.callMain(func() {
		v, err := w.liveView(ref)
		if err != nil {
			startErr = err
			return
		}
		a := w.app
		p := a.pg
		if p.printNew == nil || p.printSettings == nil || p.pageSetupNew == nil || p.paperNewCustom == nil {
			startErr = pageUnsupported("printing")
			return
		}
		op := p.printNew(v.widget)
		if op == 0 {
			startErr = errors.New("krate: webkit_print_operation_new failed")
			return
		}
		settings := p.printSettings()
		p.printSet(settings, "printer", "Print to File")
		p.printSet(settings, "output-file-format", "pdf")
		p.printSet(settings, "output-uri", (&url.URL{Scheme: "file", Path: path}).String())
		setup := p.pageSetupNew()
		paper := p.paperNewCustom("krate", "Krate", o.PageWidth, o.PageHeight, 2) // GTK_UNIT_INCH
		p.pageSetPaper(setup, paper)
		p.paperFree(paper)
		if o.Landscape {
			p.pageSetOrient(setup, 1)
		}
		m := o.Margins
		p.pageSetTop(setup, m.Top, 2)
		p.pageSetBottom(setup, m.Bottom, 2)
		p.pageSetLeft(setup, m.Left, 2)
		p.pageSetRight(setup, m.Right, 2)
		p.printSetting(op, settings)
		p.printSetSetup(op, setup)
		// Backgrounds are a setting of the view; put it back afterwards.
		if p.wkSettingsPB != nil {
			ws := a.api.wkViewGetSettings(v.widget)
			prev := p.wkSettingsGPB(ws)
			p.wkSettingsPB(ws, b2i(o.PrintBackground))
			restore = func() { p.wkSettingsPB(ws, prev) }
		}
		a.nextTok++
		id := a.nextTok
		a.printJobs[id] = job
		a.api.gSignalConnectData(op, "finished", cbPrintDone, id, 0, 0)
		a.api.gSignalConnectData(op, "failed", cbPrintFail, id, 0, 0)
		p.printPrint(op)
		// The operation owns itself while it runs; drop our references.
		p.unref(settings)
		p.unref(setup)
	}) {
		return nil, errors.New("krate: application is not running")
	}
	if startErr != nil {
		return nil, startErr
	}
	defer func() {
		if restore != nil {
			w.app.Dispatch(restore)
		}
	}()
	select {
	case err := <-job.done:
		if err != nil {
			return nil, err
		}
	case <-time.After(60 * time.Second):
		return nil, errors.New("krate: printing did not finish in time")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 5 || string(b[:4]) != "%PDF" {
		return nil, errors.New("krate: the print job produced no PDF")
	}
	return b, nil
}

// PrintDialog opens the print dialog of a view. It returns once the dialog
// is requested; the dialog runs on the main loop.
func (w *Window) PrintDialog(ref caps.ViewRef) (err error) {
	if !w.app.callMain(func() {
		var v *view
		if v, err = w.liveView(ref); err != nil {
			return
		}
		a := w.app
		if a.pg.printNew == nil || a.pg.printDialog == nil {
			err = pageUnsupported("printing")
			return
		}
		widget, win := v.widget, w.win
		// run_dialog runs its own loop until the dialog closes, so it must
		// not run inside this call.
		a.Dispatch(func() {
			if !w.alive || w.viewByRef(ref) == nil || w.viewByRef(ref).widget != widget {
				return
			}
			op := a.pg.printNew(widget)
			if op == 0 {
				return
			}
			a.pg.printDialog(op, win)
			a.pg.unref(op)
		})
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// topLevelTitles lists the titles of the GTK top-level windows. For tests.
func (a *App) topLevelTitles() (titles []string, wins []uintptr) {
	a.callMain(func() {
		if a.pg.toplevels == nil {
			return
		}
		l := a.pg.toplevels()
		for _, p := range a.slistPtrs0(l) {
			titles = append(titles, goString(a.api.gtkWindowGetTitle(p)))
			wins = append(wins, p)
		}
		if a.pg.glistFree != nil {
			a.pg.glistFree(l)
		}
	})
	return
}

// slistPtrs0 reads a GList (or GSList: both start with data and next) without
// freeing it.
func (a *App) slistPtrs0(list uintptr) []uintptr {
	var out []uintptr
	for n := list; n != 0; {
		node := *(*unsafe.Pointer)(unsafe.Pointer(&n))
		out = append(out, *(*uintptr)(node))
		n = *(*uintptr)(unsafe.Add(node, 8))
	}
	return out
}
