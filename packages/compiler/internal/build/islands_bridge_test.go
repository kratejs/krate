package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// setupIslandProject writes a page that renders a compiled external package
// (fake-lib) and, when withPreact is set, copies the examples' Preact fixture
// into the project's node_modules.
func setupIslandProject(t *testing.T, root string, withPreact bool) *config.Config {
	t.Helper()
	if withPreact {
		preactSrc := filepath.Join("..", "..", "..", "..", "examples", "node_modules", "preact")
		if _, err := os.Stat(preactSrc); err != nil {
			t.Skipf("preact fixture not installed: %v", err)
		}
		if err := copyTree(t, preactSrc, filepath.Join(root, "node_modules", "preact")); err != nil {
			t.Fatalf("copying preact fixture: %v", err)
		}
	}

	libDir := filepath.Join(root, "node_modules", "fake-lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "package.json"), []byte(`{"name":"fake-lib","version":"1.0.0","main":"index.js"}`), 0644); err != nil {
		t.Fatal(err)
	}
	libSrc := `
		import { jsx } from 'react/jsx-runtime';
		export function Badge({ label }) {
			return jsx('span', { className: 'badge', children: label });
		}
		export function Frame({ children }) {
			return jsx('section', { className: 'frame', children });
		}
		import { useState } from 'react';
		export function Counter() {
			const [n] = useState(7);
			return jsx('b', { className: 'counter', children: n });
		}
		export function BrowserOnly() {
			return jsx('span', { className: 'browser-only', children: String(window.innerWidth) });
		}
		export function List({ items }) {
			return jsx('ul', { className: 'list', children: items.map((item, i) => jsx('li', { children: item.label + '-' + i })) });
		}
	`
	if err := os.WriteFile(filepath.Join(libDir, "index.js"), []byte(libSrc), 0644); err != nil {
		t.Fatal(err)
	}

	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		import { createSignal } from '@krate/runtime';
		import { Badge } from 'fake-lib';
		export default function Page() {
			const [label, setLabel] = createSignal('hello');
			return (
				<div>
					<Badge label={label()} onPing={setLabel} />
					<button onClick={() => setLabel('bye')}>x</button>
				</div>
			);
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	return cfg
}

// TestIslandSignalPropBridge verifies that a page-scope signal is evaluated in
// the page, published to the island boot values, and passed as a prop.
func TestIslandSignalPropBridge(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "data-krate-island") {
		t.Fatalf("expected an island placeholder:\n%s", html)
	}
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, ">hello</span>") {
		t.Fatalf("expected a server-rendered island subtree:\n%s", html)
	}
	if !strings.Contains(html, `type="importmap"`) {
		t.Fatalf("expected the shared Preact import map:\n%s", html)
	}

	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsBytes), `"label":label()`) {
		t.Fatalf("island boot values should evaluate the page signal:\n%s", jsBytes)
	}
	if !strings.Contains(string(jsBytes), `"setLabel":setLabel`) {
		t.Fatalf("island boot values should bridge the page setter as a function prop:\n%s", jsBytes)
	}
	if !strings.Contains(string(jsBytes), "__krate_island_subscribe") {
		t.Fatalf("island boot should subscribe the island to page signal changes:\n%s", jsBytes)
	}

	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(chunks) == 0 {
		t.Fatalf("expected an island chunk: %v", err)
	}
	chunk, err := os.ReadFile(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunk), "badge") {
		t.Fatalf("island chunk does not contain the external component:\n%s", chunk)
	}
	if !strings.Contains(string(chunk), "KrateIslandBoundary") || !strings.Contains(string(chunk), "data-krate-island-error") {
		t.Fatalf("island chunk is missing its error boundary:\n%s", chunk)
	}
	if !strings.Contains(string(chunk), "hydrate(") {
		t.Fatalf("island chunk must hydrate server markup, not replace it:\n%s", chunk)
	}
}

