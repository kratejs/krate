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

func decodeFile(t *testing.T, path string) (image.Image, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return img, len(data)
}

// flatImage is a two-colour graphic, like most logos and illustrations.
func flatImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 240, G: 240, B: 240, A: 255}
			if (x/100+y/100)%2 == 0 {
				c = color.RGBA{R: 20, G: 60, B: 200, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func fallbackPath(root string, e SrcsetEntry) string {
	return filepath.Join(root, ".krate", "cache", "images", filepath.Base(e.FilePath))
}

func TestPNGVariantsAreNotLargerThanASmallSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "flat.png")
	writeTestPNG(t, src, flatImage(800, 500))
	srcInfo, _ := os.Stat(src)

	res, err := ProcessImage(root, src, 0, 0, 82, false)
	if err != nil {
		t.Fatal(err)
	}
	last := res.Fallback[len(res.Fallback)-1]
	if last.Width != 800 {
		t.Fatalf("last variant width = %d, want 800", last.Width)
	}
	_, size := decodeFile(t, fallbackPath(root, last))
	if int64(size) > srcInfo.Size() {
		t.Errorf("full-size PNG variant is %d bytes, source is %d", size, srcInfo.Size())
	}
}

func TestPNGVariantsStayPaletted(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "flat.png")
	writeTestPNG(t, src, flatImage(800, 500))
	res, err := ProcessImage(root, src, 0, 0, 82, false)
	if err != nil {
		t.Fatal(err)
	}
	img, _ := decodeFile(t, fallbackPath(root, res.Fallback[len(res.Fallback)-1]))
	if _, ok := img.(*image.Paletted); !ok {
		t.Errorf("flat full-size variant decoded as %T, want *image.Paletted", img)
	}
}

func TestIndexedSourceVariantsKeepItsPalette(t *testing.T) {
	pal := color.Palette{color.RGBA{255, 255, 255, 255}, color.RGBA{200, 30, 30, 255}, color.RGBA{30, 30, 200, 255}}
	src := image.NewPaletted(image.Rect(0, 0, 1000, 600), pal)
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			src.SetColorIndex(x, y, uint8((x/37+y/53)%3))
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, "idx.png")
	writeTestPNG(t, path, src)
	res, err := ProcessImage(root, path, 640, 0, 82, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Fallback {
		img, _ := decodeFile(t, fallbackPath(root, e))
		p, ok := img.(*image.Paletted)
		if !ok {
			t.Fatalf("%dw variant decoded as %T, want *image.Paletted", e.Width, img)
		}
		if len(p.Palette) > len(pal) {
			t.Errorf("%dw palette has %d colours, source has %d", e.Width, len(p.Palette), len(pal))
		}
	}
}

func TestEncodePNGKeepsPhotosTruecolour(t *testing.T) {
	data, err := EncodePNG(testImage(64, 64))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.Paletted); ok {
		t.Error("an image with thousands of colours must not be quantized")
	}
}

func TestPlaceholderIsSmall(t *testing.T) {
	flat := GeneratePlaceholder(flatImage(800, 500))
	if !strings.HasPrefix(flat, "data:image/") || len(flat) > 400 {
		t.Errorf("flat placeholder = %d chars (%.40s)", len(flat), flat)
	}
	photo := GeneratePlaceholder(testImage(800, 500))
	if len(photo) > 900 {
		t.Errorf("photo placeholder = %d chars", len(photo))
	}
}
