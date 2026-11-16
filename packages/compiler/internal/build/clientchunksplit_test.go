package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// clientChunkSplitFiles is a project of "use client" boundaries that share a
// large package and use different exports of it, with a dynamic import, a
// class component, a CommonJS dependency, a Node built-in and a server-only
// module.
func clientChunkSplitFiles() map[string]string {
	var big strings.Builder
	big.WriteString("export function widgetA() { return \"cls-alpha-only \" + core(1); }\n")
	big.WriteString("export function widgetB() { return \"cls-beta-only \" + core(2); }\n")
	big.WriteString("export function useBetaState() { return \"cls-beta-hook\"; }\n")
	big.WriteString("export function shared() { return \"cls-shared \" + core(3); }\n")
	big.WriteString("export function core(n) { let s = ''; ")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&big, "s += helper%d(n); ", i)
	}
	big.WriteString("return s; }\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&big, "function helper%d(n) { return n > %d ? \"big-%d\" : \"\"; }\n", i, i, i)
	}
	return map[string]string{
		"node_modules/big-lib/package.json": `{"name":"big-lib","version":"1.0.0","module":"index.js","main":"index.js","sideEffects":false}`,
		"node_modules/big-lib/index.js":     big.String(),
		"node_modules/cjs-lib/package.json": `{"name":"cjs-lib","version":"1.0.0","main":"index.js"}`,
		"node_modules/cjs-lib/index.js":     "exports.fmt = function (x) { return 'cls-cjs ' + x; };\n",
		"src/components/A.tsx": `"use client";
import { useState } from "react";
import { widgetA, shared } from "big-lib";
import { fmt } from "cjs-lib";
export default function A() {
  const [n, setN] = useState(0);
  return <button class={n > 1 ? "a-open" : "a-closed"} onClick={() => setN(n + 1)}>{widgetA()}{shared()}{fmt(n)}</button>;
}`,
		"src/components/B.tsx": `"use client";
import { widgetB, shared } from "big-lib";
export default function B() { return <p class="b-static">{widgetB()}{shared()}</p>; }`,
		"src/lib/lazy.ts": `import { shared } from "big-lib";
export function later() { return "cls-lazy " + shared(); }`,
		"src/components/C.tsx": `"use client";
import { shared } from "big-lib";
export default function C() {
  return <button onClick={() => import("../lib/lazy").then((m) => m.later())}>{shared()}</button>;
}`,
		"src/components/E.tsx": `"use client";
import { Component } from "react";
import { fmt } from "cjs-lib";
export default class E extends Component { render() { return <i class="e-class">{fmt(1)}</i>; } }`,
		"src/components/F.tsx": `"use client";
export default function F() { return <b class="f-only">f</b>; }`,
		"src/lib/disk.ts": `import { readFileSync } from "node:fs";
export function load() { return readFileSync("x", "utf8"); }`,
		"src/components/G.tsx": `"use client";
import { load } from "../lib/disk";
export default function G() { return <button onClick={() => load()}>g</button>; }`,
		"src/db.ts": `import "server-only";
export const secret = "s3cret";`,
		"src/components/D.tsx": `"use client";
import { secret } from "../db";
export default function D() { return <p>{secret}</p>; }`,
		"src/pages/index.tsx": `import A from "../components/A";
import B from "../components/B";
import C from "../components/C";
import E from "../components/E";
import F from "../components/F";
export default async function Home() {
  await Promise.resolve();
  return <main><A /><B /><C /><E /><F /></main>;
}`,
	}
}

// literalTokenRe finds the contents of the string literals of a chunk.
var literalTokenRe = regexp.MustCompile(`"([^"\\\n]*)"|'([^'\\\n]*)'`)

