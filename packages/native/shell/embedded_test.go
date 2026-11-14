package shell

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
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
