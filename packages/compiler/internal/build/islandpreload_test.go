package build

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func islandExt(id, hydrate string) irtree.ExternalComponent {
	return irtree.ExternalComponent{ID: id, Hydrate: hydrate}
}

func TestIslandGraphFromMetafile(t *testing.T) {
	t.Parallel()
	meta := `{"outputs":{
		"dist/chunks/island_aaa.js":{"imports":[
			{"path":"dist/chunks/isl-ABC.js","kind":"import-statement"},
			{"path":"preact/compat","kind":"import-statement","external":true},
			{"path":"dist/chunks/isl-LAZY.js","kind":"dynamic-import"},
			{"path":"./isn_0123456789ab.js","kind":"import-statement","external":true},
			{"path":"https://cdn.example/x.js","kind":"import-statement","external":true}]},
		"dist/chunks/isl-ABC.js":{"imports":[{"path":"preact","kind":"import-statement","external":true}]},
		"dist/chunks/isl-LAZY.js":{"imports":[]},
		"dist/chunks/island_aaa.js.map":{"imports":[]}
	}}`
	g := islandModuleGraph{}
	if err := g.addMetafile(meta); err != nil {
		t.Fatal(err)
	}
	want := islandModuleGraph{
		"/chunks/island_aaa.js": {"/chunks/isl-abc.js", "preact/compat", "/chunks/isn_0123456789ab.js"},
		"/chunks/isl-abc.js":    {"preact"},
		"/chunks/isl-lazy.js":   {},
	}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("graph:\n got %v\nwant %v", g, want)
	}
}

func TestIslandPreloadClosure(t *testing.T) {
	t.Parallel()
	g := islandModuleGraph{
		"/chunks/island_a.js":    {"/chunks/isl-shared.js", "preact/compat"},
		"/chunks/island_b.js":    {"/chunks/isl-shared.js", "/chunks/isl-b.js"},
		"/chunks/isl-shared.js":  {"preact"},
		"/chunks/isl-b.js":       {"/chunks/isl-shared.js", "unmapped-pkg"},
		"/chunks/preact.x.js":    {},
		"/chunks/compat.y.js":    {"preact", "preact/hooks"},
		"/chunks/hooks.z.js":     {"preact"},
		"/chunks/isl-cycle-1.js": {"/chunks/isl-cycle-2.js"},
		"/chunks/isl-cycle-2.js": {"/chunks/isl-cycle-1.js"},
	}
	importMap := map[string]string{
		"preact":        "/chunks/preact.x.js",
		"preact/compat": "/chunks/compat.y.js",
		"preact/hooks":  "/chunks/hooks.z.js",
	}
	got := g.preloadClosure([]string{"/chunks/island_a.js", "/chunks/island_b.js", "/chunks/other.js"}, importMap)
	want := []string{
		"/chunks/preact.x.js", "/chunks/hooks.z.js", "/chunks/compat.y.js",
		"/chunks/isl-shared.js", "/chunks/island_a.js",
		"/chunks/isl-b.js", "/chunks/island_b.js",
		"/chunks/other.js",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("closure:\n got %v\nwant %v", got, want)
	}
	if got := g.preloadClosure([]string{"/chunks/isl-cycle-1.js"}, nil); !reflect.DeepEqual(got, []string{"/chunks/isl-cycle-2.js", "/chunks/isl-cycle-1.js"}) {
		t.Fatalf("cycle: %v", got)
	}
}

func TestIslandBootLineStrategies(t *testing.T) {
	t.Parallel()
	url := "/base/chunks/island_abc.js"
	for _, tc := range []struct {
		hydrate, want string
		literal       bool
	}{
		{"", "m.mount(\"i1\",el)", true},
		{"idle", "requestIdleCallback(go,{timeout:2000})", false},
		{"visible", "new IntersectionObserver(", false},
	} {
		e := islandExt("i1", tc.hydrate)
		line := islandBootLine(e, url)
		if !strings.Contains(line, tc.want) {
			t.Errorf("%q: boot line lacks %q:\n%s", tc.hydrate, tc.want, line)
		}
		// Only an island that mounts on load names its chunk whole, so the
		// page preloads only those.
		got := dynamicChunkURLs(line, "/base")
		if tc.literal != (len(got) == 1 && got[0] == "/chunks/island_abc.js") {
			t.Errorf("%q: preload scan found %v", tc.hydrate, got)
		}
	}
}

var modulePreloadRe = regexp.MustCompile(`<link rel="modulepreload" href="([^"]+)">`)

