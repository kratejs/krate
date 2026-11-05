package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hooksProject is scProject with the runtime package linked, so
// generateStaticParams can be executed.
func hooksProject(t *testing.T, files map[string]string) (*Builder, string) {
	t.Helper()
	ensureRuntimeDist(t)
	b, root := scProject(t, files)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	return b, root
}

const routeHooksPost = `import { usePathname, useParams, useSearchParams } from "@krate/runtime";
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function Post() {
  const pathname = usePathname();
  const params = useParams();
  const search = useSearchParams();
  return <main><p id="p">{pathname}</p><p id="s">{params.slug}</p><p id="q">{String(search.get("q"))}</p></main>;
}
`

// The router hooks fold to the page's own route at build time, so the static
// HTML is right before any script runs: the concrete URL for a page expanded
// from generateStaticParams, the template's placeholder for the dynamic route
// (the server swaps it per request), and the plain path for a static page.
func TestRouterHooksFillStaticHTML(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/index.tsx": `import { usePathname } from "@krate/runtime";
export default function Home() { const p = usePathname(); return <main><p id="p">{p}</p><p id="act" class={p === "/" ? "on" : "off"}>x</p></main>; }`,
		"src/pages/blog/[slug].tsx": routeHooksPost,
		"src/pages/docs/[...rest].tsx": `import { useParams } from "@krate/runtime";
export default function Docs() { const params = useParams(); return <p id="r">{String(params.rest)}</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	home := read("index.html")
	if !strings.Contains(home, "<!--/k:3-->") || !strings.Contains(home, ">/<!--/k:3-->") {
		t.Errorf("home must render its own path:\n%s", home)
	}
	if !strings.Contains(home, "class=on") {
		t.Errorf("a pathname comparison must fold at build time:\n%s", home)
	}
	for _, slug := range []string{"a", "b"} {
		page := read("blog/" + slug + "/index.html")
		for _, want := range []string{">/blog/" + slug + "<", ">" + slug + "<", ">null<"} {
			if !strings.Contains(page, want) {
				t.Errorf("/blog/%s lacks %q:\n%s", slug, want, page)
			}
		}
		if strings.Contains(page, "KRATE_PARAM") {
			t.Errorf("/blog/%s must not carry a placeholder:\n%s", slug, page)
		}
		if !strings.Contains(page, `id=__krate_page`) || !strings.Contains(page, `"r":"/blog/[slug]"`) {
			t.Errorf("a dynamic page names its route pattern for useParams():\n%s", page)
		}
	}
	tmpl := read("blog/[slug]/index.html")
	if !strings.Contains(tmpl, "/blog/"+dynamicParamSentinel("slug")) || !strings.Contains(tmpl, ">"+dynamicParamSentinel("slug")+"<") {
		t.Errorf("the dynamic template carries placeholders the server fills in:\n%s", tmpl)
	}
	if strings.Contains(home, "__krate_page") {
		t.Errorf("a static page needs no route pattern:\n%s", home)
	}
}

// A module that imports a router hook is lowered like React source: the hook
// result is read through krateHook, so it follows the URL after navigation.
func TestRouterHooksAreReadLive(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `import { usePathname, useRouter } from "@krate/runtime";
export default function Home() {
  const pathname = usePathname();
  const router = useRouter();
  return <button id="b" onClick={() => router.push("/x")}>{pathname}</button>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	js, _ := filepath.Glob(filepath.Join(root, "dist", "index.*.js"))
	if len(js) != 1 {
		t.Fatalf("page scripts: %v", js)
	}
	data, _ := os.ReadFile(js[0])
	for _, want := range []string{"krateHook(usePathname())", "krateHook(useRouter())", `.push("/x")`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("page JS lacks %q:\n%s", want, data)
		}
	}
}

// useSelectedLayoutSegment(s) in a layout file count from that layout's depth.
func TestLayoutSegmentHooksGetTheLayoutDepth(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
		"src/pages/dash/_layout.tsx": `import { useSelectedLayoutSegment, useSelectedLayoutSegments } from "@krate/runtime";
export default function L({ children }: any) {
  const seg = useSelectedLayoutSegment();
  const segs = useSelectedLayoutSegments();
  return <section id="dash"><i id="seg">{String(seg)}</i><b id="segs">{segs.join("/")}</b>{children}</section>;
}`,
		"src/pages/dash/settings/index.tsx": `export default function S() { return <p>settings</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	js, _ := filepath.Glob(filepath.Join(root, "dist", "dash", "settings", "index.*.js"))
	if len(js) == 0 {
		js, _ = filepath.Glob(filepath.Join(root, "dist", "dash", "settings", "*.js"))
	}
	var all strings.Builder
	for _, f := range js {
		data, _ := os.ReadFile(f)
		all.Write(data)
	}
	page, _ := os.ReadFile(filepath.Join(root, "dist", "dash", "settings", "index.html"))
	all.Write(page)
	if !strings.Contains(all.String(), "useSelectedLayoutSegment(void 0,1)") && !strings.Contains(all.String(), "useSelectedLayoutSegment(undefined,1)") {
		t.Errorf("the layout's segment hook must carry the layout depth (1):\n%s", all.String())
	}
}
