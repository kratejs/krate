package build

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestCompiledExternalStoreServerSnapshot: a "use client" component compiled
// to Krate's runtime renders useSyncExternalStore's getServerSnapshot at build
// time when it is a named function, as React's server render does, rather
// than getSnapshot. One that cannot run at build time (it reads window) leaves
// the value to the client without failing the build.
func TestCompiledExternalStoreServerSnapshot(t *testing.T) {
	t.Parallel()
	ensureRuntimeDist(t)
	b, root := scProject(t, map[string]string{
		"src/lib.ts": `export const sub = (cb: () => void) => () => {};
export function serverFromModule() { return 'mod'; }
export function serverTouchesWindow() { return (window as any).foo ?? 'w'; }
`,
		"src/components/A.tsx": `'use client';
import { useSyncExternalStore } from 'react';
import { sub, serverFromModule, serverTouchesWindow } from '../lib';
function local() { return 'loc'; }
export default function A() {
  const b = useSyncExternalStore(sub, () => 'c2', serverFromModule);
  const c = useSyncExternalStore(sub, () => 'c3', serverTouchesWindow);
  const d = useSyncExternalStore(sub, () => 'c4', local);
  const e = useSyncExternalStore(sub, () => 'c5', () => 'arrow');
  return <p><i id="b">{b}</i><i id="c">{c}</i><i id="d">{d}</i><i id="e">{e}</i></p>;
}
`,
		"src/pages/index.tsx": `import A from '../components/A';
export default function Page() { return <main><A /></main>; }
`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := regexp.MustCompile(`<!--[^>]*-->`).ReplaceAllString(string(data), "")
	text := func(id string) string {
		m := regexp.MustCompile(`id="?` + id + `"?>([^<]*)</i>`).FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("no #%s in:\n%s", id, data)
		}
		return m[1]
	}
	for id, want := range map[string]string{"b": "mod", "c": "", "d": "loc", "e": "arrow"} {
		if got := text(id); got != want {
			t.Errorf("#%s: server rendered %q, want %q\n%s", id, got, want, data)
		}
	}
}
