package build

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func metaRoutesProject(t *testing.T, fixture string) (string, *config.Config) {
	t.Helper()
	return serverlessProject(t, fixture)
}

func decodePNG(t *testing.T, root, rel string) image.Image {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" {
		t.Fatalf("%s is not a PNG: %v %s", rel, err, format)
	}
	return img
}

func pixel(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// The file conventions build to real files: sitemap.xml, robots.txt,
// manifest.webmanifest and PNGs, with og:image and twitter:image in the heads.
func TestMetadataRouteFilesBuild(t *testing.T) {
	root, _ := metaRoutesProject(t, "metaroutes_app")
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return string(data)
	}
	sitemap := read("sitemap.xml")
	for _, want := range []string{
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">`,
		"<loc>https://convention.example/</loc>", "<lastmod>2026-01-02T00:00:00.000Z</lastmod>", "<changefreq>weekly</changefreq>", "<priority>1</priority>",
		`<xhtml:link rel="alternate" hreflang="de" href="https://convention.example/de/blog/a/" />`,
	} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("sitemap.xml lacks %q:\n%s", want, sitemap)
		}
	}
	if strings.Contains(sitemap, "plugin.example") {
		t.Errorf("the metadata file wins over the sitemap plugin:\n%s", sitemap)
	}
	robots := read("robots.txt")
	for _, want := range []string{"User-Agent: *\nAllow: /\nDisallow: /private/\n", "User-Agent: BadBot\nDisallow: /\n", "Sitemap: https://convention.example/sitemap.xml\n"} {
		if !strings.Contains(robots, want) {
			t.Errorf("robots.txt lacks %q:\n%s", want, robots)
		}
	}
	if m := read("manifest.webmanifest"); !strings.Contains(m, `"short_name": "Conv"`) || !strings.Contains(m, `"display": "standalone"`) {
		t.Errorf("manifest.webmanifest:\n%s", m)
	}

	// A real PNG of the declared size, drawn from the JSX.
	og := decodePNG(t, root, "opengraph-image.png")
	if b := og.Bounds(); b.Dx() != 1200 || b.Dy() != 630 {
		t.Errorf("opengraph-image.png is %v", b)
	}
	if c := pixel(og, 5, 5); c != (color.NRGBA{0x0b, 0x3d, 0x91, 255}) {
		t.Errorf("the background colour of the card is %v", c)
	}
	light := 0
	for y := 200; y < 430; y++ {
		for x := 100; x < 1100; x++ {
			if c := pixel(og, x, y); c.R > 220 && c.G > 220 && c.B > 220 {
				light++
			}
		}
	}
	if light < 3000 {
		t.Errorf("the headline should draw white text (%d light pixels)", light)
	}
	tw := decodePNG(t, root, "twitter-image.png")
	if b := tw.Bounds(); b.Dx() != 1200 || b.Dy() != 600 {
		t.Errorf("twitter-image.png is %v", b)
	}
	if left, right := pixel(tw, 5, 300), pixel(tw, 1190, 300); left.R < 200 || left.G > 120 || right.G < 120 {
		t.Errorf("the twitter card is a red to amber gradient: %v %v", left, right)
	}
	// One image per generateStaticParams entry, drawn from its own params.
	alpha, beta := decodePNG(t, root, "blog/alpha/opengraph-image.png"), decodePNG(t, root, "blog/beta/opengraph-image.png")
	if pixel(alpha, 5, 5) == pixel(beta, 5, 5) {
		t.Error("each blog entry gets its own image")
	}

	// og:image and twitter:image are in the heads, automatically.
	home := headOf(read("index.html"))
	for _, want := range []string{
		"<meta property=og:image content=https://site.example/opengraph-image.png>",
		"<meta property=og:image:width content=1200>", "<meta property=og:image:height content=630>", "<meta property=og:image:alt content=The home card>",
		"<meta name=twitter:image content=https://site.example/twitter-image.png>", "<meta name=twitter:card content=summary_large_image>",
	} {
		if !strings.Contains(home, want) {
			t.Errorf("the home head lacks %q:\n%s", want, home)
		}
	}
	post := headOf(read("blog/alpha/index.html"))
	if !strings.Contains(post, "og:image content=/blog/alpha/opengraph-image.png") {
		t.Errorf("the post head names its own image:\n%s", post)
	}
	if strings.Contains(post, "/blog/beta/") || !strings.Contains(post, "twitter:image") {
		t.Errorf("the post inherits the root twitter image and not another post's image:\n%s", post)
	}
}

