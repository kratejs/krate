//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Print, find, zoom, capture, downloads and site data on WebView2. Written
// against WebView2.h (checked with the header tool for slots and ids); the GUI
// tests in gui_page_test.go run it in CI on windows-latest.
//
//   - Zoom is ICoreWebView2Controller put_ZoomFactor. The browser accelerator
//     keys (Ctrl plus, minus, 0 and Ctrl+wheel) are WebView2's own and stay on.
//   - Print: ICoreWebView2_16::ShowPrintUI opens the system dialog (older
//     runtimes fall back to window.print()); PDF output is
//     ICoreWebView2_7::PrintToPdf to a temporary file.
//   - Capture is CapturePreview into a memory stream.
//   - Find has no WebView2.h interface in this SDK version, so it runs
//     window.find and counts matches in the page text.
//   - Downloads are the ICoreWebView2_4 DownloadStarting event. The app origin
//     is an https origin served through WebResourceRequested, so its downloads
//     take the same path as any other (untested).
//   - Cookies are ICoreWebView2CookieManager; clearing site data is
//     ICoreWebView2Profile2::ClearBrowsingData.
//
// Floating point arguments are passed as their bit patterns in the integer
// argument slots: the Go runtime's Windows call stub copies the first four
// slots into the XMM registers too, which is where a double at that position
// belongs.

var (
	_ caps.PageControls = (*Window)(nil)
	_ caps.PageEvents   = (*App)(nil)
	_ caps.Downloads    = (*App)(nil)
	_ caps.SiteData     = (*App)(nil)
)

// Interrupt reason of a download the user (or Cancel) stopped.
const dlInterruptUserCanceled = 26

const (
	dlStateInterrupted = 1
	dlStateCompleted   = 2
)

func f64(f float64) uintptr { return uintptr(math.Float64bits(f)) }

func pageUnsupported(what string) error {
	return fmt.Errorf("%w: %s needs a newer WebView2 runtime", krate.ErrUnsupported, what)
}

// onView runs fn on the main thread with the view, which must have a webview.
func (w *Window) onView(ref caps.ViewRef, fn func(v *view) error) error {
	var err error
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		switch {
		case w.destroyed || v == nil:
			err = fmt.Errorf("windows: view %d is not in this window", ref)
		case w.released || w.releasing:
			err = errReleased
		case v.webview == nil || v.controller == nil:
			err = fmt.Errorf("windows: view %d has no webview yet", ref)
		default:
			err = fn(v)
		}
	})
	return err
}

// ZoomFactor returns the zoom of a view.
func (w *Window) ZoomFactor(ref caps.ViewRef) (z float64, err error) {
	err = w.onView(ref, func(v *view) error {
		if hr := vcall(v.controller, ctlGetZoomFactor, uintptr(unsafe.Pointer(&z))); failed(hr) {
			return fmt.Errorf("windows: get_ZoomFactor: 0x%08x", uint32(hr))
		}
		return nil
	})
	return
}

// SetZoomFactor sets the zoom of a view.
func (w *Window) SetZoomFactor(ref caps.ViewRef, f float64) error {
	if f != f || f < caps.MinZoom || f > caps.MaxZoom {
		return krate.Errorf("bad_args", "zoom factor %v must be between %v and %v", f, caps.MinZoom, caps.MaxZoom)
	}
	return w.onView(ref, func(v *view) error {
		if hr := vcall(v.controller, ctlPutZoomFactor, f64(f)); failed(hr) {
			return fmt.Errorf("windows: put_ZoomFactor: 0x%08x", uint32(hr))
		}
		v.zoom = f
		return nil
	})
}

// scriptResult runs js in v and calls fn with the JSON of its result. Main
// thread; fn runs on the main thread too.
func scriptResult(v *view, js string, fn func(hr uintptr, json string)) {
	h := newHandler(iidExecuteScriptHandler, true, func(hr, result uintptr) uintptr {
		s := ""
		if !failed(hr) && result != 0 {
			s = xw.UTF16PtrToString((*uint16)(ptr(result)))
		}
		fn(hr, s)
		return sOK
	})
	if hr := vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(js))), uintptr(h)); failed(hr) {
		fn(hr, "")
	}
}

