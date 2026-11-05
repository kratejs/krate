package ogimage

import (
	"image/color"
	"math"
	"strconv"
	"strings"
)

// rgba is a non-premultiplied colour with float components in 0..1.
type rgba struct{ r, g, b, a float64 }

var namedColors = map[string]rgba{
	"transparent": {0, 0, 0, 0},
	"black":       {0, 0, 0, 1}, "white": {1, 1, 1, 1}, "red": {1, 0, 0, 1}, "green": {0, 128.0 / 255, 0, 1},
	"blue": {0, 0, 1, 1}, "yellow": {1, 1, 0, 1}, "orange": {1, 165.0 / 255, 0, 1}, "purple": {128.0 / 255, 0, 128.0 / 255, 1},
	"pink": {1, 192.0 / 255, 203.0 / 255, 1}, "gray": {128.0 / 255, 128.0 / 255, 128.0 / 255, 1}, "grey": {128.0 / 255, 128.0 / 255, 128.0 / 255, 1},
	"cyan": {0, 1, 1, 1}, "magenta": {1, 0, 1, 1}, "lime": {0, 1, 0, 1}, "navy": {0, 0, 128.0 / 255, 1}, "teal": {0, 128.0 / 255, 128.0 / 255, 1},
	"silver": {192.0 / 255, 192.0 / 255, 192.0 / 255, 1}, "maroon": {128.0 / 255, 0, 0, 1}, "olive": {128.0 / 255, 128.0 / 255, 0, 1},
	"gold": {1, 215.0 / 255, 0, 1}, "indigo": {75.0 / 255, 0, 130.0 / 255, 1}, "violet": {238.0 / 255, 130.0 / 255, 238.0 / 255, 1},
	"coral": {1, 127.0 / 255, 80.0 / 255, 1}, "salmon": {250.0 / 255, 128.0 / 255, 114.0 / 255, 1}, "tomato": {1, 99.0 / 255, 71.0 / 255, 1},
	"crimson": {220.0 / 255, 20.0 / 255, 60.0 / 255, 1}, "turquoise": {64.0 / 255, 224.0 / 255, 208.0 / 255, 1}, "tan": {210.0 / 255, 180.0 / 255, 140.0 / 255, 1},
	"beige": {245.0 / 255, 245.0 / 255, 220.0 / 255, 1}, "ivory": {1, 1, 240.0 / 255, 1}, "lavender": {230.0 / 255, 230.0 / 255, 250.0 / 255, 1},
	"brown": {165.0 / 255, 42.0 / 255, 42.0 / 255, 1}, "skyblue": {135.0 / 255, 206.0 / 255, 235.0 / 255, 1},
}

// parseColor reads #rgb, #rgba, #rrggbb, #rrggbbaa, rgb(), rgba(), hsl(), hsla()
// and a short list of named colours.
func parseColor(s string) (rgba, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		switch len(h) {
		case 3, 4:
			var b strings.Builder
			for _, ch := range h {
				b.WriteRune(ch)
				b.WriteRune(ch)
			}
			h = b.String()
		case 6, 8:
		default:
			return rgba{}, false
		}
		v, err := strconv.ParseUint(h, 16, 64)
		if err != nil {
			return rgba{}, false
		}
		if len(h) == 6 {
			return rgba{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255, 1}, true
		}
		return rgba{float64(v>>24&255) / 255, float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255}, true
	}
	open := strings.IndexByte(s, '(')
	if open > 0 && strings.HasSuffix(s, ")") {
		fn := s[:open]
		parts := splitArgs(s[open+1 : len(s)-1])
		nums := make([]float64, 0, 4)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			pct := strings.HasSuffix(p, "%")
			p = strings.TrimSuffix(strings.TrimSuffix(p, "%"), "deg")
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return rgba{}, false
			}
			if pct {
				f /= 100
				if fn == "rgb" || fn == "rgba" {
					f *= 255
				}
			}
			nums = append(nums, f)
		}
		switch fn {
		case "rgb", "rgba":
			if len(nums) < 3 {
				return rgba{}, false
			}
			a := 1.0
			if len(nums) > 3 {
				a = nums[3]
			}
			return rgba{nums[0] / 255, nums[1] / 255, nums[2] / 255, a}, true
		case "hsl", "hsla":
			if len(nums) < 3 {
				return rgba{}, false
			}
			a := 1.0
			if len(nums) > 3 {
				a = nums[3]
			}
			r, g, b := hslToRGB(nums[0], nums[1], nums[2])
			return rgba{r, g, b, a}, true
		}
	}
	return rgba{}, false
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

// splitArgs splits on top-level commas (not inside parentheses).
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

func (c rgba) mix(o rgba, t float64) rgba {
	return rgba{c.r + (o.r-c.r)*t, c.g + (o.g-c.g)*t, c.b + (o.b-c.b)*t, c.a + (o.a-c.a)*t}
}

func (c rgba) std() color.NRGBA {
	q := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	return color.NRGBA{q(c.r), q(c.g), q(c.b), q(c.a)}
}