// TestIslandLocalComponent verifies a local React-compatible component used
// inside an external subtree is bundled into that island and server-rendered.
func TestIslandLocalComponent(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	pagesDir := filepath.Join(root, "src", "pages")
	compDir := filepath.Join(root, "src", "components")
	if err := os.MkdirAll(compDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(compDir, "local-card.tsx"), `
		import { jsx } from 'react/jsx-runtime';
		import { useState } from 'react';
		import { Badge } from 'fake-lib';
		export default function LocalCard({ label }) {
			const [count] = useState(3);
			return jsx('div', { className: 'local-card', children: jsx(Badge, { label: label + ':' + count }) });
		}
	`)
	write(filepath.Join(pagesDir, "index.tsx"), `
		import { createSignal } from '@krate/runtime';
		import { Frame } from 'fake-lib';
		import LocalCard from '../components/local-card';
		export default function Page() {
			const [label] = createSignal('hello');
			return <Frame><LocalCard label={label()} /></Frame>;
		}
	`)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "local-card") || !strings.Contains(html, "hello:3") {
		t.Fatalf("local island component was not server-rendered:\n%s", html)
	}
	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(chunks) == 0 {
		t.Fatalf("expected an island chunk: %v", err)
	}
	chunk, err := os.ReadFile(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunk), "local-card") {
		t.Fatalf("island chunk does not contain the local component:\n%s", chunk)
	}
}

// TestIslandPerIslandChunks verifies each external subtree gets its own
// dynamically imported chunk and SSR markup, rather than one page-wide bundle.
func TestIslandPerIslandChunks(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { Badge } from 'fake-lib';
		export default function Page() {
			return <div><Badge label="one" /><Badge label="two" /></div>;
		}
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 per-island chunks, got %d: %v", len(chunks), chunks)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(htmlBytes), "data-krate-island-ssr"); got != 2 {
		t.Fatalf("expected 2 server-rendered islands, got %d:\n%s", got, htmlBytes)
	}
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(jsBytes), "import(\"/chunks/island."); got != 2 {
		t.Fatalf("expected 2 dynamic island imports, got %d:\n%s", got, jsBytes)
	}
}

func TestIslandSSRWithHooks(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { Counter } from 'fake-lib';
		export default function Page() { return <Counter />; }
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(htmlBytes), "data-krate-island-ssr") || !strings.Contains(string(htmlBytes), ">7</b>") {
		t.Fatalf("hook island was not server-rendered:\n%s", htmlBytes)
	}
}

func TestIslandSSREscapesTextProps(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { Badge } from 'fake-lib';
		export default function Page() { return <Badge label={'<b>&</b>'} />; }
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "&lt;b>&amp;&lt;/b>") {
		t.Fatalf("island SSR did not escape text:\n%s", html)
	}
	if strings.Contains(html, "<b>&amp;") || strings.Contains(html, "<b>&") {
		t.Fatalf("island SSR emitted unescaped text:\n%s", html)
	}
}

func TestIslandSSRUnknownPropFallsBackClientOnly(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { createSignal } from '@krate/runtime';
		import { Badge } from 'fake-lib';
		export default function Page() {
			const [label] = createSignal(getLabel());
			return <Badge label={label()} />;
		}
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if strings.Contains(html, "data-krate-island-ssr") {
		t.Fatalf("unknown initial prop must not be server-rendered:\n%s", html)
	}
	if !strings.Contains(html, "data-krate-island") {
		t.Fatalf("client-only island placeholder missing:\n%s", html)
	}
	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(chunks) == 0 {
		t.Fatalf("expected a client island chunk: %v", err)
	}
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsBytes), `"label":label()`) {
		t.Fatalf("client-only island lost the live prop bridge:\n%s", jsBytes)
	}
}

func TestIslandSSRModuleFailureFallsBackClientOnly(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { BrowserOnly } from 'fake-lib';
		export default function Page() { return <BrowserOnly />; }
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(htmlBytes), "data-krate-island-ssr") {
		t.Fatalf("browser-only island must not be server-rendered:\n%s", htmlBytes)
	}
	chunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(chunks) == 0 {
		t.Fatalf("expected a client island chunk: %v", err)
	}
	chunk, err := os.ReadFile(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chunk), "browser-only") || !strings.Contains(string(chunk), "innerWidth") {
		t.Fatalf("browser-only component missing from client chunk:\n%s", chunk)
	}
}

