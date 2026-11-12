//go:build linux

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Downloads = (*App)(nil)

// downloadAPI holds the optional WebKitDownload bindings.
type downloadAPI struct {
	setDestination func(d uintptr, dest string)
	setOverwrite   func(d uintptr, on int32)
	getDestination func(d uintptr) uintptr
	cancel         func(d uintptr)
	getRequest     func(d uintptr) uintptr
	getResponse    func(d uintptr) uintptr
	getView        func(d uintptr) uintptr
	received       func(d uintptr) uint64
	respLength     func(r uintptr) uint64
	respMIME       func(r uintptr) uintptr
	respFilename   func(r uintptr) uintptr
	polRequest     func(d uintptr) uintptr
	polResponse    func(d uintptr) uintptr
	polSupported   func(d uintptr) int32
}

func loadDownloadAPI() *downloadAPI {
	d := &downloadAPI{}
	b := &binder{}
	wk, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		return d
	}
	b.bind(&d.setDestination, wk, "webkit_download_set_destination")
	b.bind(&d.setOverwrite, wk, "webkit_download_set_allow_overwrite")
	b.bind(&d.getDestination, wk, "webkit_download_get_destination")
	b.bind(&d.cancel, wk, "webkit_download_cancel")
	b.bind(&d.getRequest, wk, "webkit_download_get_request")
	b.bind(&d.getResponse, wk, "webkit_download_get_response")
	b.bind(&d.getView, wk, "webkit_download_get_web_view")
	b.bind(&d.received, wk, "webkit_download_get_received_data_length")
	b.bind(&d.respLength, wk, "webkit_uri_response_get_content_length")
	b.bind(&d.respMIME, wk, "webkit_uri_response_get_mime_type")
	b.bind(&d.respFilename, wk, "webkit_uri_response_get_suggested_filename")
	b.bind(&d.polRequest, wk, "webkit_response_policy_decision_get_request")
	b.bind(&d.polResponse, wk, "webkit_response_policy_decision_get_response")
	b.bind(&d.polSupported, wk, "webkit_response_policy_decision_is_mime_type_supported")
	return d
}

var (
	cbDlStarted, cbDlDecide, cbDlData, cbDlFinished, cbDlFailed uintptr
)

// download is one running download. Main thread only.
type download struct {
	id        uint64
	obj       uintptr
	view      caps.ViewRef
	url       string
	path      string
	total     int64
	decided   bool
	failed    bool
	lastEmit  time.Time
	lastBytes int64
}

func (a *App) newDownloadID() uint64 { return a.nextDL.Add(1) }

func (a *App) initDownloads() {
	a.dl = loadDownloadAPI()
	a.sd = &caps.SchemeDownloader{
		Handler: a.handler,
		NextID:  a.newDownloadID,
		Decide: func(r caps.DownloadRequest, decide func(string)) {
			a.mu.Lock()
			fn := a.dlStart
			a.mu.Unlock()
			if fn == nil {
				decide("")
				return
			}
			a.Dispatch(func() { fn(r, decide) })
		},
		Emit: func(e caps.DownloadEvent) {
			a.mu.Lock()
			fn := a.dlEvent
			a.mu.Unlock()
			if fn != nil {
				a.Dispatch(func() { fn(e) })
			}
		},
	}
	a.downloads = map[uint64]*download{}
	cbDlStarted = purego.NewCallback(dlStartedCallback)
	cbDlDecide = purego.NewCallback(dlDecideCallback)
	cbDlData = purego.NewCallback(dlDataCallback)
	cbDlFinished = purego.NewCallback(dlFinishedCallback)
	cbDlFailed = purego.NewCallback(dlFailedCallback)
	a.api.gSignalConnectData(a.ctx, "download-started", cbDlStarted, 0, 0, 0)
}

