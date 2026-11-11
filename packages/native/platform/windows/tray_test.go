package windows

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unsafe"
)

func TestNotifyIconDataLayout(t *testing.T) {
	var d notifyIconData
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout is pinned for 64-bit targets (amd64, arm64)")
	}
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"sizeof", unsafe.Sizeof(d), 976},
		{"size", unsafe.Offsetof(d.size), 0},
		{"hwnd", unsafe.Offsetof(d.hwnd), 8},
		{"id", unsafe.Offsetof(d.id), 16},
		{"flags", unsafe.Offsetof(d.flags), 20},
		{"callbackMessage", unsafe.Offsetof(d.callbackMessage), 24},
		{"icon", unsafe.Offsetof(d.icon), 32},
		{"tip", unsafe.Offsetof(d.tip), 40},
		{"state", unsafe.Offsetof(d.state), 296},
		{"stateMask", unsafe.Offsetof(d.stateMask), 300},
		{"info", unsafe.Offsetof(d.info), 304},
		{"version", unsafe.Offsetof(d.version), 816},
		{"infoTitle", unsafe.Offsetof(d.infoTitle), 820},
		{"infoFlags", unsafe.Offsetof(d.infoFlags), 948},
		{"guidItem", unsafe.Offsetof(d.guidItem), 952},
		{"balloonIcon", unsafe.Offsetof(d.balloonIcon), 968},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestTrayEventKind(t *testing.T) {
	cases := map[uint32]string{
		ninSelect: "click", ninKeySelect: "click", wmLButtonDblClk: "double-click", wmContextMenu: "right-click",
	}
	for ev, want := range cases {
		if got, ok := trayEventKind(ev); !ok || got != want {
			t.Errorf("event %#x = %q %v", ev, got, ok)
		}
	}
	// Raw button-up and mouse-move messages are ignored: version 4 also
	// sends NIN_SELECT / WM_CONTEXTMENU for them.
	for _, ev := range []uint32{0x0202, 0x0205, 0x0200, 0x0201, 0x0204} {
		if _, ok := trayEventKind(ev); ok {
			t.Errorf("event %#x must not be reported", ev)
		}
	}
}

func TestDecodeTrayMessage(t *testing.T) {
	// Anchor (-5, 1080) on a monitor left of the primary, icon id 1, WM_CONTEXTMENU.
	wp := uintptr(uint16(0xFFFB)) | uintptr(1080)<<16
	lp := uintptr(wmContextMenu) | uintptr(1)<<16
	ev, id, x, y := decodeTrayMessage(wp, lp)
	if ev != wmContextMenu || id != 1 || x != -5 || y != 1080 {
		t.Errorf("decoded %#x %d %d %d", ev, id, x, y)
	}
}

func TestTipUnits(t *testing.T) {
	if u := tipUnits("hi", 128); len(u) != 3 || u[2] != 0 {
		t.Errorf("short tip = %v", u)
	}
	long := make([]rune, 300)
	for i := range long {
		long[i] = 'x'
	}
	if u := tipUnits(string(long), 128); len(u) != 128 || u[127] != 0 || u[126] != 'x' {
		t.Errorf("long tip len %d", len(u))
	}
	// 126 BMP chars then a surrogate pair: the pair does not fit and must
	// not be split.
	s := string(long[:126]) + "\U0001F600"
	u := tipUnits(s, 128)
	if len(u) != 127 || u[125] != 'x' || u[126] != 0 {
		t.Errorf("surrogate tip len %d", len(u))
	}
	for _, c := range u[:len(u)-1] {
		if c >= 0xD800 && c < 0xE000 {
			t.Errorf("tip contains half a surrogate pair")
		}
	}
	if u := tipUnits("", 128); len(u) != 1 || u[0] != 0 {
		t.Errorf("empty tip = %v", u)
	}
	if trayTooltip("", "Title") != "Title" || trayTooltip("Tip", "Title") != "Tip" {
		t.Error("trayTooltip fallback")
	}
}

func makePNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPNGToIconBits(t *testing.T) {
	bits, err := pngToIconBits(makePNG(t, 64, 64, color.NRGBA{R: 10, G: 20, B: 30, A: 255}), 16)
	if err != nil {
		t.Fatal(err)
	}
	if bits.size != 16 || len(bits.bgra) != 16*16*4 {
		t.Fatalf("size %d, %d bytes", bits.size, len(bits.bgra))
	}
	// BGRA order, opaque.
	if p := bits.bgra[:4]; p[0] != 30 || p[1] != 20 || p[2] != 10 || p[3] != 255 {
		t.Errorf("first pixel = %v", p)
	}

	// A wide image is letterboxed: top and bottom rows stay transparent.
	bits, err = pngToIconBits(makePNG(t, 64, 16, color.NRGBA{R: 255, A: 255}), 16)
	if err != nil {
		t.Fatal(err)
	}
	if bits.bgra[3] != 0 {
		t.Errorf("letterbox row is not transparent: %v", bits.bgra[:4])
	}
	mid := (8*16 + 8) * 4
	if bits.bgra[mid+2] != 255 || bits.bgra[mid+3] != 255 {
		t.Errorf("middle pixel = %v", bits.bgra[mid:mid+4])
	}

	// Fully transparent input stays transparent.
	bits, _ = pngToIconBits(makePNG(t, 8, 8, color.NRGBA{}), 8)
	for _, b := range bits.bgra {
		if b != 0 {
			t.Fatal("transparent PNG produced color")
		}
	}

	for _, bad := range [][]byte{nil, []byte("not a png")} {
		if _, err := pngToIconBits(bad, 16); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := pngToIconBits(makePNG(t, 4, 4, color.NRGBA{A: 255}), 0); err == nil {
		t.Error("size 0 accepted")
	}
}
