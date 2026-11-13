package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReactiveHeadKeepsItsBindings: a <title> bound to state, hoisted from a
// component, keeps the hydration marker its binding finds.
func TestReactiveHeadKeepsItsBindings(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `import { useState } from "react";
function Inbox(props: { unread: number }) {
  return <section><title>{"Inbox (" + props.unread + ")"}</title><p>{props.unread}</p></section>;
}
export default function Page() {
  const [n, setN] = useState(3);
  return <div><Inbox unread={n} /><button id="read" onClick={() => setN(n - 1)}>read</button></div>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if head := html[:strings.Index(html, "</head>")]; !strings.Contains(head, "<title data-k") {
		t.Errorf("the reactive title needs its hydration marker:\n%s", head)
	}
}

func TestInjectServerGlobalsSkipsBoundNames(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want string
	}{
		"plain":        {`export default () => <Head><title>x</title></Head>`, "Head"},
		"imported":     {`import { Head } from "@krate/runtime";` + "\n" + `export default () => <Head/>`, ""},
		"aliased":      {`import { Head as H } from "@krate/runtime"; export default () => <Head/>`, "Head"},
		"multi-import": {"import {\n  createSignal,\n  Head,\n} from \"@krate/runtime\";\nexport default () => <Head/>", ""},
		"declared":     {`function Head() { return null }` + "\n" + `export default () => <Head/>`, ""},
		"twice":        {injectServerGlobals(`export default () => <Head/>`), ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := injectServerGlobals(tc.src)
			injected := strings.HasPrefix(got, "import { "+tc.want+" } from '@krate/runtime/server';")
			if tc.want == "" {
				injected = got == tc.src
			}
			if !injected {
				t.Fatalf("injectServerGlobals(%q) = %q", tc.src, got)
			}
		})
	}
}

func TestDedupeHeadLastKeepsTheLastTagPerKey(t *testing.T) {
	head := "<title>A</title>\n<meta name=\"description\" content=\"a\">\n<meta property=\"og:image\" content=\"1\">\n<title>B</title>\n<meta name=\"description\" content=\"b\">\n"
	got := dedupeHeadLast(head)
	want := "<meta property=\"og:image\" content=\"1\">\n<title>B</title>\n<meta name=\"description\" content=\"b\">\n"
	if got != want {
		t.Fatalf("dedupeHeadLast:\n got %q\nwant %q", got, want)
	}
	if first := dedupeHead(head); !strings.HasPrefix(first, "<title>A</title>") || strings.Contains(first, "<title>B</title>") {
		t.Fatalf("dedupeHead must keep the first tag per key: %q", first)
	}
}

func TestLiftIslandHead(t *testing.T) {
	body, head := liftIslandHead(`<div><krate-head><title>T</title><krate-head><meta name="a" content="b"></krate-head></krate-head><p>x</p></div>`)
	if body != `<div><p>x</p></div>` {
		t.Errorf("body = %q", body)
	}
	if head != `<title>T</title><meta name="a" content="b">` {
		t.Errorf("head = %q", head)
	}
}

// TestHeadFromComponentsInPrerenderedPages covers the shapes of a <Head> that
// lives in a component: stateless, interactive, nested and in a layout. The
// last <Head> of the page wins, as it does for request-time renders, and the
// page wins over its layout.
func TestHeadFromComponentsInPrerenderedPages(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/seo.tsx": `export function Seo({ title }: { title: string }) {
  return <Head><title>{title}</title><meta name="description" content="from seo" /></Head>;
}
export function Wrapped({ title }: { title: string }) {
  return <section><Seo title={title} /></section>;
}`,
		"src/components/counter.tsx": `import { createSignal } from "@krate/runtime";
export function Counter() {
  const [n, setN] = createSignal(0);
  return <div><Head><meta name="x-counter" content={"n" + n()} /></Head><button onClick={() => setN(n() + 1)}>{n()}</button></div>;
}`,
		"src/pages/_layout.tsx": `import { Seo } from "../components/seo";
export default function Layout({ children }: { children: any }) {
  return <div><Seo title="Layout title" />{children}</div>;
}`,
		"src/pages/index.tsx": `import { Seo, Wrapped } from "../components/seo";
import { Counter } from "../components/counter";
export default function Home() {
  return <main><Wrapped title="First" /><Seo title="Second" /><Counter /></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	head := html[:strings.Index(html, "</head>")]
	if n := strings.Count(head, "<title>"); n != 1 || !strings.Contains(head, "<title>Second</title>") {
		t.Errorf("want exactly one <title>Second</title> (last wins, page over layout):\n%s", head)
	}
	if strings.Count(head, `name=description`) != 1 && strings.Count(head, `name="description"`) != 1 {
		t.Errorf("want one description:\n%s", head)
	}
	if !strings.Contains(head, "x-counter") {
		t.Errorf("the interactive component's tag must be in the head:\n%s", head)
	}
}

// TestSegmentPageHeadComponentShipsNoJS is the prerendered 404 of the gap
// report: a component inside <Head> that has state must not make the page load
// the runtime and a hydration script, because its markup is not in the body.
func TestSegmentPageHeadComponentShipsNoJS(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/tags.tsx": `import { createSignal } from "@krate/runtime";
export function Tags({ desc }: { desc: string }) {
  const [d] = createSignal(desc);
  return <meta name="description" content={d()} />;
}`,
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
		"src/pages/404.tsx": `// @static
import { Tags } from "../components/tags";
export default function NotFound() {
  return <main><Head><title>Not found</title><Tags desc="missing" /></Head><h1>404</h1></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "404.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if strings.Contains(html, "<script src") || strings.Contains(html, "runtime.") {
		t.Errorf("a 404 whose only interactive component is in <Head> must ship no JavaScript:\n%s", html)
	}
	if !strings.Contains(html, "<title>Not found</title>") || !strings.Contains(html, "missing") {
		t.Errorf("the head must carry the title and the component's tag:\n%s", html)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "__404")); err == nil {
		t.Errorf("no hydration directory expected for the 404 page")
	}
}

