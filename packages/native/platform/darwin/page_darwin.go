//go:build darwin

package darwin

// Print, find, zoom and capture on WKWebView (compile-only until CI runs
// gui_page_test.go on macos-latest).
//
//   - Zoom is WKWebView.pageZoom (macOS 11). Cmd plus, minus and 0 are items of
//     the View menu (buildMenu) that act on the focused web view.
//   - Find is findString:withConfiguration: (macOS 11). WKFindResult only says
//     whether a match exists, so the count comes from the visible text of the
//     page (caps.FindCountScript) and the index of the highlighted match is
//     followed by caps.FindTracker.
//   - Capture is takeSnapshotWithConfiguration:, converted to PNG through
//     NSBitmapImageRep.
//   - PDF output and the print dialog use printOperationWithPrintInfo: (macOS
//     11) run as a sheet on the window; a PDF is an NSPrintSaveJob to a
//     temporary file.

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.PageControls = (*Window)(nil)
	_ caps.PageEvents   = (*App)(nil)
)

func unsupported(what, need string) error {
	return fmt.Errorf("%w: %s needs %s", krate.ErrUnsupported, what, need)
}

// onView runs fn on the main thread with a live view of the window.
func (w *Window) onView(ref caps.ViewRef, fn func(v *view) error) error {
	if !w.app.ready.Load() && !isMainThread() {
		return errors.New("darwin: the app is not running yet; call this after Run's onReady callback")
	}
	var err error
	runOnMain(func() {
		withPool(func() {
			v := w.viewByRef(ref)
			switch {
			case w.closed || v == nil:
				err = fmt.Errorf("darwin: view %d is not in this window", ref)
			case v.web == 0:
				err = errors.New("darwin: the window is released; Show it first")
			default:
				err = fn(v)
			}
		})
	})
	return err
}

// ZoomFactor returns the page zoom of a view.
func (w *Window) ZoomFactor(ref caps.ViewRef) (z float64, err error) {
	err = w.onView(ref, func(v *view) error {
		if !respondsTo(v.web, sel("pageZoom")) {
			return unsupported("page zoom", "macOS 11")
		}
		z = objc.Send[float64](v.web, sel("pageZoom"))
		return nil
	})
	return
}

// SetZoomFactor sets the page zoom of a view.
func (w *Window) SetZoomFactor(ref caps.ViewRef, f float64) error {
	if f != f || f < caps.MinZoom || f > caps.MaxZoom {
		return krate.Errorf("bad_args", "zoom factor %v must be between %v and %v", f, caps.MinZoom, caps.MaxZoom)
	}
	return w.onView(ref, func(v *view) error {
		if !respondsTo(v.web, sel("setPageZoom:")) {
			return unsupported("page zoom", "macOS 11")
		}
		v.web.Send(sel("setPageZoom:"), f)
		v.zoom = f
		return nil
	})
}

// zoomStep is the action of the View menu items: +1 and -1 step through the
// zoom levels, 0 resets. It acts on the focused web view of the key window.
func (a *App) zoomStep(dir int) {
	v := a.focusedView()
	if v == nil || v.web == 0 || !respondsTo(v.web, sel("pageZoom")) {
		return
	}
	cur := objc.Send[float64](v.web, sel("pageZoom"))
	next := 1.0
	switch {
	case dir > 0:
		next = caps.NextZoom(cur, true)
	case dir < 0:
		next = caps.NextZoom(cur, false)
	}
	v.web.Send(sel("setPageZoom:"), next)
	v.zoom = next
}

// focusedView is the view that has the keyboard focus in the key window, or
// that window's primary view. Main thread.
func (a *App) focusedView() *view {
	key := a.nsApp.Send(sel("keyWindow"))
	if key == 0 {
		return nil
	}
	w := a.windows[key]
	if w == nil {
		return nil
	}
	for r := key.Send(sel("firstResponder")); r != 0; {
		if v := a.byWeb[r]; v != nil {
			return v
		}
		if !respondsTo(r, sel("superview")) {
			break
		}
		r = r.Send(sel("superview"))
	}
	return w.pv
}

// Find.

// OnFind sets the handler for find results.
func (a *App) OnFind(fn func(caps.FindEvent)) {
	extMu.Lock()
	findHandler = fn
	extMu.Unlock()
}

var findHandler func(caps.FindEvent)

