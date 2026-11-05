package ogimage

import (
	"image"
	"image/color"
	"math"

	xdraw "golang.org/x/image/draw"
)

// drawTarget is a canvas the painters draw onto.
type drawTarget struct {
	img *image.RGBA
}

func (t drawTarget) uniform(c rgba) *image.Uniform { return image.NewUniform(c.std()) }

// over composites c (non-premultiplied, alpha a) onto the pixel.
func over(img *image.RGBA, x, y int, c rgba, cov float64) {
	if !(image.Point{X: x, Y: y}.In(img.Rect)) {
		return
	}
	a := c.a * cov
	if a <= 0 {
		return
	}
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+4 : i+4]
	inv := 1 - a
	p[0] = uint8(math.Round(c.r*a*255 + float64(p[0])*inv))
	p[1] = uint8(math.Round(c.g*a*255 + float64(p[1])*inv))
	p[2] = uint8(math.Round(c.b*a*255 + float64(p[2])*inv))
	p[3] = uint8(math.Round(a*255 + float64(p[3])*inv))
}

type rect struct{ x, y, w, h float64 }

// sdf is the signed distance from (px, py) to a rounded rectangle (negative inside).
func (r rect) sdf(px, py float64, radii [4]float64) float64 {
	cx, cy := r.x+r.w/2, r.y+r.h/2
	dx, dy := px-cx, py-cy
	var rad float64
	switch {
	case dx < 0 && dy < 0:
		rad = radii[0]
	case dx >= 0 && dy < 0:
		rad = radii[1]
	case dx >= 0 && dy >= 0:
		rad = radii[2]
	default:
		rad = radii[3]
	}
	rad = math.Min(rad, math.Min(r.w, r.h)/2)
	qx := math.Abs(dx) - (r.w/2 - rad)
	qy := math.Abs(dy) - (r.h/2 - rad)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - rad
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// fill paints the rounded rectangle r with paint(x, y), anti-aliased.
func fill(img *image.RGBA, r rect, radii [4]float64, paint func(x, y float64) rgba) {
	x0, y0 := int(math.Floor(r.x))-1, int(math.Floor(r.y))-1
	x1, y1 := int(math.Ceil(r.x+r.w))+1, int(math.Ceil(r.y+r.h))+1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cov := clamp01(0.5 - r.sdf(float64(x)+0.5, float64(y)+0.5, radii))
			if cov > 0 {
				over(img, x, y, paint(float64(x)+0.5, float64(y)+0.5), cov)
			}
		}
	}
}

func solid(c rgba) func(x, y float64) rgba { return func(float64, float64) rgba { return c } }

func gradientPaint(g gradient, r rect) func(x, y float64) rgba {
	stops := g.stops
	at := func(t float64) rgba {
		t = clamp01(t)
		if t <= stops[0].pos {
			return stops[0].c
		}
		for i := 1; i < len(stops); i++ {
			if t <= stops[i].pos {
				span := stops[i].pos - stops[i-1].pos
				if span <= 0 {
					return stops[i].c
				}
				return stops[i-1].c.mix(stops[i].c, (t-stops[i-1].pos)/span)
			}
		}
		return stops[len(stops)-1].c
	}
	cx, cy := r.x+r.w/2, r.y+r.h/2
	if g.radial {
		rx, ry := math.Max(r.w/2*math.Sqrt2, 1), math.Max(r.h/2*math.Sqrt2, 1)
		return func(x, y float64) rgba { return at(math.Hypot((x-cx)/rx, (y-cy)/ry)) }
	}
	rad := g.angle * math.Pi / 180
	dx, dy := math.Sin(rad), -math.Cos(rad)
	length := math.Abs(r.w*dx) + math.Abs(r.h*dy)
	if length < 1 {
		length = 1
	}
	return func(x, y float64) rgba { return at(((x-cx)*dx+(y-cy)*dy)/length + 0.5) }
}