// TestHeadInReactIsland: a component that runs as a Preact island uses <Head>
// like any other component. The island renders on the server at build time,
// its <Head> content goes to the document head and the island body has none.
func TestHeadInReactIsland(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/seo-icon.tsx": `import { Check } from "lucide-react";
export function SeoIcon({ title }: { title: string }) {
  return <div><Head><title>{title}</title><meta name="description" content="island head" /></Head><Check id="ic" /></div>;
}`,
		"src/pages/index.tsx": `import { SeoIcon } from "../components/seo-icon";
export default function Home() { return <main><SeoIcon title="Island title" /></main>; }`,
	})
	withPreact(t, root)
	linkReactPackages(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	head := html[:strings.Index(html, "</head>")]
	if !strings.Contains(head, "<title>Island title</title>") || !strings.Contains(head, "island head") {
		t.Errorf("the island's <Head> must reach the document head:\n%s", head)
	}
	body := html[strings.Index(html, "<body"):]
	if strings.Contains(body, "krate-head") || !strings.Contains(body, `id=ic`) && !strings.Contains(body, `id="ic"`) {
		t.Errorf("the island body must be rendered without the head wrapper:\n%s", body)
	}
}

// TestHeadAndTagGlobalsInServerComponents: components imported by an SSR page
// can use <Head> as a global or import it from @krate/runtime. Both used to
// fail at request time (ReferenceError, then a missing export).
func TestHeadAndTagGlobalsInServerComponents(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/seo.tsx": `export function Seo({ title }: { title: string }) {
  return <Head><title>{title}</title></Head>;
}`,
		"src/components/imported.tsx": `import { Head } from "@krate/runtime";
export function Imported({ desc }: { desc: string }) {
  return <Head><meta name="description" content={desc} /></Head>;
}`,
		"src/pages/index.tsx": `import { Seo } from "../components/seo";
import { Imported } from "../components/imported";
export default async function Home() {
  const who = await Promise.resolve("ada");
  return <main><Seo title={"Hello " + who} /><Imported desc={"about " + who} /><p>{who}</p></main>;
}`,
	})
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, header, html := getBody(t, base+"/")
	if status != 200 || header.Get("X-Krate-Error") != "" {
		t.Fatalf("status %d, X-Krate-Error %q:\n%s", status, header.Get("X-Krate-Error"), html)
	}
	head := html[:strings.Index(html, "</head>")]
	if !strings.Contains(head, "<title>Hello ada</title>") || !strings.Contains(head, "about ada") {
		t.Errorf("request-time <Head> from components must reach the head:\n%s", head)
	}
	if strings.Contains(html[strings.Index(html, "<body"):], "krate-head") {
		t.Errorf("head markers leaked into the body:\n%s", html)
	}
}