func TestIslandSharedSignalAcrossIslands(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { createSignal } from '@krate/runtime';
		import { Badge } from 'fake-lib';
		export default function Page() {
			const [label] = createSignal('shared');
			return <div><Badge label={label()} /><Badge label={label()} /></div>;
		}
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(htmlBytes), "data-krate-island-ssr"); got != 2 {
		t.Fatalf("expected 2 server-rendered islands, got %d:\n%s", got, htmlBytes)
	}
	if got := strings.Count(string(htmlBytes), ">shared</span>"); got != 2 {
		t.Fatalf("expected both islands to render shared signal initial, got %d:\n%s", got, htmlBytes)
	}
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(jsBytes), `"label":label()`); got != 2 {
		t.Fatalf("expected both islands to bridge the same signal, got %d:\n%s", got, jsBytes)
	}
}

func TestIslandSSRArrayObjectProps(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	page := `
		import { createSignal } from '@krate/runtime';
		import { List } from 'fake-lib';
		export default function Page() {
			const [items] = createSignal([{ label: 'a' }, { label: 'b' }]);
			return <List items={items()} />;
		}
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, "<li>a-0</li>") || !strings.Contains(html, "<li>b-1</li>") {
		t.Fatalf("array/object island props were not server-rendered:\n%s", html)
	}
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(jsFiles) == 0 {
		t.Fatalf("expected a hydration bundle: %v", err)
	}
	jsBytes, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsBytes), `"items":items()`) {
		t.Fatalf("array prop bridge missing:\n%s", jsBytes)
	}
}

func TestIslandLocalNamedImport(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	compDir := filepath.Join(root, "src", "components")
	if err := os.MkdirAll(compDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compDir, "named-card.tsx"), []byte(`
		import { jsx } from 'react/jsx-runtime';
		export function NamedCard({ label }) {
			return jsx('strong', { className: 'named-card', children: label });
		}
	`), 0644); err != nil {
		t.Fatal(err)
	}
	page := `
		import { Frame } from 'fake-lib';
		import { NamedCard } from '../components/named-card';
		export default function Page() { return <Frame><NamedCard label="named" /></Frame>; }
	`
	if err := os.WriteFile(filepath.Join(root, "src", "pages", "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(htmlBytes), "data-krate-island-ssr") || !strings.Contains(string(htmlBytes), "named-card") {
		t.Fatalf("named local import was not server-rendered:\n%s", htmlBytes)
	}
}

// TestIslandMissingPreactHint verifies the build fails with an actionable

// install hint when Preact is not in the project.
func TestIslandMissingPreactHint(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, false)
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("expected a missing-Preact build error")
	}
	if !strings.Contains(err.Error(), "pnpm add preact") {
		t.Fatalf("expected an install hint, got: %v", err)
	}
}

// copyTree copies src into dst, following symlinks (pnpm's node_modules uses
// them for package entries).
func copyTree(t *testing.T, src, dst string) error {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
func TestIslandChunksMinified(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cfg.Minify = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	islands, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "island.*.js"))
	if err != nil || len(islands) == 0 {
		t.Fatalf("expected an island chunk: %v", err)
	}
	island, err := os.ReadFile(islands[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(island), "// ../../node_modules") {
		t.Errorf("production island chunk is not minified")
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(htmlBytes), "data-krate-island-ssr") || !strings.Contains(string(htmlBytes), ">hello</span>") {
		t.Fatalf("minified build lost island SSR markup:\n%s", htmlBytes)
	}

	preactChunks, err := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "preact-preact.*.js"))
	if err != nil || len(preactChunks) == 0 {
		t.Fatalf("expected Preact chunks: %v", err)
	}
	preact, err := os.ReadFile(preactChunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preact), "// ../../node_modules") {
		t.Errorf("production Preact chunk is not minified")
	}
}
