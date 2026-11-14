package hostts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

func bundleUI(t *testing.T, root string, res *Result, page string) *bundler.Bundle {
	t.Helper()
	bnd := bundler.New(root)
	bnd.SetSourceOverlays(res.Overlays)
	b, err := bnd.Bundle(filepath.Join(root, page))
	if err != nil {
		t.Fatalf("UI bundle: %v", err)
	}
	return b
}

func moduleSources(b *bundler.Bundle) (string, []string) {
	var all strings.Builder
	var paths []string
	for _, m := range b.Modules {
		paths = append(paths, filepath.ToSlash(m.Path))
		all.WriteString(m.SourceCode)
		all.WriteString("\n")
	}
	return all.String(), paths
}

func TestUIBundleGetsStubNotHostCode(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/lib/sync.host.ts": `import { db } from "@krate/app/db";
export async function syncNow() { return db.insert({ secret: "HOSTONLY" }); }
`,
		"src/pages/index.tsx": `import { syncNow } from "../lib/sync.host";
export default function Home() { return <button onClick={() => syncNow()}>sync</button>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	b := bundleUI(t, root, res, "src/pages/index.tsx")
	src, paths := moduleSources(b)
	if strings.Contains(src, "HOSTONLY") {
		t.Fatalf("host code reached the UI bundle:\n%s", src)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "@krate/app/db.js") {
			t.Fatalf("host-only dependency %s shipped to the UI", p)
		}
	}
	if !strings.Contains(src, `__krateCall("src/lib/sync.host.syncNow", a)`) {
		t.Fatalf("stub missing from the UI bundle:\n%s", src)
	}
}
