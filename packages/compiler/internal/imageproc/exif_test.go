package imageproc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
)

// exifSegment builds an APP1 Exif segment holding only an Orientation tag.
func exifSegment(bo binary.ByteOrder, orientation uint16) []byte {
	var tiff bytes.Buffer
	if bo == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	u16 := func(v uint16) { _ = binary.Write(&tiff, bo, v) }
	u32 := func(v uint32) { _ = binary.Write(&tiff, bo, v) }
	u16(42)
	u32(8)
	u16(1) // one IFD entry
	u16(0x0112)
	u16(3)
	u32(1)
	u16(orientation)
	u16(0)
	u32(0) // no next IFD
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// jpegWithOrientation encodes img as a JPEG and inserts an EXIF orientation.
func jpegWithOrientation(t *testing.T, img image.Image, bo binary.ByteOrder, orientation uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	out := append([]byte{}, raw[:2]...)
	out = append(out, exifSegment(bo, orientation)...)
	return append(out, raw[2:]...)
}

func TestJPEGOrientationParsing(t *testing.T) {
	plain := bytes.Buffer{}
	_ = jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil)
	if got := jpegOrientation(plain.Bytes()); got != 1 {
		t.Errorf("no EXIF = %d, want 1", got)
	}
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for o := uint16(1); o <= 8; o++ {
			data := jpegWithOrientation(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), bo, o)
			if got := jpegOrientation(data); got != int(o) {
				t.Errorf("%v orientation %d parsed as %d", bo, o, got)
			}
		}
	}
	for _, bad := range []uint16{0, 9, 300} {
		data := jpegWithOrientation(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), binary.LittleEndian, bad)
		if got := jpegOrientation(data); got != 1 {
			t.Errorf("invalid orientation %d parsed as %d, want 1", bad, got)
		}
	}
	for _, junk := range [][]byte{nil, {0xFF}, {0xFF, 0xD8}, {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 'E'}, []byte("not a jpeg at all")} {
		if got := jpegOrientation(junk); got != 1 {
			t.Errorf("junk %q = %d, want 1", junk, got)
		}
	}
}

// markerImage is 2 wide and 3 tall with a distinct colour per pixel.
func markerImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(10 + x), G: uint8(10 + y), B: 0, A: 255})
		}
	}
	return img
}

func TestApplyOrientation(t *testing.T) {
	// For each orientation: the output size and where source pixel (0,0), the
	// top-left corner, lands.
	cases := []struct {
		o, w, h, x, y int
	}{
		{1, 2, 3, 0, 0},
		{2, 2, 3, 1, 0}, // mirror horizontally
		{3, 2, 3, 1, 2}, // rotate 180
		{4, 2, 3, 0, 2}, // mirror vertically
		{5, 3, 2, 0, 0}, // transpose
		{6, 3, 2, 2, 0}, // rotate 90 clockwise: top-left goes to top-right
		{7, 3, 2, 2, 1}, // transverse
		{8, 3, 2, 0, 1}, // rotate 90 counter-clockwise: top-left goes to bottom-left
	}
	for _, c := range cases {
		out := applyOrientation(markerImage(), c.o)
		b := out.Bounds()
		if b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("orientation %d size = %dx%d, want %dx%d", c.o, b.Dx(), b.Dy(), c.w, c.h)
			continue
		}
		r, g, _, _ := out.At(c.x, c.y).RGBA()
		if r>>8 != 10 || g>>8 != 10 {
			t.Errorf("orientation %d: source top-left not at (%d,%d), pixel there = %d,%d", c.o, c.x, c.y, r>>8, g>>8)
		}
	}
}

func TestProcessImageAppliesEXIFOrientation(t *testing.T) {
	root := t.TempDir()
	srcPath := filepath.Join(root, "public", "rot.jpg")
	_ = os.MkdirAll(filepath.Dir(srcPath), 0755)
	// 300x100 landscape: left half red, right half blue, tagged orientation 6
	// (display rotated 90 clockwise), so it should show as 100x300 with the red
	// half on top.
	src := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 300; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 150 {
				c = color.RGBA{B: 255, A: 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	if err := os.WriteFile(srcPath, jpegWithOrientation(t, src, binary.LittleEndian, 6), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := ProcessImage(root, srcPath, 0, 0, 82, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 100 || res.Height != 300 {
		t.Fatalf("dimensions = %dx%d, want 100x300", res.Width, res.Height)
	}
	last := res.WebP[len(res.WebP)-1]
	f, err := os.Open(filepath.Join(root, ".krate", "cache", "images", filepath.Base(last.FilePath)))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	v, err := webp.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bounds().Dx() != 100 || v.Bounds().Dy() != 300 {
		t.Fatalf("variant is %v, want 100x300", v.Bounds())
	}
	top, _, _, _ := v.At(50, 40).RGBA()
	_, _, bottomB, _ := v.At(50, 260).RGBA()
	if top>>8 < 200 || bottomB>>8 < 200 {
		t.Errorf("variant not rotated: top red=%d bottom blue=%d", top>>8, bottomB>>8)
	}
}

func writeTestGIF(t *testing.T, path string, frames int) {
	t.Helper()
	pal := []color.Color{color.RGBA{A: 255}, color.RGBA{R: 255, A: 255}}
	g := &gif.GIF{}
	for i := 0; i < frames; i++ {
		fr := image.NewPaletted(image.Rect(0, 0, 50, 40), pal)
		fr.SetColorIndex(i, i, 1)
		g.Image = append(g.Image, fr)
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessImageGIF(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "public")
	_ = os.MkdirAll(dir, 0755)

	still := filepath.Join(dir, "still.gif")
	writeTestGIF(t, still, 1)
	res, err := ProcessImage(root, still, 0, 0, 82, false)
	if err != nil {
		t.Fatalf("static GIF: %v", err)
	}
	if res.Width != 50 || res.Height != 40 || len(res.WebP) == 0 || res.FallbackMime != "image/png" {
		t.Errorf("static GIF result = %+v", res)
	}

	anim := filepath.Join(dir, "anim.gif")
	writeTestGIF(t, anim, 3)
	res, err = ProcessImage(root, anim, 25, 0, 82, false)
	if err != nil {
		t.Fatalf("animated GIF: %v", err)
	}
	if len(res.WebP) != 0 || len(res.Fallback) != 0 {
		t.Errorf("animated GIF must pass through, got variants %+v", res)
	}
	if res.Width != 25 || res.Height != 20 || res.FallbackMime != "image/gif" {
		t.Errorf("animated GIF dims/mime = %dx%d %s", res.Width, res.Height, res.FallbackMime)
	}
}
