package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStampSetFollowsTheFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.js")
	if err := os.WriteFile(a, []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	// A path that is not a file (the stand-in of a batched entry) is no input.
	s := stampFiles([]string{a, filepath.Join(dir, ".krate-batch-1", "entry.js")}, time.Now())
	if len(s) != 1 || !s.fresh() {
		t.Fatalf("stamps %v, fresh %v; want the one file, fresh", s, s.fresh())
	}
	if err := os.WriteFile(a, []byte("two!"), 0644); err != nil {
		t.Fatal(err)
	}
	if s.fresh() {
		t.Fatal("an edited file still counts as unchanged")
	}
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	if s.fresh() {
		t.Fatal("a file that changed size counts as unchanged")
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if s.fresh() {
		t.Fatal("a deleted file counts as unchanged")
	}
	var none stampSet
	if none.fresh() {
		t.Fatal("no stamps must never match")
	}
}

func TestStampFilesNeverTrustsAFileChangedDuringTheBuild(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.js")
	started := time.Now().Add(-time.Second)
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// The file was written after the build began, so the build may have read
	// it half way: it is never taken as unchanged.
	if s := stampFiles([]string{a}, started); s.fresh() {
		t.Fatal("a file written during the build counts as unchanged")
	}
}

func TestMetafileAllInputsKeepsFilesTheOutputDropped(t *testing.T) {
	meta := `{"inputs":{"src/a.ts":{"bytes":1},"src/dropped.ts":{"bytes":1},"krate-island-entry:x":{"bytes":1}},"outputs":{"o.js":{"inputs":{"src/a.ts":{"bytesInOutput":4},"src/dropped.ts":{"bytesInOutput":0}}}}}`
	got := metafileAllInputs("/root", meta)
	want := map[string]bool{"/root/src/a.ts": true, "/root/src/dropped.ts": true}
	if len(got) != len(want) {
		t.Fatalf("inputs %v, want %v", got, want)
	}
	for _, p := range got {
		if !want[filepath.ToSlash(p)] {
			t.Fatalf("unexpected input %s in %v", p, got)
		}
	}
}

const devMemoToggle = `"use client";
import { useState } from "react";
export default function Toggle() {
  const [on, setOn] = useState(false);
  return <button onClick={() => setOn(!on)}>{on ? "on" : "off"} LABEL</button>;
}
`

// A dev rebuild repeats only the builds an edit can change: the island chunks
// and the server render modules are kept while their files and entries stay,
// and built again when one of the files they read is edited.
func TestDevRebuildReusesBuildsTheEditDidNotTouch(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":       "export default function Home() { return <h1>home one</h1>; }\n",
		"src/components/Toggle.tsx": strings.Replace(devMemoToggle, "LABEL", "first", 1),
		"src/pages/toggle.tsx": `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>toggle one</h1><Toggle /></main>; }
`,
		"src/components/Other.tsx": strings.Replace(strings.ReplaceAll(devMemoToggle, "Toggle", "Other"), "LABEL", "other", 1),
		"src/pages/other.tsx": `import Other from "../components/Other";
export default async function Page() { return <main><h1>other one</h1><Other /></main>; }
`,
	})
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	withPreact(t, root)
	captureProgress(t)
	b.DevMode = true
	UseDevOutDir(root, b.Cfg)
	b.Prof = NewProfiler(true)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	reused := func() (islands, ssr int) {
		b.Prof.mu.Lock()
		defer b.Prof.mu.Unlock()
		return b.Prof.counts["dev:island-bundles-reused"], b.Prof.counts["dev:ssr-modules-reused"]
	}
	early := func() int {
		b.Prof.mu.Lock()
		defer b.Prof.mu.Unlock()
		return b.Prof.counts["dev:early-server-bundles"]
	}
	page := filepath.Join(root, "src", "pages", "toggle.tsx")
	edits := 0
	write := func(rel, src string) {
		t.Helper()
		writeProjectFile(t, root, rel, src)
		// Edits are told apart by their time stamps; a file that changed
		// within the build is never trusted, so the edit is dated in the past.
		edits++
		past := time.Now().Add(-time.Minute + time.Duration(edits)*time.Second)
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), past, past); err != nil {
			t.Fatal(err)
		}
	}
	rebuild := func() {
		t.Helper()
		b.buildStart = time.Time{}
		if _, err := b.BuildPages([]string{page}); err != nil {
			t.Fatal(err)
		}
	}
	// Two rebuilds: the first one records what the next may reuse.
	write("src/pages/toggle.tsx", strings.Replace(readProjectFile(t, root, "src/pages/toggle.tsx"), "toggle one", "toggle two", 1))
	rebuild()
	write("src/pages/toggle.tsx", strings.Replace(readProjectFile(t, root, "src/pages/toggle.tsx"), "toggle two", "toggle three", 1))
	i0, s0 := reused()
	e0 := early()
	rebuild()
	i1, s1 := reused()
	if early() <= e0 {
		t.Fatal("the page's server bundle was not compiled while the page built, though its render modules were kept")
	}
	_ = i0
	if s1 <= s0 {
		t.Fatalf("a page edit rebuilt the server render modules (reused %d -> %d)", s0, s1)
	}
	out := filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles")
	if !devMemoTreeHas(t, out, "toggle three") {
		t.Fatal("the rebuilt page bundle does not hold the edit")
	}

	// Editing the client component changes what both builds read.
	write("src/components/Toggle.tsx", strings.Replace(devMemoToggle, "LABEL", "second", 1))
	e1 := early()
	rebuild()
	if early() != e1 {
		t.Fatal("a server bundle was compiled early against render modules an edit was about to change")
	}
	i2, s2 := reused()
	if s2 != s1 || i2 != i1 {
		t.Fatalf("an edited client component was taken as unchanged (render modules reused %d -> %d, island chunks %d -> %d)", s1, s2, i1, i2)
	}
	if !devMemoTreeHas(t, filepath.Join(out, "client"), "second") {
		t.Fatal("the server render module does not hold the edited component")
	}
	if !devMemoTreeHas(t, filepath.Join(b.Cfg.OutDir, "chunks"), "second") {
		t.Fatal("the island chunk does not hold the edited component")
	}
}