// Anything ImageResponse cannot draw fails the build with the list, never a
// silently wrong image.
func TestImageResponseUnsupportedFailsTheBuild(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <p>x</p>; }`,
		"src/pages/opengraph-image.tsx": `import { ImageResponse } from "@krate/runtime/og";
export default function Image() {
  return new ImageResponse(<div style={{ transform: "rotate(3deg)", display: "grid" }}><svg /></div>, { width: 600, height: 300 });
}`,
	})
	err := b.BuildAll()
	if err == nil {
		t.Fatal("an image with unsupported features must fail the build")
	}
	msg := err.Error()
	for _, want := range []string{"KR7131", "transform", "display: grid", "<svg>"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error lacks %q:\n%s", want, msg)
		}
	}
	if _, serr := os.Stat(filepath.Join(root, "dist", "opengraph-image.png")); serr == nil {
		t.Error("no PNG may be written for an image that cannot be drawn")
	}
}

func TestMetadataRoutePluginOverrideIsSaid(t *testing.T) {
	var out string
	root, _ := func() (string, *config.Config) {
		return metaRoutesProject(t, "metaroutes_app")
	}()
	_ = root
	// The fixture sets seo.baseUrl, so the sitemap plugin would have run.
	out = captureStderr(t, func() {
		b, _ := hooksProject(t, map[string]string{
			"src/pages/index.tsx":  `export default function Home() { return <p>x</p>; }`,
			"src/pages/sitemap.ts": `export default function s() { return [{ url: "https://c.example/" }]; }`,
		})
		b.Cfg.SEO.BaseURL = "https://plugin.example"
		if err := b.BuildAll(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "KR7133") || !strings.Contains(out, "sitemap.ts") {
		t.Errorf("the override must be announced (KR7133):\n%s", out)
	}
}

func TestMetadataRouteDetection(t *testing.T) {
	for name, want := range map[string]bool{
		"sitemap.ts": true, "robots.js": true, "manifest.ts": true, "opengraph-image.tsx": true, "twitter-image.png": true,
		"sitemap.tsx": false, "robots.tsx": false, "manifest.json.ts": false, "index.tsx": false, "opengraph-image.css": false,
	} {
		if got := isMetadataRouteFile(name); got != want {
			t.Errorf("isMetadataRouteFile(%q) = %v, want %v", name, got, want)
		}
	}
}

// ImageResponse in a Node route is drawn by the Krate host over the internal
// endpoint, with the same renderer and diagnostics as the build.
func TestImageResponsePerRequestOnTheHost(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	ensureHandlerDist(t)
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	h := newTestRevalidateHost(t)
	script := fmt.Sprintf(`import { ImageResponse } from %q;
const card = { type: "div", props: { style: { width: "100%%", height: "100%%", display: "flex", backgroundColor: "#0b3d91", color: "white", fontSize: 40 } }, children: ["hello"] };
const ok = new ImageResponse(card, { width: 320, height: 160 });
const bytes = new Uint8Array(await ok.arrayBuffer());
console.log(ok.headers.get("content-type"), bytes.length > 100, Array.from(bytes.slice(0, 4)).join(","));
const bad = new ImageResponse({ type: "div", props: { style: { display: "grid" } }, children: [] }, { width: 10, height: 10 });
try { await bad.arrayBuffer(); console.log("no error"); } catch (e) { console.log("error:", String(e.message).includes("display: grid")); }
`, "file://"+filepath.ToSlash(filepath.Join(repo, "packages", "runtime", "dist", "og.js")))
	dir := t.TempDir()
	f := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(f, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", f)
	cmd.Env = append(os.Environ(), h.EnvList()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := "image/png true 137,80,78,71\nerror: true\n"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
	// Without the secret the endpoint refuses.
	resp, err := http.Post(h.url+"/__krate/internal/image", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("unauthenticated image request: %d", resp.StatusCode)
	}
}