// OnFind sets the handler for find results.
func (a *App) OnFind(fn func(caps.FindEvent)) {
	extMu.Lock()
	a.findHandler = fn
	extMu.Unlock()
}

func (a *App) emitFind(e caps.FindEvent) {
	extMu.Lock()
	fn := a.findHandler
	extMu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// Find searches with window.find and counts the matches in the page text.
func (w *Window) Find(ref caps.ViewRef, text string, o caps.FindOptions) error {
	if text == "" {
		return w.StopFind(ref)
	}
	return w.onView(ref, func(v *view) error {
		fresh := !v.find.Start(text, o)
		scriptResult(v, caps.FindScript(text, o, fresh), func(hr uintptr, js string) {
			total := 0
			if !failed(hr) {
				var f float64
				if json.Unmarshal([]byte(js), &f) == nil {
					total = int(f)
				}
			}
			active := v.find.Report(total)
			w.app.emitFind(caps.FindEvent{View: v.ref, Text: text, Matches: total, Active: active, Final: true})
		})
		return nil
	})
}

// StopFind clears the highlight.
func (w *Window) StopFind(ref caps.ViewRef) error {
	return w.onView(ref, func(v *view) error {
		v.find.Reset()
		vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(caps.StopFindScript))), 0)
		return nil
	})
}

// newMemStream makes an empty read-write IStream.
func newMemStream() unsafe.Pointer {
	if pSHCreateMemStream.Find() != nil {
		return nil
	}
	r, _, _ := pSHCreateMemStream.Call(0, 0)
	return ptr(r)
}

// readStream reads a stream from its start to its end.
func readStream(s unsafe.Pointer) []byte {
	// IStream::Seek(0, STREAM_SEEK_SET, NULL); the LARGE_INTEGER is passed by
	// value in one register.
	vcall(s, istreamSeek, 0, 0, 0)
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uint32
		hr := vcall(s, istreamRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if failed(hr) || n == 0 {
			return out
		}
	}
}

