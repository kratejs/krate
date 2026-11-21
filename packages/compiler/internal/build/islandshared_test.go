package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// sharedCounterProject writes a page with a Preact island and a real-React
// island that both import modules with module-level state and no React:
// src/lib/counter.ts (no imports), src/lib/tally.ts (imports a sibling with no
// imports) and the workspace package @ws/shared (no React dependency); and
// modules with state that do reach React: src/lib/hits.ts (imports react) and
// src/lib/use-hits.ts (through hits.ts). With boundary the page is async and
// both components are "use client" boundaries, so the Preact graph also
// injects the boundary shim into every module.
func sharedCounterProject(t *testing.T, boundary bool) (string, *config.Config) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	e2eEnv(t)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	realReactFixtureFrom(t, root, repo)
	writeProjectFile(t, root, "src/lib/counter.ts", `let count = 0;
export function next() {
	if (count < 0) throw new Error('shared-counter-mark');
	return ++count;
}
export function spare() { return 'counter-spare-mark'; }
`)
	// An npm library both islands import directly: shared, and still tree
	// shaken to what they use.
	writeProjectFile(t, root, "node_modules/neutrallib/package.json", `{"name":"neutrallib","version":"1.0.0","type":"module","main":"index.js","sideEffects":false}`)
	writeProjectFile(t, root, "node_modules/neutrallib/index.js", `export function used() { return 'neutral-used-mark'.length; }
export function unused() { return 'neutral-unused-mark'; }
export default function tag() { return 'neutral-default-mark'.length; }
`)
	writeProjectFile(t, root, "src/lib/tally-base.ts", `export const start = 0;
`)
	writeProjectFile(t, root, "src/lib/tally.ts", `import { start } from './tally-base';
let n = start;
export const tally = () => ++n;
`)
	writeProjectFile(t, root, "packages/ws-shared/package.json", `{"name":"@ws/shared","version":"1.0.0","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "packages/ws-shared/index.js", `let wsCount = 0;
export const bump = () => ++wsCount;
`)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@ws"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "packages", "ws-shared"), filepath.Join(root, "node_modules", "@ws", "shared")); err != nil {
		t.Skipf("cannot link the workspace package: %v", err)
	}
	// A workspace package whose b.ts imports a type back from a.ts: no
	// runtime cycle, shared, and a.ts runs after b.ts.
	writeProjectFile(t, root, "packages/ws-order/package.json", `{"name":"@ws/order","version":"1.0.0","main":"src/index.ts"}`)
	writeProjectFile(t, root, "packages/ws-order/src/index.ts", "export * from './a';\nexport * from './b';\n")
	writeProjectFile(t, root, "packages/ws-order/src/a.ts", "import { B } from './b';\nexport type T = string;\nexport const A = [...B];\n")
	writeProjectFile(t, root, "packages/ws-order/src/b.ts", "import type { T } from './a';\nexport const B: T[] = ['x'];\n")
	if err := os.Symlink(filepath.Join(root, "packages", "ws-order"), filepath.Join(root, "node_modules", "@ws", "order")); err != nil {
		t.Skipf("cannot link the workspace package: %v", err)
	}
	// A real cycle (b-kinds -> template -> a-stack -> b-kinds) where a-stack
	// spreads an array of b-kinds at its top level, as fx-stack and fx-kinds
	// do in a real app. The islands enter it at a-stack, so b-kinds runs
	// first. The cycle is shared as a whole and must run in that order too
	// (run from b-kinds, a-stack throws: EXTRA is not iterable).
	writeProjectFile(t, root, "src/lib/cyc/b-kinds.ts", `import { tpl } from './template';
export const EXTRA = ['bevel'];
export const viaTemplate = () => tpl();
`)
	writeProjectFile(t, root, "src/lib/cyc/template.ts", `import { stackKinds } from './a-stack';
export const tpl = () => stackKinds();
`)
	writeProjectFile(t, root, "src/lib/cyc/a-stack.ts", `import { EXTRA } from './b-kinds';
export const KINDS = ['kind-shadow', ...EXTRA];
export const stackKinds = () => KINDS;
`)
	// The word Link in a comment, a type, a string, an object key and a member
	// name, none of them the compiler-provided Link the boundary shim injects.
	writeProjectFile(t, root, "src/lib/links.ts", `/** Link a number property to a colour. */
export type Link = { to: string };
const desc = 'Link layer properties to an option.';
const o = { Link: 1 };
let linkCount = 0;
export const linked = (l?: Link) => (desc && o.Link && !l ? ++linkCount : 0);
`)
	writeProjectFile(t, root, "src/lib/use-hits.ts", `import { useHits } from './hits';
let uses = 0;
export function useCountedHits() {
	uses++;
	return useHits();
}
`)
	writeProjectFile(t, root, "src/lib/hits.ts", `import { useState } from 'react';
let hits = 0;
export function useHits() {
	const [n] = useState(() => ++hits);
	return n;
}
`)
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import tag, { used } from 'neutrallib';
import { bump } from '@ws/shared';
import { A } from '@ws/order';
import { KINDS } from '../lib/cyc/a-stack';
import { EXTRA, viaTemplate } from '../lib/cyc/b-kinds';
import { linked } from '../lib/links';
import { next } from '../lib/counter';
import { tally } from '../lib/tally';
import { useCountedHits } from '../lib/use-hits';
export default function Scene() {
	const [n, setN] = useState('0');
	useCountedHits();
	useEffect(() => { setN(next() + ',' + tally() + ',' + bump() + ',' + linked()); }, []);
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{n}</p>
			<p id="real-extra">{JSON.stringify(A) + '|' + KINDS.join(',') + '|' + EXTRA.length + '|' + viaTemplate().length}</p>
			<i hidden>{used() + tag()}</i>
		</div>
	);
}
`)
	// A hook from an npm package built on React makes Count a Preact island.
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useOnce(fn) {
	const [v, setV] = useState(0);
	useEffect(() => { setV(fn()); }, []);
	return v;
}
`)
	directive, async := "", ""
	if boundary {
		directive, async = "'use client';\n", "async "
	}
	writeProjectFile(t, root, "src/components/Count.tsx", directive+`import { useOnce } from 'countlib';
