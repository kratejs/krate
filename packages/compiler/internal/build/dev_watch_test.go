package build

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
	go func() { _ = Watch(b, 30*time.Millisecond, hub) }()
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
	if err := os.Remove(filepath.Join(root, "src", "pages", "newp.tsx")); err != nil {
		t.Fatal(err)
	}
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
	if err := os.Remove(filepath.Join(root, "public", "new.txt")); err != nil {
		t.Fatal(err)
	}
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
	defer first.Close()
	taken := first.Addr().(*net.TCPAddr).Port
	if _, err := bindServerListener("127.0.0.1", taken, false); err == nil {
		t.Fatal("serve must fail on a taken port")
	}
	ln, err := bindServerListener("127.0.0.1", taken, true)
	if err != nil {
		t.Fatalf("dev must auto-increment: %v", err)
	}
	defer ln.Close()
	if ln.Addr().(*net.TCPAddr).Port == taken {
		t.Fatal("dev bound the taken port")
	}
}

func TestNestedErrorPagesConflict(t *testing.T) {
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
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("two 404 pages must be a build error, not a coin flip")
	}
	for _, want := range []string{"404.tsx", "blog/404.tsx"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s: %v", want, err)
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
