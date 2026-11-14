package imageproc

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}
	return img
}

func writeTestPNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestProcessImageGeneratesWebpAndFallback(t *testing.T) {
	root := t.TempDir()
	srcRel := filepath.Join("public", "photo.png")
	srcPath := filepath.Join(root, srcRel)
	_ = os.MkdirAll(filepath.Dir(srcPath), 0755)
	writeTestPNG(t, srcPath, testImage(1600, 1000))

	res, err := ProcessImage(root, srcPath, 800, 500, 82, true)
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}

	// WebP srcset with the responsive breakpoints + original width.
	if len(res.WebP) == 0 {
		t.Fatal("expected webp srcset entries")
	}
	for _, e := range res.WebP {
		if !strings.HasSuffix(e.FilePath, ".webp") {
			t.Errorf("webp entry not .webp: %s", e.FilePath)
		}
		if !strings.HasPrefix(e.FilePath, "/_krate/images/") {
			t.Errorf("webp entry missing URL prefix: %s", e.FilePath)
		}
		if _, err := os.Stat(filepath.Join(root, ".krate", "cache", "images", filepath.Base(e.FilePath))); err != nil {
			t.Errorf("webp variant missing on disk: %v", err)
		}
	}

	// Source is opaque PNG -> fallback stays PNG.
	if res.FallbackMime != "image/png" {
		t.Errorf("FallbackMime = %q, want image/png", res.FallbackMime)
	}
	if len(res.Fallback) == 0 || !strings.HasSuffix(res.Fallback[0].FilePath, ".png") {
		t.Errorf("fallback entries missing: %+v", res.Fallback)
	}

	// Display size reflects the requested width (800x500).
	if res.Width != 800 || res.Height != 500 {
		t.Errorf("Width/Height = %d/%d, want 800/500", res.Width, res.Height)
	}
	if res.AspectRatio < 1.59 || res.AspectRatio > 1.61 {
		t.Errorf("AspectRatio = %.3f, want ~1.6", res.AspectRatio)
	}
	if !strings.HasPrefix(res.Placeholder, "data:") {
		t.Error("expected a placeholder data URI")
	}
}