import { used } from 'neutrallib';
import { bump } from '@ws/shared';
import { A } from '@ws/order';
import { KINDS } from '../lib/cyc/a-stack';
import { EXTRA, viaTemplate } from '../lib/cyc/b-kinds';
import { linked } from '../lib/links';
import { next } from '../lib/counter';
import { tally } from '../lib/tally';
import { useCountedHits } from '../lib/use-hits';
const all = () => next() + ',' + tally() + ',' + bump() + ',' + linked();
export default function Count() {
	useCountedHits();
	const n = useOnce(all);
	return (
		<div>
			<p id="preact" data-used={used()}>{n}</p>
			<p id="preact-extra">{JSON.stringify(A) + '|' + KINDS.join(',') + '|' + EXTRA.length + '|' + viaTemplate().length}</p>
		</div>
	);
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
import Count from '../components/Count';
export default `+async+`function Page() { return <main><h1>home</h1><Scene /><Count /></main>; }
`)
	return root, cfg
}

// TestIslandModuleSharedAcrossFlavours: modules a Preact island and a
// real-React island both import, with no React anywhere in their imports, ship
// once, in shared chunks both import, and are not reported; a stateful module
// that reaches React is reported with its import chain.
func TestIslandModuleSharedAcrossFlavours(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	for _, boundary := range []bool{false, true} {
		name := "islands"
		if boundary {
			name = "client boundaries"
		}
		t.Run(name, func(t *testing.T) {
			root, cfg := sharedCounterProject(t, boundary)
			out, err := captureBuild(New(root, cfg))
			if err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			if boundary {
				if shims, _ := filepath.Glob(filepath.Join(cfg.OutDir, ".krate", "boundary-link.*.mjs")); len(shims) == 0 {
					t.Fatal("the page has no client boundary, so the case it covers is not exercised")
				}
			}
			checkSharedChunks(t, cfg, out)
		})
	}
}

func checkSharedChunks(t *testing.T, cfg *config.Config, out string) {
	t.Helper()
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	var importers []string
	marks := map[string][]string{}
	all := []string{"shared-counter-mark", "neutral-used-mark", "neutral-default-mark", "neutral-unused-mark", "counter-spare-mark", "++n", "++wsCount", "++linkCount", "kind-shadow"}
	for _, c := range chunks {
		text := readBuilt(t, c)
		for _, m := range all {
			if strings.Contains(text, m) {
				marks[m] = append(marks[m], filepath.Base(c))
			}
		}
		if !strings.HasPrefix(filepath.Base(c), "isn") && strings.Contains(text, "./"+islandNeutralEntryPrefix) {
			importers = append(importers, filepath.Base(c))
		}
	}
	// counter.ts (no imports), tally.ts (a sibling with no imports), the
	// workspace package and an npm library both import directly.
	// links.ts mentions Link only in a comment, a type, a string, an object
	// key and a member name, so it is shared even where the shim is injected.
	for _, m := range []string{"shared-counter-mark", "++n", "++wsCount", "++linkCount", "neutral-used-mark", "neutral-default-mark"} {
		if len(marks[m]) != 1 || !strings.HasPrefix(marks[m][0], "isn") {
			t.Errorf("%q should ship once, in a shared isn chunk; found in %v", m, marks[m])
		} else if marks[m][0] != strings.ToLower(marks[m][0]) {
			t.Errorf("shared chunk name is not lower-case: %s", marks[m][0])
		}
	}
	// The cycle ships once, in a shared chunk, like any other shared module.
	if len(marks["kind-shadow"]) != 1 || !strings.HasPrefix(marks["kind-shadow"][0], "isn") {
		t.Errorf("want the import cycle in exactly one shared isn chunk, found in %v", marks["kind-shadow"])
	}
	// One Preact and one real-React chunk at least.
	if len(importers) < 2 {
		t.Errorf("want both island bundles to import the shared chunks, importers: %v", importers)
	}
	for _, m := range []string{"neutral-unused-mark", "counter-spare-mark"} {
		if len(marks[m]) > 0 {
			t.Errorf("an export no island uses was not tree shaken: %s in %v", m, marks[m])
		}
	}
	for _, want := range []string{
		`src/lib/hits.ts is bundled twice`,
		`src/lib/hits.ts -> "react"`,
		`src/lib/use-hits.ts -> src/lib/hits.ts -> "react"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want KR8017 output to contain %q, got:\n%s", want, out)
		}
	}
	for _, bad := range []string{"counter.ts is bundled twice", "tally.ts is bundled twice", "tally-base.ts is bundled twice", "links.ts", "ws-shared", "ws-order", "cyc/", "import cycle", `("")`, `-> ""`} {
		if strings.Contains(out, bad) {
			t.Errorf("KR8017 output must not contain %q:\n%s", bad, out)
		}
	}
}

