package ogimage

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"  // register decoders for <img>
	_ "image/jpeg" // register decoders for <img>
	"image/png"
	"net/url"
	"strings"

	_ "golang.org/x/image/webp" // register decoders for <img>
)

// MaxDimension bounds the canvas so a mistake cannot ask for a gigabyte image.
const MaxDimension = 4096

// Render draws the spec to a PNG. It returns an *Error that lists every
// unsupported feature the tree uses instead of drawing it wrong.
func Render(spec *Spec) ([]byte, error) {
	if spec == nil || spec.Tree == nil {
		return nil, fmt.Errorf("ImageResponse needs an element to draw")
	}
	if spec.Width <= 0 || spec.Height <= 0 || spec.Width > MaxDimension || spec.Height > MaxDimension {
		return nil, fmt.Errorf("image size %dx%d is outside 1 to %d pixels", spec.Width, spec.Height, MaxDimension)
	}
	is := &issues{}
	fonts := newFontSet(spec.Fonts, is)
	root := defaultStyle()
	b := &builder{is: is}
	tree := b.build(spec.Tree, root, "root")
	if tree == nil {
		return nil, fmt.Errorf("ImageResponse needs an element to draw")
	}
	loadImages(tree, is)
	if err := is.err(); err != nil {
		return nil, err
	}
	// The root fills the canvas.
	tree.x, tree.y, tree.w, tree.h = 0, 0, float64(spec.Width), float64(spec.Height)
	if tree.isText() {
		is.add("root: the top-level value must be an element, not text")
		return nil, is.err()
	}
	l := &layouter{fonts: fonts, memo: map[measureKey][2]float64{}}
	l.layoutNode(tree)
	if err := is.err(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, spec.Width, spec.Height))
	// An opaque white page behind transparent areas, like a browser screenshot.
	draw.Draw(img, img.Rect, image.White, image.Point{}, draw.Src)
	tree.paintTree(img, 0, 0, fonts)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// loadImages decodes every <img> (data: URIs; the caller inlines files and
// remote images before rendering).
func loadImages(n *node, is *issues) {
	if n.tag == "img" {
		img, err := decodeSrc(n.src)
		if err != nil {
			is.add("%s: <img src>: %v", n.path, err)
		} else {
			n.img = img
		}
		if n.st.width.auto() && n.st.height.auto() && img == nil {
			is.add("%s: <img> needs a width and height", n.path)
		}
	}
	for _, c := range n.children {
		loadImages(c, is)
	}
}

func decodeSrc(src string) (image.Image, error) {
	if !strings.HasPrefix(src, "data:") {
		return nil, fmt.Errorf("%q is not a data: URI; ImageResponse inlines local files and remote URLs before rendering", truncate(src, 40))
	}
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, fmt.Errorf("malformed data: URI")
	}
	meta, payload := src[5:comma], src[comma+1:]
	var raw []byte
	if strings.HasSuffix(meta, ";base64") {
		var err error
		if raw, err = base64.StdEncoding.DecodeString(payload); err != nil {
			return nil, fmt.Errorf("unreadable base64 image: %v", err)
		}
	} else {
		s, err := url.PathUnescape(payload)
		if err != nil {
			return nil, fmt.Errorf("unreadable image data: %v", err)
		}
		raw = []byte(s)
	}
	if strings.Contains(meta, "svg") {
		return nil, fmt.Errorf("SVG images are not supported; use PNG, JPEG, GIF or WebP")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("cannot decode the image (PNG, JPEG, GIF and WebP are supported): %v", err)
	}
	return img, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
