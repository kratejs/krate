package pkg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/jpeg" // icon sources may be JPEG
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

// Icon is the application icon at several sizes, derived from one source
// image.
type Icon struct {
	src image.Image
	png map[int][]byte
	// pyramid caches the halved copies of src by width.
	pyramid map[int]image.Image
	// Generated reports that no icon was configured and a placeholder was
	// made.
	Generated bool
	// Small reports a source smaller than 512 pixels, which gives soft large
	// icons.
	Small bool
}

// LoadIcon reads the source icon. A missing path gives a generated
// placeholder derived from seed.
func LoadIcon(p, seed string) (*Icon, error) {
	if p == "" {
		return &Icon{src: placeholderIcon(seed), png: map[int][]byte{}, Generated: true}, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s is not a PNG or JPEG image: %w", p, err)
	}
	b := img.Bounds()
	if b.Dx() < 16 || b.Dy() < 16 {
		return nil, fmt.Errorf("%s is %dx%d; an icon needs at least 256x256 (1024x1024 is best)", p, b.Dx(), b.Dy())
	}
	ic := &Icon{src: squared(img), png: map[int][]byte{}}
	ic.Small = max(b.Dx(), b.Dy()) < 512
	return ic, nil
}

// squared pads a non-square image with transparency to a square.
func squared(img image.Image) image.Image {
	b := img.Bounds()
	if b.Dx() == b.Dy() {
		return img
	}
	n := max(b.Dx(), b.Dy())
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	off := image.Pt((n-b.Dx())/2, (n-b.Dy())/2)
	draw.Draw(dst, image.Rectangle{Min: off, Max: off.Add(b.Size())}, img, b.Min, draw.Src)
	return dst
}

// placeholderIcon is a rounded square in a color taken from the seed.
func placeholderIcon(seed string) image.Image {
	h := fnv.New32a()
	h.Write([]byte(seed))
	v := h.Sum32()
	base := color.NRGBA{R: uint8(60 + v&0x7f), G: uint8(60 + (v>>8)&0x7f), B: uint8(100 + (v>>16)&0x7f), A: 255}
	const n = 1024
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	const r = 200.0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			cx, cy := float64(x)+0.5, float64(y)+0.5
			dx, dy := 0.0, 0.0
			if cx < r {
				dx = r - cx
			} else if cx > n-r {
				dx = cx - (n - r)
			}
			if cy < r {
				dy = r - cy
			} else if cy > n-r {
				dy = cy - (n - r)
			}
			if dx*dx+dy*dy > r*r {
				continue
			}
			t := uint8(float64(y) / n * 60)
			img.SetNRGBA(x, y, color.NRGBA{R: sat(base.R, t), G: sat(base.G, t), B: sat(base.B, t), A: 255})
		}
	}
	return img
}

func sat(c, t uint8) uint8 {
	if int(c)+int(t) > 255 {
		return 255
	}
	return c + t
}

// PNG returns the icon scaled to size x size and PNG encoded.
func (ic *Icon) PNG(size int) ([]byte, error) {
	if b, ok := ic.png[size]; ok {
		return b, nil
	}
	src := ic.source(size)
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, err
	}
	ic.png[size] = buf.Bytes()
	return ic.png[size], nil
}

// source returns the smallest image of a halving pyramid that is at least
// size pixels wide, so a 16 pixel icon is not scaled from 1024 pixels in
// one step.
func (ic *Icon) source(size int) image.Image {
	cur := ic.src
	for {
		w := cur.Bounds().Dx()
		half := w / 2
		if half < size || half < 16 {
			return cur
		}
		if next, ok := ic.pyramid[half]; ok {
			cur = next
			continue
		}
		dst := image.NewNRGBA(image.Rect(0, 0, half, half))
		draw.CatmullRom.Scale(dst, dst.Bounds(), cur, cur.Bounds(), draw.Src, nil)
		if ic.pyramid == nil {
			ic.pyramid = map[int]image.Image{}
		}
		ic.pyramid[half] = dst
		cur = dst
	}
}

// icnsTypes lists the PNG-capable ICNS element types and their pixel sizes.
var icnsTypes = []struct {
	Type string
	Size int
}{
	{"icp4", 16}, {"icp5", 32}, {"icp6", 64},
	{"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
	{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512},
}

// ICNS builds an Apple icon file: an "icns" container of PNG-encoded
// elements (ic07 to ic10 for 128 to 1024 pixels, plus the retina and
// small variants).
func (ic *Icon) ICNS() ([]byte, error) {
	var body bytes.Buffer
	for _, t := range icnsTypes {
		data, err := ic.PNG(t.Size)
		if err != nil {
			return nil, err
		}
		body.WriteString(t.Type)
		putBin(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	putBin(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// icoSizes are the sizes of the Windows icon, smallest first.
var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

// icoImages returns the PNG images of the Windows icon.
func (ic *Icon) icoImages() ([]int, [][]byte, error) {
	var data [][]byte
	for _, s := range icoSizes {
		b, err := ic.PNG(s)
		if err != nil {
			return nil, nil, err
		}
		data = append(data, b)
	}
	return icoSizes, data, nil
}

// ICO builds a Windows icon file whose entries are PNG images from 16 to 256
// pixels.
func (ic *Icon) ICO() ([]byte, error) {
	sizes, imgs, err := ic.icoImages()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	putBin(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s >= 256 {
			dim = 0
		}
		out.Write([]byte{dim, dim, 0, 0})
		putBin(&out, binary.LittleEndian, uint16(1))  // planes
		putBin(&out, binary.LittleEndian, uint16(32)) // bits per pixel
		putBin(&out, binary.LittleEndian, uint32(len(imgs[i])))
		putBin(&out, binary.LittleEndian, uint32(offset))
		offset += len(imgs[i])
	}
	for _, b := range imgs {
		out.Write(b)
	}
	return out.Bytes(), nil
}

// hicolorSizes are the PNG sizes installed on Linux.
var hicolorSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

// putBin appends v in the given byte order. Writing fixed-size values to a
// bytes.Buffer cannot fail, so an error means a programming mistake.
func putBin(b *bytes.Buffer, order binary.ByteOrder, v any) {
	if err := binary.Write(b, order, v); err != nil {
		panic("pkg: binary write: " + err.Error())
	}
}
