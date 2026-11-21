package build

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// islandHashFiles: an async layout whose provider island and a page's island
// share a module, and a synchronous page whose package island its page
// script imports.
var islandHashFiles = map[string]string{
	"src/components/shared.ts": `import { createContext } from 'react';
export const Ctx = createContext('none');
export const VERSION = 'v1';
`,
	"src/components/Provider.tsx": `'use client';
import { Ctx, VERSION } from './shared';
export default function Provider({ children }: { children: any }) { return <Ctx.Provider value={VERSION}><div id="prov">{children}</div></Ctx.Provider>; }
`,
	"src/components/Reader.tsx": `'use client';
import { useContext, useState } from 'react';
import { Ctx } from './shared';
export default function Reader() { const v = useContext(Ctx); const [n, setN] = useState(0); return <button id="reader" onClick={() => setN(n + 1)}>{v}:{n}</button>; }
`,
	"src/pages/app/_layout.tsx": `import Provider from '../../components/Provider';
export default async function L({ children }: { children: any }) { await Promise.resolve(); return <Provider><main>{children}</main></Provider>; }
`,
	"src/pages/app/index.tsx": `import Reader from '../../components/Reader';
export default async function P() { await Promise.resolve(); return <section><Reader /></section>; }
`,
	"src/pages/index.tsx": `import { Counter } from 'pkg-widgets';
import { createSignal } from '@krate/runtime';
export default function Home() { const [x, setX] = createSignal(0); return <main><button id="k" onClick={() => setX(x() + 1)}>{x()}</button><Counter /></main>; }
`,
	"node_modules/pkg-widgets/package.json": `{"name":"pkg-widgets","version":"1.0.0","type":"module","main":"index.js"}`,
	"node_modules/pkg-widgets/index.js": `import { jsx } from 'react/jsx-runtime';
import { useState } from 'react';
export function Counter() { const [n, setN] = useState(0); return jsx('button', { id: 'c', onClick: () => setN(n + 1), children: String(n) }); }
`,
}

// servedFiles lists the files a build serves (not its documents, which are
// never cached, nor its precompressed copies) with the md5 of each.
func servedFiles(t *testing.T, outDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if isUnderKrateDir(outDir, p) {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".html", ".br", ".gz":
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sum := md5.Sum(data)
		rel, _ := filepath.Rel(outDir, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return out
}

var (
	stableEntryRe = regexp.MustCompile(`island_[0-9a-f]{12}\.js`)
	hashedEntryRe = regexp.MustCompile(`^chunks/island_([0-9a-f]{12})\.[0-9a-f]{10}\.js$`)
	entryRefRe    = regexp.MustCompile(`/chunks/island_[0-9a-f.]+\.js`)
)

// TestIslandEntriesNamedByContent builds a site twice, with a change in a
// module two islands share in between, and checks that no served file keeps
// its name while its content changes: the island entries that import the
// changed shared chunk get new names, the one that does not keeps its name,
// and every document and server bundle names files that exist. An entry used
// to keep its name across builds (it is named from its source), so a cache
// that kept it paired one build's island with another build's chunks.
func TestIslandEntriesNamedByContent(t *testing.T) {
	t.Parallel()
	ensureRuntimeDist(t)
	b, root := scProject(t, islandHashFiles)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("first build: %v\n%s", err, out)
	}
	first := servedFiles(t, b.Cfg.OutDir)
	checkEntryRefs(t, b.Cfg.OutDir)
	writeProjectFile(t, root, "src/components/shared.ts", strings.Replace(islandHashFiles["src/components/shared.ts"], "'v1'", "'v2'", 1))
	b2 := New(root, b.Cfg)
	if out, err := captureBuild(b2); err != nil {
		t.Fatalf("second build: %v\n%s", err, out)
	}
	second := servedFiles(t, b.Cfg.OutDir)
	checkEntryRefs(t, b.Cfg.OutDir)

	for name, sum := range second {
		if old, ok := first[name]; ok && old != sum {
			t.Errorf("%s kept its name with different content", name)
		}
	}
	entries := func(files map[string]string) map[string]string {
		out := map[string]string{}
		for name := range files {
			if strings.HasPrefix(name, "chunks/island_") && strings.HasSuffix(name, ".js") {
				m := hashedEntryRe.FindStringSubmatch(name)
				if m == nil {
					t.Errorf("island entry %s is not named by its content", name)
					continue
				}
				out[m[1]] = name
			}
		}
		return out
	}
	before, after := entries(first), entries(second)
	if len(before) != 3 || len(after) != 3 {
		t.Fatalf("want 3 island entries in each build, got %v and %v", before, after)
	}
	changed := 0
	for id, name := range after {
		if before[id] != name {
			changed++
		}
	}
	// The provider and the reader import the shared module's chunk; the
	// package island does not.
	if changed != 2 {
		t.Errorf("want the 2 entries that import the changed module renamed, got %d:\n%v\n%v", changed, before, after)
	}
	var manifest struct {
		HashedAssets []string `json:"hashedAssets"`
	}
	data, err := os.ReadFile(ManifestPath(b.Cfg.OutDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range after {
		found := false
		for _, a := range manifest.HashedAssets {
			found = found || a == "/"+name
		}
		if !found {
			t.Errorf("%s is not in the manifest's hashedAssets (it is served revalidated)", name)
		}
	}
}

// checkEntryRefs fails when a document, script or server output names an
// island entry by its source name, or one that does not exist.
func checkEntryRefs(t *testing.T, outDir string) {
	t.Helper()
	_ = filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !renameTextExts[filepath.Ext(p)] {
			return nil
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(outDir, p)
		if m := stableEntryRe.Find(data); m != nil {
			t.Errorf("%s names the island entry %s by its source name", rel, m)
		}
		for _, ref := range entryRefRe.FindAll(data, -1) {
			if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(string(ref), "/")))); err != nil {
				t.Errorf("%s names %s, which the build did not write", rel, ref)
			}
		}
		return nil
	})
}