// OnDownloadStart sets the handler that chooses download destinations.
func (a *App) OnDownloadStart(fn func(r caps.DownloadRequest, decide func(path string))) {
	a.mu.Lock()
	a.dlStart = fn
	a.mu.Unlock()
}

// OnDownload sets the handler for download events.
func (a *App) OnDownload(fn func(caps.DownloadEvent)) {
	a.mu.Lock()
	a.dlEvent = fn
	a.mu.Unlock()
}

// CancelDownload cancels a running download.
func (a *App) CancelDownload(id uint64) (err error) {
	if a.sd.Cancel(id) {
		return nil
	}
	if !a.callMain(func() {
		d := a.downloads[id]
		switch {
		case d == nil:
			err = krate.Errorf("not_found", "no running download %d", id)
		case a.dl.cancel == nil:
			err = fmt.Errorf("%w: downloads cannot be cancelled in this WebKitGTK", krate.ErrUnsupported)
		default:
			a.dl.cancel(d.obj)
		}
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

func (a *App) emitDownload(d *download, state caps.DownloadState, errMsg string) {
	a.mu.Lock()
	fn := a.dlEvent
	a.mu.Unlock()
	if fn == nil {
		return
	}
	recv := d.lastBytes
	if a.dl.received != nil && d.obj != 0 && state != caps.DownloadStarted {
		recv = int64(a.dl.received(d.obj))
	}
	fn(caps.DownloadEvent{ID: d.id, View: d.view, State: state, URL: d.url, Path: d.path, Received: recv, Total: d.total, Error: errMsg})
}

func dlStartedCallback(ctx, dl, _ uintptr) uintptr {
	a := theApp
	a.mu.Lock()
	handled := a.dlStart != nil
	a.mu.Unlock()
	if !handled || a.dl.setDestination == nil {
		return 0 // WebKit's default destination applies
	}
	d := &download{id: a.newDownloadID(), obj: dl, total: -1}
	if a.dl.getRequest != nil {
		if req := a.dl.getRequest(dl); req != 0 {
			d.url = goString(a.api.wkURIRequestURI(req))
		}
	}
	var owner *view
	if a.dl.getView != nil {
		if wv := a.dl.getView(dl); wv != 0 {
			a.mu.Lock()
			owner = a.byWidget[wv]
			a.mu.Unlock()
			if owner != nil {
				d.view = owner.ref
			}
		}
	}
	if strings.HasPrefix(d.url, Origin+"/") && owner != nil {
		// WebKit cannot download the app scheme; refuse its download and do it
		// here. Cancelling emits "failed", which nothing listens to.
		a.dl.cancel(dl)
		a.startSchemeDownload(owner, d.url, true)
		return 0
	}
	a.api.gObjectRef(dl)
	a.downloads[d.id] = d
	a.api.gSignalConnectData(dl, "decide-destination", cbDlDecide, uintptr(d.id), 0, 0)
	a.api.gSignalConnectData(dl, "received-data", cbDlData, uintptr(d.id), 0, 0)
	a.api.gSignalConnectData(dl, "finished", cbDlFinished, uintptr(d.id), 0, 0)
	a.api.gSignalConnectData(dl, "failed", cbDlFailed, uintptr(d.id), 0, 0)
	return 0
}

func dlDecideCallback(dl, suggested, id uintptr) uintptr {
	a := theApp
	d := a.downloads[uint64(id)]
	if d == nil {
		return 0
	}
	a.mu.Lock()
	fn := a.dlStart
	a.mu.Unlock()
	if fn == nil {
		return 0
	}
	req := caps.DownloadRequest{ID: d.id, View: d.view, URL: d.url, SuggestedName: goString(suggested), Total: -1}
	if a.dl.getResponse != nil {
		if r := a.dl.getResponse(dl); r != 0 {
			if a.dl.respLength != nil {
				if n := a.dl.respLength(r); n > 0 {
					req.Total = int64(n)
					d.total = req.Total
				}
			}
			if a.dl.respMIME != nil {
				req.MIME = goString(a.dl.respMIME(r))
			}
		}
	}
	d.decided = true
	once := false
	decide := func(path string) {
		a.Dispatch(func() {
			if once || a.downloads[d.id] == nil {
				return
			}
			once = true
			if path == "" {
				a.dl.cancel(d.obj)
				return
			}
			d.path = path
			a.dl.setOverwrite(d.obj, 1)
			a.dl.setDestination(d.obj, path)
			a.emitDownload(d, caps.DownloadStarted, "")
		})
	}
	fn(req, decide)
	return 1 // the destination is set when decide is called
}

func dlDataCallback(dl, n, id uintptr) uintptr {
	a := theApp
	d := a.downloads[uint64(id)]
	if d == nil || d.path == "" {
		return 0
	}
	now := time.Now()
	if now.Sub(d.lastEmit) < 100*time.Millisecond {
		return 0
	}
	d.lastEmit = now
	a.emitDownload(d, caps.DownloadProgress, "")
	return 0
}

func dlFailedCallback(dl, gerr, id uintptr) uintptr {
	a := theApp
	d := a.downloads[uint64(id)]
	if d == nil {
		return 0
	}
	d.failed = true
	// WEBKIT_DOWNLOAD_ERROR_CANCELLED_BY_USER is 400.
	if code := gerrCode(gerr); code == 400 {
		a.emitDownload(d, caps.DownloadCancelled, "")
	} else {
		a.emitDownload(d, caps.DownloadFailed, gerrMessage(gerr))
	}
	return 0
}

func dlFinishedCallback(dl, id uintptr) uintptr {
	a := theApp
	d := a.downloads[uint64(id)]
	if d == nil {
		return 0
	}
	delete(a.downloads, d.id)
	if !d.failed {
		if d.path == "" && a.dl.getDestination != nil {
			d.path = goString(a.dl.getDestination(dl))
		}
		a.emitDownload(d, caps.DownloadCompleted, "")
	}
	a.api.gObjectUnref(dl)
	return 0
}

// responsePolicy turns a main-resource response that is an attachment, or a
// type WebKit cannot show, into a download. WebKit cannot download a page of
// the app scheme, so those are fetched here (see caps.SchemeDownloader).
func responsePolicy(view, decision uintptr) uintptr {
	a := theApp
	d := a.dl
	if d.polResponse == nil || d.polRequest == nil || d.respFilename == nil {
		return 0
	}
	req := d.polRequest(decision)
	if req == 0 {
		return 0
	}
	uri := goString(a.api.wkURIRequestURI(req))
	if !strings.HasPrefix(uri, Origin+"/") {
		return 0
	}
	resp := d.polResponse(decision)
	if resp == 0 {
		return 0
	}
	attachment := d.respFilename(resp) != 0
	supported := d.polSupported == nil || d.polSupported(decision) != 0
	if !attachment && supported {
		return 0
	}
	v := a.viewByRef(view)
	if v == nil || !a.downloadsWanted() {
		return 0
	}
	a.api.wkPolicyIgnore(decision)
	a.startSchemeDownload(v, uri, false)
	return 1
}

// downloadsWanted reports whether a handler took over downloads.
func (a *App) downloadsWanted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dlStart != nil
}

// startSchemeDownload downloads a URL of the app origin for a view. lookup
// asks the page for the download attribute of the link that points at it.
func (a *App) startSchemeDownload(v *view, uri string, lookup bool) {
	if !lookup || v.widget == 0 {
		a.sd.Start(v.ref, uri, "")
		return
	}
	js := `(function(){var u=` + jsString(uri) + `,l=document.querySelectorAll("a[download]");` +
		`for(var i=0;i<l.length;i++){if(l[i].href===u)return l[i].getAttribute("download")||""}return ""})()`
	ref := v.ref
	a.evalString(v.widget, js, func(name string) { a.sd.Start(ref, uri, name) })
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
