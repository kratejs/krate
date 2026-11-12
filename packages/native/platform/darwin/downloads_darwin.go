//go:build darwin

package darwin

// Downloads on WKDownload (macOS 11.3). The navigation delegate turns a link
// with the download attribute, an attachment response or a blob URL into a
// download when the SDK took over downloads (OnDownloadStart); a page of the
// app origin cannot be downloaded by WebKit, so those are fetched from the
// app's handler (caps.SchemeDownloader). Compile-only until CI runs
// gui_page_test.go on macos-latest.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Downloads = (*App)(nil)

// WKNavigationActionPolicy and WKNavigationResponsePolicy values.
const (
	policyCancel   = 0
	policyAllow    = 1
	policyDownload = 2
)

// macDownload is a download the SDK handler controls. Main thread only.
type macDownload struct {
	id       uint64
	obj      objc.ID
	view     caps.ViewRef
	url      string
	path     string
	total    int64
	finished bool
}

type dlState struct {
	mu      sync.Mutex
	start   func(caps.DownloadRequest, func(string))
	event   func(caps.DownloadEvent)
	byObj   map[objc.ID]*macDownload
	byID    map[uint64]*macDownload
	nextID  atomic.Uint64
	scheme  *caps.SchemeDownloader
	initted sync.Once
}

var dls = &dlState{byObj: map[objc.ID]*macDownload{}, byID: map[uint64]*macDownload{}}

func (a *App) schemeDownloader() *caps.SchemeDownloader {
	dls.initted.Do(func() {
		dls.scheme = &caps.SchemeDownloader{
			Handler: a.opts.Handler,
			NextID:  func() uint64 { return dls.nextID.Add(1) },
			Decide: func(r caps.DownloadRequest, decide func(string)) {
				dls.mu.Lock()
				fn := dls.start
				dls.mu.Unlock()
				if fn == nil {
					decide("")
					return
				}
				dispatch(func() { fn(r, decide) })
			},
			Emit: func(e caps.DownloadEvent) { dispatch(func() { emitDownload(e) }) },
		}
	})
	return dls.scheme
}

// OnDownloadStart sets the handler that chooses download destinations.
func (a *App) OnDownloadStart(fn func(caps.DownloadRequest, func(string))) {
	dls.mu.Lock()
	dls.start = fn
	dls.mu.Unlock()
}

// OnDownload sets the handler for download events.
func (a *App) OnDownload(fn func(caps.DownloadEvent)) {
	dls.mu.Lock()
	dls.event = fn
	dls.mu.Unlock()
}

func (a *App) downloadsWanted() bool {
	dls.mu.Lock()
	defer dls.mu.Unlock()
	return dls.start != nil
}

func emitDownload(e caps.DownloadEvent) {
	dls.mu.Lock()
	fn := dls.event
	dls.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// CancelDownload cancels a running download.
func (a *App) CancelDownload(id uint64) error {
	if a.schemeDownloader().Cancel(id) {
		return nil
	}
	var err error
	runOnMain(func() {
		withPool(func() {
			d := dls.byID[id]
			if d == nil {
				err = krate.Errorf("not_found", "no running download %d", id)
				return
			}
			blk := objc.NewBlock(func(_ objc.Block, resume objc.ID) {})
			d.obj.Send(sel("cancel:"), objc.ID(blk))
			blk.Release()
		})
	})
	return err
}

func (d *macDownload) emit(state caps.DownloadState, errMsg string) {
	var recv int64
	if d.obj != 0 && !d.finished {
		if p := d.obj.Send(sel("progress")); p != 0 {
			recv = objc.Send[int64](p, sel("completedUnitCount"))
		}
	}
	if state == caps.DownloadCompleted && d.total > 0 {
		recv = d.total
	}
	emitDownload(caps.DownloadEvent{ID: d.id, View: d.view, State: state, URL: d.url, Path: d.path, Received: recv, Total: d.total, Error: errMsg})
}

// hasAttachment reports whether an NSHTTPURLResponse says "attachment".
func hasAttachment(resp objc.ID) bool {
	if resp == 0 || !respondsTo(resp, sel("allHeaderFields")) {
		return false
	}
	h := resp.Send(sel("allHeaderFields"))
	for _, k := range []string{"Content-Disposition", "content-disposition"} {
		if v := h.Send(sel("objectForKey:"), nsString(k)); v != 0 {
			return strings.HasPrefix(strings.ToLower(strings.TrimSpace(goString(v))), "attachment")
		}
	}
	return false
}

// startSchemeDownload downloads a URL of the app origin for a view, asking
// the page for the download attribute of the link that points at it.
func (a *App) startSchemeDownload(v *view, uri string, lookup bool) {
	sd := a.schemeDownloader()
	if !lookup || v == nil || v.web == 0 {
		var ref caps.ViewRef
		if v != nil {
			ref = v.ref
		}
		sd.Start(ref, uri, "")
		return
	}
	js := `(function(){var u=` + jsQuote(uri) + `,l=document.querySelectorAll("a[download]");` +
		`for(var i=0;i<l.length;i++){if(l[i].href===u)return l[i].getAttribute("download")||""}return ""})()`
	ref := v.ref
	blk := objc.NewBlock(func(_ objc.Block, result, err objc.ID) {
		name := ""
		if err == 0 && result != 0 {
			name = goString(result)
		}
		sd.Start(ref, uri, name)
	})
	v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(blk))
	blk.Release()
}

