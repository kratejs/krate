package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestSuspenseUseRendersFallbackAndTemplatesContent(t *testing.T) {
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { Suspense, use, useEffect } from 'react';
		const userPromise = new Promise((r) => setTimeout(() => r({ name: 'Ada' }), 10));
		function User() {
			const user = use(userPromise);
			useEffect(() => { document.title = user.name; }, [user]);
			return <p id="user">{user.name}</p>;
		}
		export default function Page() {
			return <main><Suspense fallback={<p id="fb">loading</p>}><User /></Suspense></main>;
		}
	`})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := regexp.MustCompile(`<!--suspense:([^>]+)--><!--k:s([^>]+)--><p id="?fb"?>loading</p><!--/k:s[^>]+--><!--/suspense:[^>]+-->`).FindStringSubmatch(html)
	if m == nil || m[1] != m[2] {
		t.Fatalf("expected the fallback inside the boundary slot:\n%s", html)
	}
	id := m[1]
	if !regexp.MustCompile(`<template data-kc="?s` + regexp.QuoteMeta(id) + `"? data-kb="?c"?><p id="?user"?><!--k:`).MatchString(html) {
		t.Fatalf("expected the content in a hoisted template:\n%s", html)
	}
	if !strings.Contains(js, `__krateBoundary("`+id+`",function(){`) {
		t.Fatalf("expected the component to run inside its boundary:\n%s", js)
	}
	// Effects of the content wait for the reveal: the component's owner is
	// declared inside the boundary's content branch, initially hidden.
	if !strings.Contains(js, `[["s`+id+`","c",0]]`) {
		t.Fatalf("expected the owner to sit in the boundary's content branch:\n%s", js)
	}
	if !strings.Contains(js, "__krateUse(()=>__krateRead(userPromise))") || strings.Count(js, "__krateUse(") != 1 {
		t.Fatalf("expected one use() declaration:\n%s", js)
	}
}

func TestLazyNamedExportLoaders(t *testing.T) {
	root := t.TempDir()
	for rel, src := range map[string]string{
		"src/components/Chart.tsx":  `export function Chart() { return <p id="chart">chart body</p>; }`,
		"src/components/Editor.tsx": `export function Editor() { return <p id="editor">editor body</p>; }`,
		"src/pages/index.tsx": `
		import { lazy, Suspense } from 'react';
		const Chart = lazy(() => import('../components/Chart').then((m) => m.Chart));
		const Editor = lazy(() => import('../components/Editor').then(m => ({ default: m.Editor })));
		export default function Page() {
			return <main>
				<Suspense fallback={<p id="fb1">loading chart</p>}><Chart /></Suspense>
				<Suspense fallback={<p id="fb2">loading editor</p>}><Editor /></Suspense>
			</main>;
		}
	`} {
		writeFileRel(t, root, rel, src)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, want := range []string{
		`<!--k:s2-1--><p id="fb1">loading chart</p>`, `<template data-kc="s2-1" data-kb="c"><p id="chart">chart body</p></template>`,
		`<!--k:s2-2--><p id="fb2">loading editor</p>`, `<template data-kc="s2-2" data-kb="c"><p id="editor">editor body</p></template>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	for _, name := range []string{"Chart", "Editor"} {
		if chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "lazy-"+name+".*.js")); len(chunks) != 1 {
			t.Errorf("expected one lazy-%s chunk, got %v", name, chunks)
		}
	}
}
