package sdkupdater

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/updater"
	"github.com/kratejs/krate/packages/native/wire"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "r"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "l"))
	if err := sdkos.Configure(sdkos.App{ID: "com.acme.upd", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	allowHTTP = true
	t.Cleanup(func() { allowHTTP = false; Configure(Config{}) })
	return home
}

func registry(t *testing.T, perms ...string) *scheme.Registry {
	t.Helper()
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	r := scheme.NewRegistry()
	Register(r, set)
	return r
}

func feed(t *testing.T, version string) (*httptest.Server, string) {
	t.Helper()
	priv, pub, err := updater.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	k, _ := updater.PrivateKeyFromSeed(priv)
	art := bytes.Repeat([]byte("update"), 5000)
	sig, _ := updater.Sign(k, bytes.NewReader(art))
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(updater.Feed{Version: version, Notes: "hello", URL: srv.URL + "/a.tar.gz", Signature: sig, Size: int64(len(art))})
	})
	mux.HandleFunc("/a.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(art) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, pub
}

func TestUnconfigured(t *testing.T) {
	isolate(t)
	c := sdktest.New(t, registry(t, "updater"))
	if r := c.Call("krate/updater.check", nil); r.ErrCode != "unsupported" {
		t.Fatalf("%+v", r)
	}
}

func TestDeniedWithoutPermission(t *testing.T) {
	isolate(t)
	c := sdktest.New(t, registry(t, "kv"))
	for _, id := range []string{"krate/updater.check", "krate/updater.apply", "krate/updater.relaunch"} {
		r := c.Call(id, nil)
		if r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"updater"}` {
			t.Errorf("%s: %+v", id, r)
		}
	}
}

func TestCheckThenApplyNeedsDownload(t *testing.T) {
	isolate(t)
	srv, pub := feed(t, "1.1.0")
	Configure(Config{URL: srv.URL + "/feed", PublicKey: pub})
	c := sdktest.New(t, registry(t, "updater"))
	if r := c.Call("krate/updater.apply", nil); r.ErrCode != "no_update" {
		t.Fatalf("apply before check: %+v", r)
	}
	r := c.Call("krate/updater.check", nil)
	if r.Failed || !strings.Contains(r.OK, `"version":"1.1.0"`) || !strings.Contains(r.OK, `"notes":"hello"`) || !strings.Contains(r.OK, `"size":30000`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/updater.apply", nil); r.ErrCode != "not_downloaded" {
		t.Fatalf("apply before download: %+v", r)
	}
}

func TestCheckUpToDate(t *testing.T) {
	isolate(t)
	srv, pub := feed(t, "1.0.0")
	Configure(Config{URL: srv.URL + "/feed", PublicKey: pub})
	c := sdktest.New(t, registry(t, "updater"))
	if r := c.Call("krate/updater.check", nil); r.Failed || r.OK != "null" {
		t.Fatalf("%+v", r)
	}
}

func TestDownloadStream(t *testing.T) {
	home := isolate(t)
	srv, pub := feed(t, "1.1.0")
	Configure(Config{URL: srv.URL + "/feed", PublicKey: pub})
	reg := registry(t, "updater")
	c := sdktest.New(t, reg)
	if r := c.Call("krate/updater.check", nil); r.Failed {
		t.Fatalf("%+v", r)
	}
	ui := httptest.NewServer(scheme.New(scheme.Options{Registry: reg}))
	defer ui.Close()

	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String("krate/updater.download")
	e.Key("args")
	e.BeginArray()
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	resp, err := http.Post(ui.URL+"/__krate/call", "application/x-krate-frames", bytes.NewReader(e.Frame()))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := wire.ReadFrame(resp.Body)
	raw, _ := d.RawValue()
	resp.Body.Close()
	var res struct{ Stream string }
	json.Unmarshal([]byte(strings.ReplaceAll(string(raw), `"stream"`, `"Stream"`)), &res)
	if res.Stream == "" {
		t.Fatalf("no stream: %s", raw)
	}
	sresp, err := http.Get(ui.URL + "/__krate/stream/" + res.Stream)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	last := ""
	for {
		fd, err := wire.ReadFrame(sresp.Body)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := fd.RawValue()
		if strings.Contains(string(raw), `"end"`) {
			break
		}
		if strings.Contains(string(raw), `"err"`) {
			t.Fatalf("stream error %s", raw)
		}
		last = string(raw)
	}
	if !strings.Contains(last, `"done":30000`) || !strings.Contains(last, `"total":30000`) {
		t.Fatalf("last progress frame %s", last)
	}
	var found string
	filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, "a.tar.gz") {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("verified file not staged under the cache dir")
	}
}

func TestDownloadStreamHonorsWindowScope(t *testing.T) {
	isolate(t)
	srv, pub := feed(t, "1.1.0")
	Configure(Config{URL: srv.URL + "/feed", PublicKey: pub})
	set, err := permission.Parse([]string{"updater", "kv"})
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	Register(reg, set)
	if err := set.Scope("narrow", []string{"kv"}); err != nil {
		t.Fatal(err)
	}
	e := wire.NewEncoder()
	e.BeginArray()
	e.EndArray()
	args, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	ctx := krate.WithWindow(context.Background(), "narrow")
	err = reg.InvokeStream(ctx, "krate/updater.download", args, func([]byte) error { return nil })
	var ke *krate.Error
	if !errors.As(err, &ke) || ke.Code != "permission_denied" {
		t.Fatalf("a window scoped without updater reached the download stream: %v", err)
	}
}
