package build

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
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

func TestImageRemoteNotAllowedIsBuildError(t *testing.T) {
	_, _, err := buildImagePage(t, `export default function P() {
  return <Image src="https://evil.example/x.png" alt="x" width={10} height={10} />;
}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "KR8802") || !strings.Contains(err.Error(), "remotePatterns") {
		t.Fatalf("want KR8802 mentioning remotePatterns, got %v", err)
	}
}

func TestImageDefaultSizesShrinkBelowRequestedWidth(t *testing.T) {
	html, _, err := buildImagePage(t, `export default function P() {
  return <Image src="/small.png" alt="x" width={200} />;
}
`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `(max-width: 200px) 100vw, 200px`) {
		i := strings.Index(html, "sizes=")
		t.Fatalf("default sizes should be viewport-aware, got %q", html[i:min(len(html), i+60)])
	}
}

// remotePatterns built in krate.config.ts from a value that only a .env file
// defines: the host builds, other hosts and other schemes or ports do not.
func TestImageRemotePatternsBuiltFromEnvFile(t *testing.T) {
	build := func(src string) error {
		root := t.TempDir()
		writeFileRel(t, root, ".env", "PUBLIC_ASSET_BASE_URL=http://assets.example.test:9000\n")
		writeFileRel(t, root, "krate.config.ts", `const remotePatterns = [{ protocol: 'https', hostname: 'cdn.example.test' }];
if (process.env.PUBLIC_ASSET_BASE_URL) {
  const u = new URL(process.env.PUBLIC_ASSET_BASE_URL);
  remotePatterns.push({ protocol: u.protocol.replace(':', ''), hostname: u.hostname, port: u.port });
}
export default { images: { remotePatterns } };
`)
		writeFileRel(t, root, "src/pages/index.tsx", `export default function P() {
  return <Image src="`+src+`" alt="x" width={10} height={10} />;
}
`)
		files, err := environ.Load(root, "production")
		if err != nil {
			t.Fatal(err)
		}
		prev := environ.Current
		environ.Current = files
		defer func() { environ.Current = prev }()
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.PublicDir = filepath.Join(root, "public")
		return New(root, cfg).BuildAll()
	}
	if err := build("http://assets.example.test:9000/a.png"); err != nil {
		t.Fatalf("env-built host should build: %v", err)
	}
	for _, src := range []string{
		"https://assets.example.test:9000/a.png",
		"http://assets.example.test:9001/a.png",
		"http://assets.example.test/a.png",
		"https://evil.example/x.png",
	} {
		if err := build(src); err == nil || !strings.Contains(err.Error(), "KR8802") {
			t.Errorf("%s: want KR8802, got %v", src, err)
		}
	}
}
