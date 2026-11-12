package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// e2eBuildNativeDev builds a project the way `krate native dev` does: the dev
// overlay is in the pages and the manifest enables the developer tools.
func e2eBuildNativeDev(t *testing.T, root, out string) string {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	b := New(root, cfg)
	b.DevMode = true
	res, err := b.BuildNativeApp(NativeAppOptions{OutDir: out, Dev: true})
	if err != nil {
		t.Fatalf("native dev build: %v", err)
	}
	return res.AppDir
}

// e2eNoPageErrors fails the test when the page of a dev build reported an error
// to the terminal while it ran.
func e2eNoPageErrors(t *testing.T, s *e2eShell) {
	t.Helper()
	time.Sleep(300 * time.Millisecond) // reports are posted asynchronously
	var bad []string
	for _, l := range strings.Split(s.errs.String(), "\n") {
		if strings.Contains(l, "[webview]") {
			bad = append(bad, l)
		}
	}
	if len(bad) > 0 {
		t.Errorf("the page reported errors:\n%s", strings.Join(bad, "\n"))
	}
}

// e2eWriteProject writes files (path to content) under a new directory inside
// examples/ (the build finds the runtime bundles by walking up to the checkout)
// that resolves @krate/* through the node_modules of examples/native-views. The
// directory is removed when the test ends.
func e2eWriteProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp(filepath.Join(repoRoot(t), "examples"), ".e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Symlink(filepath.Join(repoRoot(t), "examples", "native-views", "node_modules"), filepath.Join(dir, "node_modules")); err != nil {
		requireE2E(t, "cannot link node_modules: %v", err)
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const e2eErrorsConfig = `import { defineConfig } from '@krate/core';

export default defineConfig({
  entry: 'src/pages/index.tsx',
  pagesDir: 'src/pages',
  outDir: 'dist',
  native: {
    appId: 'com.example.errors',
    productName: 'Native Errors',
    version: '1.0.0',
    window: { width: 700, height: 500, resizable: true },
  },
});
`

// The main page throws in a timer, rejects a promise and logs console.error as
// it starts, then opens a view whose page throws too. Nothing is thrown while
// the build prerenders the page (there is no document then).
const e2eErrorsIndex = `import { createSignal, createEffect } from '@krate/runtime';
import { main } from '@krate/app/window';

export default function Home() {
  const [status, setStatus] = createSignal('starting');
  createEffect(() => {
    setTimeout(() => { throw new Error('krate-e2e-uncaught'); }, 0);
    Promise.reject(new Error('krate-e2e-rejection'));
    console.error('krate-e2e-console', 42);
    main().addView({ id: 'bad', path: '/bad', side: 'right', size: 0.4 })
      .then(() => { setStatus('opened'); document.getElementById('boom')?.click(); }, (e) => setStatus('failed ' + String(e)));
  });
  return (
    <main>
      <h1 id="status">{status()}</h1>
      <button id="boom" onClick={() => { throw new Error('krate-e2e-click'); }}>boom</button>
    </main>
  );
}
`

const e2eErrorsBad = `import { createEffect } from '@krate/runtime';

export default function Bad() {
  createEffect(() => {
    setTimeout(() => { throw new Error('krate-e2e-view-uncaught'); }, 0);
  });
  return <main><p id="bad">bad view</p></main>;
}
`

// TestNativeE2EPageErrorsGUI runs a page that throws as it starts through the
// real shell in dev mode: every uncaught error, unhandled rejection and
// console.error of the window and of a view must reach the shell's stderr (the
// terminal of `krate native dev`) tagged with the window or view id.
func TestNativeE2EPageErrorsGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	if _, err := os.Stat(filepath.Join(repoRoot(t), "examples", "native-views", "node_modules", "@krate", "app", "dist", "index.js")); err != nil {
		requireE2E(t, "examples/native-views has no built @krate/app (run pnpm install and pnpm build)")
	}
	root := e2eWriteProject(t, map[string]string{
		"krate.config.ts":     e2eErrorsConfig,
		"src/pages/index.tsx": e2eErrorsIndex,
		"src/pages/bad.tsx":   e2eErrorsBad,
		"package.json":        `{"name":"native-errors","private":true,"type":"module"}`,
	})
	work := t.TempDir()
	appDir := e2eBuildNativeDev(t, root, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)

	s := e2eStartShell(t, bin, appDir)
	s.waitReady(60 * time.Second)

	want := []string{
		"krate-e2e-uncaught",
		"unhandled rejection: krate-e2e-rejection",
		"console.error: krate-e2e-console 42",
		"krate-e2e-view-uncaught",
	}
	// The click handler's error is mapped to the source by the dev build's
	// source map.
	want = append(want, "krate-e2e-click")
	var got string
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		got = s.errs.String()
		missing := ""
		for _, w := range want {
			if !strings.Contains(got, w) {
				missing = w
				break
			}
		}
		if missing == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the terminal never got %q; shell stderr:\n%s", missing, got)
		}
	}
	// The line names the window; the view's error names the view.
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "krate-e2e-view-uncaught") && strings.HasPrefix(l, "[webview] ") {
			if strings.HasPrefix(l, "[webview] main:") || strings.HasPrefix(l, "[webview] ?unknown") {
				t.Errorf("the view's error is not attributed to the view: %q", l)
			}
		}
		if strings.Contains(l, "krate-e2e-uncaught") && strings.HasPrefix(l, "[webview] ") && !strings.HasPrefix(l, "[webview] main:") {
			t.Errorf("the window's error is not attributed to main: %q", l)
		}
	}
	if !regexp.MustCompile(`src/pages/index\.tsx:\d+:\d+`).MatchString(got) {
		t.Errorf("the click handler's error is not mapped to src/pages/index.tsx:\n%s", got)
	}
	// The same errors show in the dev overlay.
	var n int
	if err := s.query(`document.getElementById("krate-dev-root") ? 1 : 0`, &n); err != nil || n != 1 {
		t.Errorf("the dev overlay is not mounted (n=%d, err=%v)", n, err)
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