// TestBrowserIslandModuleSharedAcrossFlavours: both islands call next(),
// tally() and bump() from the shared modules once, so between them they see
// 1,1,1 and 2,2,2.
func TestBrowserIslandModuleSharedAcrossFlavours(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_shared.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	for _, boundary := range []bool{false, true} {
		name := "islands"
		if boundary {
			name = "client boundaries"
		}
		t.Run(name, func(t *testing.T) {
			root, cfg := sharedCounterProject(t, boundary)
			if out, err := captureBuild(New(root, cfg)); err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			base, err := startKrateServer(t, root, cfg)
			if err != nil {
				t.Fatalf("serve: %v", err)
			}
			out, err := exec.Command("node", script, browser, base).CombinedOutput()
			if err != nil {
				if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
					requireE2E(t, "%s browser is not installed: %s", browser, out)
				}
				t.Fatalf("browser checks failed: %v\n%s", err, out)
			}
		})
	}
}

func TestSharedImportsUsed(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/p/a.ts": "isn_aaaaaaaaaaaa.js", "/p/b.ts": "isn_bbbbbbbbbbbb.js", "/p/c.ts": "isn_cccccccccccc.js", "/p/d.ts": "isn_dddddddddddd.js", "/p/e.ts": "isn_eeeeeeeeeeee.js", "/p/f.ts": "isn_ffffffffffff.js", "/p/g.ts": "isn_999999999999.js"}
	out := []api.OutputFile{{Path: "/out/island_1.js", Contents: []byte(
		`import x,{next as o,prev}from"./isn_aaaaaaaaaaaa.js";import{"odd-name" as q}from"./isn_ffffffffffff.js";import*as N from"./isn_bbbbbbbbbbbb.js";` +
			`import"./isn_cccccccccccc.js";export{y as z}from"./isn_aaaaaaaaaaaa.js";const l=()=>import("./isn_dddddddddddd.js");` +
			// A stub imports every name: only the referenced locals count, a
			// spread is a reference and a property name is not.
			`import{kept as k,spread as s,dropped as d,alsoDropped}from"./isn_999999999999.js";console.log(x,o(prev),k(),[...s],w.d,w.alsoDropped);`)}}
	used := sharedImportsUsed([][]api.OutputFile{out}, files)
	a := used["/p/a.ts"]
	if a == nil || a.all || !a.names["default"] || !a.names["next"] || !a.names["prev"] || !a.names["y"] || len(a.names) != 4 {
		t.Errorf("a.ts: %+v", a)
	}
	if b := used["/p/b.ts"]; b == nil || !b.all {
		t.Errorf("a namespace import needs every export: %+v", b)
	}
	if c := used["/p/c.ts"]; c == nil || c.all || len(c.names) != 0 {
		t.Errorf("a bare import needs only the module's effects: %+v", c)
	}
	if d := used["/p/d.ts"]; d == nil || !d.all {
		t.Errorf("import() needs every export: %+v", d)
	}
	if f := used["/p/f.ts"]; f == nil || !f.all {
		t.Errorf("an import it cannot read keeps every export: %+v", f)
	}
	if g := used["/p/g.ts"]; g == nil || g.all || !g.names["kept"] || !g.names["spread"] || len(g.names) != 2 {
		t.Errorf("g.ts should need kept and spread only: %+v", g)
	}
	if e := used["/p/e.ts"]; e != nil {
		t.Errorf("a module no graph imports needs no chunk: %+v", e)
	}
}

