package darwin

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func filled(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestInspectTrayIcon(t *testing.T) {
	black := encodePNG(t, filled(18, 18, color.NRGBA{0, 0, 0, 255}))
	info, err := inspectTrayIcon(black)
	if err != nil || info.W != 18 || info.H != 18 || !info.Template {
		t.Errorf("black 18x18: %+v %v", info, err)
	}
	white := encodePNG(t, filled(16, 16, color.NRGBA{255, 255, 255, 200}))
	if info, _ := inspectTrayIcon(white); !info.Template {
		t.Error("white-on-alpha icon should be a template")
	}
	red := encodePNG(t, filled(16, 16, color.NRGBA{220, 30, 30, 255}))
	if info, _ := inspectTrayIcon(red); info.Template {
		t.Error("a red icon must keep its colors")
	}
	// Retina size is scaled down to 18 points.
	big := encodePNG(t, filled(72, 36, color.NRGBA{0, 0, 0, 255}))
	if info, _ := inspectTrayIcon(big); info.H != 18 || info.W != 36 {
		t.Errorf("72x36 -> %+v", info)
	}
	// Two gray tones far apart are not a single tone.
	two := filled(4, 4, color.NRGBA{0, 0, 0, 255})
	two.SetNRGBA(1, 1, color.NRGBA{200, 200, 200, 255})
	if info, _ := inspectTrayIcon(encodePNG(t, two)); info.Template {
		t.Error("two grays must not be a template")
	}
	empty := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)))
	if info, _ := inspectTrayIcon(empty); info.Template {
		t.Error("an empty image is not a template")
	}
}

func TestInspectTrayIconErrors(t *testing.T) {
	if _, err := inspectTrayIcon([]byte("not a png")); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := inspectTrayIcon(nil); err == nil {
		t.Error("empty accepted")
	}
	huge := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, trayMaxEdgePx+1, 4)))
	if _, err := inspectTrayIcon(huge); err == nil {
		t.Error("oversized accepted")
	}
}

func TestTrayPointSize(t *testing.T) {
	for _, c := range []struct {
		w, h   int
		pw, ph float64
	}{{16, 16, 16, 16}, {18, 18, 18, 18}, {36, 36, 18, 18}, {44, 22, 36, 18}} {
		if w, h := trayPointSize(c.w, c.h); w != c.pw || h != c.ph {
			t.Errorf("trayPointSize(%d,%d) = %v,%v; want %v,%v", c.w, c.h, w, h, c.pw, c.ph)
		}
	}
}
