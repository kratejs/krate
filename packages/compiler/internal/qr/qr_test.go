package qr

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReedSolomonKnownVector(t *testing.T) {
	// "HELLO WORLD", version 1, as in the standard's worked example.
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	if got := reedSolomon(data, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("reedSolomon = %v, want %v", got, want)
	}
}

func TestEncodeSizesAndPatterns(t *testing.T) {
	cases := map[string]int{
		"a":                                  21,
		"http://localhost:3000":              25,
		"http://192.168.100.200:3000/docs/x": 29,
		strings.Repeat("x", 100):             37,
	}
	for text, wantSize := range cases {
		m, err := Encode(text)
		if err != nil {
			t.Fatalf("Encode(%.20q): %v", text, err)
		}
		if len(m) != wantSize {
			t.Errorf("Encode(%.20q) size = %d, want %d", text, len(m), wantSize)
		}
		for _, o := range [][2]int{{0, 0}, {len(m) - 7, 0}, {0, len(m) - 7}} {
			if !finderAt(m, o[0], o[1]) {
				t.Errorf("Encode(%.20q): finder pattern missing at %v", text, o)
			}
		}
		if !m[len(m)-8][8] {
			t.Errorf("Encode(%.20q): dark module missing", text)
		}
	}
}

func finderAt(m Matrix, x0, y0 int) bool {
	for y := 0; y < 7; y++ {
		for x := 0; x < 7; x++ {
			ring := max(abs(x-3), abs(y-3))
			want := ring != 2
			if m[y0+y][x0+x] != want {
				return false
			}
		}
	}
	return true
}

func TestEncodeFormatInfoIsAValidCodeword(t *testing.T) {
	m, err := Encode("http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	// Read the first copy of the format bits and check the BCH remainder.
	var bits int
	get := func(i int, x, y int) {
		if m[y][x] {
			bits |= 1 << uint(i)
		}
	}
	for i := 0; i <= 5; i++ {
		get(i, 8, i)
	}
	get(6, 8, 7)
	get(7, 8, 8)
	get(8, 7, 8)
	for i := 9; i < 15; i++ {
		get(i, 14-i, 8)
	}
	bits ^= 0x5412
	if bits>>13 != 1 {
		t.Fatalf("format bits %015b do not say error correction level L", bits)
	}
	rem := bits >> 10
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	if rem != bits&0x3FF {
		t.Errorf("format BCH remainder %x does not match %x", rem, bits&0x3FF)
	}
}

func TestEncodeTooLong(t *testing.T) {
	if _, err := Encode(strings.Repeat("x", 272)); err != ErrTooLong {
		t.Errorf("err = %v, want ErrTooLong", err)
	}
	if _, err := Encode(strings.Repeat("x", 271)); err != nil {
		t.Errorf("271 bytes must fit: %v", err)
	}
}

func TestLinesShape(t *testing.T) {
	m, err := Encode("hi")
	if err != nil {
		t.Fatal(err)
	}
	lines := m.Lines()
	want := (len(m) + 8 + 1) / 2
	if len(lines) != want {
		t.Fatalf("%d lines, want %d", len(lines), want)
	}
	for _, l := range lines {
		if n := len([]rune(l)); n != len(m)+8 {
			t.Fatalf("line has %d cells, want %d", n, len(m)+8)
		}
	}
	if got := m.ANSILines()[0]; !strings.HasPrefix(got, "\x1b[30;47m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("ANSILines does not set black on white: %q", got)
	}
}

// TestWritePNGs writes the symbols to $KRATE_QR_PNG_DIR so a scanner can check
// them. It is skipped unless the variable is set.
func TestWritePNGs(t *testing.T) {
	dir := os.Getenv("KRATE_QR_PNG_DIR")
	if dir == "" {
		t.Skip("KRATE_QR_PNG_DIR not set")
	}
	texts := map[string]string{
		"short": "http://localhost:3000",
		"lan":   "http://192.168.1.42:3000",
		"long":  "http://192.168.100.200:3000/docs/getting-started/?utm=" + strings.Repeat("abc", 30),
		"v7":    strings.Repeat("0123456789", 12),
	}
	for name, text := range texts {
		m, err := Encode(text)
		if err != nil {
			t.Fatal(err)
		}
		const scale, quiet = 8, 4
		size := (len(m) + 2*quiet) * scale
		img := image.NewGray(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				mx, my := x/scale-quiet, y/scale-quiet
				c := color.Gray{Y: 255}
				if mx >= 0 && my >= 0 && mx < len(m) && my < len(m) && m[my][mx] {
					c = color.Gray{Y: 0}
				}
				img.SetGray(x, y, c)
			}
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
