package build

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
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
