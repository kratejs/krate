package pkg

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestIcon(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	p := filepath.Join(t.TempDir(), "icon.png")
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func TestICNSStructure(t *testing.T) {
	ic, err := LoadIcon(writeTestIcon(t, 1024, 1024), "x")
	if err != nil {
		t.Fatal(err)
	}
	if ic.Small || ic.Generated {
		t.Fatal("flags")
	}
	b, err := ic.ICNS()
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:4]) != "icns" || int(binary.BigEndian.Uint32(b[4:8])) != len(b) {
		t.Fatalf("bad header/length")
	}
	seen := map[string]int{}
	for off := 8; off < len(b); {
		typ := string(b[off : off+4])
		n := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		img, err := png.Decode(bytes.NewReader(b[off+8 : off+n]))
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		seen[typ] = img.Bounds().Dx()
		off += n
	}
	for typ, want := range map[string]int{"ic07": 128, "ic08": 256, "ic09": 512, "ic10": 1024, "ic11": 32, "ic12": 64, "icp4": 16} {
		if seen[typ] != want {
			t.Errorf("%s is %d px, want %d", typ, seen[typ], want)
		}
	}
}

func TestICOStructure(t *testing.T) {
	ic, err := LoadIcon(writeTestIcon(t, 600, 400), "x") // non-square gets padded
	if err != nil {
		t.Fatal(err)
	}
	b, err := ic.ICO()
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(b[0:]) != 0 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		t.Fatal("not an icon")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	if n != len(icoSizes) {
		t.Fatalf("%d entries", n)
	}
	for i := 0; i < n; i++ {
		e := b[6+16*i:]
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		img, err := png.Decode(bytes.NewReader(b[off : off+size]))
		if err != nil {
			t.Fatal(err)
		}
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		if img.Bounds().Dx() != w || w != icoSizes[i] {
			t.Errorf("entry %d: %d px, header says %d", i, img.Bounds().Dx(), w)
		}
	}
}

func TestPlaceholderAndErrors(t *testing.T) {
	ic, err := LoadIcon("", "com.acme.app")
	if err != nil || !ic.Generated {
		t.Fatalf("%v", err)
	}
	if _, err := ic.PNG(64); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIcon(filepath.Join(t.TempDir(), "nope.png"), ""); err == nil {
		t.Fatal("missing file accepted")
	}
	junk := filepath.Join(t.TempDir(), "j.png")
	os.WriteFile(junk, []byte("not an image"), 0o644)
	if _, err := LoadIcon(junk, ""); err == nil {
		t.Fatal("junk accepted")
	}
	small, _ := LoadIcon(writeTestIcon(t, 128, 128), "")
	if !small.Small {
		t.Fatal("small flag")
	}
}
