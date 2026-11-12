package caps

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestProgressValidate(t *testing.T) {
	for _, tc := range []struct {
		in   Progress
		want Progress
		bad  bool
	}{
		{Progress{ProgressNormal, 0.5}, Progress{ProgressNormal, 0.5}, false},
		{Progress{ProgressNormal, 7}, Progress{ProgressNormal, 1}, false},
		{Progress{ProgressError, -1}, Progress{ProgressError, 0}, false},
		{Progress{ProgressIndeterminate, 0.9}, Progress{ProgressIndeterminate, 0}, false},
		{Progress{ProgressNone, 0.9}, Progress{ProgressNone, 0}, false},
		{Progress{"", 0}, Progress{}, true},
		{Progress{"weird", 0}, Progress{}, true},
		{Progress{ProgressNormal, math.NaN()}, Progress{}, true},
		{Progress{ProgressPaused, math.Inf(1)}, Progress{}, true},
	} {
		got, err := tc.in.Validate()
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Errorf("Validate(%+v) = %+v, %v", tc.in, got, err)
		}
	}
}

func twoDisplays() []Display {
	return []Display{
		{ID: "a", Primary: true, X: 0, Y: 0, Width: 1920, Height: 1080, WorkX: 0, WorkY: 30, WorkWidth: 1920, WorkHeight: 1050, Scale: 1},
		{ID: "b", X: 1920, Y: -200, Width: 2560, Height: 1440, WorkX: 1920, WorkY: -200, WorkWidth: 2560, WorkHeight: 1400, Scale: 2},
	}
}

func TestDisplayForRect(t *testing.T) {
	ds := twoDisplays()
	for _, tc := range []struct {
		r    Rect
		want string
	}{
		{Rect{100, 100, 800, 600}, "a"},
		{Rect{2000, 0, 800, 600}, "b"},
		{Rect{1500, 100, 800, 600}, "a"}, // 420 px wide on a, 380 on b
		{Rect{1700, 100, 800, 600}, "b"},
		{Rect{-5000, 10, 100, 100}, "a"}, // off every display: nearest
		{Rect{9000, 10, 100, 100}, "b"},
	} {
		got, ok := DisplayForRect(ds, tc.r)
		if !ok || got.ID != tc.want {
			t.Errorf("DisplayForRect(%+v) = %s, want %s", tc.r, got.ID, tc.want)
		}
	}
	if _, ok := DisplayForRect(nil, Rect{}); ok {
		t.Error("no displays must report !ok")
	}
}

func TestCenterIn(t *testing.T) {
	ds := twoDisplays()
	if x, y := CenterIn(ds[0], 800, 600); x != 560 || y != 30+225 {
		t.Errorf("center on a = %d,%d", x, y)
	}
	if x, y := CenterIn(ds[1], 1000, 800); x != 1920+780 || y != -200+300 {
		t.Errorf("center on b = %d,%d", x, y)
	}
	if x, y := CenterIn(ds[0], 4000, 3000); x != 0 || y != 30 {
		t.Errorf("oversized pins to the corner, got %d,%d", x, y)
	}
}

func TestFindDisplay(t *testing.T) {
	ds := twoDisplays()
	if d, ok := FindDisplay(ds, ""); !ok || d.ID != "a" {
		t.Errorf("primary = %+v", d)
	}
	if d, ok := FindDisplay(ds, "b"); !ok || d.ID != "b" {
		t.Errorf("b = %+v", d)
	}
	if _, ok := FindDisplay(ds, "zz"); ok {
		t.Error("unknown id found")
	}
}

func TestBadgeLabel(t *testing.T) {
	for in, want := range map[string]string{"": "", "  ": "", "3": "3", "42": "42", "100": "99+", "-1": "-1", "ab": "ab", "abc": "!", "new!": "!"} {
		if got := BadgeLabel(in); got != want {
			t.Errorf("BadgeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBadgeImage(t *testing.T) {
	blank := BadgeImage("", 32)
	n12 := BadgeImage("12", 32)
	n13 := BadgeImage("13", 32)
	if blank.Bounds().Dx() != 32 {
		t.Fatalf("size %v", blank.Bounds())
	}
	white := func(i *image.NRGBA) int {
		n := 0
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				if c := i.NRGBAAt(x, y); c.R == 0xFF && c.G == 0xFF && c.A == 0xFF {
					n++
				}
			}
		}
		return n
	}
	if white(blank) != 0 {
		t.Error("an empty badge has no glyph")
	}
	if white(n12) == 0 || white(n13) == 0 {
		t.Error("glyphs missing")
	}
	same := true
	for i := range n12.Pix {
		if n12.Pix[i] != n13.Pix[i] {
			same = false
		}
	}
	if same {
		t.Error("12 and 13 draw the same")
	}
	if c := n12.NRGBAAt(0, 0); c.A != 0 {
		t.Error("corner must be transparent")
	}
	if c := n12.NRGBAAt(16, 2); c.R != 0xD9 {
		t.Errorf("circle fill = %+v", c)
	}
}

func TestTopLeftY(t *testing.T) {
	// A 1080-high primary display; a window at the bottom-left origin with
	// height 600 sits 480 from the top.
	if got := TopLeftY(1080, 0, 600); got != 480 {
		t.Errorf("got %d", got)
	}
	// A display stacked above the primary has a positive Cocoa y and a
	// negative top-left y.
	if got := TopLeftY(1080, 1080, 1440); got != -1440 {
		t.Errorf("got %d", got)
	}
	if got := TopLeftY(900.4, 25, 875); got != 0 {
		t.Errorf("menu bar offset: got %d", got)
	}
}

func TestOnBatteryFromSourceType(t *testing.T) {
	for in, want := range map[string]bool{"Battery Power": true, " battery power\n": true, "AC Power": false, "UPS Power": false, "": false} {
		if got := OnBatteryFromSourceType(in); got != want {
			t.Errorf("%q = %v", in, got)
		}
	}
}

func TestBGRAPremultiplied(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	got := BGRAPremultiplied(img)
	want := []byte{50, 100, 200, 255, 25, 50, 100, 128}
	if string(got) != string(want) {
		t.Errorf("got %v want %v", got, want)
	}
}