func emitFind(e caps.FindEvent) {
	extMu.Lock()
	fn := findHandler
	extMu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// evalInt evaluates a script that returns a number and calls fn with it, -1
// when the script failed. Main thread; fn runs on the main thread.
func evalInt(web objc.ID, js string, fn func(n int)) {
	blk := objc.NewBlock(func(_ objc.Block, result, err objc.ID) {
		n := -1
		if err == 0 && result != 0 {
			n = int(objc.Send[int64](result, sel("integerValue")))
		}
		fn(n)
	})
	web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(blk))
	blk.Release() // WebKit holds its own copy
}

// Find searches a view and highlights the match.
func (w *Window) Find(ref caps.ViewRef, text string, o caps.FindOptions) error {
	if text == "" {
		return w.StopFind(ref)
	}
	return w.onView(ref, func(v *view) error {
		s := sel("findString:withConfiguration:completionHandler:")
		if !respondsTo(v.web, s) {
			return unsupported("find in page", "macOS 11")
		}
		continuing := v.find.Start(text, o)
		cfg := cls("WKFindConfiguration").Send(sel("new"))
		defer release(cfg)
		cfg.Send(sel("setBackwards:"), o.Backward)
		cfg.Send(sel("setCaseSensitive:"), o.MatchCase)
		cfg.Send(sel("setWraps:"), !o.NoWrap)
		web := v.web
		done := func(total int) {
			active := v.find.Report(total)
			emitFind(caps.FindEvent{View: v.ref, Text: text, Matches: v.find.Total(), Active: active, Final: true})
		}
		blk := objc.NewBlock(func(_ objc.Block, result objc.ID) {
			found := result != 0 && objc.Send[bool](result, sel("matchFound"))
			switch {
			case !found:
				done(0)
			case continuing:
				done(v.find.Total())
			default:
				evalInt(web, caps.FindCountScript(text, o.MatchCase), func(n int) {
					if n < 1 {
						n = 1 // the engine found a match the text count cannot see
					}
					done(n)
				})
			}
		})
		web.Send(s, nsString(text), cfg, objc.ID(blk))
		blk.Release()
		return nil
	})
}

// StopFind clears the highlight.
func (w *Window) StopFind(ref caps.ViewRef) error {
	return w.onView(ref, func(v *view) error {
		v.find.Reset()
		v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.StopFindScript), objc.ID(0))
		return nil
	})
}

// Capture.

// pngOf converts an NSImage to PNG bytes.
func pngOf(image objc.ID) []byte {
	tiff := image.Send(sel("TIFFRepresentation"))
	if tiff == 0 {
		return nil
	}
	rep := cls("NSBitmapImageRep").Send(sel("imageRepWithData:"), tiff)
	if rep == 0 {
		return nil
	}
	const nsBitmapImageFileTypePNG = 4
	data := rep.Send(sel("representationUsingType:properties:"), nsBitmapImageFileTypePNG, cls("NSDictionary").Send(sel("dictionary")))
	return dataBytes(data)
}