// Capture returns a PNG of the visible area of a view.
func (w *Window) Capture(ref caps.ViewRef, _ caps.CaptureOptions) ([]byte, error) {
	stream := newMemStream()
	if stream == nil {
		return nil, errors.New("windows: cannot create a memory stream")
	}
	defer comRelease(stream)
	done := make(chan uintptr, 1)
	err := w.onView(ref, func(v *view) error {
		h := newHandler(iidWV2CapturePreviewCompletedHandler, true, func(hr, _ uintptr) uintptr {
			done <- hr
			return sOK
		})
		if hr := vcall(v.webview, wvCapturePreview, 0 /* PNG */, uintptr(stream), uintptr(h)); failed(hr) {
			return fmt.Errorf("windows: CapturePreview: 0x%08x", uint32(hr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case hr := <-done:
		if failed(hr) {
			return nil, fmt.Errorf("windows: CapturePreview failed: 0x%08x", uint32(hr))
		}
	case <-time.After(30 * time.Second):
		return nil, errors.New("windows: the view did not produce a capture in time")
	}
	var png []byte
	w.app.callMain(func() { png = readStream(stream) })
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		return nil, errors.New("windows: the capture is not a PNG")
	}
	return png, nil
}

// printSettings builds an ICoreWebView2PrintSettings for o. The caller owns
// the result. Main thread.
func (a *App) printSettings(o caps.PDFOptions) (unsafe.Pointer, error) {
	env6 := queryInterface(a.env, iidWV2Environment6)
	if env6 == nil {
		return nil, pageUnsupported("PDF output (ICoreWebView2Environment6)")
	}
	defer comRelease(env6)
	var ps uintptr
	if hr := vcall(env6, env6CreatePrintSet, uintptr(unsafe.Pointer(&ps))); failed(hr) || ps == 0 {
		return nil, fmt.Errorf("windows: CreatePrintSettings: 0x%08x", uint32(hr))
	}
	s := ptr(ps)
	orient := uintptr(0)
	if o.Landscape {
		orient = 1
	}
	m := o.Margins
	vcall(s, psPutOrientation, orient)
	vcall(s, psPutPageWidth, f64(o.PageWidth))
	vcall(s, psPutPageHeight, f64(o.PageHeight))
	vcall(s, psPutMarginTop, f64(m.Top))
	vcall(s, psPutMarginBottom, f64(m.Bottom))
	vcall(s, psPutMarginLeft, f64(m.Left))
	vcall(s, psPutMarginRight, f64(m.Right))
	if o.PrintBackground {
		vcall(s, psPutBackgrounds, 1)
	}
	vcall(s, psPutHeaderFooter, 0)
	return s, nil
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
	done := make(chan uintptr, 1)
	err = w.onView(ref, func(v *view) error {
		wv7 := queryInterface(v.webview, iidWV2_7)
		if wv7 == nil {
			return pageUnsupported("PDF output (ICoreWebView2_7)")
		}
		defer comRelease(wv7)
		ps, err := w.app.printSettings(o)
		if err != nil {
			return err
		}
		defer comRelease(ps)
		h := newHandler(iidWV2PrintToPdfCompletedHandler, true, func(hr, ok uintptr) uintptr {
			if !failed(hr) && ok == 0 {
				hr = eFail
			}
			done <- hr
			return sOK
		})
		if hr := vcall(wv7, wv7PrintToPdf, uintptr(unsafe.Pointer(wstr(path))), uintptr(ps), uintptr(h)); failed(hr) {
			return fmt.Errorf("windows: PrintToPdf: 0x%08x", uint32(hr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case hr := <-done:
		if failed(hr) {
			return nil, fmt.Errorf("windows: PrintToPdf failed: 0x%08x", uint32(hr))
		}
	case <-time.After(60 * time.Second):
		return nil, errors.New("windows: printing did not finish in time")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 5 || string(b[:4]) != "%PDF" {
		return nil, errors.New("windows: the print job produced no PDF")
	}
	return b, nil
}

// PrintDialog opens the system print dialog.
func (w *Window) PrintDialog(ref caps.ViewRef) error {
	return w.onView(ref, func(v *view) error {
		wv16 := queryInterface(v.webview, iidWV2_16)
		if wv16 == nil {
			// Older runtimes: the page's own print command.
			vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr("window.print()"))), 0)
			return nil
		}
		defer comRelease(wv16)
		if hr := vcall(wv16, wv16ShowPrintUI, 1 /* COREWEBVIEW2_PRINT_DIALOG_KIND_SYSTEM */); failed(hr) {
			return fmt.Errorf("windows: ShowPrintUI: 0x%08x", uint32(hr))
		}
		return nil
	})
}

// Downloads.

// winDownload is a download the handler took over. Main thread.
type winDownload struct {
	id        uint64
	op        unsafe.Pointer
	view      caps.ViewRef
	url       string
	path      string
	total     int64
	cancelled bool
	lastEmit  time.Time
}

type dlState struct {
	mu      sync.Mutex
	start   func(caps.DownloadRequest, func(string))
	event   func(caps.DownloadEvent)
	running map[uint64]*winDownload
	nextID  atomic.Uint64
}

var dl = &dlState{running: map[uint64]*winDownload{}}

// OnDownloadStart sets the handler that chooses download destinations.
func (a *App) OnDownloadStart(fn func(caps.DownloadRequest, func(string))) {
	dl.mu.Lock()
	dl.start = fn
	dl.mu.Unlock()
}

// OnDownload sets the handler for download events.
func (a *App) OnDownload(fn func(caps.DownloadEvent)) {
	dl.mu.Lock()
	dl.event = fn
	dl.mu.Unlock()
}

// CancelDownload cancels a running download.
func (a *App) CancelDownload(id uint64) error {
	var err error
	a.callMain(func() {
		d := dl.running[id]
		if d == nil {
			err = krate.Errorf("not_found", "no running download %d", id)
			return
		}
		d.cancelled = true
		vcall(d.op, dlOpCancel)
	})
	return err
}

func (d *winDownload) emit(state caps.DownloadState, errMsg string) {
	dl.mu.Lock()
	fn := dl.event
	dl.mu.Unlock()
	if fn == nil {
		return
	}
	var recv int64
	if d.op != nil {
		vcall(d.op, dlOpGetReceived, uintptr(unsafe.Pointer(&recv)))
	}
	fn(caps.DownloadEvent{ID: d.id, View: d.view, State: state, URL: d.url, Path: d.path, Received: recv, Total: d.total, Error: errMsg})
}

// watchDownloads installs the DownloadStarting handler of a view, when the
// runtime has ICoreWebView2_4. Main thread.
func (a *App) watchDownloads(v *view) {
	wv4 := queryInterface(v.webview, iidWV2_4)
	if wv4 == nil {
		return
	}
	defer comRelease(wv4)
	h := newHandler(iidWV2DownloadStartingEventHandler, false, func(sender, args uintptr) uintptr {
		a.downloadStarting(v, ptr(args))
		return sOK
	})
	var token int64
	vcall(wv4, wv4AddDownloadStart, uintptr(h), uintptr(unsafe.Pointer(&token)))
	// The webview holds the handler now; our creator reference goes with the
	// webview's.
	comRelease(h)
}

func (a *App) downloadStarting(v *view, args unsafe.Pointer) {
	dl.mu.Lock()
	start := dl.start
	dl.mu.Unlock()
	if start == nil || args == nil {
		return // WebView2's own download UI applies
	}
	var opp uintptr
	if failed(vcall(args, dlArgsGetOperation, uintptr(unsafe.Pointer(&opp)))) || opp == 0 {
		return
	}
	op := ptr(opp)
	defPath := comString(args, dlArgsGetResultPath)
	d := &winDownload{id: dl.nextID.Add(1), op: op, view: v.ref, url: comString(op, dlOpGetURI), total: -1}
	vcall(op, dlOpGetTotal, uintptr(unsafe.Pointer(&d.total)))
	if d.total <= 0 {
		d.total = -1
	}
	req := caps.DownloadRequest{ID: d.id, View: v.ref, URL: d.url, SuggestedName: baseName(defPath), MIME: comString(op, dlOpGetMIME), Total: d.total}

	// Keep the arguments and a deferral until the destination is chosen.
	var defp uintptr
	vcall(args, dlArgsPutHandled, 1) // no WebView2 download dialog
	if failed(vcall(args, dlArgsGetDeferral, uintptr(unsafe.Pointer(&defp)))) || defp == 0 {
		return
	}
	deferral := ptr(defp)
	comAddRef(args)
	dl.running[d.id] = d
	var once sync.Once
	decide := func(path string) {
		once.Do(func() {
			a.Dispatch(func() {
				defer comRelease(args)
				defer comRelease(deferral)
				if path == "" {
					d.cancelled = true
					vcall(args, dlArgsPutCancel, 1)
					delete(dl.running, d.id)
					d.emit(caps.DownloadCancelled, "")
					comRelease(op)
					return
				}
				d.path = path
				vcall(args, dlArgsPutResultPath, uintptr(unsafe.Pointer(wstr(path))))
				a.trackDownload(d)
				d.emit(caps.DownloadStarted, "")
				vcall(deferral, deferralComplete)
			})
		})
	}
	start(req, decide)
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// trackDownload watches the progress and the end of a download. Main thread.
func (a *App) trackDownload(d *winDownload) {
	var token int64
	hBytes := newHandler(iidWV2BytesReceivedChangedEventHandler, false, func(sender, args uintptr) uintptr {
		if now := time.Now(); now.Sub(d.lastEmit) >= 100*time.Millisecond {
			d.lastEmit = now
			d.emit(caps.DownloadProgress, "")
		}
		return sOK
	})
	vcall(d.op, dlOpAddBytes, uintptr(hBytes), uintptr(unsafe.Pointer(&token)))
	comRelease(hBytes)
	hState := newHandler(iidWV2StateChangedEventHandler, false, func(sender, args uintptr) uintptr {
		var state, reason int32
		vcall(d.op, dlOpGetState, uintptr(unsafe.Pointer(&state)))
		switch state {
		case dlStateCompleted:
			delete(dl.running, d.id)
			d.emit(caps.DownloadCompleted, "")
			comRelease(d.op)
		case dlStateInterrupted:
			vcall(d.op, dlOpGetInterrupt, uintptr(unsafe.Pointer(&reason)))
			delete(dl.running, d.id)
			if d.cancelled || reason == dlInterruptUserCanceled {
				d.emit(caps.DownloadCancelled, "")
			} else {
				d.emit(caps.DownloadFailed, fmt.Sprintf("the download was interrupted (reason %d)", reason))
			}
			comRelease(d.op)
		}
		return sOK
	})
	vcall(d.op, dlOpAddState, uintptr(unsafe.Pointer(hState)), uintptr(unsafe.Pointer(&token)))
	comRelease(hState)
}

// Site data.

// anyWebview returns a live webview of the app (the cookie jar and the
// profile are shared by all of them). Main thread.
func (a *App) anyWebview() unsafe.Pointer {
	for _, w := range a.windows {
		if w.released {
			continue
		}
		for _, v := range w.allViews() {
			// Never a remote view: its profile is not the app's.
			if v.webview != nil && v.rv == nil {
				return v.webview
			}
		}
	}
	return nil
}

// cookieManager returns an owned ICoreWebView2CookieManager. Main thread.
func (a *App) cookieManager() (unsafe.Pointer, error) {
	wv := a.anyWebview()
	if wv == nil {
		return nil, errors.New("windows: no webview is open; cookies are reachable once a window exists")
	}
	wv2 := queryInterface(wv, iidWV2_2)
	if wv2 == nil {
		return nil, pageUnsupported("cookies (ICoreWebView2_2)")
	}
	defer comRelease(wv2)
	var cm uintptr
	if hr := vcall(wv2, wv2GetCookieManager, uintptr(unsafe.Pointer(&cm))); failed(hr) || cm == 0 {
		return nil, fmt.Errorf("windows: get_CookieManager: 0x%08x", uint32(hr))
	}
	return ptr(cm), nil
}

var sameSiteNames = [...]caps.SameSite{caps.SameSiteNone, caps.SameSiteLax, caps.SameSiteStrict}

func readCookie(c unsafe.Pointer) caps.Cookie {
	out := caps.Cookie{
		Name: comString(c, cookieGetName), Value: comString(c, cookieGetValue),
		Domain: comString(c, cookieGetDomain), Path: comString(c, cookieGetPath),
	}
	var b int32
	vcall(c, cookieGetSecure, uintptr(unsafe.Pointer(&b)))
	out.Secure = b != 0
	vcall(c, cookieGetHTTPOnly, uintptr(unsafe.Pointer(&b)))
	out.HTTPOnly = b != 0
	vcall(c, cookieGetIsSession, uintptr(unsafe.Pointer(&b)))
	if b == 0 {
		var exp float64
		vcall(c, cookieGetExpires, uintptr(unsafe.Pointer(&exp)))
		out.Expires = time.Unix(int64(exp), 0).UTC()
	}
	var ss int32
	vcall(c, cookieGetSameSite, uintptr(unsafe.Pointer(&ss)))
	if ss >= 0 && int(ss) < len(sameSiteNames) {
		out.SameSite = sameSiteNames[ss]
	}
	return out
}

// siteCall runs start on the main thread with the cookie manager and waits
// for the completion handler it installs to send a result.
func (a *App) siteCall(what string, start func(cm unsafe.Pointer, done func(any, error)) error) (any, error) {
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	var startErr error
	a.callMain(func() {
		cm, err := a.cookieManager()
		if err != nil {
			startErr = err
			return
		}
		startErr = start(cm, func(v any, err error) {
			comRelease(cm)
			ch <- result{v, err}
		})
		if startErr != nil {
			comRelease(cm)
		}
	})
	if startErr != nil {
		return nil, startErr
	}
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("windows: %s did not finish in time", what)
	}
}

// Cookies lists the cookies that would be sent to origin.
func (a *App) Cookies(origin string) ([]caps.Cookie, error) {
	uri, _, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return nil, err
	}
	v, err := a.siteCall("listing cookies", func(cm unsafe.Pointer, done func(any, error)) error {
		h := newHandler(iidWV2GetCookiesCompletedHandler, true, func(hr, list uintptr) uintptr {
			if failed(hr) || list == 0 {
				done(nil, fmt.Errorf("windows: GetCookies: 0x%08x", uint32(hr)))
				return sOK
			}
			l := ptr(list)
			var n uint32
			vcall(l, cookieListGetCount, uintptr(unsafe.Pointer(&n)))
			out := make([]caps.Cookie, 0, n)
			for i := uint32(0); i < n; i++ {
				var cp uintptr
				if failed(vcall(l, cookieListGetAt, uintptr(i), uintptr(unsafe.Pointer(&cp)))) || cp == 0 {
					continue
				}
				out = append(out, readCookie(ptr(cp)))
				comRelease(ptr(cp))
			}
			done(out, nil)
			return sOK
		})
		if hr := vcall(cm, cookMgrGetCookies, uintptr(unsafe.Pointer(wstr(uri))), uintptr(h)); failed(hr) {
			return fmt.Errorf("windows: GetCookies: 0x%08x", uint32(hr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]caps.Cookie), nil
}

// SetCookie stores a cookie.
func (a *App) SetCookie(origin string, c caps.Cookie) error {
	_, host, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return err
	}
	if c.Name == "" || strings.ContainsAny(c.Name, "=;, \t\r\n") {
		return krate.Errorf("bad_args", "cookie name %q is not valid", c.Name)
	}
	domain, path := c.Domain, c.Path
	if domain == "" {
		domain = host
	}
	if path == "" {
		path = "/"
	}
	_, err = a.siteCall("setting a cookie", func(cm unsafe.Pointer, done func(any, error)) error {
		var cp uintptr
		if hr := vcall(cm, cookMgrCreate, uintptr(unsafe.Pointer(wstr(c.Name))), uintptr(unsafe.Pointer(wstr(c.Value))),
			uintptr(unsafe.Pointer(wstr(domain))), uintptr(unsafe.Pointer(wstr(path))), uintptr(unsafe.Pointer(&cp))); failed(hr) || cp == 0 {
			return fmt.Errorf("windows: CreateCookie: 0x%08x", uint32(hr))
		}
		ck := ptr(cp)
		defer comRelease(ck)
		if !c.Expires.IsZero() {
			vcall(ck, cookiePutExpires, f64(float64(c.Expires.Unix())))
		}
		vcall(ck, cookiePutHTTPOnly, uintptr(b2u(c.HTTPOnly)))
		vcall(ck, cookiePutSecure, uintptr(b2u(c.Secure)))
		for i, n := range sameSiteNames {
			if n == c.SameSite {
				vcall(ck, cookiePutSameSite, uintptr(i))
			}
		}
		hr := vcall(cm, cookMgrAddOrUpdate, uintptr(ck))
		if failed(hr) {
			return fmt.Errorf("windows: AddOrUpdateCookie: 0x%08x", uint32(hr))
		}
		done(nil, nil) // the call completes synchronously
		return nil
	})
	return err
}

func b2u(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

var cookMgrDeleteWithDomainPath = vtIndex("ICoreWebView2CookieManager", "DeleteCookiesWithDomainAndPath")

// DeleteCookie removes the cookies of the origin with this name. The runtime
// applies a deletion asynchronously and DeleteCookies has no completion, so
// the call waits until the cookie is gone. When the URL-based deletion does
// not remove it, the cookie is deleted by its own domain and path.
func (a *App) DeleteCookie(origin string, c caps.Cookie) error {
	uri, _, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return err
	}
	_, err = a.siteCall("deleting a cookie", func(cm unsafe.Pointer, done func(any, error)) error {
		if hr := vcall(cm, cookMgrDeleteCookies, uintptr(unsafe.Pointer(wstr(c.Name))), uintptr(unsafe.Pointer(wstr(uri)))); failed(hr) {
			return fmt.Errorf("windows: DeleteCookies: 0x%08x", uint32(hr))
		}
		done(nil, nil)
		return nil
	})
	if err != nil {
		return err
	}
	left, err := a.waitCookieGone(origin, c.Name)
	if err != nil || len(left) == 0 {
		return err
	}
	for _, k := range left {
		_, err = a.siteCall("deleting a cookie", func(cm unsafe.Pointer, done func(any, error)) error {
			if hr := vcall(cm, cookMgrDeleteWithDomainPath, uintptr(unsafe.Pointer(wstr(k.Name))), uintptr(unsafe.Pointer(wstr(k.Domain))), uintptr(unsafe.Pointer(wstr(k.Path)))); failed(hr) {
				return fmt.Errorf("windows: DeleteCookiesWithDomainAndPath: 0x%08x", uint32(hr))
			}
			done(nil, nil)
			return nil
		})
		if err != nil {
			return err
		}
	}
	_, err = a.waitCookieGone(origin, c.Name)
	return err
}

// waitCookieGone polls until no cookie called name is sent to origin, for at
// most two seconds, and returns the ones still there.
func (a *App) waitCookieGone(origin, name string) ([]caps.Cookie, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		cs, err := a.Cookies(origin)
		if err != nil {
			return nil, err
		}
		var left []caps.Cookie
		for _, k := range cs {
			if k.Name == name {
				left = append(left, k)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Bits of COREWEBVIEW2_BROWSING_DATA_KINDS.
const (
	dataFileSystems   = 0x1
	dataIndexedDB     = 0x2
	dataLocalStorage  = 0x4
	dataWebSQL        = 0x8
	dataCacheStorage  = 0x10
	dataAllDOMStorage = 0x20
	dataCookies       = 0x40
	dataDiskCache     = 0x100
	dataServiceWork   = 0x8000
)

// ClearSiteData removes cookies, caches and storage. Each kind is cleared by
// its own ClearBrowsingData call with its own deadline, so a kind the runtime
// never completes is named in the error and does not hold the others back.
func (a *App) ClearSiteData(o caps.ClearSiteDataOptions) error {
	all := o.Everything()
	type part struct {
		name  string
		kinds uint32
	}
	var parts []part
	if all || o.Cookies {
		parts = append(parts, part{"cookies", dataCookies})
	}
	if all || o.Cache {
		parts = append(parts, part{"disk cache", dataDiskCache})
	}
	if all || o.Storage {
		// The parts of ALL_DOM_STORAGE one by one: on windows-11-arm the
		// combined kind never completed, and one call each names the culprit.
		parts = append(parts,
			part{"file systems", dataFileSystems},
			part{"IndexedDB", dataIndexedDB},
			part{"local storage", dataLocalStorage},
			part{"cache storage", dataCacheStorage},
			part{"WebSQL", dataWebSQL},
			part{"service workers", dataServiceWork})
	}
	for _, p := range parts {
		if err := a.clearKinds(o, p.kinds, p.name); err != nil {
			// WebSQL (removed from Chromium) and the legacy FileSystem API
			// hold nothing in an app that does not use them, and some
			// runtimes never complete clearing them (seen on Windows on
			// ARM, where the DevTools fallback answered ERROR_INVALID_STATE
			// too). A timeout there is not an error.
			if (p.kinds == dataWebSQL || p.kinds == dataFileSystems) && errors.Is(err, errClearTimeout) {
				continue
			}
			return err
		}
	}
	return nil
}

var errClearTimeout = errors.New("did not finish in time")

// clearKinds runs one ClearBrowsingData call and waits for it.
func (a *App) clearKinds(o caps.ClearSiteDataOptions, kinds uint32, name string) error {
	done := make(chan error, 1)
	var startErr error
	fallback := false
	a.callMain(func() {
		wv := a.anyWebview()
		if wv == nil {
			startErr = errors.New("windows: no webview is open")
			return
		}
		wv13 := queryInterface(wv, iidWV2_13)
		if wv13 == nil {
			fallback = true
			return
		}
		defer comRelease(wv13)
		var pp uintptr
		if failed(vcall(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&pp)))) || pp == 0 {
			startErr = errors.New("windows: get_Profile failed")
			return
		}
		profile := ptr(pp)
		defer comRelease(profile)
		p2 := queryInterface(profile, iidWV2Profile2)
		if p2 == nil {
			fallback = true
			return
		}
		defer comRelease(p2)
		h := newHandler(iidWV2ClearBrowsingDataCompletedHandler, true, func(hr, _ uintptr) uintptr {
			if failed(hr) {
				done <- fmt.Errorf("windows: ClearBrowsingData (%s): 0x%08x", name, uint32(hr))
			} else {
				done <- nil
			}
			return sOK
		})
		if hr := vcall(p2, profile2ClearData, uintptr(kinds), uintptr(h)); failed(hr) {
			startErr = fmt.Errorf("windows: ClearBrowsingData (%s): 0x%08x", name, uint32(hr))
		}
	})
	if fallback {
		// clearWithoutProfile is main-thread COM like the call above.
		var err error
		a.callMain(func() {
			if kinds == dataCookies {
				err = a.clearWithoutProfile(caps.ClearSiteDataOptions{Cookies: true})
			} else {
				err = a.clearWithoutProfile(o)
			}
		})
		return err
	}
	if startErr != nil {
		return startErr
	}
	select {
	case err := <-done:
		return err
	case <-time.After(clearKindDeadline):
		cdpErr := a.clearOriginByProtocol(kinds)
		if cdpErr == errNoCDPStorageType {
			return fmt.Errorf("windows: clearing %s: %w", name, errClearTimeout)
		}
		if cdpErr != nil {
			return fmt.Errorf("windows: clearing %s: %w; the DevTools fallback failed too: %v", name, errClearTimeout, cdpErr)
		}
		return nil
	}
}

// clearKindDeadline is how long one ClearBrowsingData call may take before
// the DevTools protocol path is tried (on windows-11-arm the DOM storage
// kinds never completed).
const clearKindDeadline = 10 * time.Second

var errNoCDPStorageType = errors.New("no DevTools storage type")

// cdpStorageType maps a ClearBrowsingData DOM storage kind to the
// Storage.clearDataForOrigin type list, or "" when there is none.
func cdpStorageType(kinds uint32) string {
	switch kinds {
	case dataFileSystems:
		return "file_systems"
	case dataIndexedDB:
		return "indexeddb"
	case dataLocalStorage:
		return "local_storage"
	case dataWebSQL:
		return "websql"
	case dataCacheStorage:
		return "cache_storage"
	case dataServiceWork:
		return "service_workers"
	}
	return ""
}

// clearOriginByProtocol clears one DOM storage kind of the app origin through
// the DevTools protocol, for a runtime whose ClearBrowsingData never
// completes it. Any goroutine.
func (a *App) clearOriginByProtocol(kinds uint32) error {
	types := cdpStorageType(kinds)
	if types == "" {
		return errNoCDPStorageType
	}
	var ref caps.ViewRef
	found := false
	a.callMain(func() {
		for _, w := range a.windows {
			if w.released {
				continue
			}
			for _, v := range w.allViews() {
				if !found && v.webview != nil && v.rv == nil {
					ref, found = v.ref, true
				}
			}
		}
	})
	if !found {
		return errors.New("no webview is open")
	}
	params := fmt.Sprintf(`{"origin":%q,"storageTypes":%q}`, Origin, types)
	_, err := a.CDP(ref, "Storage.clearDataForOrigin", params)
	return err
}

// clearWithoutProfile handles a runtime without ICoreWebView2Profile2: only
// cookies can be cleared (through the cookie manager). Main thread.
func (a *App) clearWithoutProfile(o caps.ClearSiteDataOptions) error {
	if o.Cache || o.Storage || o.Everything() && !o.Cookies {
		if !o.Cookies || o.Cache || o.Storage {
			return pageUnsupported("clearing the cache and storage (ICoreWebView2Profile2)")
		}
	}
	cm, err := a.cookieManager()
	if err != nil {
		return err
	}
	defer comRelease(cm)
	if hr := vcall(cm, cookMgrDeleteAll); failed(hr) {
		return fmt.Errorf("windows: DeleteAllCookies: 0x%08x", uint32(hr))
	}
	return nil
}
