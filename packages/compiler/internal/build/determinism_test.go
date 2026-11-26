package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// TestBuildIsDeterministic verifies two independent builds of the same sources
// produce byte-identical output (stable content hashes, no map-order leakage).
func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	const page = `
import { createSignal } from '@krate/runtime';
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div class="p-4 flex"><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}
`
	build := func() map[string][]byte {
		root := t.TempDir()
		writeFileRel(t, root, "src/pages/index.tsx", page)
		writeFileRel(t, root, "src/pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.Minify = false
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		files := map[string][]byte{}
		err := filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(cfg.OutDir, p)
			data, _ := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		return files
	}

	a := build()
	b := build()

	if len(a) != len(b) {
		t.Fatalf("file count differs: %d vs %d", len(a), len(b))
	}
	for name, dataA := range a {
		dataB, ok := b[name]
		if !ok {
			t.Errorf("file %s missing from second build", name)
			continue
		}
		if string(dataA) != string(dataB) {
			t.Errorf("file %s differs between builds:\n--- A ---\n%s\n--- B ---\n%s", name, dataA, dataB)
		}
	}
}

func TestMergeModuleConstantsEntryWins(t *testing.T) {
	t.Parallel()
	parse := func(name, src string) *ast.Program {
		prog, errs := tsstrip.Parse(name, src, tsstrip.Options{})
		if len(errs) > 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		return prog
	}
	dep := parse("dep.ts", `export const label = "from-dep"; const inner = "dep-inner"; export { inner as exportedInner };`)
	other := parse("other.ts", `const inner = "other-inner"; export { inner as otherAlias };`)
	entry := parse("page.tsx", `export const label = "from-entry"; export default function P() { return <p>{label}</p>; }`)

	consts, _, _ := mergeModuleConstants(entry, []annotator.ModuleSource{
		{Program: dep, Path: "src/dep.ts"},
		{Program: other, Path: "src/other.ts"},
	})
	if consts["label"] != "from-entry" {
		t.Errorf("entry should win on collision: got label=%q, want \"from-entry\"", consts["label"])
	}
	if consts["otherAlias"] != "other-inner" {
		t.Errorf("export rename should use declaring module binding: got otherAlias=%q, want \"other-inner\"", consts["otherAlias"])
	}
}

