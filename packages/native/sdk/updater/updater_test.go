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
	"runtime"
	"strings"
	"testing"
	"time"

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
	sig, _ := updater.Sign(k, updater.Meta{AppID: "com.acme.upd", Target: runtime.GOOS + "-" + runtime.GOARCH, Version: version}, bytes.NewReader(art))
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(updater.Feed{Version: version, Notes: "hello", URL: srv.URL + "/a.tar.gz", Signature: sig, Size: int64(len(art))}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("/a.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(art); err != nil {
			t.Error(err)
		}
	})
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
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), `"stream"`, `"Stream"`)), &res); err != nil {
		t.Fatal(err)
	}
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
	if err := filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, "a.tar.gz") {
			found = p
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
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

// seedPending writes the state of an applied but unconfirmed update to 1.0.0
// (the version isolate configures) with real kept files.
func seedPending(t *testing.T, starts int) (dir, install string) {
	t.Helper()
	dir, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	install = t.TempDir()
	prev := filepath.Join(install, ".krate-prev")
	for p, body := range map[string]string{
		filepath.Join(install, "app"):     "new binary",
		filepath.Join(prev, "app"):        "old binary",
		filepath.Join(install, "keep.md"): "x",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st := updater.State{
		Phase: updater.PhasePending, From: "0.9.0", To: "1.0.0", Starts: starts,
		Prev: &updater.PrevInfo{Kind: updater.KindTree, Target: install, Exe: filepath.Join(install, "app"), Dir: prev, Placed: []string{"app"}, Trashed: []string{"app"}},
	}
	b, _ := json.Marshal(st)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, install
}

func TestBootRollsBackAfterUnconfirmedStart(t *testing.T) {
	isolate(t)
	t.Setenv(NoRelaunchEnv, "1")
	Configure(Config{URL: "https://x/feed", PublicKey: "k"})
	dir, install := seedPending(t, 1)
	code := -1
	exit = func(c int) { code = c }
	defer func() { exit = os.Exit }()
	Boot(nil)
	if code != 0 {
		t.Fatalf("exit code %d, want 0 after a rollback", code)
	}
	if b, _ := os.ReadFile(filepath.Join(install, "app")); string(b) != "old binary" {
		t.Fatalf("app = %q", b)
	}
	c := sdktest.New(t, registry(t, "updater"))
	r := c.Call("krate/updater.status", nil)
	if r.Failed || !strings.Contains(r.OK, `"failed":"1.0.0"`) || !strings.Contains(r.OK, `"restored":"0.9.0"`) || !strings.Contains(r.OK, `"unconfirmed":false`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/updater.ackRollback", nil); r.Failed {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/updater.status", nil); !strings.Contains(r.OK, `"rollback":null`) {
		t.Fatalf("%+v", r)
	}
	if st := updater.LoadState(dir); st.Event != nil || len(st.Failed) != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestBootFirstStartIsWatchedAndConfirms(t *testing.T) {
	isolate(t)
	t.Setenv(GraceEnv, "0")
	Configure(Config{URL: "https://x/feed", PublicKey: "k"})
	dir, install := seedPending(t, 0)
	code := -1
	exit = func(c int) { code = c }
	defer func() { exit = os.Exit }()
	Boot(nil)
	if code != -1 {
		t.Fatal("a first start must not roll back")
	}
	c := sdktest.New(t, registry(t, "updater"))
	if r := c.Call("krate/updater.status", nil); !strings.Contains(r.OK, `"unconfirmed":true`) {
		t.Fatalf("%+v", r)
	}
	// The page being served confirms after the (zero) grace period.
	PageServed()
	deadline := time.Now().Add(5 * time.Second)
	for updater.LoadState(dir).Phase != updater.PhaseIdle {
		if time.Now().After(deadline) {
			t.Fatal("start never confirmed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The state is cleared before the kept copy is deleted (so a crash in
	// between never loses the rollback target); wait for the deletion too.
	for {
		if _, err := os.Stat(filepath.Join(install, ".krate-prev")); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kept previous version survived the confirmation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := c.Call("krate/updater.markHealthy", nil); r.Failed {
		t.Fatalf("markHealthy when nothing is pending must succeed: %+v", r)
	}
}

func TestManualConfirmIgnoresPageServedButCleanExitCountsOnlyWhenAuto(t *testing.T) {
	isolate(t)
	t.Setenv(GraceEnv, "0")
	Configure(Config{URL: "https://x/feed", PublicKey: "k", ManualConfirm: true})
	dir, _ := seedPending(t, 0)
	Boot(nil)
	PageServed()
	CleanExit()
	time.Sleep(100 * time.Millisecond)
	if updater.LoadState(dir).Phase != updater.PhasePending {
		t.Fatal("confirm \"app\" was confirmed by the shell")
	}
	c := sdktest.New(t, registry(t, "updater"))
	if r := c.Call("krate/updater.markHealthy", nil); r.Failed {
		t.Fatalf("%+v", r)
	}
	if updater.LoadState(dir).Phase != updater.PhaseIdle {
		t.Fatal("markHealthy did not confirm")
	}
}

func TestCleanExitAfterPageServedConfirms(t *testing.T) {
	isolate(t)
	t.Setenv(GraceEnv, "600000")
	Configure(Config{URL: "https://x/feed", PublicKey: "k"})
	dir, _ := seedPending(t, 0)
	Boot(nil)
	CleanExit()
	if updater.LoadState(dir).Phase != updater.PhasePending {
		t.Fatal("a quit before any page was served confirmed the start")
	}
	PageServed()
	CleanExit()
	if updater.LoadState(dir).Phase != updater.PhaseIdle {
		t.Fatal("a normal quit after the first page did not confirm")
	}
}

func TestSetChannelPersistsAndFillsTheURL(t *testing.T) {
	isolate(t)
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()
	Configure(Config{URL: srv.URL + "/{channel}/latest.json", PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="})
	c := sdktest.New(t, registry(t, "updater"))
	c.Call("krate/updater.check", nil)
	if seen != "/stable/latest.json" {
		t.Fatalf("default channel path %q", seen)
	}
	if r := c.Call("krate/updater.setChannel", str("beta")); r.Failed {
		t.Fatalf("%+v", r)
	}
	c.Call("krate/updater.check", nil)
	if seen != "/beta/latest.json" {
		t.Fatalf("beta channel path %q", seen)
	}
	if r := c.Call("krate/updater.status", nil); !strings.Contains(r.OK, `"channel":"beta"`) {
		t.Fatalf("%+v", r)
	}
	if r := c.Call("krate/updater.setChannel", str("../x")); r.ErrCode != "bad_args" {
		t.Fatalf("%+v", r)
	}
}

func TestNewIDsNeedThePermission(t *testing.T) {
	isolate(t)
	c := sdktest.New(t, registry(t, "kv"))
	for _, id := range []string{"krate/updater.markHealthy", "krate/updater.status", "krate/updater.ackRollback", "krate/updater.setChannel"} {
		if r := c.Call(id, nil); r.ErrCode != "permission_denied" {
			t.Errorf("%s: %+v", id, r)
		}
	}
}

func str(s string) func(*wire.Encoder) {
	return func(e *wire.Encoder) {
		e.String(s)
	}
}

// TestBootCleansUpOnlyWithoutPendingUpdate: the leftovers of an earlier update
// are removed at a start with nothing pending, never while an unconfirmed
// start still needs the kept previous version.
func TestBootCleansUpOnlyWithoutPendingUpdate(t *testing.T) {
	isolate(t)
	cleaned := make(chan struct{}, 4)
	cleanupStale = func(string) { cleaned <- struct{}{} }
	defer func() { cleanupStale = updater.CleanupStale }()
	exit = func(int) {}
	defer func() { exit = os.Exit }()

	Configure(Config{URL: "https://x/feed", PublicKey: "k"})
	seedPending(t, 0)
	Boot(nil)
	select {
	case <-cleaned:
		t.Fatal("cleaned up during an unconfirmed first start")
	case <-time.After(200 * time.Millisecond):
	}

	isolate(t)
	Configure(Config{URL: "https://x/feed", PublicKey: "k"})
	Boot(nil)
	select {
	case <-cleaned:
	case <-time.After(5 * time.Second):
		t.Fatal("no cleanup at a start with nothing pending")
	}
}
