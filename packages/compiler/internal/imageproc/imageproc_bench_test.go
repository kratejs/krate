package imageproc

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// benchImagePNG writes a large PNG once and returns its bytes, so the benchmark
// measures ProcessImage (decode + resize + encode) rather than image setup.
func benchImagePNG(b *testing.B, w, h int) []byte {
	b.Helper()
	path := filepath.Join(b.TempDir(), "src.png")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := png.Encode(f, testImage(w, h)); err != nil {
		b.Fatal(err)
	}
	f.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	return data
}

// BenchmarkProcessImageFull covers the cold path (fresh cache): decode, resize,
// WebP + fallback encode for every srcset width.
func BenchmarkProcessImageFull(b *testing.B) {
	data := benchImagePNG(b, 3000, 2000)
	base := b.TempDir()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root := filepath.Join(base, fmt.Sprintf("r%d", i))
		src := filepath.Join(root, "public", "photo.png")
		if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(src, data, 0644); err != nil {
			b.Fatal(err)
		}
		if _, err := ProcessImage(root, src, 2000, 0, 82, true); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessImageCacheHit covers the warm path: the meta + variant files
// already exist, so no decode/resize/encode happens.
func BenchmarkProcessImageCacheHit(b *testing.B) {
	data := benchImagePNG(b, 3000, 2000)
	root := b.TempDir()
	src := filepath.Join(root, "public", "photo.png")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(src, data, 0644); err != nil {
		b.Fatal(err)
	}
	if _, err := ProcessImage(root, src, 2000, 0, 82, true); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ProcessImage(root, src, 2000, 0, 82, true); err != nil {
			b.Fatal(err)
		}
	}
}
