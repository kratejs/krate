// Package thumb makes PNG thumbnails of captured screens and windows. It is
// pure Go so the platform layers share it and it can be tested everywhere.
package thumb

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
)

// Scale returns img scaled to width pixels wide, keeping the aspect ratio,
// with a box filter (each target pixel averages the source pixels it covers).
// A source that is already at most width wide is returned as a copy. Width
// must be positive.
func Scale(img image.Image, width int) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || width <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	if sw <= width {
		out := image.NewRGBA(image.Rect(0, 0, sw, sh))
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	dw := width
	dh := sh * dw / sw
	if dh < 1 {
		dh = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, a, n = r+pr, g+pg, bl+pb, a+pa, n+1
				}
			}
			o := out.PixOffset(x, y)
			out.Pix[o+0], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), uint8(a/n>>8)
		}
	}
	return out
}

// PNG scales img to width and encodes it. A zero or negative width keeps the
// size. It returns nil when encoding fails.
func PNG(img image.Image, width int) []byte {
	if width > 0 {
		img = Scale(img, width)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// ScalePNG decodes a PNG, scales it to width and encodes it again. On a
// decode error it returns nil.
func ScalePNG(data []byte, width int) []byte {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return PNG(img, width)
}

// BGRA wraps top-down 32-bit BGRA pixels (a Windows DIB section, a Core
// Graphics bitmap) as an image. stride is the row size in bytes. The alpha
// channel is forced opaque: screen captures often leave it zero.
func BGRA(pix []byte, w, h, stride int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := pix[y*stride : y*stride+w*4]
		o := out.PixOffset(0, y)
		for x := 0; x < w; x++ {
			out.Pix[o+x*4+0], out.Pix[o+x*4+1], out.Pix[o+x*4+2], out.Pix[o+x*4+3] = row[x*4+2], row[x*4+1], row[x*4+0], 255
		}
	}
	return out
}
