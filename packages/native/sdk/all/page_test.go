package all

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/cookies"
	"github.com/kratejs/krate/packages/native/sdk/downloads"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/page"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

type pageRig struct {
	host *sdk.Host
	plat *fakeplat.App
	main *fakeplat.Window
	c    *sdktest.Caller
}

func newPageRig(t *testing.T, perms ...string) *pageRig {
	t.Helper()
	isolate(t)
	set, err := permission.Parse(append([]string{"window"}, perms...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(scheme.NewRegistry(), Options{AppID: "com.acme.page"}); err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterAreas(reg, set)
	host := sdk.NewHost()
	plat := fakeplat.New()
	host.Attach(plat)
	t.Cleanup(host.Close)
	RegisterUI(reg, host, set, nil)
	w, err := plat.NewWindow(platform.WindowOptions{Title: "main", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	host.AddWindow("main", w)
	return &pageRig{host: host, plat: plat, main: w.(*fakeplat.Window), c: sdktest.New(t, reg)}
}

func (r *pageRig) addView(caller, window string, fields func(e *wire.Encoder)) sdktest.Result {
	return r.c.CallFrom(caller, "krate/window.addView", func(e *wire.Encoder) {
		e.String(window)
		e.BeginObject()
		if fields != nil {
			fields(e)
		}
		e.EndObject()
	})
}

func str(s string) func(e *wire.Encoder) { return func(e *wire.Encoder) { e.String(s) } }

func TestPageZoomPerView(t *testing.T) {
	r := newPageRig(t, "page")
	zoom := func(from, view string) float64 {
		t.Helper()
		res := r.c.CallFrom(from, "krate/page.zoom", str(view))
		if res.Failed {
			t.Fatalf("zoom: %+v", res)
		}
		f, err := res.OKDec.Float()
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if z := zoom("main", ""); z != 1 {
		t.Fatalf("default zoom %v", z)
	}
	res := r.addView("main", "main", func(e *wire.Encoder) { e.Key("id"); e.String("side") })
	if res.Failed {
		t.Fatal(res)
	}
	// An empty view is the caller's own; a named one is that view.
	if res := r.c.CallFrom("main", "krate/page.setZoom", func(e *wire.Encoder) { e.String(""); e.Float(1.5) }); res.Failed {
		t.Fatalf("setZoom: %+v", res)
	}
	if res := r.c.CallFrom("main", "krate/page.setZoom", func(e *wire.Encoder) { e.String("side"); e.Float(2) }); res.Failed {
		t.Fatalf("setZoom side: %+v", res)
	}
	if z := zoom("main", "main"); z != 1.5 {
		t.Fatalf("main zoom %v", z)
	}
	if z := zoom("side", ""); z != 2 {
		t.Fatalf("side zoom from its own page: %v", z)
	}
	for _, bad := range []float64{0.1, 9, 0} {
		res := r.c.CallFrom("main", "krate/page.setZoom", func(e *wire.Encoder) { e.String(""); e.Float(bad) })
		if res.ErrCode != "bad_args" {
			t.Errorf("zoom %v: %+v", bad, res)
		}
	}
	if res := r.c.CallFrom("main", "krate/page.zoom", str("nope")); res.ErrCode != "not_found" {
		t.Fatalf("missing view: %+v", res)
	}
}

func TestPagePrintFindCapture(t *testing.T) {
	r := newPageRig(t, "page")
	r.plat.FindMatches = 3

	res := r.c.CallFrom("main", "krate/page.print", str(""))
	if res.Failed {
		t.Fatalf("print: %+v", res)
	}
	res = r.c.CallFrom("main", "krate/page.printToPdf", func(e *wire.Encoder) {
		e.String("")
		e.BeginObject()
		e.Key("pageSize")
		e.String("Letter")
		e.Key("landscape")
		e.Bool(true)
		e.Key("printBackground")
		e.Bool(true)
		e.Key("margins")
		e.BeginObject()
		e.Key("top")
		e.Float(1)
		e.Key("right")
		e.Float(0.5)
		e.Key("bottom")
		e.Float(1)
		e.Key("left")
		e.Float(0.5)
		e.EndObject()
		e.EndObject()
	})
	if res.Failed {
		t.Fatalf("printToPdf: %+v", res)
	}
	pdf, err := res.OKDec.Bytes()
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf %q %v", pdf, err)
	}
	o := r.main.LastPDF()
	if o.PageWidth != 8.5 || o.PageHeight != 11 || !o.Landscape || !o.PrintBackground || o.Margins == nil || *o.Margins != (caps.Margins{Top: 1, Right: 0.5, Bottom: 1, Left: 0.5}) {
		t.Fatalf("options reached the platform as %+v", o)
	}
	// Defaults are A4 with 0.4 inch margins.
	if res := r.c.CallFrom("main", "krate/page.printToPdf", str("")); res.Failed {
		t.Fatal(res)
	}
	if o := r.main.LastPDF(); o.PageWidth != 8.27 || o.PageHeight != 11.69 || *o.Margins != caps.DefaultMargins {
		t.Fatalf("default options %+v", o)
	}
	for name, fields := range map[string]func(e *wire.Encoder){
		"unknown size": func(e *wire.Encoder) { e.Key("pageSize"); e.String("Quarto") },
		"negative":     func(e *wire.Encoder) { e.Key("pageWidth"); e.Float(-1); e.Key("pageHeight"); e.Float(4) },
	} {
		res := r.c.CallFrom("main", "krate/page.printToPdf", func(e *wire.Encoder) {
			e.String("")
			e.BeginObject()
			fields(e)
			e.EndObject()
		})
		if res.ErrCode != "bad_args" {
			t.Errorf("%s: %+v", name, res)
		}
	}

	got := make(chan page.FindEvent, 4)
	cancel, err := page.OnFind(func(e page.FindEvent) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	time.Sleep(100 * time.Millisecond)
	if res := r.c.CallFrom("main", "krate/page.find", func(e *wire.Encoder) {
		e.String("")
		e.String("needle")
		e.BeginObject()
		e.Key("matchCase")
		e.Bool(true)
		e.EndObject()
	}); res.Failed {
		t.Fatalf("find: %+v", res)
	}
	select {
	case e := <-got:
		if e.View != "main" || e.Text != "needle" || e.Matches != 3 || e.Active != 1 || !e.Final {
			t.Fatalf("find event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no find event")
	}
	if res := r.c.CallFrom("main", "krate/page.stopFind", str("")); res.Failed {
		t.Fatal(res)
	}

	res = r.c.CallFrom("main", "krate/page.capture", str(""))
	if png, err := res.OKDec.Bytes(); res.Failed || err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("capture: %+v %v", res, err)
	}
	calls := r.main.PageCalls()
	if len(calls) < 6 {
		t.Fatalf("platform calls: %v", calls)
	}
}

func TestPageNeedsPermission(t *testing.T) {
	r := newPageRig(t, "kv")
	for _, id := range []string{"krate/page.zoom", "krate/page.capture", "krate/page.stopFind", "krate/page.print"} {
		if res := r.c.CallFrom("main", id, str("")); res.ErrCode != "permission_denied" {
			t.Errorf("%s: %+v", id, res)
		}
	}
	for _, id := range []string{"krate/downloads.list", "krate/cookies.clearSiteData"} {
		if res := r.c.CallFrom("main", id, nil); res.ErrCode != "permission_denied" {
			t.Errorf("%s: %+v", id, res)
		}
	}
}

func TestDownloadsPolicy(t *testing.T) {
	r := newPageRig(t, "downloads")
	dir := filepath.Join(t.TempDir(), "dl")
	cfg := func(dir string, ask bool) sdktest.Result {
		return r.c.CallFrom("main", "krate/downloads.configure", func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("dir")
			e.String(dir)
			e.Key("ask")
			e.Bool(ask)
			e.EndObject()
		})
	}
	if res := cfg("relative/dir", false); res.ErrCode != "bad_args" {
		t.Fatalf("relative dir: %+v", res)
	}
	if res := cfg(dir, false); res.Failed {
		t.Fatalf("configure: %+v", res)
	}
	req := caps.DownloadRequest{ID: 1, View: r.main.PrimaryView(), URL: "krate://app/f", SuggestedName: "../report.pdf", Total: 10}
	first := r.plat.StartDownload(req)
	if first != filepath.Join(dir, ".._report.pdf") && filepath.Dir(first) != dir {
		t.Fatalf("destination %q", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.ID, req.SuggestedName = 2, "a.txt"
	a := r.plat.StartDownload(req)
	if a != filepath.Join(dir, "a.txt") {
		t.Fatalf("a.txt placed at %q", a)
	}
	os.WriteFile(a, []byte("x"), 0o644)
	req.ID = 3
	if b := r.plat.StartDownload(req); b != filepath.Join(dir, "a (1).txt") {
		t.Fatalf("second a.txt placed at %q", b)
	}
	if len(r.plat.FileOpts) != 0 {
		t.Fatalf("a silent download asked: %+v", r.plat.FileOpts)
	}

	// Asking: the save dialog gets the suggested name and the folder.
	cfg(dir, true)
	r.plat.FileResult = []string{filepath.Join(dir, "chosen.txt")}
	req.ID, req.SuggestedName = 4, "pick.txt"
	if got := r.plat.StartDownload(req); got != filepath.Join(dir, "chosen.txt") {
		t.Fatalf("dialog choice not used: %q", got)
	}
	o := r.plat.FileOpts[len(r.plat.FileOpts)-1]
	if o.Mode != caps.SaveFile || o.Name != "pick.txt" || o.Dir != dir || !o.ConfirmWrite {
		t.Fatalf("save dialog options %+v", o)
	}
	// Declining the dialog cancels the download.
	r.plat.FileResult = nil
	req.ID = 5
	if got := r.plat.StartDownload(req); got != "" {
		t.Fatalf("declined dialog gave %q", got)
	}
	// With nothing configured, every download asks.
	cfg("", false)
	r.plat.FileResult = []string{"/x/y.txt"}
	req.ID = 6
	if got := r.plat.StartDownload(req); got != "/x/y.txt" {
		t.Fatalf("default policy: %q", got)
	}
}

func TestDownloadsEventsListCancel(t *testing.T) {
	r := newPageRig(t, "downloads")
	got := make(chan downloads.Event, 16)
	cancel, err := downloads.OnEvent(func(e downloads.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	time.Sleep(100 * time.Millisecond)
	view := r.main.PrimaryView()
	r.plat.StartDownload(caps.DownloadRequest{ID: 7, View: view, URL: "krate://app/big", SuggestedName: "big.bin"})
	r.plat.EmitDownload(caps.DownloadEvent{ID: 7, View: view, State: caps.DownloadStarted, URL: "krate://app/big", Path: "/d/big.bin", Total: 100})
	r.plat.EmitDownload(caps.DownloadEvent{ID: 7, View: view, State: caps.DownloadProgress, URL: "krate://app/big", Path: "/d/big.bin", Received: 50, Total: 100})
	for _, want := range []downloads.State{downloads.Started, downloads.Progress} {
		select {
		case e := <-got:
			if e.State != want || e.ID != 7 || e.View != "main" || e.Path != "/d/big.bin" || e.Total != 100 {
				t.Fatalf("event %+v, want state %s", e, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s event", want)
		}
	}
	list, err := downloads.List()
	if err != nil || len(list) != 1 || list[0].State != downloads.Progress || list[0].Received != 50 {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := downloads.Cancel(7); err != nil {
		t.Fatal(err)
	}
	if c := r.plat.Cancelled; len(c) != 1 || c[0] != 7 {
		t.Fatalf("platform cancels %v", c)
	}
	r.plat.EmitDownload(caps.DownloadEvent{ID: 7, View: view, State: caps.DownloadCancelled, Path: "/d/big.bin"})
	<-got
	if err := downloads.Cancel(7); err == nil {
		t.Fatal("cancelling a finished download succeeded")
	}
	if res := r.c.CallFrom("main", "krate/downloads.cancel", func(e *wire.Encoder) { e.Int(0) }); res.ErrCode != "bad_args" {
		t.Fatalf("id 0: %+v", res)
	}
	list, _ = downloads.List()
	if len(list) != 1 || list[0].State != downloads.Cancelled {
		t.Fatalf("list after cancel %+v", list)
	}
}

func TestCookies(t *testing.T) {
	r := newPageRig(t, "cookies")
	const url = "https://api.example.com"
	if err := cookies.Set(url, cookies.Cookie{Name: "sid", Value: "1", HTTPOnly: true, SameSite: "lax", Expires: time.UnixMilli(1893456000000)}); err != nil {
		t.Fatal(err)
	}
	if err := cookies.Set(url, cookies.Cookie{Name: "theme", Value: "dark"}); err != nil {
		t.Fatal(err)
	}
	list, err := cookies.List(url)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	c, ok, err := cookies.Get(url, "sid")
	if err != nil || !ok || c.Value != "1" || !c.HTTPOnly || c.SameSite != "lax" || c.Expires.UnixMilli() != 1893456000000 {
		t.Fatalf("get %+v %v %v", c, ok, err)
	}
	if _, ok, err := cookies.Get(url, "missing"); ok || err != nil {
		t.Fatalf("get missing: %v %v", ok, err)
	}
	if err := cookies.Delete(url, "sid"); err != nil {
		t.Fatal(err)
	}
	if list, _ := cookies.List(url); len(list) != 1 || list[0].Name != "theme" {
		t.Fatalf("after delete %+v", list)
	}
	// The app origin is the default and has no cookies on this platform.
	if res := r.c.CallFrom("main", "krate/cookies.list", nil); res.ErrCode != "unsupported" {
		t.Fatalf("app origin: %+v", res)
	}
	if res := r.c.CallFrom("main", "krate/cookies.list", str("not a url")); res.ErrCode != "bad_args" {
		t.Fatalf("bad url: %+v", res)
	}
	if res := r.c.CallFrom("main", "krate/cookies.set", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("name")
		e.String("a")
		e.Key("sameSite")
		e.String("sometimes")
		e.EndObject()
	}); res.ErrCode != "bad_args" {
		t.Fatalf("bad sameSite: %+v", res)
	}
	if res := r.c.CallFrom("main", "krate/cookies.set", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("value")
		e.String("v")
		e.EndObject()
	}); res.ErrCode != "bad_args" {
		t.Fatalf("nameless cookie: %+v", res)
	}
	if err := cookies.ClearSiteData(cookies.ClearOptions{Storage: true}); err != nil {
		t.Fatal(err)
	}
	if list, _ := cookies.List(url); len(list) != 1 {
		t.Fatalf("clearing storage removed cookies: %+v", list)
	}
	if err := cookies.ClearSiteData(cookies.ClearOptions{}); err != nil {
		t.Fatal(err)
	}
	if list, _ := cookies.List(url); len(list) != 0 {
		t.Fatalf("clearing everything left cookies: %+v", list)
	}
	if cl := r.plat.Cleared(); len(cl) != 2 || !cl[0].Storage || cl[0].Cookies {
		t.Fatalf("platform clears %+v", cl)
	}
}