// The island chunk build is kept while the entries and their files stay, and
// runs again when a file they read is edited.
func TestDevIslandChunksAreKeptUntilTheirFilesChange(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/components/Toggle.tsx": strings.Replace(devMemoToggle, "LABEL", "first", 1),
		"src/pages/toggle.tsx": `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>toggle</h1><Toggle /></main>; }
`,
	})
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	withPreact(t, root)
	captureProgress(t)
	b.DevMode = true
	UseDevOutDir(root, b.Cfg)
	b.Prof = NewProfiler(true)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	reused := func() int {
		b.Prof.mu.Lock()
		defer b.Prof.mu.Unlock()
		return b.Prof.counts["dev:island-bundles-reused"]
	}
	b.buildStart = time.Now()
	if err := b.writeIslandBundles(); err != nil {
		t.Fatal(err)
	}
	if reused() != 1 {
		t.Fatalf("island chunks were built again with nothing changed (reused %d)", reused())
	}
	past := time.Now().Add(-time.Minute)
	writeProjectFile(t, root, "src/components/Toggle.tsx", strings.Replace(devMemoToggle, "LABEL", "second", 1))
	if err := os.Chtimes(filepath.Join(root, "src", "components", "Toggle.tsx"), past, past); err != nil {
		t.Fatal(err)
	}
	b.buildStart = time.Now()
	if err := b.writeIslandBundles(); err != nil {
		t.Fatal(err)
	}
	if reused() != 1 {
		t.Fatalf("an edited component was taken as unchanged (reused %d)", reused())
	}
	if !devMemoTreeHas(t, filepath.Join(b.Cfg.OutDir, "chunks"), "second") {
		t.Fatal("the island chunk does not hold the edited component")
	}
}

func readProjectFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// devMemoTreeHas reports whether a file under dir holds text.
func devMemoTreeHas(t *testing.T, dir, text string) bool {
	t.Helper()
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), text) {
				found = true
			}
		}
		return nil
	})
	return found
}

func TestDevPageWorkersFillTheCoresButNotMore(t *testing.T) {
	cores := buildWorkerLimit()
	if got := devPageWorkers(1); got != cores {
		t.Fatalf("one page gets %d workers, want %d", got, cores)
	}
	if got := devPageWorkers(cores + 1); got != cores+1 {
		t.Fatalf("%d pages get %d workers, want one each", cores+1, got)
	}
	if got := devPageWorkers(1000); got != 2*cores {
		t.Fatalf("1000 pages get %d workers, want %d", got, 2*cores)
	}
}
