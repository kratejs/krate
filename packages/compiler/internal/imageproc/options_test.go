package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeSizedPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "src.png")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProcessImageWithBreakpointsAndFormats(t *testing.T) {
	src := writeSizedPNG(t, 1600, 1000)
	res, err := ProcessImageWith(t.TempDir(), src, 0, 0, Options{Breakpoints: []int{400, 800}, Formats: []string{"original"}})
	if err != nil {
		t.Fatalf("ProcessImageWith: %v", err)
	}
	if len(res.WebP) != 0 {
		t.Errorf("expected no webp variants, got %d", len(res.WebP))
	}
	want := []int{400, 800, 1600}
	if len(res.Fallback) != len(want) {
		t.Fatalf("fallback widths = %d, want %d", len(res.Fallback), len(want))
	}
	for i, e := range res.Fallback {
		if e.Width != want[i] {
			t.Errorf("fallback[%d] width = %d, want %d", i, e.Width, want[i])
		}
	}
}

func TestProcessImageWithWebPOnly(t *testing.T) {
	src := writeSizedPNG(t, 1000, 500)
	res, err := ProcessImageWith(t.TempDir(), src, 0, 0, Options{Formats: []string{"webp"}, Breakpoints: []int{300}})
	if err != nil {
		t.Fatalf("ProcessImageWith: %v", err)
	}
	if len(res.Fallback) != 0 {
		t.Errorf("expected no fallback variants, got %d", len(res.Fallback))
	}
	if len(res.WebP) != 2 {
		t.Fatalf("webp widths = %d, want 2 (300,1000)", len(res.WebP))
	}
}
