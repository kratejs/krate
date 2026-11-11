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

func TestInlineImportDroppedFromUIBundle(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/pages/index.tsx": inlinePage,
		"src/lib/ui.ts":       "export const useThing = () => 'x';\n",
	}, nil)
	mustNoErrors(t, ds)
	b := bundleUI(t, root, res, "src/pages/index.tsx")
	src, paths := moduleSources(b)
	if strings.Contains(src, "db.insert") {
		t.Fatalf("host function in the UI bundle:\n%s", src)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "@krate/app/db.js") {
			t.Fatalf("@krate/app/db shipped to the UI")
		}
	}
	if !strings.Contains(src, `__krateCall("src/pages/index#addTask", a)`) {
		t.Fatalf("stub missing:\n%s", src)
	}
}

func TestPartialImportKeepsOtherBindings(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/util.ts": "export const keep = 1;\nexport const only = 2;\n",
		"src/pages/index.jsx": `import { keep, only } from "../lib/util";

export async function work() {
  "@host";
  return only;
}

export default function P() { return <p onClick={work}>{keep}</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	var ui string
	for p, s := range res.Overlays {
		if strings.HasSuffix(p, "index.jsx") {
			ui = s
		}
	}
	if !strings.Contains(ui, `import { keep } from "../lib/util";`) || strings.Contains(ui, "only") {
		t.Fatalf("UI:\n%s", ui)
	}
}

func TestHostFunctionUsingAnotherHostModuleDirectly(t *testing.T) {
	_, res, ds := buildProject(t, map[string]string{
		"src/lib/sync.host.ts": "export async function syncNow() { return 'synced'; }\n",
		"src/pages/index.tsx": `import { syncNow } from "../lib/sync.host";

@host
export async function wrapper() {
  return syncNow();
}

export default function P() { return <p onClick={wrapper}>x</p>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	// sync.host is only called by host code, so it is bundled but not callable.
	if got := strings.Join(ids(res), ","); got != "src/pages/index#wrapper" {
		t.Fatalf("registered %s", got)
	}
	rt := runHost(t, res)
	if got := evalJSON(t, rt, `__krateHost.regs["src/pages/index#wrapper"].fn()`); got != `"synced"` {
		t.Fatalf("wrapper() = %s", got)
	}
}