// TestIslandPagePreloadsGraph: the islands a page mounts on load share one
// entry, and the page preloads that entry with every module it imports
// statically (the Preact chunks through the import map), but nothing only a
// client:visible or client:idle island or a lazy import() loads.
func TestIslandPagePreloadsGraph(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	lib := func(name, src string) {
		writeProjectFile(t, root, "node_modules/"+name+"/package.json", `{"name":"`+name+`","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
		writeProjectFile(t, root, "node_modules/"+name+"/index.js", src)
	}
	lib("fake-deferred-core", `
		export function heavy(s) { return 'HEAVY_CORE:' + s; }
	`)
	lib("fake-deferred", `
		import { jsx } from 'react/jsx-runtime';
		import { heavy } from 'fake-deferred-core';
		export function Seen({ label }) { return jsx('em', { className: 'seen', children: heavy(label) }); }
	`)
	lib("fake-idle", `
		import { jsx } from 'react/jsx-runtime';
		import { heavy } from 'fake-deferred-core';
		export function Later({ label }) { return jsx('u', { className: 'later', children: heavy(label) }); }
	`)
	writeProjectFile(t, root, "node_modules/fake-lazy/package.json", `{"name":"fake-lazy","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`)
	writeProjectFile(t, root, "node_modules/fake-lazy/extra.js", `export const extra = 'LAZY_EXTRA';`)
	writeProjectFile(t, root, "node_modules/fake-lazy/index.js", `
		import { jsx } from 'react/jsx-runtime';
		import { useEffect, useState } from 'react';
		export function Lazy() {
			const [v, setV] = useState('wait');
			useEffect(() => { import('./extra.js').then((m) => setV(m.extra)); }, []);
			return jsx('s', { className: 'lazy', children: v });
		}
	`)
	writeProjectFile(t, root, "src/pages/index.tsx", `
		import { Badge, Counter } from 'fake-lib';
		import { Seen } from 'fake-deferred';
		import { Later } from 'fake-idle';
		import { Lazy } from 'fake-lazy';
		export default function Page() {
			return (
				<main>
					<Badge label="one" />
					<Counter />
					<Lazy />
					<Seen client:visible label="two" />
					<Later client:idle label="three" />
				</main>
			);
		}
	`)
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	entries := islandEntryFiles(cfg.OutDir)
	if len(entries) != 3 {
		t.Fatalf("want one entry for the islands that mount on load and one per deferred island, got %v", entries)
	}
	html := readBuilt(t, filepath.Join(cfg.OutDir, "index.html"))
	var preloads []string
	for _, m := range modulePreloadRe.FindAllStringSubmatch(html, -1) {
		preloads = append(preloads, m[1])
	}
	seen := map[string]bool{}
	var code strings.Builder
	for _, p := range preloads {
		if seen[p] {
			t.Fatalf("%s is preloaded twice: %v", p, preloads)
		}
		seen[p] = true
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(p, "/"))))
		if err != nil {
			t.Fatalf("preloaded file is missing: %v", err)
		}
		code.Write(data)
	}
	var preactN int
	for _, p := range preloads {
		if strings.Contains(p, "/chunks/preact-") {
			preactN++
		}
	}
	if preactN != 4 {
		t.Fatalf("want the 4 import-mapped Preact chunks preloaded, got %v", preloads)
	}
	all := code.String()
	for _, want := range []string{"badge", "counter"} {
		if !strings.Contains(all, want) {
			t.Fatalf("the preloaded modules lack the %q island:\n%v", want, preloads)
		}
	}
	var built strings.Builder
	chunkFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	for _, f := range chunkFiles {
		data, _ := os.ReadFile(f)
		built.Write(data)
	}
	for _, bad := range []string{"HEAVY_CORE", "LAZY_EXTRA", "className: \"seen\"", "className: \"later\""} {
		if !strings.Contains(built.String(), bad) {
			t.Fatalf("no chunk contains %q", bad)
		}
		if strings.Contains(all, bad) {
			t.Fatalf("a module only a deferred island or a lazy import loads is preloaded (%s): %v", bad, preloads)
		}
	}
	// Dependencies come before the modules that import them.
	pos := map[string]int{}
	for i, p := range preloads {
		pos[p] = i
	}
	for _, p := range preloads {
		data, _ := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(p, "/"))))
		for _, m := range islandSharedRe.FindAllStringSubmatch(string(data), -1) {
			dep := "/chunks/" + m[1]
			if i, ok := pos[dep]; ok && i > pos[p] {
				t.Fatalf("%s is preloaded after %s, which imports it: %v", dep, p, preloads)
			}
		}
	}
}