// TestIslandShareCheckChains: the Preact graph injects a shim into every
// module (a page with client boundaries), which must not stop React-free
// modules from being shared; the reasons name the real import chain.
func TestIslandShareCheckChains(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "src/uses-link.ts", "export const L = Link;\n")
	imp := func(path, spec string) islandMetaImport {
		return islandMetaImport{Path: path, Kind: "import-statement", Original: spec}
	}
	synth := func(path string) islandMetaImport {
		return islandMetaImport{Path: path, Kind: "import-statement", External: true}
	}
	type in = struct {
		Format  string             `json:"format"`
		Imports []islandMetaImport `json:"imports"`
	}
	graph := func(preact bool) *islandMetafile {
		react := imp("node_modules/react/index.js", "react")
		if preact {
			react = islandMetaImport{Path: "preact/compat", Kind: "import-statement", Original: "react", External: true}
		}
		m := &islandMetafile{Inputs: map[string]in{
			"src/protection.ts":              {Format: "esm", Imports: []islandMetaImport{imp("src/wasm-kdf.ts", "./wasm-kdf"), imp("src/generated.ts", "@/generated")}},
			"src/wasm-kdf.ts":                {Format: "esm"},
			"src/generated.ts":               {Format: "esm"},
			"src/store.ts":                   {Format: "esm", Imports: []islandMetaImport{imp("node_modules/zustand/index.mjs", "zustand")}},
			"node_modules/zustand/index.mjs": {Format: "esm", Imports: []islandMetaImport{react}},
			"src/uses-cjs.ts":                {Format: "esm", Imports: []islandMetaImport{imp("node_modules/cjs/index.js", "cjs")}},
			"node_modules/cjs/index.js":      {Format: "cjs"},
			"src/uses-link.ts":               {Format: "esm"},
			"src/island.tsx":                 {Format: "esm", Imports: []islandMetaImport{react, imp("src/protection.ts", "./protection"), imp("src/store.ts", "./store"), imp("node_modules/cjs/index.js", "cjs"), imp("src/uses-link.ts", "./uses-link")}},
		}}
		if !preact {
			m.Inputs["node_modules/react/index.js"] = in{Format: "cjs"}
		}
		for p, v := range m.Inputs {
			v.Imports = append(v.Imports, synth("/x/.krate/context-keys.mjs"))
			if preact {
				v.Imports = append(v.Imports, synth("/x/.krate/boundary-link.mjs"))
			}
			m.Inputs[p] = v
		}
		return m
	}
	metas := []*islandMetafile{graph(false), graph(true)}
	plan := planSharedIslandModules(root, metas, []string{"Link"})
	key := func(p string) string { return canonicalMetaKey(root, p) }
	for _, p := range []string{"src/protection.ts", "src/wasm-kdf.ts", "src/generated.ts"} {
		if !plan.shared[key(p)] {
			t.Errorf("%s reaches no React and should be shared", p)
		}
	}
	for _, p := range []string{"src/store.ts", "node_modules/cjs/index.js", "src/uses-cjs.ts", "src/uses-link.ts", "src/island.tsx"} {
		if plan.shared[key(p)] {
			t.Errorf("%s must not be shared", p)
		}
	}
	c := newIslandShareCheck(root, metas, []string{"Link"})
	c.close()
	for p, want := range map[string]string{
		"src/store.ts":     `src/store.ts -> zustand/index.mjs -> "react"`,
		"src/uses-link.ts": `src/uses-link.ts (uses the compiler-provided Link`,
	} {
		if got, _ := c.chain(key(p)); !strings.HasPrefix(got, want) {
			t.Errorf("chain of %s = %q, want prefix %q", p, got, want)
		}
	}
	if _, react := c.chain(key("src/store.ts")); !react {
		t.Error("the zustand chain ends at react")
	}
}