func TestDocsBuildDeterministicWithModuleHelpers(t *testing.T) {
	t.Parallel()
	themeSrc := `
import { createCSSToggle, onCleanup, onMount } from "@krate/runtime";

const BASE_PREFIX = "/docs";
const SITE_NAME = "Guidebook";
const BANNER_LABEL = SITE_NAME + " (" + BASE_PREFIX + ")";

function cleanSlash(raw: string | undefined): string {
  if (!raw) return "";
  return raw.endsWith("/") ? raw.slice(0, -1) : raw;
}

function routeFromPath(src: string | undefined): string {
  const s = src || "";
  const trimmed = s === "index" ? "" : s.endsWith("/index") ? s.slice(0, -6) : s;
  return trimmed === "" ? BASE_PREFIX : BASE_PREFIX + "/" + cleanSlash(trimmed);
}

function buildNavGraph(rawItems: any[] | undefined, active: string, groups: { dir: string; title: string }[]) {
  const raw = rawItems || [];
  const graph: any[] = [];
  for (const item of raw) {
    if (!item.children || item.children.length === 0) {
      const u = cleanSlash(item.url);
      graph.push({ title: item.title || "", url: u, active: u === active, children: [] });
    }
  }
  for (const g of groups) {
    const found = raw.find(
      (it) =>
        (it.children || []).length > 0 &&
        cleanSlash((it.children || [])[0].url).indexOf(BASE_PREFIX + "/" + g.dir + "/") === 0,
    );
    if (!found) continue;
    const kids: any[] = [];
    for (const k of found.children || []) {
      const ku = cleanSlash(k.url);
      kids.push({ title: k.title || "", url: ku, active: ku === active, children: [] });
    }
    graph.push({ title: g.title, url: "", active: kids.some((k) => k.active), children: kids });
  }
  return graph;
}

function NavList(props: { graph: any[]; active: string }) {
  return (
    <ul class="gb-nav" data-active={props.active}>
      {props.graph.map((node) =>
        node.children.length > 0 ? (
          <li>
            <span>{node.title}</span>
            <ul>
              {node.children.map((kid: any) => (
                <li><a href={kid.url} data-current={kid.active ? "true" : "false"}>{kid.title}</a></li>
              ))}
            </ul>
          </li>
        ) : (
          <li><a href={node.url} data-current={node.active ? "true" : "false"}>{node.title}</a></li>
        ),
      )}
    </ul>
  );
}

function DrawerPanel(props: { graph: any[]; active: string }) {
  const drawer = createCSSToggle(false);
  onMount(() => {
    const noop = () => {};
    window.addEventListener("resize", noop);
    onCleanup(() => window.removeEventListener("resize", noop));
  });
  return (
    <div class="gb-drawer">
      <button type="button" {...drawer.trigger()}>Menu</button>
      <div {...drawer.panel(true)}>
        <NavList graph={props.graph} active={props.active} />
      </div>
    </div>
  );
}

export default function GuidebookLayout(props: any) {
  const opts = props.options || {};
  const groups = opts.groups || [];
  const active = routeFromPath(props.currentPath);
  const navGraph = buildNavGraph(props.sidebarItems, active, groups);
  return (
    <div class="gb-root">
      <Head><title>{props.pageTitle + " - " + BANNER_LABEL}</title></Head>
      <DrawerPanel graph={navGraph} active={active} />
      <main>
        <h1>{props.pageTitle}</h1>
        <div class="gb-body">{props.children}</div>
      </main>
    </div>
  );
}
`
	writeFixture := func(root string) {
		mustWrite := func(rel, content string) {
			p := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatalf("mkdir %s: %v", p, err)
			}
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
		}
		mustWrite("src/pages/index.tsx", `export default function Home() { return <main>Home</main>; }`)
		mustWrite("components/theme/layout.tsx", themeSrc)
		mustWrite("content/docs/index.mdx", "---\ntitle: Welcome\n---\n\n# Welcome\n\nHome text.\n")
		for _, sec := range []string{"alpha", "beta", "gamma"} {
			for i := 1; i <= 5; i++ {
				mustWrite(
					fmt.Sprintf("content/docs/%s/doc-%d.mdx", sec, i),
					fmt.Sprintf("---\ntitle: %s Doc %d\n---\n\n# %s Doc %d\n\nBody %d.\n", sec, i, sec, i, i),
				)
			}
		}
	}

	buildOnce := func(run int) map[string]string {
		root := t.TempDir()
		writeFixture(root)
		cfg := config.Default()
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.Tailwind.Enabled = false
		cfg.Minify = false
		cfg.Plugins = []config.PluginConfig{
			{
				Name: "docs",
				Options: map[string]any{
					"contentDir": "content/docs",
					"title":      "Guidebook",
					"search":     map[string]any{"enabled": false},
					"theme": map[string]any{
						"name":   "guidebook-theme",
						"module": "./components/theme/layout.tsx",
						"options": map[string]any{
							"groups": []map[string]any{
								{"dir": "alpha", "title": "Alpha"},
								{"dir": "beta", "title": "Beta"},
								{"dir": "gamma", "title": "Gamma"},
							},
						},
					},
				},
			},
		}
		b := New(root, cfg)
		if err := b.BuildAll(); err != nil {
			t.Fatalf("run %d build failed: %v", run, err)
		}
		pages := map[string]string{}
		docsOut := filepath.Join(root, "dist", "docs")
		err := filepath.Walk(docsOut, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(p, ".html") {
				rel, _ := filepath.Rel(docsOut, p)
				data, rerr := os.ReadFile(p)
				if rerr != nil {
					return rerr
				}
				pages[filepath.ToSlash(rel)] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("run %d walk dist/docs: %v", run, err)
		}
		if len(pages) != 16 {
			t.Fatalf("run %d: got %d docs HTML pages, want 16", run, len(pages))
		}
		return pages
	}

	first := buildOnce(1)
	for run := 2; run <= 3; run++ {
		got := buildOnce(run)
		for rel, wantHTML := range first {
			if got[rel] != wantHTML {
				t.Fatalf("run %d: page %s HTML differed from run 1", run, rel)
			}
		}
	}
}
