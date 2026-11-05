package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

// A dynamic route that is not closed keeps its template; generateMetadata runs
// with the route's placeholders, and the server fills in each request's value.
func TestGenerateMetadataOnDynamicTemplateUsesPlaceholders(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/user/[id].tsx": `export const dynamicParams = true;
export async function generateMetadata({ params }: any) {
  const { id } = await params;
  return { title: "User " + id };
}
export default function User() { return <p>user</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	head := headOf(readMetaDist(t, root, "user/[id]/index.html"))
	if !strings.Contains(head, "<title>User "+dynamicParamSentinel("id")+"</title>") {
		t.Errorf("the template's title carries the placeholder:\n%s", head)
	}
	if got := applyDynamicRouteParams(readMetaDist(t, root, "user/[id]/index.html"), map[string]string{"id": "42"}); !strings.Contains(got, "<title>User 42</title>") {
		t.Errorf("the placeholder becomes the request's param:\n%s", headOf(got))
	}
}

// A generateMetadata that throws at build time does not fail the build or lose
// the page: it warns (KR7122) with the error and keeps what the layouts declare.
func TestGenerateMetadataThatThrowsKeepsThePage(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/_layout.tsx": `export const metadata = { description: "layout" };
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/page.tsx": `export function generateMetadata() { throw new Error("lookup exploded"); }
export default function P() { return <p id="body">still here</p>; }`,
	})
	out := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if !strings.Contains(out, "KR7122") || !strings.Contains(out, "lookup exploded") {
		t.Errorf("a failing generateMetadata must warn with its message (KR7122):\n%s", out)
	}
	html := readMetaDist(t, root, "page/index.html")
	if !strings.Contains(html, "still here") || !strings.Contains(headOf(html), "content=layout") {
		t.Errorf("the page and the layout's tags survive:\n%s", html)
	}
}

// generateMetadata can fetch at build time: the embedded engine has no fetch,
// so the chain runs in Node.
func TestGenerateMetadataFetchesAtBuildTime(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, `{"title":"Fetched title"}`)
	}))
	defer srv.Close()
	b, root := hooksProject(t, map[string]string{
		"src/pages/page.tsx": `export async function generateMetadata() {
  const r = await fetch("` + srv.URL + `/post");
  const data = await r.json();
  return { title: data.title };
}
export default function P() { return <p>x</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if head := headOf(readMetaDist(t, root, "page/index.html")); !strings.Contains(head, "<title>Fetched title</title>") {
		t.Errorf("the fetched title belongs in the head:\n%s", head)
	}
	if hits == 0 {
		t.Error("generateMetadata never reached the server")
	}
}

// An SSR page resolves its chain per request: nothing is baked, and the server
// bundle carries the layouts' modules.
func TestRequestTimePageBakesNoMetadataAndBundlesTheChain(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/_layout.tsx": `export const metadata = { description: "layout" };
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/live.tsx": `export const config = { ssr: true };
export async function generateMetadata() { return { title: "Live" }; }
export default function P() { return <p>live</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	head := headOf(readMetaDist(t, root, "live/index.html"))
	if strings.Contains(head, "<title>Live</title>") || strings.Contains(head, "content=layout") {
		t.Errorf("a request-time page must not bake its metadata:\n%s", head)
	}
	bundle := readMetaDist(t, root, ".krate/server-bundles/live.server.mjs")
	if !strings.Contains(bundle, "__krateMetadata") || !strings.Contains(bundle, "src/pages/_layout.tsx") {
		t.Errorf("the page bundle must export the layout chain:\n%.600s", bundle)
	}
}

// A viewport export (themeColor, colorScheme, the viewport meta) is part of
// the chain like metadata.
func TestViewportExportBecomesTags(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/page.tsx": `export const viewport = { width: "device-width", initialScale: 1, themeColor: "#123456" };
export default function P() { return <p>x</p>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	head := headOf(readMetaDist(t, root, "page/index.html"))
	for _, want := range []string{"name=viewport content=width=device-width, initial-scale=1>", "name=theme-color content=#123456"} {
		if !strings.Contains(head, want) {
			t.Errorf("head lacks %q:\n%s", want, head)
		}
	}
	if strings.Count(head, "name=viewport") != 1 {
		t.Errorf("the page's viewport replaces the default instead of repeating it:\n%s", head)
	}
}

func TestModuleMetadataExports(t *testing.T) {
	cases := []struct {
		src      string
		meta, fn bool
	}{
		{"export const metadata = {};", true, false},
		{"export async function generateMetadata() {}", false, true},
		{"export function generateMetadata() {}\nexport const metadata = {}", true, true},
		{"export const generateMetadata = async () => ({})", false, true},
		{"export { metadata, generateMetadata } from './m'", true, true},
		{"const metadata = {}; export default function P() {}", false, false},
		{"// export const metadata = {}", false, false},
	}
	for _, c := range cases {
		m, f := moduleMetadataExports(c.src)
		if m != c.meta || f != c.fn {
			t.Errorf("%q: got (%v, %v), want (%v, %v)", c.src, m, f, c.meta, c.fn)
		}
	}
}