func TestFreeGlobalUse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	names := regexp.MustCompile(`\b(Link|Image)\b`)
	for src, want := range map[string]string{
		"/** Link a property. */\nexport const a = 1;\n":                            "",
		"export const s = 'Link layer properties';\n":                               "",
		"export const o = { Link: 1 };\nexport const v = o.Link;\n":                 "",
		"export type Link = { to: string };\nexport const f = (l: Link) => l.to;\n": "",
		"import { Link } from './tags';\nexport const C = Link;\n":                  "",
		"function Link() {}\nexport const C = Link;\n":                              "",
		"export const C = Link;\n":                                                  "Link",
		"export const tags = { Link };\n":                                           "Link",
		"export const make = () => [Image];\n":                                      "Image",
	} {
		p := filepath.Join(root, "m.ts")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		if got := freeGlobalUse(p, names); got != want {
			t.Errorf("freeGlobalUse(%q) = %q, want %q", src, got, want)
		}
	}
}

// TestIslandShareCheckCycle: an import cycle is shared as a whole, entered at
// the member the islands reach first, and a shared module that imports a
// member does so through the member's facade.
func TestIslandShareCheckCycle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	imp := func(path, spec string) islandMetaImport {
		return islandMetaImport{Path: path, Kind: "import-statement", Original: spec}
	}
	type in = struct {
		Format  string             `json:"format"`
		Imports []islandMetaImport `json:"imports"`
	}
	type out = struct {
		EntryPoint string `json:"entryPoint"`
	}
	graph := func() *islandMetafile {
		return &islandMetafile{Inputs: map[string]in{
			"src/kinds.ts":    {Format: "esm", Imports: []islandMetaImport{imp("src/template.ts", "./template")}},
			"src/template.ts": {Format: "esm", Imports: []islandMetaImport{imp("src/stack.ts", "./stack")}},
			"src/stack.ts":    {Format: "esm", Imports: []islandMetaImport{imp("src/kinds.ts", "./kinds")}},
			"src/user.ts":     {Format: "esm", Imports: []islandMetaImport{imp("src/stack.ts", "./stack")}},
			"src/plain.ts":    {Format: "esm"},
			"src/island.tsx":  {Format: "esm", Imports: []islandMetaImport{imp("src/user.ts", "./user"), imp("src/kinds.ts", "./kinds"), imp("src/plain.ts", "./plain")}},
		}, Outputs: map[string]out{"island.js": {EntryPoint: "src/island.tsx"}}}
	}
	metas := []*islandMetafile{graph(), graph()}
	metas[1].Inputs["src/island.tsx"] = in{Format: "esm", Imports: append(metas[1].Inputs["src/island.tsx"].Imports, islandMetaImport{Path: "preact/compat", Kind: "import-statement", Original: "react", External: true})}
	plan := planSharedIslandModules(root, metas, nil)
	key := func(p string) string { return canonicalMetaKey(root, p) }
	for _, p := range []string{"src/plain.ts", "src/kinds.ts", "src/template.ts", "src/stack.ts", "src/user.ts"} {
		if !plan.shared[key(p)] {
			t.Errorf("%s should be shared", p)
		}
	}
	if len(plan.cycles) != 1 || len(plan.cycles[0].members) != 3 {
		t.Fatalf("want one cycle of three modules, got %+v", plan.cycles)
	}
	// island.tsx -> user.ts -> stack.ts comes before island.tsx -> kinds.ts.
	if got := plan.cycles[0].root; got != key("src/stack.ts") {
		t.Errorf("cycle root = %s, want stack.ts, where the island enters it", got)
	}
	if got := plan.inner[neutralEdgeKey(key("src/user.ts"), "./stack")]; got != key("src/stack.ts") {
		t.Errorf("user.ts should import stack.ts through its facade, inner = %v", plan.inner)
	}
	for _, p := range []string{"src/stack.ts", "src/kinds.ts"} {
		if plan.facades[key(p)] == "" {
			t.Errorf("%s is imported from outside the cycle and needs a facade: %v", p, plan.facades)
		}
	}
	if plan.facades[key("src/template.ts")] != "" {
		t.Errorf("template.ts is only imported inside the cycle and needs no facade")
	}
	c := newIslandShareCheck(root, metas, nil)
	c.settle()
	if got := c.display(filepath.ToSlash(filepath.Join(canonicalPath(filepath.Dir(root)), "shared", "src", "index.ts"))); got != "../shared/src/index.ts" {
		t.Errorf("a workspace package next to the app displays as %q, want ../shared/src/index.ts", got)
	}
}

