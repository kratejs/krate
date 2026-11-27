package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// serverTierProject is a site whose @server pages render an interactive
// component, directly or as the children of a wrapper component.
func serverTierProject(t *testing.T) string {
	t.Helper()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/data/rows.json":  `{"items":[{"slug":"one","title":"One","steps":[{"points":2},{"points":3}]},{"slug":"two","title":"Two","steps":[{"points":5}]}]}`,
		"src/components/score.tsx": `import { createSignal, onMount } from "@krate/runtime";
export default function Score(props: { title: string; steps: { points: number }[]; cacheKey: string }) {
  const [n, setN] = createSignal(0);
  onMount(() => { document.body.dataset.key = props.cacheKey; });
  return <div class="score"><h2>{props.title}</h2><button id="add" onClick={() => setN(n() + props.steps[0].points)}>{n()}</button></div>;
}`,
		"src/components/frame.tsx": `// @server
export default function Frame({ title, children }: { title: string; children: any }) {
  return <div class="frame"><header>{title}</header><main id="main">{children}</main><footer>end</footer></div>;
}`,
		"src/components/plain-frame.tsx": `export function PlainFrame({ children }: { children: any }) {
  return <section class="plain">{children}</section>;
}`,
		"src/pages/wrapped.tsx": `// @server
import data from "../data/rows.json";
import Frame from "../components/frame";
import Score from "../components/score";
export default function Wrapped() {
  const it = data.items[1];
  return <Frame title={it.title}><Score title={it.title} steps={it.steps} cacheKey={"k:" + it.slug} /></Frame>;
}`,
		"src/pages/direct/[slug].tsx": `// @server
import data from "../../data/rows.json";
import Score from "../../components/score";
export function generateStaticParams() { return data.items.map((i) => ({ slug: i.slug })); }
export default function Direct({ params }) {
  const it = data.items.find((x) => x.slug === params.slug);
  if (!it) return null;
  return <main><Score title={it.title} steps={it.steps} cacheKey={"k:" + it.slug} /></main>;
}`,
		"src/pages/framed/[slug].tsx": `// @server
import data from "../../data/rows.json";
import Frame from "../../components/frame";
import { PlainFrame } from "../../components/plain-frame";
import Score from "../../components/score";
export function generateStaticParams() { return data.items.map((i) => ({ slug: i.slug })); }
export default function Framed({ params }) {
  const it = data.items.find((x) => x.slug === params.slug);
  if (!it) return null;
  return <Frame title={it.title}><PlainFrame><Score title={it.title} steps={it.steps} cacheKey={"k:" + it.slug} /></PlainFrame></Frame>;
}`,
	})
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	logs := builderStderr(b)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logs())
	}
	return root
}

// pageScript reads the hydration script a built page loads.
func pageScript(t *testing.T, root, page string) string {
	t.Helper()
	html := readDistPage(t, root, page+"/index.html")
	m := regexp.MustCompile(`src=/?(` + regexp.QuoteMeta(page) + `/index\.[a-z0-9]+\.js)`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("%s loads no page script:\n%s", page, html)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(m[1])))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestServerTierPagesHydrateTheirInteractiveChildren: an interactive component
// a @server page renders keeps its markup when it is the children of a
// wrapper component, and its props reach the browser as the literals the
// build computed. Values only the server render read stay out of the script.
func TestServerTierPagesHydrateTheirInteractiveChildren(t *testing.T) {
	t.Parallel()
	root := serverTierProject(t)
	cases := []struct {
		page, key, points string
		wrapped           bool
	}{
		{"wrapped", "k:two", "[{points:5}]", true},
		{"direct/one", "k:one", "[{points:2},{points:3}]", false},
		{"framed/two", "k:two", "[{points:5}]", true},
	}
	for _, c := range cases {
		html := readDistPage(t, root, c.page+"/index.html")
		if !strings.Contains(html, `id=add`) && !strings.Contains(html, `id="add"`) {
			t.Errorf("%s dropped the interactive child:\n%s", c.page, html)
			continue
		}
		if c.wrapped && !regexp.MustCompile(`<main id="?main"?><`).MatchString(html) {
			t.Errorf("%s: the wrapper's children are not in its <main>:\n%s", c.page, html)
		}
		js := pageScript(t, root, c.page)
		reg := regexp.MustCompile(`__krate_props\["[^"]+Score_c0"\]=\{([^}]*(?:\}[^}]*)*?)\}(?:\}|;)`).FindString(js)
		if reg == "" {
			t.Errorf("%s registers no props for its interactive child:\n%s", c.page, js)
			continue
		}
		if !strings.Contains(reg, `cacheKey:"`+c.key+`"`) || !strings.Contains(reg, "steps:"+c.points) {
			t.Errorf("%s registers %s, want cacheKey %q and steps %s as literals", c.page, reg, c.key, c.points)
		}
		if strings.Contains(js, "title") || strings.Contains(js, "items") {
			t.Errorf("%s ships build-time-only values to the browser:\n%s", c.page, js)
		}
	}
}