func (n *node) paintBox(img *image.RGBA, ox, oy float64, opacityMul float64) {
	st := n.st
	r := rect{ox + n.x, oy + n.y, n.w, n.h}
	// Shadows sit under the box.
	for _, sh := range st.shadows {
		sr := rect{r.x + sh.x - sh.spread, r.y + sh.y - sh.spread, r.w + 2*sh.spread, r.h + 2*sh.spread}
		blur := math.Max(sh.blur, 0.0001)
		x0, y0 := int(math.Floor(sr.x-blur))-1, int(math.Floor(sr.y-blur))-1
		x1, y1 := int(math.Ceil(sr.x+sr.w+blur))+1, int(math.Ceil(sr.y+sr.h+blur))+1
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				d := sr.sdf(float64(x)+0.5, float64(y)+0.5, st.radius)
				cov := clamp01(0.5 - d/blur)
				if sh.blur == 0 {
					cov = clamp01(0.5 - d)
				}
				if cov > 0 {
					over(img, x, y, sh.c, cov*cov*(3-2*cov))
				}
			}
		}
	}
	if st.bg.a > 0 {
		fill(img, r, st.radius, solid(st.bg))
	}
	for i := len(st.bgImages) - 1; i >= 0; i-- {
		fill(img, r, st.radius, gradientPaint(st.bgImages[i], r))
	}
	b := st.border
	if b[0] > 0 || b[1] > 0 || b[2] > 0 || b[3] > 0 {
		inner := rect{r.x + b[3], r.y + b[0], r.w - b[1] - b[3], r.h - b[0] - b[2]}
		var innerRad [4]float64
		for i := range innerRad {
			innerRad[i] = math.Max(0, st.radius[i]-math.Max(b[0], math.Max(b[1], math.Max(b[2], b[3]))))
		}
		x0, y0 := int(math.Floor(r.x))-1, int(math.Floor(r.y))-1
		x1, y1 := int(math.Ceil(r.x+r.w))+1, int(math.Ceil(r.y+r.h))+1
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				px, py := float64(x)+0.5, float64(y)+0.5
				outer := clamp01(0.5 - r.sdf(px, py, st.radius))
				if outer == 0 {
					continue
				}
				in := clamp01(0.5 - inner.sdf(px, py, innerRad))
				if inner.w <= 0 || inner.h <= 0 {
					in = 0
				}
				if cov := outer * (1 - in); cov > 0 {
					over(img, x, y, st.borderColor, cov)
				}
			}
		}
	}
	if n.tag == "img" && n.img != nil {
		n.paintImage(img, r)
	}
}

func (n *node) paintImage(img *image.RGBA, r rect) {
	src := n.img
	b := src.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	dst := r
	switch n.st.objectFit {
	case "contain":
		s := math.Min(r.w/iw, r.h/ih)
		dst = rect{r.x + (r.w-iw*s)/2, r.y + (r.h-ih*s)/2, iw * s, ih * s}
	case "cover":
		s := math.Max(r.w/iw, r.h/ih)
		dst = rect{r.x + (r.w-iw*s)/2, r.y + (r.h-ih*s)/2, iw * s, ih * s}
	}
	// Scale the image onto a scratch canvas, then composite it through the
	// rounded box so the corners and the box edge clip it.
	scratch := image.NewRGBA(img.Rect)
	target := image.Rect(int(math.Round(dst.x)), int(math.Round(dst.y)), int(math.Round(dst.x+dst.w)), int(math.Round(dst.y+dst.h)))
	if target.Empty() {
		return
	}
	xdraw.CatmullRom.Scale(scratch, target, src, b, xdraw.Over, nil)
	compositeThrough(img, scratch, r, n.st.radius, 1)
}

// compositeThrough draws src over dst inside the rounded rectangle r.
func compositeThrough(dst, src *image.RGBA, r rect, radii [4]float64, alpha float64) {
	x0, y0 := int(math.Floor(r.x))-1, int(math.Floor(r.y))-1
	x1, y1 := int(math.Ceil(r.x+r.w))+1, int(math.Ceil(r.y+r.h))+1
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !(image.Point{X: x, Y: y}.In(src.Rect)) {
				continue
			}
			cov := clamp01(0.5-r.sdf(float64(x)+0.5, float64(y)+0.5, radii)) * alpha
			if cov <= 0 {
				continue
			}
			i := src.PixOffset(x, y)
			a := float64(src.Pix[i+3]) / 255
			if a == 0 {
				continue
			}
			// src is premultiplied.
			c := rgba{float64(src.Pix[i]) / 255 / a, float64(src.Pix[i+1]) / 255 / a, float64(src.Pix[i+2]) / 255 / a, a}
			over(dst, x, y, c, cov)
		}
	}
}

// paintTree paints n and its subtree. (ox, oy) is the absolute origin of n's parent.
func (n *node) paintTree(img *image.RGBA, ox, oy float64, fonts *fontSet) {
	if n.isText() {
		n.paintText(img, ox, oy)
		return
	}
	group := n.st.opacity < 1 || n.st.overflow == "hidden"
	canvas := img
	if group {
		canvas = image.NewRGBA(img.Rect)
	}
	n.paintBox(canvas, ox, oy, 1)
	for _, c := range n.children {
		c.paintTree(canvas, ox+n.x, oy+n.y, fonts)
	}
	if group {
		r := rect{ox + n.x, oy + n.y, n.w, n.h}
		radii := [4]float64{}
		if n.st.overflow == "hidden" {
			radii = n.st.radius
		} else {
			// Opacity alone does not clip: widen to the canvas.
			r = rect{float64(img.Rect.Min.X), float64(img.Rect.Min.Y), float64(img.Rect.Dx()), float64(img.Rect.Dy())}
		}
		compositeThrough(img, canvas, r, radii, n.st.opacity)
	}
}

func (n *node) paintText(img *image.RGBA, ox, oy float64) {
	run := n.run
	if run == nil {
		return
	}
	x0, y0 := ox+n.x, oy+n.y
	for i, line := range run.lines {
		x := x0
		switch n.st.textAlign {
		case "center":
			x += (n.w - run.widths[i]) / 2
		case "right":
			x += n.w - run.widths[i]
		}
		drawString(drawTarget{img}, run.face, line, x, y0+float64(i)*run.lineH+run.ascent, n.st.color, n.st.letterSpacing)
	}
}

var _ = color.NRGBA{}
