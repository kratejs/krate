package scene3d

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Color handling mirrors packages/3d/src/runtime/color.ts so a static color
// and a reactive one (converted in the browser) agree.

var (
	hex3Re = regexp.MustCompile(`^#([0-9a-f]{3})$`)
	hex6Re = regexp.MustCompile(`^#([0-9a-f]{6})$`)
	rgbRe  = regexp.MustCompile(`^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)`)
)

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func unpackRGB(n uint32) [3]int { return [3]int{int(n>>16) & 255, int(n>>8) & 255, int(n) & 255} }

// ParseColor reads a CSS color string (#rgb, #rrggbb, rgb(), a name) to sRGB
// bytes. Anything unrecognised is white, like the runtime.
func ParseColor(s string) [3]int {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, ok := namedColors[s]; ok {
		return unpackRGB(n)
	}
	if m := hex3Re.FindStringSubmatch(s); m != nil {
		var out [3]int
		for i := 0; i < 3; i++ {
			v, _ := strconv.ParseUint(strings.Repeat(string(m[1][i]), 2), 16, 32)
			out[i] = int(v)
		}
		return out
	}
	if m := hex6Re.FindStringSubmatch(s); m != nil {
		v, _ := strconv.ParseUint(m[1], 16, 32)
		return unpackRGB(uint32(v))
	}
	if m := rgbRe.FindStringSubmatch(s); m != nil {
		var out [3]int
		for i := 0; i < 3; i++ {
			out[i], _ = strconv.Atoi(m[i+1])
		}
		return out
	}
	return [3]int{255, 255, 255}
}

func linearOf(rgb [3]int) [3]float64 {
	return [3]float64{srgbToLinear(float64(rgb[0]) / 255), srgbToLinear(float64(rgb[1]) / 255), srgbToLinear(float64(rgb[2]) / 255)}
}

// ColorToLinear converts a CSS color string to a linear RGB triple in 0..1.
func ColorToLinear(s string) [3]float64 { return linearOf(ParseColor(s)) }

// NumberColorToLinear converts a 0xRRGGBB number to a linear RGB triple.
func NumberColorToLinear(n float64) [3]float64 {
	return linearOf(unpackRGB(uint32(int64(n))))
}