// TestWriteIslandChunkFilesRenames: shared chunk names are lower-cased in the
// file names and in every file that names them, as replacing each name in
// turn does, source map names included.
func TestWriteIslandChunkFilesRenames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := []api.OutputFile{
		{Path: "/out/island_one.js", Contents: []byte(`import{a}from"./isl-AB2CD3EF.js";import"./isl-ZZ7Y6X5W.js";import"./isl-abc23456.js";a("isl-AB2CD3EF.js");`)},
		{Path: "/out/isl-AB2CD3EF.js", Contents: []byte("import\"./isl-ZZ7Y6X5W.js\";export const a=x=>x;\n//# sourceMappingURL=isl-AB2CD3EF.js.map\n")},
		{Path: "/out/isl-AB2CD3EF.js.map", Contents: []byte(`{"file":"isl-AB2CD3EF.js"}`)},
		{Path: "/out/isl-ZZ7Y6X5W.js", Contents: []byte("export{};")},
		{Path: "/out/isl-abc23456.js", Contents: []byte("export const s=\"isl-ZZ7Y6X5WQ.js\";")},
	}
	// What replacing each name in turn gives.
	want := map[string]string{}
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.HasSuffix(base, ".map") {
			continue
		}
		data := string(f.Contents)
		for _, g := range files {
			from := filepath.Base(g.Path)
			if strings.HasPrefix(from, islandSharedStem) {
				data = strings.ReplaceAll(data, from, strings.ToLower(from))
			}
		}
		want[strings.ToLower(base)] = data
	}
	keep := map[string]bool{}
	if err := writeIslandChunkFiles(files, dir, islandSharedStem, keep); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(want) {
		t.Errorf("wrote %d files, want %d", len(entries), len(want))
	}
	for name, data := range want {
		if !keep[name] {
			t.Errorf("%s is not kept", name)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if string(got) != data {
			t.Errorf("%s:\n got %s\nwant %s", name, got, data)
		}
	}
	if !strings.Contains(want["island_one.js"], `"./isl-ab2cd3ef.js"`) || !strings.Contains(want["isl-ab2cd3ef.js"], "isl-ab2cd3ef.js.map") {
		t.Errorf("the reference does not rename, so the test checks nothing: %v", want)
	}
}

