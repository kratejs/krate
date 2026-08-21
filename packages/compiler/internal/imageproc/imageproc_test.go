package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/webp"
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

func TestEncodeWebPValidAndSmall(t *testing.T) {
	img := testImage(640, 400)
	data, err := EncodeWebP(img, 80)
	if err != nil {
		t.Fatalf("EncodeWebP: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Contains(data, []byte("WEBP")) {
		t.Errorf("output is not a WebP file: %q", string(data[:12]))
	}
	// 640x400 lossy photo should be well under a raw 640*400*4 = 1MB.
	if len(data) > 100_000 {
		t.Errorf("webp too large: %d bytes", len(data))
	}

	// The pure-Go encoder must produce output the decoder can read back.
	dec, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding generated webp: %v", err)
	}
	if got := dec.Bounds().Dx(); got != 640 {
		t.Errorf("decoded width = %d, want 640", got)
	}
}

func TestGeneratePlaceholderIsDataURI(t *testing.T) {
	p := GeneratePlaceholder(testImage(800, 600))
	if !strings.HasPrefix(p, "data:image/jpeg;base64,") {
		t.Errorf("placeholder = %.40s..., want data:image/jpeg;base64,", p)
	}
}

func TestGeneratePlaceholderPNGForTransparency(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	// Fully transparent canvas
	p := GeneratePlaceholder(img)
	if !strings.HasPrefix(p, "data:image/png;base64,") {
		t.Errorf("transparent placeholder = %.40s..., want data:image/png;base64,", p)
	}
}

func TestHasAlpha(t *testing.T) {
	if hasAlpha(testImage(100, 100)) {
		t.Error("opaque image reported as having alpha")
	}
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 0})
	if !hasAlpha(img) {
		t.Error("image with transparent pixel reported as opaque")
	}
}

func TestProcessImageGeneratesWebpAndFallback(t *testing.T) {
	root := t.TempDir()
	srcRel := filepath.Join("public", "photo.png")
	srcPath := filepath.Join(root, srcRel)
	os.MkdirAll(filepath.Dir(srcPath), 0755)
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

	// Source is opaque PNG → fallback stays PNG.
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

func TestProcessImageNoPlaceholder(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "public", "photo.png")
	os.MkdirAll(filepath.Dir(srcPath), 0755)
	writeTestPNG(t, srcPath, testImage(640, 360))

	res, err := ProcessImage(root, srcPath, 0, 0, 0, false)
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}
	if res.Placeholder != "" {
		t.Errorf("placeholder should be empty, got %.30s", res.Placeholder)
	}
	if res.Width != 640 || res.Height != 360 {
		t.Errorf("intrinsic Width/Height = %d/%d, want 640/360", res.Width, res.Height)
	}
}
