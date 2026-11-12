package shell

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

func embeddedFS(withHost bool) fstest.MapFS {
	m := fstest.MapFS{
		"app/app.json":        {Data: []byte(`{"appId":"dev.krate.embedded","productName":"Embedded","version":"2.3.4","permissions":["kv"],"strictPermissions":true,"hostjs":{"bundle":"host/host.js"}}`)},
		"app/dist/index.html": {Data: []byte("<!doctype html><html><head><title>t</title></head><body>hello</body></html>")},
		"app/dist/asset.txt":  {Data: []byte("plain")},
		"app/host/host.js":    {Data: []byte(hostJSv1)},
	}
	if !withHost {
		m["app/app.json"] = &fstest.MapFile{Data: []byte(`{"appId":"dev.krate.embedded","productName":"Embedded","version":"2.3.4","permissions":["kv"],"strictPermissions":true}`)}
		delete(m, "app/host/host.js")
	}
	return m
}

func runEmbeddedBrowser(t *testing.T, fsys fstest.MapFS, glue func(*scheme.Registry)) (*Control, *http.Client) {
	t.Helper()
	return runEmbeddedBrowserFS(t, fsys, glue)
}

func runEmbeddedBrowserFS(t *testing.T, fsys fs.FS, glue ...func(*scheme.Registry)) (*Control, *http.Client) {
	t.Helper()
	var g func(*scheme.Registry)
	if len(glue) > 0 {
		g = glue[0]
	}
	var out bytes.Buffer
	var omu sync.Mutex
	ready := make(chan *Control, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunEmbedded(fsys, RunOptions{
			Browser: true,
			Out:     lockedBuf{&omu, &out},
			Logf:    t.Logf,
			GoGlue:  g,
			OnReady: func(c *Control) { ready <- c },
		})
	}()
	var ctl *Control
	select {
	case ctl = <-ready:
	case err := <-done:
		t.Fatalf("RunEmbedded returned early: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("not ready")
	}
	t.Cleanup(func() {
		ctl.Quit()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("RunEmbedded did not return after Quit")
		}
	})
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	// The token URL sets the cookie.
	resp, err := c.Get(ctl.URL())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return ctl, c
}

func TestRunEmbeddedServesGlueAndHost(t *testing.T) {
	glue := func(r *scheme.Registry) {
		r.Func("g.Hi", func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
			if err := args.BeginArray(); err != nil {
				return err
			}
			if err := args.EndArray(); err != nil {
				return err
			}
			out.String("hi from glue")
			return nil
		})
	}
	ctl, c := runEmbeddedBrowser(t, embeddedFS(hasJSHost), glue)
	base := strings.TrimSuffix(strings.SplitN(ctl.URL(), "?", 2)[0], "/")

	resp, body := get(t, c, base+"/", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "hello") {
		t.Fatalf("index: %d %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "__KRATE_WINDOW__") || strings.Index(body, "__KRATE_WINDOW__") > strings.Index(body, "<title>") {
		t.Fatalf("the window id script is not first in head: %q", body)
	}
	if _, plain := get(t, c, base+"/asset.txt", nil); plain != "plain" {
		t.Fatalf("a non-HTML asset was changed: %q", plain)
	}
	if got := callVia(t, c, base, "g.Hi", nil); !strings.Contains(got, "hi from glue") {
		t.Fatalf("glue call: %s", got)
	}
	if hasJSHost {
		got := callVia(t, c, base, "h.add", func(e *wire.Encoder) { e.Int(2); e.Int(3) })
		if !strings.Contains(got, "5") {
			t.Fatalf("host call: %s", got)
		}
	}
	// The strict manifest grants only kv: db is denied, kv is not.
	if got := callVia(t, c, base, "krate/db.query", func(e *wire.Encoder) { e.String("select 1") }); !strings.Contains(got, "permission_denied") {
		t.Fatalf("db should be denied: %s", got)
	}
	if got := callVia(t, c, base, "krate/kv.keys", nil); strings.Contains(got, "permission_denied") {
		t.Fatalf("kv should be granted: %s", got)
	}
}

func TestRunEmbeddedErrors(t *testing.T) {
	// The embedded app may sit at the root of the file system too.
	fsys := fstest.MapFS{"app.json": {Data: []byte(`{"appId":"dev.krate.root"}`)}}
	if err := RunEmbedded(fsys, RunOptions{Browser: true, Logf: t.Logf}); err == nil || !strings.Contains(err.Error(), "assets") {
		t.Fatalf("missing assets: %v", err)
	}
	if err := RunEmbedded(fstest.MapFS{}, RunOptions{Browser: true, Logf: t.Logf}); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("missing manifest: %v", err)
	}
}

func TestRunMainVersionAndFlags(t *testing.T) {
	var out bytes.Buffer
	if code := RunMain([]string{"--version"}, embeddedFS(false), nil, &out); code != 0 || strings.TrimSpace(out.String()) != "Embedded 2.3.4" {
		t.Fatalf("--version: %d %q", code, out.String())
	}
	if code := RunMain([]string{"--bogus"}, embeddedFS(false), nil, &out); code != 2 {
		t.Fatalf("unknown flag: %d", code)
	}
}

func TestInjectIntoHead(t *testing.T) {
	cases := map[string]string{
		"<!doctype html><HTML><HEAD><title>x</title></HEAD></HTML>": "<!doctype html><HTML><HEAD>" + windowIDScript + "<title>x</title></HEAD></HTML>",
		"<!doctype html><html><body>b</body></html>":                "<!doctype html><html>" + windowIDScript + "<body>b</body></html>",
		"<!doctype html>just text":                                  "<!doctype html>" + windowIDScript + "just text",
		"no markup":                                                 windowIDScript + "no markup",
	}
	for in, want := range cases {
		if got := string(injectIntoHead([]byte(in))); got != want {
			t.Errorf("injectIntoHead(%q) = %q", in, got)
		}
	}
}

func TestZipFSServesDynamicRouteNames(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"app.json":                   `{"appId":"dev.krate.zip","fallbacks":[{"pattern":"/tasks/[id]","file":"tasks/[id]/index.html"}],"permissions":["kv"],"strictPermissions":true}`,
		"dist/index.html":            "<!doctype html><head></head>home",
		"dist/tasks/[id]/index.html": "<!doctype html><head></head>task page",
	} {
		w, _ := zw.Create(name)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	fsys, err := ZipFS(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ZipFS([]byte("not a zip")); err == nil {
		t.Fatal("a damaged archive must fail")
	}
	// The zip stands in for the embedded app directory.
	ctl, c := runEmbeddedBrowserFS(t, fsys)
	base := strings.TrimSuffix(strings.SplitN(ctl.URL(), "?", 2)[0], "/")
	if _, body := get(t, c, base+"/tasks/7", nil); !strings.Contains(body, "task page") {
		t.Fatalf("dynamic fallback from the archive: %q", body)
	}
}

func TestWindowIDHandlerWithDevScript(t *testing.T) {
	const page = "<!doctype html><html><head></head><body>b</body></html>"
	h := windowIDHandlerWith(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		_, _ = w.Write([]byte(page))
	}), windowIDScript+devErrorsScript)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "__krateDriverErrs") || !strings.Contains(body, "__krate_win") {
		t.Fatalf("scripts missing: %s", body)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("Content-Length %s, body %d bytes", got, len(body))
	}
}
