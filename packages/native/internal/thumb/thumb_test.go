package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestScaleKeepsAspectAndColor(t *testing.T) {
	out := Scale(solid(1920, 1080, color.RGBA{200, 100, 50, 255}), 160)
	if out.Bounds().Dx() != 160 || out.Bounds().Dy() != 90 {
		t.Fatalf("size %v", out.Bounds())
	}
	if c := out.RGBAAt(80, 45); c != (color.RGBA{200, 100, 50, 255}) {
		t.Fatalf("pixel %v", c)
	}
}

func TestScaleAveragesAndNeverEnlarges(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		v := uint8(0)
		if x >= 2 {
			v = 200
		}
		for y := 0; y < 2; y++ {
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	out := Scale(img, 2)
	if out.Bounds().Dx() != 2 || out.Bounds().Dy() != 1 || out.RGBAAt(0, 0).R != 0 || out.RGBAAt(1, 0).R < 199 {
		t.Fatalf("%v %v %v", out.Bounds(), out.RGBAAt(0, 0), out.RGBAAt(1, 0))
	}
	if same := Scale(img, 100); same.Bounds().Dx() != 4 {
		t.Fatalf("a small image must not grow: %v", same.Bounds())
	}
	if empty := Scale(image.NewRGBA(image.Rect(0, 0, 0, 0)), 10); empty.Bounds().Dx() != 0 {
		t.Fatal("empty source")
	}
}

func TestPNGRoundTrip(t *testing.T) {
	data := PNG(solid(640, 480, color.RGBA{1, 2, 3, 255}), 64)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
		t.Fatalf("%v %v", img, err)
	}
	again := ScalePNG(data, 32)
	img, err = png.Decode(bytes.NewReader(again))
	if err != nil || img.Bounds().Dx() != 32 {
		t.Fatalf("%v %v", img, err)
	}
	if ScalePNG([]byte("not a png"), 10) != nil {
		t.Fatal("bad input must give nil")
	}
	full := PNG(solid(10, 10, color.RGBA{}), 0)
	if img, _ := png.Decode(bytes.NewReader(full)); img == nil || img.Bounds().Dx() != 10 {
		t.Fatal("width 0 keeps the size")
	}
}

func TestBGRA(t *testing.T) {
	// One pixel, stride with padding: B=1 G=2 R=3 A=0.
	pix := []byte{1, 2, 3, 0, 9, 9, 9, 9}
	img := BGRA(pix, 1, 1, 8)
	if c := img.RGBAAt(0, 0); c != (color.RGBA{3, 2, 1, 255}) {
		t.Fatalf("%v", c)
	}
}
