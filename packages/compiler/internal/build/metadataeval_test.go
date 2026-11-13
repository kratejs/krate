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