// TestSharedImportsUsedAcrossFiles: the needs of every built file are merged,
// whatever file reads them first.
func TestSharedImportsUsedAcrossFiles(t *testing.T) {
	t.Parallel()
	files := map[string]string{"/p/a.ts": "isn_aaaaaaaaaaaa.js", "/p/b.ts": "isn_bbbbbbbbbbbb.js", "/p/c.ts": "isn_cccccccccccc.js", "/p/odd.ts": "isn_odd.js"}
	var graph []api.OutputFile
	for i := range 40 {
		var text string
		switch i % 4 {
		case 0:
			text = `import{one as o}from"./isn_aaaaaaaaaaaa.js";o();`
		case 1:
			text = `import{two as t,three}from"./isn_aaaaaaaaaaaa.js";t();import"./isn_bbbbbbbbbbbb.js";`
		case 2:
			// import() of a stem of another shape needs the whole module.
			text = `const l=()=>import("./isn_odd.js");`
		default:
			text = `import{four as f}from"./isn_cccccccccccc.js";const m=()=>import("./isn_cccccccccccc.js");f(m);`
		}
		graph = append(graph, api.OutputFile{Path: "/out/island_" + strconv.Itoa(i) + ".js", Contents: []byte(text)})
	}
	graph = append(graph, api.OutputFile{Path: "/out/island_x.js.map", Contents: []byte(`import*as N from"./isn_bbbbbbbbbbbb.js"`)})
	used := sharedImportsUsed([][]api.OutputFile{graph[:20], graph[20:]}, files)
	if a := used["/p/a.ts"]; a == nil || a.all || !a.names["one"] || !a.names["two"] || len(a.names) != 2 {
		t.Errorf("a.ts should need one and two: %+v", a)
	}
	if b := used["/p/b.ts"]; b == nil || b.all || len(b.names) != 0 {
		t.Errorf("b.ts is only imported bare (a source map does not count): %+v", b)
	}
	if c := used["/p/c.ts"]; c == nil || !c.all {
		t.Errorf("c.ts is loaded with import(): %+v", c)
	}
	if o := used["/p/odd.ts"]; o == nil || !o.all {
		t.Errorf("odd.ts is loaded with import(): %+v", o)
	}
}

func TestNeutralFileRefs(t *testing.T) {
	t.Parallel()
	text := `"./isn_0123456789ab.js" ./isn_0123456789ab.js ../isn_0123456789ab.js ./isn_0123456789AB.js ./isn_0123456789a.js ./isn_0123456789abc.js ./isn_ffffffffffff.mjs ./isn_ffffffffffff.js`
	got := neutralFileRefs(text)
	want := map[string]int{"isn_0123456789ab": 3, "isn_ffffffffffff": 1}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for stem, n := range want {
		if got[stem] != n || strings.Count(text, "./"+stem+".js") != n {
			t.Errorf("%s: got %d, strings.Count %d, want %d", stem, got[stem], strings.Count(text, "./"+stem+".js"), n)
		}
	}
}

func TestForEachIndex(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1, 3, 500} {
		seen := make([]atomic.Int32, n)
		forEachIndex(n, func(i int) { seen[i].Add(1) })
		for i := range seen {
			if c := seen[i].Load(); c != 1 {
				t.Errorf("n=%d: index %d ran %d times", n, i, c)
			}
		}
	}
}
