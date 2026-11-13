package build

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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

func TestWatchRenamedDirectoryMovesRoutes(t *testing.T) {
	root, _, _ := startDevWatch(t, map[string]string{
		"src/pages/index.tsx":       devPage("home"),
		"src/pages/later/index.tsx": devPage("later"),
	}, nil)
	waitFor(t, "initial later page", func() bool { return exists(filepath.Join(root, "dist", "later", "index.html")) })
	if err := os.Rename(filepath.Join(root, "src", "pages", "later"), filepath.Join(root, "src", "pages", "later2")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "renamed route output", func() bool {
		return exists(filepath.Join(root, "dist", "later2", "index.html")) && !exists(filepath.Join(root, "dist", "later", "index.html"))
	})
	// Edits inside the renamed directory are picked up.
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "later2", "index.tsx"), []byte(devPage("edited-later")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edit inside renamed dir", func() bool {
		data, _ := os.ReadFile(filepath.Join(root, "dist", "later2", "index.html"))
		return strings.Contains(string(data), "edited-later")
	})
}

func TestWatchDeletedPageIsDropped(t *testing.T) {
	root, _, events := startDevWatch(t, map[string]string{
		"src/pages/index.tsx": devPage("home"),
		"src/pages/newp.tsx":  devPage("newp"),
	}, nil)
	waitFor(t, "initial newp", func() bool {
		return exists(filepath.Join(root, "dist", "newp", "index.html")) || exists(filepath.Join(root, "dist", "newp.html"))
	})
	removeWatched(t, filepath.Join(root, "src", "pages", "newp.tsx"))
	select {
	case ev := <-events:
		if !ev.BuildOK {
			t.Fatalf("deleting a page must not leave a build error: %+v", ev.Diagnostics)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no reload event after deleting a page")
	}
	if exists(filepath.Join(root, "dist", "newp", "index.html")) || exists(filepath.Join(root, "dist", "newp.html")) {
		t.Fatal("deleted page output still served")
	}
}

func TestWatchMirrorsPublicDirectory(t *testing.T) {
	root, _, _ := startDevWatch(t, map[string]string{
		"src/pages/index.tsx": devPage("home"),
		"public/keep.txt":     "keep",
	}, nil)
	if err := os.WriteFile(filepath.Join(root, "public", "new.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new public file", func() bool { return exists(filepath.Join(root, "dist", "new.txt")) })
	if err := os.WriteFile(filepath.Join(root, "public", "new.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edited public file", func() bool {
		d, _ := os.ReadFile(filepath.Join(root, "dist", "new.txt"))
		return string(d) == "two"
	})
	removeWatched(t, filepath.Join(root, "public", "new.txt"))
	waitFor(t, "deleted public file", func() bool { return !exists(filepath.Join(root, "dist", "new.txt")) })
}

func TestWatchConfigAndEnvRequestRestart(t *testing.T) {
	for _, name := range []string{"krate.config.ts", ".env", ".env.local", "tailwind.config.js"} {
		t.Run(name, func(t *testing.T) {
			root, b, _ := startDevWatch(t, map[string]string{"src/pages/index.tsx": devPage("home")}, nil)
			got := make(chan string, 1)
			b.DevReload = func(reason string) bool { got <- reason; return true }
			if err := os.WriteFile(filepath.Join(root, name), []byte("A=1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-got:
				if r != name {
					t.Fatalf("reason = %q, want %q", r, name)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("no restart request for %s", name)
			}
		})
	}
}

func TestIsManagedGoFileUsesConfiguredDirs(t *testing.T) {
	cfg := config.Default()
	cfg.Go.APIDirs = []string{"server/handlers"}
	b := New(t.TempDir(), cfg)
	if !b.isManagedGoFile("/p/server/handlers/users.go") {
		t.Fatal("go.apiDirs entry must be managed")
	}
	if !b.isManagedGoFile("/p/src/components/x.go") {
		t.Fatal("component dir must be managed")
	}
	if b.isManagedGoFile("/p/src/api/ignored.go") {
		t.Fatal("default src/api is not managed once go.apiDirs overrides it")
	}
}

func TestDevHubReloadHookRunsOncePerPublish(t *testing.T) {
	h := NewDevHub()
	n := 0
	h.SetReloadHook(func(DevEvent) { n++ })
	_, c1 := h.Subscribe()
	defer c1()
	_, c2 := h.Subscribe()
	defer c2()
	h.Publish(DevEvent{Type: "reload"})
	h.Publish(DevEvent{Type: "client-error"})
	if n != 1 {
		t.Fatalf("hook ran %d times, want 1", n)
	}
}

func TestEditorArgs(t *testing.T) {
	cases := []struct {
		exe  string
		user []string
		want string
	}{
		{"code", nil, "-g /f.tsx:12"},
		{"code", []string{"-g"}, "-g /f.tsx:12"},
		{"cursor", nil, "-g /f.tsx:12"},
		{"webstorm", nil, "--line 12 /f.tsx"},
		{"subl", nil, "/f.tsx:12"},
		{"vim", nil, "+12 /f.tsx"},
		{"zed", nil, "/f.tsx:12"},
	}
	for _, c := range cases {
		got := ""
		for i, a := range editorArgs(c.exe, c.user, "/f.tsx", "12") {
			if i > 0 {
				got += " "
			}
			got += a
		}
		if got != c.want {
			t.Errorf("%s %v: got %q want %q", c.exe, c.user, got, c.want)
		}
	}
}

func TestBindServerListenerDevMovesToNextPort(t *testing.T) {
	first, err := bindServerListener("127.0.0.1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	taken := first.Addr().(*net.TCPAddr).Port
	if _, err := bindServerListener("127.0.0.1", taken, false); err == nil {
		t.Fatal("serve must fail on a taken port")
	}
	ln, err := bindServerListener("127.0.0.1", taken, true)
	if err != nil {
		t.Fatalf("dev must auto-increment: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if ln.Addr().(*net.TCPAddr).Port == taken {
		t.Fatal("dev bound the taken port")
	}
}

func TestNestedErrorPagesConflict(t *testing.T) {
	root := t.TempDir()
	for rel, c := range map[string]string{
		"src/pages/index.tsx":          devPage("home"),
		"src/pages/blog/404.tsx":       devPage("BLOG404"),
		"src/pages/blog/not-found.tsx": devPage("BLOG404B"),
	} {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("two not-found files for one directory must be a build error, not a coin flip")
	}
	for _, want := range []string{"blog/404.tsx", "blog/not-found.tsx", "KR9001"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

// A root 404 and a nested one are different segments: both build.
func TestNestedNotFoundPagesAreNotAConflict(t *testing.T) {
	root := t.TempDir()
	for rel, c := range map[string]string{
		"src/pages/index.tsx":    devPage("home"),
		"src/pages/404.tsx":      devPage("ROOT404"),
		"src/pages/blog/404.tsx": devPage("BLOG404"),
	} {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("nested not-found pages must build: %v", err)
	}
	for rel, want := range map[string]string{"404.html": "ROOT404", "blog/404.html": "BLOG404"} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil || !strings.Contains(string(data), want) {
			t.Errorf("%s: want %s, got err=%v", rel, want, err)
		}
	}
}

func TestMissingPagesDirIsAClearError(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "nope")
	cfg.Entry = filepath.Join(root, "src", "index.tsx")
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "pagesDir src/nope does not exist") {
		t.Fatalf("got %v, want a pagesDir-does-not-exist error", err)
	}
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

func TestIsRootRouteOutput(t *testing.T) {
	for name, want := range map[string]bool{
		"index.html": true, "index.ab12.js": true, "index.ab12.js.map": true, "index.x.css": true,
		"manifest.json": false, "404.html": false, "sitemap.xml": false, "robots.txt": false,
	} {
		if got := isRootRouteOutput(name); got != want {
			t.Errorf("isRootRouteOutput(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDevRouteLabelNamesErrorPages(t *testing.T) {
	pages := filepath.Join(string(filepath.Separator), "p", "src", "pages")
	cases := map[string]string{
		filepath.Join(pages, "index.tsx"):                "/",
		filepath.Join(pages, "404.tsx"):                  "/404",
		filepath.Join(pages, "blog", "a.tsx"):            "/blog/a",
		filepath.Join(pages, "blog", "500.tsx"):          "/blog/500",
		filepath.Join(pages, "blog", "not-found.tsx"):    "/blog/404",
		filepath.Join(pages, "error.tsx"):                "/500",
		filepath.Join(pages, "(g)", "blog", "error.tsx"): "/blog/500",
	}
	for page, want := range cases {
		if got := devRouteLabel(page, pages); got != want {
			t.Errorf("devRouteLabel(%q) = %q, want %q", page, got, want)
		}
	}
}

func TestPruneStaleHydrationKeepsCurrentBundle(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"index.old1.js", "index.old1.js.map", "index.new.js", "index.html", "other.js"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pruneStaleHydration(dir, "index.new.js")
	for n, want := range map[string]bool{"index.old1.js": false, "index.old1.js.map": false, "index.new.js": true, "index.html": true, "other.js": true} {
		if exists(filepath.Join(dir, n)) != want {
			t.Errorf("%s exists=%v, want %v", n, !want, want)
		}
	}
}

// Adding, editing and removing segment pages in dev rebuilds their outputs and
// the pages an error.tsx boundary wraps.
func TestWatchSegmentPages(t *testing.T) {
	root, _, _ := startDevWatch(t, map[string]string{
		"src/pages/index.tsx":      devPage("home"),
		"src/pages/404.tsx":        devPage("ROOT404"),
		"src/pages/blog/index.tsx": devPage("blog"),
	}, nil)
	pages := filepath.Join(root, "src", "pages")
	read := func(rel string) string {
		data, _ := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		return string(data)
	}
	if exists(filepath.Join(root, "dist", "blog", "404.html")) {
		t.Fatal("blog has no not-found page yet")
	}
	if err := os.WriteFile(filepath.Join(pages, "blog", "not-found.tsx"), []byte(devPage("BLOG404-v1")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new segment page output", func() bool { return strings.Contains(read("blog/404.html"), "BLOG404-v1") })

	if err := os.WriteFile(filepath.Join(pages, "blog", "not-found.tsx"), []byte(devPage("BLOG404-v2")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edited segment page", func() bool { return strings.Contains(read("blog/404.html"), "BLOG404-v2") })

	// The manifest follows structural changes: the segment map lists blog.
	waitFor(t, "manifest lists the segment", func() bool {
		data, _ := os.ReadFile(ManifestPath(filepath.Join(root, "dist")))
		return strings.Contains(string(data), `"blog/404.html"`)
	})

	// An error.tsx wraps the pages below it; editing it rebuilds them.
	errSrc := func(msg string) string {
		return "export default function E({ error, reset }: any) { return <p onClick={reset}>" + msg + "</p>; }\n"
	}
	if err := os.WriteFile(filepath.Join(pages, "blog", "error.tsx"), []byte(errSrc("ERRPAGE-v1")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "boundary baked into the pages below", func() bool { return strings.Contains(read("blog/index.html"), "ERRPAGE-v1") })
	if err := os.WriteFile(filepath.Join(pages, "blog", "error.tsx"), []byte(errSrc("ERRPAGE-v2")), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "edited error page rebuilds the wrapped pages", func() bool {
		return strings.Contains(read("blog/index.html"), "ERRPAGE-v2") && strings.Contains(read("blog/500.html"), "ERRPAGE")
	})

	removeWatched(t, filepath.Join(pages, "blog", "not-found.tsx"))
	waitFor(t, "removed segment page output", func() bool { return !exists(filepath.Join(root, "dist", "blog", "404.html")) })
	removeWatched(t, filepath.Join(pages, "blog", "error.tsx"))
	waitFor(t, "boundary removed from the pages below", func() bool {
		return !strings.Contains(read("blog/index.html"), "ERRPAGE") && !exists(filepath.Join(root, "dist", "blog", "500.html"))
	})
}

// removeWatched deletes a file the dev watcher may be reading. Windows refuses
// to delete a file another process (here the rebuild the previous edit
// started) has open, so there it retries for a few seconds; elsewhere one
// attempt decides.
func removeWatched(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Remove(path)
		if err == nil {
			return
		}
		if runtime.GOOS != "windows" || os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