// dceTokens is what the CSS usage scan (pageCSSAfterDCE) finds in text: the
// candidates it contains as substrings.
func dceTokens(text string, candidates map[string]bool) []string {
	var out []string
	for c := range candidates {
		if strings.Contains(text, c) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// literalCandidates are the class-like words of the string literals of texts,
// without the import paths of chunks.
func literalCandidates(texts ...string) map[string]bool {
	out := map[string]bool{}
	for _, text := range texts {
		for _, m := range literalTokenRe.FindAllStringSubmatch(text, -1) {
			lit := m[1] + m[2]
			if strings.HasPrefix(lit, "./") || strings.HasPrefix(lit, "../") {
				continue
			}
			for _, w := range strings.Fields(lit) {
				out[w] = true
			}
		}
	}
	return out
}

// setDiff returns what a has that b lacks and what b has that a lacks.
func setDiff(a, b []string) (extra, missing []string) {
	in := func(s []string, x string) bool {
		i := sort.SearchStrings(s, x)
		return i < len(s) && s[i] == x
	}
	for _, x := range a {
		if !in(b, x) {
			extra = append(extra, x)
		}
	}
	for _, x := range b {
		if !in(a, x) {
			missing = append(missing, x)
		}
	}
	return extra, missing
}

func chunkReq(root, rel string) clientChunkReq {
	p := filepath.Join(root, filepath.FromSlash(rel))
	return clientChunkReq{key: p, resolveDir: filepath.Dir(p), entry: clientChunkEntry(p)}
}

// subset reports whether every element of a is in b.
func subset(a, b []string) bool {
	extra, _ := setDiff(a, b)
	return len(extra) == 0
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// candidateModules is a CSS module whose scoped classes are cands, for the
// usage scan of clientChunkSources.
func candidateModules(cands map[string]bool) map[string]*bundler.CSSModuleInfo {
	m := map[string]string{}
	for c := range cands {
		m[c] = c
	}
	return map[string]*bundler.CSSModuleInfo{"x.module.css": {Mappings: m}}
}

// For every boundary, what the build derives from a code-split check batch
// is what it derives from a separate build of the boundary: whether it is
// stateful, the inputs it reaches, the classes the CSS scan finds, and its
// error. The batch's own facts are bounded by the entry's own output below
// and its shared chunks above; a use the bounds do not decide checks the
// chunk on its own.
func TestClientChunkSplitBatchMatchesSeparateBuilds(t *testing.T) {
	files := clientChunkSplitFiles()
	b, root := scProject(t, files)
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	names := []string{"A", "B", "C", "E", "F", "G", "D"}
	for _, minify := range []bool{false, true} {
		for _, withD := range []bool{false, true} {
			n := len(names)
			if !withD {
				n-- // D fails the whole batch, which then builds each boundary alone
			}
			var set []clientChunkReq
			for _, c := range names[:n] {
				set = append(set, chunkReq(root, "src/components/"+c+".tsx"))
			}
			alone := New(root, b.Cfg)
			alone.Cfg.Minify = minify
			if err := alone.BuildAll(); err != nil {
				t.Fatal(err)
			}
			want := make([]*clientChunkResult, n)
			wantText := make([]string, n)
			for i, r := range set {
				want[i] = alone.buildClientChunk(r)
				if want[i].err == nil {
					wantText[i] = alone.clientChunkText[want[i].info.url]
				}
			}

			b.Cfg.Minify = minify
			b.clientChunks, b.clientChunkText = nil, nil
			jobs := make([]*roundJob, n)
			for i, r := range set {
				jobs[i] = &roundJob{input: r}
			}
			b.runClientChunkBatch(jobs)
			got := make([]*clientChunkResult, n)
			gotText := make([]string, n)
			for i, j := range jobs {
				got[i] = j.output.(*clientChunkResult)
				if got[i].err == nil {
					gotText[i] = b.clientChunkText[got[i].info.url]
				}
			}
			split := 0
			for i, r := range set {
				name := fmt.Sprintf("minify=%v withD=%v %s", minify, withD, names[i])
				g, w := got[i], want[i]
				if (g.err == nil) != (w.err == nil) || g.err != nil && g.err.Error() != w.err.Error() {
					t.Errorf("%s: error %v, alone %v", name, g.err, w.err)
					continue
				}
				if w.err != nil {
					continue
				}
				cands := literalCandidates(gotText[i], wantText[i])
				if f := g.info.split; f != nil {
					split++
					// own <= alone <= batch, for each fact.
					if statefulHookRe.MatchString(f.ownText) && !w.info.stateful || w.info.stateful && !g.info.stateful {
						t.Errorf("%s: stateful bounds own=%v batch=%v do not hold alone=%v", name, statefulHookRe.MatchString(f.ownText), g.info.stateful, w.info.stateful)
					}
					if !subset(keysOf(f.ownInputs), keysOf(w.info.inputs)) || !subset(keysOf(w.info.inputs), keysOf(g.info.inputs)) {
						t.Errorf("%s: inputs bounds do not hold:\nown %v\nalone %v\nbatch %v", name, keysOf(f.ownInputs), keysOf(w.info.inputs), keysOf(g.info.inputs))
					}
					own, mid, all := dceTokens(f.ownText, cands), dceTokens(wantText[i], cands), dceTokens(gotText[i], cands)
					if !subset(own, mid) || !subset(mid, all) {
						t.Errorf("%s: CSS scan bounds do not hold:\nown %v\nalone %v\nbatch %v", name, own, mid, all)
					}
				}
				stateful, err := b.chunkStateful(g.info)
				if err != nil || stateful != w.info.stateful {
					t.Errorf("%s: stateful %v (%v), alone %v", name, stateful, err, w.info.stateful)
				}
				var held []envPending
				for p := range g.info.inputs {
					held = append(held, envPending{path: p})
				}
				for p := range w.info.inputs {
					held = append(held, envPending{path: p})
				}
				cb := ClientBoundary{SourcePath: r.key, ChunkURL: g.info.url}
				code := b.clientCode(&bundler.Bundle{}, []ClientBoundary{cb}, held)
				for _, h := range held {
					if code[h.path] != w.info.inputs[h.path] {
						t.Errorf("%s: %s goes to the browser: %v, alone %v", name, h.path, code[h.path], w.info.inputs[h.path])
					}
				}
				texts := b.clientChunkSources([]ClientBoundary{cb}, candidateModules(cands))
				if len(texts) != 1 {
					t.Fatalf("%s: %d chunk sources", name, len(texts))
				}
				if extra, missing := setDiff(dceTokens(texts[0], cands), dceTokens(wantText[i], cands)); len(extra)+len(missing) > 0 {
					t.Errorf("%s: CSS scan finds %q more and %q fewer than alone", name, extra, missing)
				}
				switch names[i] {
				case "A", "E":
					if !w.info.stateful {
						t.Errorf("%s should be stateful", name)
					}
				case "B", "C", "F":
					if w.info.stateful {
						t.Errorf("%s should be stateless", name)
					}
				}
			}
			for i := range set {
				if (names[i] == "D" || names[i] == "G") && want[i].err == nil {
					t.Errorf("%s should fail its check", names[i])
				}
			}
			if !withD && split == 0 {
				t.Errorf("minify=%v: no boundary was checked in the split batch", minify)
			}
		}
	}
}

// Esbuild tree shakes a split batch as a whole and puts all the live code of
// a file in one chunk: a boundary's shared chunks hold code of a shared
// module that only other boundaries use, but its own output does not, and
// the batch prints the shared code once. The boundary with an import() is
// built alone.
func TestClientChunkSplitSharedChunksAreUpperBounds(t *testing.T) {
	b, root := scProject(t, clientChunkSplitFiles())
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	names := []string{"A", "B", "C", "E", "F"}
	entries := make([]batchEntry, len(names))
	for i, n := range names {
		r := chunkReq(root, "src/components/"+n+".tsx")
		entries[i] = batchEntry{Source: clientChunkSource(r.entry), ResolveDir: r.resolveDir, Sourcefile: "krate-client-boundary.js", Loader: api.LoaderJS}
	}
	opts, err := b.clientChunkOptions(b.newUnavailableGuard())
	if err != nil {
		t.Fatal(err)
	}
	outs, res := esbuildBatchSplit(opts, entries)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	own, all := map[string]string{}, map[string]string{}
	for i, n := range names {
		if outs[i].Own != 1 {
			t.Errorf("%s: %d own files, want its entry output", n, outs[i].Own)
		}
		own[n] = string(batchJS(outs[i].Files[:outs[i].Own]))
		all[n] = string(batchJS(outs[i].Files))
		if got := outs[i].Unsafe; got != (n == "C") {
			t.Errorf("%s: Unsafe %v", n, got)
		}
	}
	if len(outs[0].Files) < 2 {
		t.Errorf("A shares big-lib with B but has no chunk: %d files", len(outs[0].Files))
	}
	for _, c := range []struct{ entry, want string }{
		{"A", "cls-alpha-only"}, {"A", "big-299"}, {"B", "cls-beta-only"}, {"B", "cls-shared"}, {"E", "cls-cjs"}, {"F", "f-only"},
	} {
		if !strings.Contains(all[c.entry], c.want) {
			t.Errorf("%s misses %q", c.entry, c.want)
		}
	}
	for _, c := range []struct{ entry, not string }{
		{"A", "cls-beta-only"}, {"B", "cls-alpha-only"}, {"F", "cls-shared"}, {"B", "cls-cjs"},
	} {
		if strings.Contains(own[c.entry], c.not) {
			t.Errorf("%s's own output holds %q, which only other boundaries use", c.entry, c.not)
		}
	}
	// Where esbuild placed other boundaries' code with this one's.
	if !strings.Contains(all["A"], "cls-beta-only") {
		t.Log("esbuild split big-lib by export; the shared chunks are exact here")
	}
	var printed strings.Builder
	seen := map[string]bool{}
	for _, o := range outs {
		for _, f := range o.Files {
			if !seen[f.Path] {
				seen[f.Path] = true
				printed.Write(f.Contents)
			}
		}
	}
	if n := strings.Count(printed.String(), "\"big-150\""); n != 1 {
		t.Errorf("the batch printed big-lib's helpers %d times, want once", n)
	}
}

// A server-tree site whose boundaries share a package builds to the same
// output with and without code splitting in the check batches. A stateless
// package component shares a module with a stateful one, so its shared
// chunk names useState: it must still render as plain HTML (no island). The
// CSS-module classes of the pages are pruned the same way. (Server-tree pages
// render per request, so the CSS scan of client chunks, which runs for static
// pages, does not apply to them.)
func TestClientChunkSplitSiteOutputUnchanged(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	files := clientChunkSplitFiles()
	delete(files, "src/components/D.tsx")
	delete(files, "src/components/G.tsx")
	files["node_modules/ui-kit/package.json"] = `{"name":"ui-kit","version":"1.0.0","main":"index.js","sideEffects":false,"peerDependencies":{"react":"*"}}`
	files["node_modules/ui-kit/shared.js"] = `import { useState } from "react";
export function useThing() { const [v] = useState("kit-state"); return v; }
export function plain() { return "kit-plain"; }
`
	files["node_modules/ui-kit/icon.js"] = `"use client";
import { jsx } from "react/jsx-runtime";
import { plain } from "./shared.js";
export function Icon() { return jsx("i", { className: "kit-icon", children: plain() }); }
`
	files["node_modules/ui-kit/toggle.js"] = `"use client";
import { jsx } from "react/jsx-runtime";
import { useThing } from "./shared.js";
export function Toggle() { return jsx("b", { className: "kit-toggle", children: useThing() }); }
`
	files["src/pages/panel.module.css"] = ".frame { margin: 0; }\n.never { padding: 2px; }\n"
	files["src/pages/index.tsx"] = `import A from "../components/A";
import B from "../components/B";
import C from "../components/C";
import E from "../components/E";
import F from "../components/F";
import { Toggle } from "ui-kit/toggle.js";
import { Icon } from "ui-kit/icon.js";
export default async function Home() {
  await Promise.resolve();
  return <main><A /><B /><C /><E /><F /><Toggle /><Icon /></main>;
}`
	files["src/pages/icon.tsx"] = `import F from "../components/F";
import styles from "./panel.module.css";
export default async function IconPage() {
  await Promise.resolve();
  return <main class={styles.frame}><F /></main>;
}`
	b, root := scProject(t, files)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	cfg := b.Cfg
	cfg.Minify = true
	build := func(split bool) (map[string]string, *Builder) {
		old := clientChunkSplit
		clientChunkSplit = split
		defer func() { clientChunkSplit = old }()
		dist := filepath.Join(root, "dist")
		if err := os.RemoveAll(dist); err != nil {
			t.Fatal(err)
		}
		nb := New(root, cfg)
		if out, err := captureOutput(nb.BuildAll); err != nil {
			t.Fatalf("BuildAll split=%v: %v\n%s", split, err, out)
		}
		out := map[string]string{}
		_ = filepath.Walk(dist, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(dist, p)
			data, _ := os.ReadFile(p)
			out[filepath.ToSlash(rel)] = string(data)
			return nil
		})
		return out, nb
	}
	before := esbuildStatsSnapshot()
	split, sb := build(true)
	calls := esbuildStatsSince(before)
	unsplit, _ := build(false)
	t.Logf("esbuild calls of the split build: %v", calls)
	if calls["batch:(*Builder).runClientChunkBatchWith"].n == 0 {
		t.Error("no client chunk check batch ran")
	}
	t.Logf("package boundaries checked on their own: %d", calls["batch:(*Builder).checkClientChunkAlone"].n)
	if len(unsplit) == 0 {
		t.Fatal("the build wrote nothing")
	}
	for p, want := range unsplit {
		got, ok := split[p]
		if !ok {
			t.Errorf("%s is missing with splitting", p)
		} else if got != want {
			t.Errorf("%s differs with splitting:\n--- split\n%.2000s\n--- unsplit\n%.2000s", p, got, want)
		}
	}
	for p := range split {
		if _, ok := unsplit[p]; !ok {
			t.Errorf("%s is new with splitting", p)
		}
	}
	// A package boundary's Hydrate comes from stateful: none may keep an
	// undecided upper bound (exactChunk replaces it with its own check).
	sb.clientChunkMu.Lock()
	defer sb.clientChunkMu.Unlock()
	approx := 0
	for key, info := range sb.clientChunks {
		if info.split == nil {
			continue
		}
		approx++
		if !filepath.IsAbs(key) && !info.split.statefulSure {
			t.Errorf("package boundary %s: Hydrate was decided from the split batch's upper bound", key)
		}
	}
	if approx == 0 {
		t.Error("no boundary kept its split batch facts")
	}
}

// Boundaries that reach an import() are checked again together in an unsplit
// batch, not each on its own, and get the results of separate builds.
func TestClientChunkSplitRechecksLazyEntriesUnsplit(t *testing.T) {
	files := clientChunkSplitFiles()
	files["src/components/H.tsx"] = `"use client";
import { widgetB } from "big-lib";
export default function H() {
  return <button onClick={() => import("../lib/lazy").then((m) => m.later())}>{widgetB()}</button>;
}`
	b, root := scProject(t, files)
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	set := []clientChunkReq{
		chunkReq(root, "src/components/A.tsx"),
		chunkReq(root, "src/components/C.tsx"),
		chunkReq(root, "src/components/H.tsx"),
	}
	alone := New(root, b.Cfg)
	if err := alone.BuildAll(); err != nil {
		t.Fatal(err)
	}
	want := make([]*clientChunkResult, len(set))
	for i, r := range set {
		want[i] = alone.buildClientChunk(r)
	}
	b.clientChunks, b.clientChunkText = nil, nil
	jobs := make([]*roundJob, len(set))
	for i, r := range set {
		jobs[i] = &roundJob{input: r}
	}
	before := esbuildStatsSnapshot()
	b.runClientChunkBatch(jobs)
	calls := esbuildStatsSince(before)
	if n := calls["(*Builder).buildClientChunk"].n; n != 0 {
		t.Errorf("%d boundaries were checked on their own, want the lazy ones rechecked in one unsplit batch: %v", n, calls)
	}
	if n := calls["batch:(*Builder).runClientChunkBatchWith"].n; n != 2 {
		t.Errorf("want a split batch and one unsplit batch, got %d batches: %v", n, calls)
	}
	for i, j := range jobs {
		g, w := j.output.(*clientChunkResult), want[i]
		if g.err != nil || w.err != nil {
			t.Fatalf("%s: error %v, alone %v", set[i].key, g.err, w.err)
		}
		if g.info.stateful != w.info.stateful {
			t.Errorf("%s: stateful %v, alone %v", set[i].key, g.info.stateful, w.info.stateful)
		}
		if strings.Join(keysOf(g.info.inputs), ",") != strings.Join(keysOf(w.info.inputs), ",") {
			t.Errorf("%s: inputs %v, alone %v", set[i].key, keysOf(g.info.inputs), keysOf(w.info.inputs))
		}
	}
}
