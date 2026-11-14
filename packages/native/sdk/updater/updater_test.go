package sdkupdater

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