// Capture takes a PNG of the visible area of a view.
func (w *Window) Capture(ref caps.ViewRef, _ caps.CaptureOptions) ([]byte, error) {
	type result struct {
		png []byte
		err error
	}
	ch := make(chan result, 1)
	err := w.onView(ref, func(v *view) error {
		s := sel("takeSnapshotWithConfiguration:completionHandler:")
		if !respondsTo(v.web, s) {
			return unsupported("view capture", "macOS 10.13")
		}
		cfg := cls("WKSnapshotConfiguration").Send(sel("new"))
		defer release(cfg)
		blk := objc.NewBlock(func(_ objc.Block, image, e objc.ID) {
			withPool(func() {
				if image == 0 {
					ch <- result{nil, fmt.Errorf("darwin: snapshot failed: %s", errorText(e))}
					return
				}
				png := pngOf(image)
				if png == nil {
					ch <- result{nil, errors.New("darwin: the snapshot cannot be converted to PNG")}
					return
				}
				ch <- result{png, nil}
			})
		})
		v.web.Send(s, cfg, objc.ID(blk))
		blk.Release()
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.png, r.err
	case <-time.After(30 * time.Second):
		return nil, errors.New("darwin: the view did not produce a snapshot in time")
	}
}

// Printing.

var (
	printMu    sync.Mutex
	printWaits = map[uintptr]chan bool{}
	printNext  uintptr
)

// printDidRun is the didRunSelector of a print operation run as a sheet.
func printDidRun(_ objc.ID, ok bool, ctx uintptr) {
	printMu.Lock()
	ch := printWaits[ctx]
	delete(printWaits, ctx)
	printMu.Unlock()
	if ch != nil {
		ch <- ok
	}
}

const points = 72.0 // points per inch

// printOperation builds the NSPrintOperation of a view. Main thread.
func (a *App) printOperation(v *view, info objc.ID) (objc.ID, error) {
	s := sel("printOperationWithPrintInfo:")
	if !respondsTo(v.web, s) {
		return 0, unsupported("printing", "macOS 11")
	}
	op := v.web.Send(s, info)
	if op == 0 {
		return 0, errors.New("darwin: the web view gave no print operation")
	}
	return op, nil
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

	ch := make(chan bool, 1)
	var restore func()
	err = w.onView(ref, func(v *view) error {
		url := cls("NSURL").Send(sel("fileURLWithPath:"), nsString(path))
		info := cls("NSPrintInfo").Send(sel("new"))
		defer release(info)
		dict := info.Send(sel("dictionary"))
		dict.Send(sel("setObject:forKey:"), nsString("NSPrintSaveJob"), nsString("NSJobDisposition"))
		dict.Send(sel("setObject:forKey:"), url, nsString("NSJobSavingURL"))
		m := o.Margins
		const nsPaperOrientationLandscape = 1
		if o.Landscape {
			info.Send(sel("setOrientation:"), nsPaperOrientationLandscape)
		}
		sw, sh := o.Sheet()
		info.Send(sel("setPaperSize:"), nsSize{Width: sw * points, Height: sh * points})
		info.Send(sel("setTopMargin:"), m.Top*points)
		info.Send(sel("setBottomMargin:"), m.Bottom*points)
		info.Send(sel("setLeftMargin:"), m.Left*points)
		info.Send(sel("setRightMargin:"), m.Right*points)
		info.Send(sel("setHorizontallyCentered:"), false)
		info.Send(sel("setVerticallyCentered:"), false)

		// Backgrounds are a private preference of the web view's configuration.
		prefs := v.web.Send(sel("configuration")).Send(sel("preferences"))
		if set := sel("_setShouldPrintBackgrounds:"); respondsTo(prefs, set) {
			prev := objc.Send[bool](prefs, sel("_shouldPrintBackgrounds"))
			prefs.Send(set, o.PrintBackground)
			restore = func() { prefs.Send(set, prev) }
		}

		op, err := w.app.printOperation(v, info)
		if err != nil {
			return err
		}
		op.Send(sel("setShowsPrintPanel:"), false)
		op.Send(sel("setShowsProgressPanel:"), false)
		// The print view needs the printable size as its frame.
		op.Send(sel("view")).Send(sel("setFrame:"), nsRect{Size: nsSize{Width: (sw - m.Left - m.Right) * points, Height: (sh - m.Top - m.Bottom) * points}})
		printMu.Lock()
		printNext++
		ctx := printNext
		printWaits[ctx] = ch
		printMu.Unlock()
		op.Send(sel("runOperationModalForWindow:delegate:didRunSelector:contextInfo:"), w.win, w.app.printDelegate, sel("printOperationDidRun:success:contextInfo:"), ctx)
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if restore != nil {
			dispatch(restore)
		}
	}()
	select {
	case ok := <-ch:
		if !ok {
			return nil, errors.New("darwin: the print operation failed")
		}
	case <-time.After(60 * time.Second):
		return nil, errors.New("darwin: printing did not finish in time")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 5 || string(b[:4]) != "%PDF" {
		return nil, errors.New("darwin: the print job produced no PDF")
	}
	return b, nil
}

// PrintDialog shows the print panel as a sheet on the window.
func (w *Window) PrintDialog(ref caps.ViewRef) error {
	return w.onView(ref, func(v *view) error {
		info := cls("NSPrintInfo").Send(sel("sharedPrintInfo"))
		op, err := w.app.printOperation(v, info)
		if err != nil {
			return err
		}
		op.Send(sel("view")).Send(sel("setFrame:"), objc.Send[nsRect](v.web, sel("bounds")))
		op.Send(sel("runOperationModalForWindow:delegate:didRunSelector:contextInfo:"), w.win, objc.ID(0), objc.SEL(0), uintptr(0))
		return nil
	})
}
