package offscreen

import (
	"bytes"
	"image"
	"image/png"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Frame formats.
const (
	FormatPNG  = "png"
	FormatBGRA = "bgra"
)

// Rate limits for a frame stream.
const (
	DefaultFPS = 10
	MaxFPS     = 60
)

// ClampFPS applies the default and the limit to a requested frame rate.
func ClampFPS(fps float64) float64 {
	switch {
	case fps <= 0:
		return DefaultFPS
	case fps > MaxFPS:
		return MaxFPS
	}
	return fps
}

// Interval is the time between frames at fps.
func Interval(fps float64) time.Duration {
	return time.Duration(float64(time.Second) / ClampFPS(fps))
}

// Missed is how many frames a stream skipped while one frame took busy to
// capture, encode and deliver: the next frame starts at once, so a frame that
// took 2.5 intervals cost one skipped frame.
func Missed(busy, interval time.Duration) int {
	if interval <= 0 || busy < 2*interval {
		return 0
	}
	return int(busy/interval) - 1
}

// RawBGRA repacks a snapshot to width*4 bytes per row (no padding),
// premultiplied BGRA.
func RawBGRA(s caps.Snapshot) []byte {
	row := s.Width * 4
	if s.Stride == row {
		return s.Pix[:row*s.Height]
	}
	out := make([]byte, row*s.Height)
	for y := 0; y < s.Height; y++ {
		copy(out[y*row:(y+1)*row], s.Pix[y*s.Stride:y*s.Stride+row])
	}
	return out
}

// EncodePNG encodes a snapshot as PNG. The pixels are premultiplied BGRA;
// they are converted to straight alpha, which PNG expects. Fully opaque
// frames (the usual case) skip the division.
func EncodePNG(s caps.Snapshot) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, s.Width, s.Height))
	for y := 0; y < s.Height; y++ {
		src := s.Pix[y*s.Stride : y*s.Stride+s.Width*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+s.Width*4]
		for x := 0; x < len(src); x += 4 {
			b, g, r, a := src[x], src[x+1], src[x+2], src[x+3]
			switch a {
			case 255:
			case 0:
				b, g, r = 0, 0, 0
			default:
				b = byte((uint32(b)*255 + uint32(a)/2) / uint32(a))
				g = byte((uint32(g)*255 + uint32(a)/2) / uint32(a))
				r = byte((uint32(r)*255 + uint32(a)/2) / uint32(a))
			}
			dst[x], dst[x+1], dst[x+2], dst[x+3] = r, g, b, a
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
