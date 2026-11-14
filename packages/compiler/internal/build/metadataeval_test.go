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
