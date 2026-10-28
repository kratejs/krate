package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// nextDataServer is the backend of the migrated app's fetch() calls. It counts
// reads so the browser test can tell a cached render from a refetch.
func nextDataServer(t *testing.T) *httptest.Server {
	t.Helper()
	type post struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	var mu sync.Mutex
	gets := 0
	posts := []post{{"first-post", "First post"}, {"second-post", "Second post"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("content-type", "application/json")
		switch {
		case r.URL.Path == "/__count":
			_ = json.NewEncoder(w).Encode(map[string]int{"gets": gets})
		case r.URL.Path == "/api/posts" && r.Method == http.MethodGet:
			gets++
			_ = json.NewEncoder(w).Encode(posts)
		case r.URL.Path == "/api/posts" && r.Method == http.MethodPost:
			var in post
			_ = json.NewDecoder(r.Body).Decode(&in)
			in.Slug = strings.ToLower(strings.ReplaceAll(in.Title, " ", "-"))
			posts = append(posts, in)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(in)
		case strings.HasPrefix(r.URL.Path, "/api/posts/"):
			gets++
			slug := strings.TrimPrefix(r.URL.Path, "/api/posts/")
			for _, p := range posts {
				if p.Slug == slug {
					_ = json.NewEncoder(w).Encode(p)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMigratedNextAppBrowser migrates a real Next.js App Router project
// (testdata/nextapp) with the migration planner, builds it, serves it with the
// real Go server and sidecars, and drives it in a browser: async server
// components, tagged fetch caching, revalidateTag from a server action,
// "use client" islands, layout persistence, template remount, notFound() and a
// parallel + intercepting modal route. Runs when KRATE_BROWSER is set.
func TestMigratedNextAppBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the migrated Next.js browser test")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}

	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", "nextapp"), root); err != nil {
		t.Fatal(err)
	}
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Manual {
		t.Logf("manual item: %s: %s", m.Source, m.Reason)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{}); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	// The migrator never leaves a route unmapped silently: every manual item
	// of this fixture would be a regression.
	for _, m := range rep.Manual {
		if m.Source != "package.json" {
			t.Fatalf("the migrated fixture needs no manual work, got %+v", m)
		}
	}

	withPreact(t, root)
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	// The generated krate.config.ts imports @krate/core (which pulls in
	// @krate/plugin); the runtime is resolved from the same scope.
	scope := filepath.Join(root, "node_modules", "@krate")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"core", "plugin", "runtime"} {
		if err := os.Symlink(filepath.Join(repoRoot, "packages", pkg), filepath.Join(scope, pkg)); err != nil {
			t.Fatal(err)
		}
	}
	// The build finds the runtime bundle under <root>/packages/runtime/dist.
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Fatal(err)
	}
	data := nextDataServer(t)
	t.Setenv("DATA_URL", data.URL)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll on the migrated app: %v", err)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "nextmigrate_e2e.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser, data.URL).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("migrated app browser test failed: %v\n%s", err, out)
	}
	t.Logf("migrated app (%s):\n%s", browser, out)
}
