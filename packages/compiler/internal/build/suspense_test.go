package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildSuspenseProject builds files and returns the dist directory.
func buildSuspenseProject(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	return cfg.OutDir, New(root, cfg).BuildAll()
}

func readDist(t *testing.T, dist, glob string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dist, glob))
	if len(matches) == 0 {
		t.Fatalf("no file matches %s in %s", glob, dist)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

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

func TestSuspenseStaticContentHydrates(t *testing.T) {
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { useState, Suspense } from 'react';
		function Counter() {
			const [n, setN] = useState(0);
			return <button onClick={() => setN(n + 1)}>{n}</button>;
		}
		export default function Page() {
			return <Suspense fallback={<p>loading</p>}><Counter /></Suspense>;
		}
	`})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(html, "loading") || strings.Contains(html, "data-kc=\"s") {
		t.Fatalf("a boundary that cannot suspend bakes its content only:\n%s", html)
	}
	if !strings.Contains(js, "kbindHandler(") || strings.Contains(js, "__krateBoundary") {
		t.Fatalf("content inside a non-suspending boundary must hydrate normally:\n%s", js)
	}
}

func TestNestedSuspenseInnermostWins(t *testing.T) {
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { Suspense, use } from 'react';
		const p = Promise.resolve('v');
		function Slow() {
			const v = use(p);
			return <b>{v}</b>;
		}
		export default function Page() {
			return (
				<Suspense fallback={<p>outer</p>}>
					<h1>shell</h1>
					<Suspense fallback={<p>inner</p>}><Slow /></Suspense>
				</Suspense>
			);
		}
	`})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(html, "outer") {
		t.Fatalf("the outer boundary does not suspend and keeps its content:\n%s", html)
	}
	if !strings.Contains(html, "<h1>shell</h1>") || !strings.Contains(html, "inner") {
		t.Fatalf("expected the shell and the inner fallback:\n%s", html)
	}
	if strings.Count(html, `data-kb="c"`)+strings.Count(html, `data-kb=c`) != 1 {
		t.Fatalf("expected exactly one suspended boundary template:\n%s", html)
	}
	if strings.Count(js, "__krateBoundary(") != 1 {
		t.Fatalf("expected Slow to run in the inner boundary only:\n%s", js)
	}
}

func TestLazyComponentGetsHydrationChunk(t *testing.T) {
	dist, err := buildSuspenseProject(t, map[string]string{
		"src/pages/index.tsx": `
			import { lazy, Suspense } from 'react';
			const Heavy = lazy(() => import('../Heavy'));
			export default function Page() {
				return <main><Suspense fallback={<p>loading</p>}><Heavy label="x" /></Suspense></main>;
			}
		`,
		"src/Heavy.tsx": `
			import { useState } from 'react';
			export default function Heavy(props) {
				const [c, setC] = useState(0);
				return <button id="heavy" onClick={() => setC(c + 1)}>heavy {props.label} {c}</button>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	html := readDist(t, dist, "index.html")
	if !strings.Contains(html, "heavy x") {
		t.Fatalf("lazy content must render at build time:\n%s", html)
	}
	pageJS := readDist(t, dist, "index.*.js")
	chunk := readDist(t, dist, "lazy-Heavy.*.js")
	if !regexp.MustCompile(`__krateLazy\("/lazy-Heavy\.[a-z0-9]+\.js","[^"]+"\)`).MatchString(pageJS) {
		t.Fatalf("page script must load the lazy chunk:\n%s", pageJS)
	}
	if strings.Contains(pageJS, "createSignal(0)") {
		t.Fatalf("the lazy component's hydration code must not be in the page script:\n%s", pageJS)
	}
	if !strings.HasPrefix(chunk, "__krateLazyRun(function(){") || !strings.Contains(chunk, "kbindHandler(") {
		t.Fatalf("unexpected lazy chunk:\n%s", chunk)
	}
	if strings.Contains(pageJS, "__KRATE_LAZY_CHUNK_") {
		t.Fatalf("chunk placeholder left in the page script:\n%s", pageJS)
	}
}

func TestLazyWithoutDefaultExportFails(t *testing.T) {
	_, err := buildSuspenseProject(t, map[string]string{
		"src/pages/index.tsx": `
			import { lazy, Suspense } from 'react';
			const Heavy = lazy(() => import('../Heavy'));
			export default function Page() {
				return <Suspense fallback={<p>loading</p>}><Heavy /></Suspense>;
			}
		`,
		"src/Heavy.tsx": `export function Heavy() { return <p>x</p>; }`,
	})
	if err == nil || !strings.Contains(err.Error(), "KR4007") {
		t.Fatalf("expected a KR4007 error, got %v", err)
	}
}

func TestUsePlacementFailsWithCode(t *testing.T) {
	_, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { use } from 'react';
		const p = Promise.resolve('x');
		export default function Page() {
			return <p>{use(p)}</p>;
		}
	`})
	if err == nil || !strings.Contains(err.Error(), "KR4003") {
		t.Fatalf("expected a KR4003 error, got %v", err)
	}
}

func TestRuntimeNeedsSuspense(t *testing.T) {
	tests := []struct {
		js   string
		want bool
	}{
		{`createSignal(0);`, false},
		{`__krateBoundary("1",function(){});`, true},
		{`var s=__krateTransitionStarter(v=>x(v));`, true},
		{`__krateLazy("/a.js",null);`, true},
		{`var d=createDeferredValue(()=>q());`, true},
	}
	for _, tt := range tests {
		if got := runtimeNeedsSuspense([]*PageResult{{HydrationJS: tt.js}}); got != tt.want {
			t.Errorf("runtimeNeedsSuspense(%q) = %v, want %v", tt.js, got, tt.want)
		}
	}
}

func TestRuntimeChunkSuspenseConditional(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(repoRoot, "packages", "runtime", "dist", "krate-suspense.js")); err != nil {
		requireE2E(t, "runtime suspense bundle not built: %v", err)
		return
	}
	outDir := t.TempDir()
	read := func(suspense bool) string {
		rel := writeRuntimeChunk(outDir, true, repoRoot, runtimeChunkOptions{SPA: true, Suspense: suspense})
		data, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if strings.Contains(read(false), "__krateBoundary") {
		t.Error("suspense runtime included when no page needed it")
	}
	if !strings.Contains(read(true), "__krateBoundary") {
		t.Error("suspense runtime omitted when requested")
	}
}

func TestSlotIDsStayUniqueAfterChildComponent(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": `
		import { useState, Suspense } from 'react';
		function Msg() {
			const [x, setX] = useState(0);
			return <i onClick={() => setX(x + 1)}>{x}</i>;
		}
		export default function Page() {
			const [a, setA] = useState(0);
			return (
				<div>
					<section><p><button onClick={() => setA(a + 1)}>one</button></p><Suspense fallback={<b>w</b>}><Msg /></Suspense></section>
					<section><p><button onClick={() => setA(a + 3)}>two</button></p></section>
				</div>
			);
		}
	`})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ids := regexp.MustCompile(`<button data-k="?([^" >]+)`).FindAllStringSubmatch(html, -1)
	if len(ids) != 2 || ids[0][1] == ids[1][1] {
		t.Fatalf("buttons after a child component need distinct slot IDs: %v\n%s", ids, html)
	}
}
