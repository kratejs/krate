package caps

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"unicode/utf8"
)

// EncodePNGFromBGRA encodes a 32-bit image whose pixels are stored as the
// bytes B, G, R, A (a little-endian ARGB32 word, the layout cairo calls
// ARGB32 and RGB24). premultiplied says whether the colors are premultiplied
// by alpha (cairo ARGB32 is); opaque ignores the alpha byte (cairo RGB24
// leaves it undefined). stride is the byte length of a row.
func EncodePNGFromBGRA(width, height, stride int, data []byte, premultiplied, opaque bool) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, errors.New("krate: the image is empty")
	}
	if stride < width*4 || len(data) < stride*(height-1)+width*4 {
		return nil, errors.New("krate: the image data is shorter than its size")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		src := data[y*stride : y*stride+width*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < width; x++ {
			b, g, r, a := src[x*4], src[x*4+1], src[x*4+2], src[x*4+3]
			switch {
			case opaque:
				a = 255
			case premultiplied && a != 0 && a != 255:
				r = uint8((uint32(r)*255 + uint32(a)/2) / uint32(a))
				g = uint8((uint32(g)*255 + uint32(a)/2) / uint32(a))
				b = uint8((uint32(b)*255 + uint32(a)/2) / uint32(a))
			}
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = r, g, b, a
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UniqueName returns name, or "name (1).ext", "name (2).ext", ... for the
// first variant that exists reports false for. Path separators in name are
// replaced, so the result is a plain file name; an empty name becomes
// "download".
func UniqueName(name string, exists func(candidate string) bool) string {
	name = SafeFileName(name)
	if !exists(name) {
		return name
	}
	ext := ""
	base := name
	for i := len(name) - 1; i > 0; i-- {
		if name[i] == '.' {
			base, ext = name[:i], name[i:]
			break
		}
	}
	for n := 1; ; n++ {
		c := base + " (" + itoa(n) + ")" + ext
		if !exists(c) {
			return c
		}
	}
}

// SafeFileName makes a server-proposed file name safe to join to a folder:
// no directory part, no control characters, never "." or "..", at most 200
// bytes.
func SafeFileName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == 0 || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			out = append(out, '_')
		case r < 0x20 || r == 0x7f:
		default:
			out = append(out, r)
		}
	}
	s := string(out)
	for len(s) > 0 && (s[0] == '.' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '.' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	if len(s) > 200 {
		s = s[:200]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1] // do not cut a UTF-8 sequence
		}
	}
	if s == "" {
		return "download"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
