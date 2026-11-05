package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func buildTierProject(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	return root, New(root, cfg).BuildAll()
}

func readTierDist(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTierDecoratorsBuild(t *testing.T) {
	root, err := buildTierProject(t, map[string]string{
		"src/components/Stats.tsx": "@server\nexport default function Stats() { return <em>stats</em>; }\n",
		"src/pages/index.tsx": `import Stats from "../components/Stats";
@server
export default function Page() { return <main><Stats /></main>; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := readTierDist(t, root, "index.html")
	if !strings.Contains(html, "<em>stats</em>") || !strings.Contains(html, "data-krate-server") {
		t.Errorf("not rendered as a server page:\n%s", html)
	}
}

// The generateStaticParams bootstrap bundles the page with esbuild, which
// rejects decorators on functions.
func TestTierDecoratorOnGeneratedPage(t *testing.T) {
	root, err := buildTierProject(t, map[string]string{
		"src/pages/[id].tsx": `@server
export default function Page({ params }: any) { return <main>id-{params.id}</main>; }
export function generateStaticParams() { return [{ id: "a" }]; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if html := readTierDist(t, root, "a/index.html"); !strings.Contains(html, "id-a") {
		t.Errorf("page not generated:\n%s", html)
	}
}

// A runtime component is found by its decorator and bundled per file, and the
// server bundle of the page that imports it must not see the decorator either.
func TestRuntimeTierDecoratorBundles(t *testing.T) {
	root, err := buildTierProject(t, map[string]string{
		"src/components/Stats.tsx": "@runtime\nexport default function Stats() { return <em>stats</em>; }\n",
		"src/pages/index.tsx": `import Stats from "../components/Stats";
@server
export default function Page() { return <main><Stats /></main>; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ServerOutPath(filepath.Join(root, "dist"), "server-components"), "Stats.runtime.js")); err != nil {
		t.Errorf("no runtime bundle: %v", err)
	}
	if !strings.Contains(readTierDist(t, root, "index.html"), "region:") {
		t.Error("page has no region for the runtime component")
	}
}

func TestMisplacedRuntimeDecoratorFailsTheBuild(t *testing.T) {
	_, err := buildTierProject(t, map[string]string{
		"src/pages/index.tsx": `@runtime
function Live() { return <em>x</em>; }
export default function Page() { return <main><Live /></main>; }
`,
	})
	if err == nil || !strings.Contains(err.Error(), "KR7115") {
		t.Fatalf("got %v", err)
	}
}

func TestJSDecoratorBuilds(t *testing.T) {
	root, err := buildTierProject(t, map[string]string{
		"src/lib.js": "function log(v, c) { return v; }\nexport class A { @log m() { return 'ok'; } }\n",
		"src/pages/index.jsx": `import { A } from "../lib.js";
export default function Page() { return <main>{new A().m()}</main>; }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	readTierDist(t, root, "index.html")
}
