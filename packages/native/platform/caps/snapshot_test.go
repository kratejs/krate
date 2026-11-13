package caps

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestSnapshotFromPNG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	s, err := SnapshotFromPNG(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 2 || s.Height != 1 || s.Stride != 8 {
		t.Fatalf("%dx%d stride %d", s.Width, s.Height, s.Stride)
	}
	// Opaque pixel: B, G, R, A.
	if got := s.Pix[:4]; got[0] != 30 || got[1] != 20 || got[2] != 10 || got[3] != 255 {
		t.Fatalf("opaque pixel %v", got)
	}
	// Half transparent pixel is premultiplied: 200*128/255 = 100, 100*128/255 = 50, 50*128/255 = 25.
	if got := s.Pix[4:8]; got[3] != 128 || got[2] < 99 || got[2] > 101 || got[1] < 49 || got[1] > 51 || got[0] < 24 || got[0] > 26 {
		t.Fatalf("premultiplied pixel %v", got)
	}
	if _, err := SnapshotFromPNG([]byte("not a png")); err == nil {
		t.Fatal("garbage decoded")
	}
}
