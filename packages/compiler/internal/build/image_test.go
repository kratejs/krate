package build

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func solidImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	return img
}

// buildImagePage builds one page and returns its HTML and the build error.
func buildImagePage(t *testing.T, page string, mutate func(*config.Config)) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "public", "small.png"), solidImage(200, 100))
	writePNG(t, filepath.Join(root, "public", "square.png"), solidImage(300, 300))
	writeFileRel(t, root, "src/pages/index.tsx", page)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	if mutate != nil {
		mutate(cfg)
	}
	err := New(root, cfg).BuildAll()
	html := ""
	if data, rerr := os.ReadFile(filepath.Join(cfg.OutDir, "index.html")); rerr == nil {
		html = string(data)
	}
	return html, root, err
}

func TestImageMissingFileIsPositionedBuildError(t *testing.T) {
	html, root, err := buildImagePage(t, `export default function P() {
  return <main><Image src="/missing.jpg" alt="x" /></main>;
}
`, nil)
	if err == nil {
		t.Fatalf("expected a build error, got html:\n%s", html)
	}
	msg := err.Error()
	for _, want := range []string{"KR8801", "index.tsx:2:", "public/missing.jpg", "hint:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "reading image") || strings.Contains(msg, root+"/public") {
		t.Errorf("error leaks the absolute image path:\n%s", msg)
	}
	if strings.Contains(html, "[Image:") {
		t.Errorf("error marker shipped in html:\n%s", html)
	}
}

func TestImageRemoteNotAllowedIsBuildError(t *testing.T) {
	_, _, err := buildImagePage(t, `export default function P() {
  return <Image src="https://evil.example/x.png" alt="x" width={10} height={10} />;
}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "KR8802") || !strings.Contains(err.Error(), "remotePatterns") {
		t.Fatalf("want KR8802 mentioning remotePatterns, got %v", err)
	}
}

func TestImageResolvesConstantsAndHonorsDimensions(t *testing.T) {
	html, _, err := buildImagePage(t, `const one = "/small.png";
export default function P() {
  const w = 150;
  return (
    <main>
      <Image src={one} alt="a" width={w} />
      <Image src="/square.png" alt="b" width={120} height={60} />
      <Image src="/small.png" alt="c" style="border:1px solid red" />
    </main>
  );
}
`, nil)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	imgs := regexp.MustCompile(`<img[^>]*>`).FindAllString(html, -1)
	if len(imgs) != 3 {
		t.Fatalf("want 3 <img>, got %d:\n%s", len(imgs), html)
	}
	// a: constant src resolved, requested width kept and never upscaled.
	if !strings.Contains(imgs[0], `width=150`) && !strings.Contains(imgs[0], `width="150"`) {
		t.Errorf("width attr not honored: %s", imgs[0])
	}
	if !strings.Contains(imgs[0], "max-width:100%") || regexp.MustCompile(`(^|[^-])width:100%`).MatchString(imgs[0]) {
		t.Errorf("image must not be stretched to 100%% width: %s", imgs[0])
	}
	if !strings.Contains(imgs[0], "aspect-ratio:150/75") {
		t.Errorf("aspect ratio from the 200x100 source: %s", imgs[0])
	}
	// b: both dimensions requested keep both, cropped with object-fit.
	if !strings.Contains(imgs[1], "aspect-ratio:120/60") || !strings.Contains(imgs[1], "object-fit:cover") {
		t.Errorf("width+height not honored: %s", imgs[1])
	}
	if !regexp.MustCompile(`height=("?)60`).MatchString(imgs[1]) {
		t.Errorf("height attr missing: %s", imgs[1])
	}
	// c: a user style merges into the single style attribute.
	if strings.Count(imgs[2], "style=") != 1 || !strings.Contains(imgs[2], "border:1px solid red") || !strings.Contains(imgs[2], "aspect-ratio") {
		t.Errorf("user style not merged into one attribute: %s", imgs[2])
	}
}

func TestImageDynamicWidthIsError(t *testing.T) {
	_, _, err := buildImagePage(t, `export default function P(props: { w: number }) {
  return <Image src="/small.png" alt="a" width={props.w} />;
}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "KR8803") {
		t.Fatalf("want KR8803 for a non-build-time width, got %v", err)
	}
}

func TestImageMissingAltWarnsButExplicitEmptyDoesNot(t *testing.T) {
	root := t.TempDir()
	b := New(root, config.Default())
	src := func(s string) []imageDiag {
		prog := parseTestProgram(t, s)
		return b.transformImages(prog)
	}
	ds := src(`export default function P() { return <Image src="/missing-ok.png" />; }`)
	hasAlt := false
	for _, d := range ds {
		if d.code == "KR8804" && d.warn {
			hasAlt = true
		}
	}
	if !hasAlt {
		t.Errorf("expected KR8804 warning, got %+v", ds)
	}
	for _, d := range src(`export default function P() { return <Image src="/missing-ok.png" alt="" />; }`) {
		if d.code == "KR8804" {
			t.Errorf("alt=\"\" must not warn: %+v", d)
		}
	}
}

func TestImageBasePathPrefixesGeneratedURLs(t *testing.T) {
	html, _, err := buildImagePage(t, `export default function P() { return <Image src="/small.png" alt="a" />; }
`, func(c *config.Config) { c.BasePath = "/sub" })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for _, m := range regexp.MustCompile(`(?:src|srcset|srcSet)="?([^">]+)`).FindAllStringSubmatch(html, -1) {
		for _, u := range strings.Split(m[1], ",") {
			u = strings.TrimSpace(strings.Fields(u + " ")[0])
			if strings.HasPrefix(u, "/_krate/images/") || u == "/small.png" {
				t.Errorf("generated URL %q is not prefixed with the base path", u)
			}
		}
	}
	if !strings.Contains(html, "/sub/_krate/images/") || !strings.Contains(html, "/sub/small.png") {
		t.Errorf("expected prefixed image URLs:\n%s", html)
	}
}

func parseTestProgram(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	return prog
}

func TestImageAcceptsExponentFormPixelCounts(t *testing.T) {
	for _, w := range []string{"1000", "2000", "10000", "3000"} {
		html, _, err := buildImagePage(t, `export default function P() {
  return <Image src="/small.png" alt="a" width={`+w+`} />;
}
`, nil)
		if err != nil {
			t.Fatalf("width={%s}: %v", w, err)
		}
		if !strings.Contains(html, "<img") {
			t.Errorf("width={%s}: no <img> rendered:\n%s", w, html)
		}
	}
}

func TestParsePixels(t *testing.T) {
	ok := map[string]int{"640": 640, "1e3": 1000, "2e3": 2000, "1e4": 10000, " 12 ": 12, "1.2e3": 1200, "0": 0}
	for in, want := range ok {
		if got, err := parsePixels(in); err != nil || got != want {
			t.Errorf("parsePixels(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"1.5", "abc", "", "1e99", "NaN", "Inf", "50%"} {
		if _, err := parsePixels(in); err == nil {
			t.Errorf("parsePixels(%q) should fail", in)
		}
	}
}
