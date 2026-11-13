package build

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func devPage(name string) string {
	return "export default function Page() { return <h1>" + name + "</h1>; }\n"
}

// startDevWatch builds a project and starts Watch on it, returning the root,
// the hub's event channel and a stop func.
func startDevWatch(t *testing.T, files map[string]string, mutate func(*config.Config)) (string, *Builder, <-chan DevEvent) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	if mutate != nil {
		mutate(cfg)
	}
	b := New(root, cfg)
	b.DevMode = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	hub := NewDevHub()
	events, cancel := hub.Subscribe()
	t.Cleanup(cancel)
	// Stop the watcher and wait for it before t.TempDir's cleanup, which runs
	// later: a rebuild in flight would otherwise write into the removed tree.
	ctx, stop := context.WithCancel(context.Background())
	b.SetContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Watch(b, 30*time.Millisecond, hub)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	time.Sleep(300 * time.Millisecond)
	return root, b, events
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestWatchPicksUpNewNestedDirectory(t *testing.T) {
	root, _, _ := startDevWatch(t, map[string]string{"src/pages/index.tsx": devPage("home")}, nil)
	deep := filepath.Join(root, "src", "pages", "blog", "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "index.tsx"), []byte(devPage("deep")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new nested page output", func() bool { return exists(filepath.Join(root, "dist", "blog", "deep", "index.html")) })

	// Files written into the new directory afterwards are watched too.
	if err := os.WriteFile(filepath.Join(deep, "second.tsx"), []byte(devPage("second")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "page added to a new directory after creation", func() bool {
		return exists(filepath.Join(root, "dist", "blog", "deep", "second", "index.html")) || exists(filepath.Join(root, "dist", "blog", "deep", "second.html"))
	})
}

func httpGet(t *testing.T, url, accept string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestServeDevRestartsOnRequestAndShowsErrorShell(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Server.Host = "127.0.0.1"
	port := freePort(t)
	cfg.DevServer.Port = port
	if err := os.MkdirAll(cfg.PagesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No pages and a published build error: the first build "failed".
	hub := NewDevHub()
	hub.Publish(DevEvent{Type: "reload", Diagnostics: []diag.Diagnostic{{File: "src/pages/index.tsx", Line: 3, Message: "boom <b>", Hint: "fix it"}}})

	errc := make(chan error, 1)
	go func() { errc <- ServeDev(root, cfg, hub, time.Now()) }()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitFor(t, "dev server up", func() bool {
		code, _ := httpGet(t, base+"/healthz", "")
		return code == 200
	})

	code, body := httpGet(t, base+"/", "text/html")
	if code == 200 && strings.Contains(body, "Index of") {
		t.Fatalf("failed build must not serve a directory listing:\n%s", body)
	}
	for _, want := range []string{"Build failed", "boom &lt;b&gt;", "hint: fix it", "/__krate/dev.js", "__KRATE_DEV__"} {
		if !strings.Contains(body, want) {
			t.Errorf("error shell missing %q:\n%s", want, body)
		}
	}
	if code, js := httpGet(t, base+"/__krate/dev.js", ""); code != 200 || js == "" {
		t.Errorf("dev bundle endpoint = %d", code)
	}

	// A clean rebuild stops the shell.
	hub.Publish(DevEvent{Type: "reload"})
	if _, body := httpGet(t, base+"/", "text/html"); strings.Contains(body, "Build failed") {
		t.Errorf("shell still served after the build recovered")
	}

	// Port is taken while it runs, free again after a restart request.
	if _, err := bindServerListener("127.0.0.1", port, false); err == nil {
		t.Error("expected the port to be in use while the dev server runs")
	}
	hub.RequestRestart()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrDevRestart) {
			t.Fatalf("ServeDev returned %v, want ErrDevRestart", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ServeDev did not shut down on a restart request")
	}
	ln, err := bindServerListener("127.0.0.1", port, false)
	if err != nil {
		t.Fatalf("port not released after restart: %v", err)
	}
	_ = ln.Close()
}
