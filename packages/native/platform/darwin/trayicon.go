//go:build darwin

package darwin

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// Pure parts of the status item icon handling.

const (
	// trayHeightPt is the height in points an icon is scaled down to. The
	// menu bar is 22 pt high on most Macs; 18 pt leaves a margin.
	trayHeightPt = 18.0
	// trayMaxEdgePx bounds the decoded PNG so a huge image cannot exhaust
	// memory while the template check scans it.
	trayMaxEdgePx = 1024
)

// trayIconInfo is what the AppKit side needs to know about a PNG.
type trayIconInfo struct {
	// W and H are the size in points to give the NSImage.
	W, H float64
	// Template is true for a single-tone image: AppKit then draws only its
	// alpha channel in the menu bar color, which adapts to light, dark and
	// highlighted states.
	Template bool
}

// inspectTrayIcon validates PNG bytes and works out the point size and the
// template flag.
func inspectTrayIcon(data []byte) (trayIconInfo, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return trayIconInfo{}, fmt.Errorf("darwin: tray icon is not a valid PNG: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > trayMaxEdgePx || cfg.Height > trayMaxEdgePx {
		return trayIconInfo{}, fmt.Errorf("darwin: tray icon is %dx%d pixels; use at most %d on a side", cfg.Width, cfg.Height, trayMaxEdgePx)
	}
	w, h := trayPointSize(cfg.Width, cfg.Height)
	info := trayIconInfo{W: w, H: h}
	if img, err := png.Decode(bytes.NewReader(data)); err == nil {
		info.Template = isSingleTone(img)
	}
	return info, nil
}

// trayPointSize returns the size in points: the pixel size, or scaled down to
// trayHeightPt high (keeping the aspect ratio) when taller than that.
func trayPointSize(w, h int) (float64, float64) {
	fw, fh := float64(w), float64(h)
	if fh <= trayHeightPt {
		return fw, fh
	}
	return fw * trayHeightPt / fh, trayHeightPt
}

// isSingleTone reports whether every visible pixel has (nearly) the same gray
// color, for example black or white artwork on a transparent background with
// anti-aliased edges. An image with no visible pixel is not a template.
func isSingleTone(img image.Image) bool {
	const grayTol, toneTol = 12, 24
	b := img.Bounds()
	var ref [3]int
	have := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			// Un-premultiply to 8 bits.
			c := [3]int{int(r * 255 / a), int(g * 255 / a), int(bl * 255 / a)}
			if absInt(c[0]-c[1]) > grayTol || absInt(c[1]-c[2]) > grayTol || absInt(c[0]-c[2]) > grayTol {
				return false
			}
			if !have {
				ref, have = c, true
				continue
			}
			for i := range c {
				if absInt(c[i]-ref[i]) > toneTol {
					return false
				}
			}
		}
	}
	return have
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
