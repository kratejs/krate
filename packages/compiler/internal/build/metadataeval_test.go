package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readMetaDist(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func headOf(html string) string {
	if i := strings.Index(html, "</head>"); i >= 0 {
		return strings.ReplaceAll(html[:i], `"`, "")
	}
	return html
}

// generateMetadata runs at build time for a static page: once per
// generateStaticParams entry, merged under the layout's metadata, with the tags
// in the page's own <head>.
func TestGenerateMetadataBakesTagsPerStaticParam(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
		"src/pages/_layout.tsx": `export const metadata = { title: { default: "Site", template: "%s | Site" }, description: "layout", metadataBase: "https://x.example" };
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/blog/[slug].tsx": `export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export async function generateMetadata({ params }: any, parent: Promise<any>) {
  const { slug } = await params;
  const above = await parent;
  return { title: "Post " + slug, openGraph: { title: "OG " + slug, images: ["/og/" + slug + ".png"], description: above.description } };
}
export default function Post({ params }: any) { return <h1>{params.slug}</h1>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"a", "b"} {
		head := headOf(readMetaDist(t, root, "blog/"+slug+"/index.html"))
		for _, want := range []string{
			"<title>Post " + slug + " | Site</title>",
			"<meta name=description content=layout>",
			"<meta property=og:title content=\"OG " + slug + "\">",
			"https://x.example/og/" + slug + ".png",
		} {
			if !strings.Contains(strings.ReplaceAll(head, `"`, ""), strings.ReplaceAll(want, `"`, "")) {
				t.Errorf("/blog/%s head lacks %q:\n%s", slug, want, head)
			}
		}
		if strings.Contains(head, "Post "+map[string]string{"a": "b", "b": "a"}[slug]) {
			t.Errorf("/blog/%s carries the other entry's title:\n%s", slug, head)
		}
	}
	// The layout's own metadata reaches pages that export none.
	home := headOf(readMetaDist(t, root, "index.html"))
	if !strings.Contains(home, "<title>Site</title>") || !strings.Contains(home, "content=layout") {
		t.Errorf("the layout's metadata must reach the home page:\n%s", home)
	}
}

// A page whose metadata is a call and that holds module-level JSX (local or
// imported) still gets its tags, and the JSX still renders.
func TestCallMetadataWithModuleLevelJSX(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/lib/seo.ts":          `export function pageMetadata(title: string, description: string) { return { title, description, openGraph: { title } }; }`,
		"src/components/hero.tsx": `export const hero = <h1>Imported Hero</h1>;`,
		"src/pages/about.tsx": `import { pageMetadata } from '../lib/seo';
export const metadata = pageMetadata('About', 'About us');
const hero = <h1>Local Hero</h1>;
export default function About() { return <main>{hero}<p>Text</p></main>; }`,
		"src/pages/team.tsx": `import { pageMetadata } from '../lib/seo';
import { hero } from '../components/hero.tsx';
export const metadata = pageMetadata('Team', 'Team page');
export default function Team() { return <main>{hero}<p>Text</p></main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	for route, want := range map[string][2]string{
		"about/index.html": {"About", "Local Hero"},
		"team/index.html":  {"Team", "Imported Hero"},
	} {
		html := readMetaDist(t, root, route)
		head := headOf(html)
		for _, w := range []string{"<title>" + want[0] + "</title>", "name=description content=", "og:title content=" + want[0]} {
			if !strings.Contains(head, w) {
				t.Errorf("%s head lacks %q:\n%s", route, w, head)
			}
		}
		if !strings.Contains(html, "<h1>"+want[1]+"</h1>") {
			t.Errorf("%s lacks the hero JSX:\n%s", route, html)
		}
	}
}

func TestMetadataExportPositionNamesTheExport(t *testing.T) {
	name, line, col, text := metadataExportPosition("import x from 'y';\n\nexport const metadata = f();\n")
	if name != "metadata" || line != 3 || col != 1 || text != "export const metadata = f();" {
		t.Errorf("got %q %d:%d %q", name, line, col, text)
	}
}

// The metadata call runs without the rest of the page module: browser code at
// module scope in an imported component, a module that reads __dirname, and JSX that uses the Link global all stay out of it.
func TestCallMetadataEvaluatesOnlyItsOwnClosure(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/lib/seo.ts":           `export function pageMetadata(title: string, description: string) { return { title, description, openGraph: { title, images: ['/og.png'] } }; }`,
		"src/components/chart.tsx": "const el = document.querySelector('#chart');\nexport function Chart() { return <i>{String(el)}</i>; }",
		"src/lib/dirs.ts":          "export const here = __dirname;\nexport const ready = true;",
		"src/pages/about.tsx": `import { pageMetadata } from '../lib/seo';
import { Chart } from '../components/chart';
import { here } from '../lib/dirs';
export const metadata = pageMetadata('About', 'About us');
const nav = <Link href="/">Home</Link>;
export default function About() { return <main>{nav}<Chart />{here}</main>; }`,
		"src/pages/404.tsx": `import { pageMetadata } from '../lib/seo';
export const metadata = pageMetadata('Missing', 'Not here');
export default function NotFound() { return <h1>Missing</h1>; }`,
	})
	logs := captureStderrFile(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	head := headOf(readMetaDist(t, root, "about/index.html"))
	for _, w := range []string{"<title>About</title>", "name=description content=", "og:title content=About", "og:image"} {
		if !strings.Contains(head, w) {
			t.Errorf("about head lacks %q:\n%s", w, head)
		}
	}
	if out := logs(); strings.Contains(out, "KR7122") {
		t.Errorf("metadata evaluation warned:\n%s", out)
	}
}

// A failing metadata export is reported at its own line, also on a special
// page, and a segment that fails is reported at that segment's export.
func TestMetadataWarningCarriesThePosition(t *testing.T) {
	b, _ := hooksProject(t, map[string]string{
		"src/lib/seo.ts":      `export function pageMetadata(): never { throw new Error("no title"); }`,
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
		"src/pages/404.tsx": `import { pageMetadata } from '../lib/seo';

export const metadata = pageMetadata();
export default function NotFound() { return <h1>Missing</h1>; }`,
		"src/pages/docs/_layout.tsx": `export async function generateMetadata() { throw new Error("layout boom"); }
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/docs/intro.tsx": `export default function Intro() { return <p>intro</p>; }`,
	})
	logs := captureStderrFile(t)
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	out := logs()
	for _, want := range []string{"src/pages/404.tsx:3:1", "src/pages/docs/_layout.tsx:1:1"} {
		if !strings.Contains(out, want) {
			t.Errorf("warnings lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ":0:0") {
		t.Errorf("a warning has no position:\n%s", out)
	}
}

// generateStaticParams runs without the page's component tree: a lazy()
// loader in an imported component does not matter to it, and the page still
// builds and renders.
func TestGenerateStaticParamsPageWithLazyBuilder(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/components/builder.tsx": `import { lazy, Suspense, useState } from 'react';
const Wizard = lazy(() => import('./setup-wizard').then((m) => m.SetupWizard));
export function Builder() {
  const [open, setOpen] = useState(false);
  return <div><button onClick={() => setOpen(true)}>open</button>{open ? <Suspense fallback={<p>loading</p>}><Wizard /></Suspense> : null}</div>;
}`,
		"src/components/setup-wizard.tsx": `export function SetupWizard() { return <p>wizard</p>; }`,
		"src/pages/site/[id].tsx": `import { Builder } from '../../components/builder';
export function generateStaticParams() { return [{ id: 'one' }]; }
export default function Site() { return <main><h1>Site page</h1><Builder /></main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	html := readMetaDist(t, root, "site/one/index.html")
	if !strings.Contains(html, "Site page") {
		t.Errorf("the generated page is missing its content:\n%s", html)
	}
}
