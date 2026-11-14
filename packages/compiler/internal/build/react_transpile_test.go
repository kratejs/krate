package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildReactPage writes a single-page project to a temp dir and returns the
// output directory plus the page's emitted HTML and hydration JS.
func buildReactPage(t *testing.T, pageSrc string) (html, js string) {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	// Keep identifiers stable so the emitted JS is assertable.
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	// Static pages correctly emit no hydration bundle; JS is empty then.
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil {
		t.Fatalf("glob hydration bundle: %v", err)
	}
	jsBytes := []byte{}
	if len(jsFiles) > 0 {
		jsBytes, err = os.ReadFile(jsFiles[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	return string(htmlBytes), string(jsBytes)
}

// TestReactUseReducerBuild verifies useReducer lowers to createReducer in the
// hydration JS and the state getter renders its SSR initial.
func TestReactUseReducerBuild(t *testing.T) {
	page := `
		import { useReducer } from 'react';
		export default function Page() {
			const [count, dispatch] = useReducer((s, a) => s + a, 7);
			return <button onClick={() => dispatch(1)}>{count}</button>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, ">7<") {
		t.Errorf("SSR should render reducer initial 7:\n%.400s", html)
	}
	if !strings.Contains(js, "createReducer") {
		t.Errorf("hydration should emit createReducer:\n%s", js)
	}
}

// buildShadcnButton writes a shadcn-style Button component plus a page and
// returns the built HTML. It exercises cva, cn, rest-spread props, defaults,
// the dynamic `asChild ? Slot : "button"` tag, and Slot class merging.
func buildShadcnButton(t *testing.T, pageSrc string) string {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	uiDir := filepath.Join(root, "src", "components", "ui")
	for _, d := range []string{pagesDir, uiDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	button := `
		import { Slot, cva, cn } from '@krate/runtime';

		const buttonVariants = cva('inline-flex items-center rounded-md', {
			variants: {
				variant: { default: 'bg-blue-600 text-white', destructive: 'bg-red-600 text-white' },
				size: { default: 'h-9 px-4', lg: 'h-10 px-8' },
			},
			defaultVariants: { variant: 'default', size: 'default' },
		});

		export function Button({ className, variant, size, asChild = false, ...props }: any) {
			const Comp = asChild ? Slot : 'button';
			return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
		}
	`
	if err := os.WriteFile(filepath.Join(uiDir, "button.tsx"), []byte(button), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func TestShadcnButtonFoldsVariantsAndSpread(t *testing.T) {
	html := buildShadcnButton(t, `
		import { Button } from '../components/ui/button';
		export default function Page() {
			return <div>
				<Button>Default</Button>
				<Button variant="destructive" size="lg" disabled>Delete</Button>
			</div>;
		}
	`)
	// Defaults fold.
	if !strings.Contains(html, "bg-blue-600 text-white h-9 px-4") {
		t.Errorf("default variant classes missing:\n%.600s", html)
	}
	if !strings.Contains(html, ">Default<") {
		t.Errorf("children should render:\n%.600s", html)
	}
	// Explicit selection folds and spread props reach the element.
	if !strings.Contains(html, "bg-red-600 text-white h-10 px-8") {
		t.Errorf("destructive/lg classes missing:\n%.600s", html)
	}
	if !strings.Contains(html, "disabled") {
		t.Errorf("spread `disabled` should reach the button:\n%.600s", html)
	}
}

// TestUseContextTranspile verifies React's createContext/useContext lower to
// the Krate context API (Ctx.useContext()) end to end through the bundler.
func TestUseContextTranspile(t *testing.T) {
	root := t.TempDir()
	mkdir := func(rel string) string {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		return full
	}
	page := `
		import { createContext, useContext } from 'react';
		const ThemeCtx = createContext('light');
		export default function Page() {
			const theme = useContext(ThemeCtx);
			return <div>{theme}</div>;
		}
	`
	if err := os.WriteFile(mkdir("src/pages/index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "light") {
		t.Errorf("context default should render:\n%s", html)
	}
	// The client runtime chunk must actually ship createContext, or hydration
	// would throw a ReferenceError (regression: it was absent from the bundle).
	assertRuntimeBundleHasContext(t)
}

// TestPortalRendersAndMounts verifies a built-in <Portal> emits its content and
// a mount call (the client moves it to document.body).
func TestPortalRendersAndMounts(t *testing.T) {
	root := t.TempDir()
	pages := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pages, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		export default function Page() {
			return (
				<div>
					<Portal><div class="overlay">Overlay</div></Portal>
				</div>
			);
		}
	`
	if err := os.WriteFile(filepath.Join(pages, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pages
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "data-krate-portal") || !strings.Contains(string(html), "Overlay") {
		t.Errorf("portal content should render:\n%s", html)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if len(jsFiles) == 0 {
		t.Fatal("expected a page hydration script for the portal")
	}
	js, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "krateMountPortals") {
		t.Errorf("page JS should mount portals:\n%s", js)
	}
}

// TestClassComponentLowering verifies a class component is lowered to a Krate
// function component (state signal, render, setState).
func TestClassComponentLowering(t *testing.T) {
	root := t.TempDir()
	pages := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pages, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		import React from 'react';
		class Counter extends React.Component {
			state = { count: 7 };
			render() {
				return (
					<button onClick={() => this.setState({ count: 8 })}>
						{this.state.count}
					</button>
				);
			}
		}
		export default Counter;
	`
	if err := os.WriteFile(filepath.Join(pages, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = pages
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "7") {
		t.Errorf("class state should render:\n%s", html)
	}
	jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if len(jsFiles) == 0 {
		t.Fatal("expected hydration JS for the class component")
	}
	js, err := os.ReadFile(jsFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "count:7") || !strings.Contains(string(js), "count:8") {
		t.Errorf("class component should lower state + setState:\n%s", js)
	}
}

// assertRuntimeBundleHasContext loads the built runtime chunk and checks the
// context API is present. Skips when the runtime dist hasn't been bundled.
func assertRuntimeBundleHasContext(t *testing.T) {
	t.Helper()
	root := findCompilerRoot(t)
	if root == "" {
		requireE2E(t, "compiler root not found")
	}
	runtimeDir := filepath.Join(filepath.Dir(root), "runtime", "dist")
	for _, name := range []string{"krate-hydrate.js", "krate-runtime.js"} {
		data, err := os.ReadFile(filepath.Join(runtimeDir, name))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), "createContext") {
			t.Errorf("%s does not ship createContext", name)
		}
		return
	}
	requireE2E(t, "runtime bundle not built")
}

// findCompilerRoot locates the packages/compiler directory from the test file.
func findCompilerRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil &&
			strings.HasSuffix(filepath.ToSlash(dir), "packages/compiler") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func writeFileRel(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
