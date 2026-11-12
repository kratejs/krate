package caps

import (
	"image"
	"image/color"
	"strconv"
	"strings"
)

// A 3x5 pixel font for the badge glyphs; each row is 3 bits, top bit left.
var badgeFont = map[rune][5]uint8{
	'0': {7, 5, 5, 5, 7},
	'1': {2, 6, 2, 2, 7},
	'2': {7, 1, 7, 4, 7},
	'3': {7, 1, 7, 1, 7},
	'4': {5, 5, 7, 1, 1},
	'5': {7, 4, 7, 1, 7},
	'6': {7, 4, 7, 5, 7},
	'7': {7, 1, 1, 1, 1},
	'8': {7, 5, 7, 5, 7},
	'9': {7, 5, 7, 1, 7},
	'+': {0, 2, 7, 2, 0},
	'!': {2, 2, 2, 0, 2},
}

// BadgeLabel shortens a badge text to what fits in a small overlay: counts
// above 99 become "99+", other text longer than two glyphs becomes "!".
func BadgeLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 0 {
		if n > 99 {
			return "99+"
		}
		return strconv.Itoa(n)
	}
	if len([]rune(text)) <= 2 {
		return text
	}
	return "!"
}

// BadgeImage draws a red round badge of size by size pixels with text on it
// (see BadgeLabel), for platforms that overlay an icon on the taskbar button.
// Glyphs the tiny font lacks are drawn as a dot.
func BadgeImage(text string, size int) *image.NRGBA {
	if size < 16 {
		size = 16
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	red := color.NRGBA{R: 0xD9, G: 0x2D, B: 0x20, A: 0xFF}
	white := color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	c := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			if dx*dx+dy*dy <= c*c {
				img.SetNRGBA(x, y, red)
			}
		}
	}
	label := []rune(BadgeLabel(text))
	if len(label) == 0 {
		return img
	}
	// Scale so the glyph row fits inside the circle with a margin.
	cols := len(label)*3 + (len(label) - 1)
	scale := int(float64(size) * 0.7 / float64(cols))
	if s2 := int(float64(size) * 0.6 / 5); s2 < scale {
		scale = s2
	}
	if scale < 1 {
		scale = 1
	}
	w, h := cols*scale, 5*scale
	ox, oy := (size-w)/2, (size-h)/2
	for i, r := range label {
		g, ok := badgeFont[r]
		if !ok {
			g = [5]uint8{0, 0, 2, 0, 0}
		}
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if g[row]&(4>>col) == 0 {
					continue
				}
				px := ox + (i*4+col)*scale
				py := oy + row*scale
				for yy := 0; yy < scale; yy++ {
					for xx := 0; xx < scale; xx++ {
						img.SetNRGBA(px+xx, py+yy, white)
					}
				}
			}
		}
	}
	return img
}

// BGRAPremultiplied returns the pixels of img as top-down
// BGRA bytes with premultiplied alpha, the layout of a 32-bit Windows bitmap
// used for a taskbar overlay icon.
func BGRAPremultiplied(img *image.NRGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			a := uint32(c.A)
			out = append(out, byte(uint32(c.B)*a/255), byte(uint32(c.G)*a/255), byte(uint32(c.R)*a/255), c.A)
		}
	}
	return out
}