// decideDownloadAction handles a navigation action that is a download. It
// returns the policy to give WebKit.
func (a *App) decideDownloadAction(webView, action objc.ID, scheme, rawURL string) int {
	if !a.downloadsWanted() {
		return policyCancel
	}
	if strings.EqualFold(scheme, Scheme) {
		a.startSchemeDownload(a.byWeb[webView], rawURL, true)
		return policyCancel
	}
	return policyDownload
}

// decideResponse implements webView:decidePolicyForNavigationResponse:. It
// returns the policy for the response.
func (a *App) decideResponse(webView, navResp objc.ID) int {
	resp := navResp.Send(sel("response"))
	if resp == 0 || !a.downloadsWanted() {
		return policyAllow
	}
	canShow := objc.Send[bool](navResp, sel("canShowMIMEType"))
	if canShow && !hasAttachment(resp) {
		return policyAllow
	}
	u := resp.Send(sel("URL"))
	if u == 0 {
		return policyAllow
	}
	if strings.EqualFold(goString(u.Send(sel("scheme"))), Scheme) {
		a.startSchemeDownload(a.byWeb[webView], goString(u.Send(sel("absoluteString"))), false)
		return policyCancel
	}
	return policyDownload
}

// attachDownload adopts a WKDownload the web view created.
func (a *App) attachDownload(webView, dlObj objc.ID) {
	if dlObj == 0 {
		return
	}
	retain(dlObj)
	d := &macDownload{id: dls.nextID.Add(1), obj: dlObj, total: -1}
	if v := a.byWeb[webView]; v != nil {
		d.view = v.ref
	}
	if req := dlObj.Send(sel("originalRequest")); req != 0 {
		if u := req.Send(sel("URL")); u != 0 {
			d.url = goString(u.Send(sel("absoluteString")))
		}
	}
	dls.byObj[dlObj] = d
	dls.byID[d.id] = d
	dlObj.Send(sel("setDelegate:"), a.dlDelegate)
	go func() {
		for {
			time.Sleep(150 * time.Millisecond)
			stop := make(chan bool, 1)
			dispatch(func() {
				if d.finished || dls.byObj[dlObj] != d {
					stop <- true
					return
				}
				if d.path != "" {
					d.emit(caps.DownloadProgress, "")
				}
				stop <- false
			})
			if <-stop {
				return
			}
		}
	}()
}

// decideDestination implements download:decideDestinationUsingResponse:
// suggestedFilename:completionHandler:.
func (a *App) decideDestination(dlObj, response, suggested, handler objc.ID) {
	d := dls.byObj[dlObj]
	dls.mu.Lock()
	start := dls.start
	dls.mu.Unlock()
	if d == nil || start == nil {
		invokeBlock1(handler, 0)
		return
	}
	req := caps.DownloadRequest{ID: d.id, View: d.view, URL: d.url, SuggestedName: goString(suggested), Total: -1}
	if response != 0 {
		if n := objc.Send[int64](response, sel("expectedContentLength")); n > 0 {
			req.Total, d.total = n, n
		}
		req.MIME = goString(response.Send(sel("MIMEType")))
	}
	var once sync.Once
	start(req, func(path string) {
		once.Do(func() {
			dispatch(func() {
				if path == "" {
					invokeBlock1(handler, 0)
					return
				}
				d.path = path
				invokeBlock1(handler, uintptr(cls("NSURL").Send(sel("fileURLWithPath:"), nsString(path))))
				d.emit(caps.DownloadStarted, "")
			})
		})
	})
}

func (a *App) downloadFinished(dlObj objc.ID) {
	d := dls.byObj[dlObj]
	if d == nil {
		return
	}
	d.emit(caps.DownloadCompleted, "")
	a.forgetDownload(d)
}

func (a *App) downloadFailed(dlObj, err objc.ID) {
	d := dls.byObj[dlObj]
	if d == nil {
		return
	}
	const nsURLErrorCancelled = -999
	if err != 0 && objc.Send[int64](err, sel("code")) == nsURLErrorCancelled {
		d.emit(caps.DownloadCancelled, "")
	} else {
		d.emit(caps.DownloadFailed, errorText(err))
	}
	a.forgetDownload(d)
}

func (a *App) forgetDownload(d *macDownload) {
	d.finished = true
	delete(dls.byObj, d.obj)
	delete(dls.byID, d.id)
	d.obj.Send(sel("setDelegate:"), objc.ID(0))
	release(d.obj)
}

var errNoStore = errors.New("darwin: the default website data store is not available")
