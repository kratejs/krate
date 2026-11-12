package fakeplat

import (
	"errors"
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.PageControls = (*Window)(nil)
	_ caps.PageEvents   = (*App)(nil)
	_ caps.Downloads    = (*App)(nil)
	_ caps.SiteData     = (*App)(nil)
)

// pageState is the web contents state the fake window keeps per view.
type pageState struct {
	zoom float64
	find string
}

// PageCalls lists the page operations the fake window ran, as "op view arg".
func (w *Window) PageCalls() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.pageCalls...)
}

func (w *Window) pageOp(v caps.ViewRef, op string, extra ...any) (*pageState, error) {
	if w.lookup(v) == nil {
		return nil, fmt.Errorf("view %d is not in this window", v)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pageCalls = append(w.pageCalls, fmt.Sprint(append([]any{op, uint64(v)}, extra...)...))
	if w.pages == nil {
		w.pages = map[caps.ViewRef]*pageState{}
	}
	p := w.pages[v]
	if p == nil {
		p = &pageState{zoom: 1}
		w.pages[v] = p
	}
	return p, nil
}

func (w *Window) PrintDialog(v caps.ViewRef) error {
	_, err := w.pageOp(v, "print")
	return err
}

// LastPDF holds the options of the last PrintPDF.
func (w *Window) LastPDF() caps.PDFOptions {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastPDF
}

func (w *Window) PrintPDF(v caps.ViewRef, o caps.PDFOptions) ([]byte, error) {
	if _, err := w.pageOp(v, "pdf"); err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.lastPDF = o
	w.mu.Unlock()
	return []byte("%PDF-1.4 fake"), nil
}

func (w *Window) Find(v caps.ViewRef, text string, o caps.FindOptions) error {
	p, err := w.pageOp(v, "find", text, o.Backward, o.MatchCase, o.NoWrap, o.Continue)
	if err != nil {
		return err
	}
	w.mu.Lock()
	p.find = text
	w.mu.Unlock()
	w.app.mu.Lock()
	fn := w.app.findFn
	n := w.app.FindMatches
	w.app.mu.Unlock()
	if fn != nil {
		fn(caps.FindEvent{View: v, Text: text, Matches: n, Active: min(1, n), Final: true})
	}
	return nil
}

func (w *Window) StopFind(v caps.ViewRef) error {
	_, err := w.pageOp(v, "stopfind")
	return err
}

func (w *Window) ZoomFactor(v caps.ViewRef) (float64, error) {
	p, err := w.pageOp(v, "zoom")
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return p.zoom, nil
}

func (w *Window) SetZoomFactor(v caps.ViewRef, f float64) error {
	p, err := w.pageOp(v, "setzoom", f)
	if err != nil {
		return err
	}
	w.mu.Lock()
	p.zoom = f
	w.mu.Unlock()
	return nil
}

func (w *Window) Capture(v caps.ViewRef, _ caps.CaptureOptions) ([]byte, error) {
	if _, err := w.pageOp(v, "capture"); err != nil {
		return nil, err
	}
	return []byte("\x89PNG\r\n\x1a\nfake"), nil
}

// OnFind implements caps.PageEvents.
func (a *App) OnFind(fn func(caps.FindEvent)) { a.mu.Lock(); a.findFn = fn; a.mu.Unlock() }

// Downloads.

// OnDownloadStart implements caps.Downloads.
func (a *App) OnDownloadStart(fn func(caps.DownloadRequest, func(string))) {
	a.mu.Lock()
	a.dlStart = fn
	a.mu.Unlock()
}

// OnDownload implements caps.Downloads.
func (a *App) OnDownload(fn func(caps.DownloadEvent)) { a.mu.Lock(); a.dlEvent = fn; a.mu.Unlock() }

// CancelDownload implements caps.Downloads.
func (a *App) CancelDownload(id uint64) error {
	a.mu.Lock()
	a.Cancelled = append(a.Cancelled, id)
	ok := a.running[id]
	a.mu.Unlock()
	if !ok {
		return errors.New("no such download")
	}
	return nil
}

// StartDownload plays a download the page started: the SDK's handler chooses
// the destination, which is returned (empty when it cancelled).
func (a *App) StartDownload(r caps.DownloadRequest) string {
	a.mu.Lock()
	fn := a.dlStart
	if a.running == nil {
		a.running = map[uint64]bool{}
	}
	a.running[r.ID] = true
	a.mu.Unlock()
	if fn == nil {
		return ""
	}
	ch := make(chan string, 1)
	fn(r, func(p string) { ch <- p })
	return <-ch
}

// EmitDownload delivers a download event.
func (a *App) EmitDownload(e caps.DownloadEvent) {
	a.mu.Lock()
	fn := a.dlEvent
	if e.State == caps.DownloadCompleted || e.State == caps.DownloadCancelled || e.State == caps.DownloadFailed {
		delete(a.running, e.ID)
	}
	a.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// Site data.

type cookieJar struct {
	mu      sync.Mutex
	cookies map[string][]caps.Cookie // by origin
	cleared []caps.ClearSiteDataOptions
}

func (a *App) jar() *cookieJar {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cookieJar == nil {
		a.cookieJar = &cookieJar{cookies: map[string][]caps.Cookie{}}
	}
	return a.cookieJar
}

// Cleared lists the ClearSiteData calls.
func (a *App) Cleared() []caps.ClearSiteDataOptions {
	j := a.jar()
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]caps.ClearSiteDataOptions(nil), j.cleared...)
}

func (a *App) Cookies(origin string) ([]caps.Cookie, error) {
	if origin == "krate://app" {
		return nil, errAppOrigin
	}
	j := a.jar()
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]caps.Cookie(nil), j.cookies[origin]...), nil
}

var errAppOrigin = fmt.Errorf("%w: no cookies for the app scheme", krate.ErrUnsupported)

func (a *App) SetCookie(origin string, c caps.Cookie) error {
	if origin == "krate://app" {
		return errAppOrigin
	}
	j := a.jar()
	j.mu.Lock()
	defer j.mu.Unlock()
	list := j.cookies[origin]
	for i := range list {
		if list[i].Name == c.Name {
			list[i] = c
			return nil
		}
	}
	j.cookies[origin] = append(list, c)
	return nil
}

func (a *App) DeleteCookie(origin string, c caps.Cookie) error {
	if origin == "krate://app" {
		return errAppOrigin
	}
	j := a.jar()
	j.mu.Lock()
	defer j.mu.Unlock()
	var keep []caps.Cookie
	for _, have := range j.cookies[origin] {
		if have.Name != c.Name {
			keep = append(keep, have)
		}
	}
	j.cookies[origin] = keep
	return nil
}

func (a *App) ClearSiteData(o caps.ClearSiteDataOptions) error {
	j := a.jar()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.cleared = append(j.cleared, o)
	if o.Everything() || o.Cookies {
		j.cookies = map[string][]caps.Cookie{}
	}
	return nil
}
