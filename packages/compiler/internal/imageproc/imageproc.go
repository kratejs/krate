package imageproc

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	webpenc "github.com/skrashevich/go-webp"
	"golang.org/x/image/webp"
)

// outputPrefix is the URL prefix for processed image variants in the build
// output. Files are written to <outDir>/_krate/images/ and served at
// /_krate/images/... (leading underscore avoids dotfile handling on static hosts).
const outputPrefix = "/_krate/images"

// SrcsetEntry is a single responsive image variant: a URL path + its display width.
type SrcsetEntry struct {
	Width    int
	FilePath string
}

// ImageResult describes the processed image output for a single <Image>.
type ImageResult struct {
	// WebP holds the WebP variants (preferred format).
	WebP []SrcsetEntry
	// Fallback holds the original-format variants (JPEG, or PNG when the
	// source has transparency) used by browsers without WebP support.
	Fallback []SrcsetEntry
	// Src is the original source URL used as the <img> fallback (browsers
	// without <picture> support). Set by the caller.
	Src string
	// Width and Height are the display dimensions used for CLS mitigation.
	Width  int
	Height int
	// AspectRatio is the intrinsic ratio (width/height).
	AspectRatio float64
	// FallbackMime is the MIME type of the fallback variants.
	FallbackMime string
	// Placeholder is a base64 data-URI low-quality image placeholder ("" if disabled).
	Placeholder string
}

func getCacheDir(root string) string {
	// Computed per call — the cache must be scoped to the current project root.
	return filepath.Join(root, ".krate", "cache", "images")
}

func ComputeCacheKey(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

func DecodeImage(r *os.File) (image.Image, string, error) {
	ext := strings.ToLower(filepath.Ext(r.Name()))
	switch ext {
	case ".jpg", ".jpeg":
		img, err := jpeg.Decode(r)
		return img, "jpeg", err
	case ".png":
		img, err := png.Decode(r)
		return img, "png", err
	case ".webp":
		img, err := webp.Decode(r)
		return img, "webp", err
	case ".gif":
		img, _, err := image.Decode(r)
		return img, "gif", err
	default:
		img, _, err := image.Decode(r)
		return img, "jpeg", err
	}
}

// EncodeWebP encodes img as a lossy WebP image at the given quality (0-100).
// Uses a pure-Go VP8 encoder — no cgo, no libwebp dependency.
func EncodeWebP(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := webpenc.Encode(&buf, img, &webpenc.Options{Lossy: true, Quality: float32(quality)}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes(), err
}

func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

// hasAlpha samples the image for transparent pixels. A stride of 8 keeps this
// fast for large photos; alpha channels are typically region-uniform.
func hasAlpha(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 8 {
		for x := b.Min.X; x < b.Max.X; x += 8 {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xffff {
				return true
			}
		}
	}
	return false
}

func ResizeBilinear(img image.Image, targetW, targetH int) image.Image {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if targetW == srcW && targetH == srcH {
		dst := image.NewRGBA(bounds)
		draw.Draw(dst, bounds, img, bounds.Min, draw.Src)
		return dst
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))

	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			srcXf := float64(x) * float64(srcW-1) / float64(targetW-1)
			srcYf := float64(y) * float64(srcH-1) / float64(targetH-1)

			x0 := int(srcXf)
			y0 := int(srcYf)
			x1 := x0 + 1
			y1 := y0 + 1

			if x1 >= srcW {
				x1 = srcW - 1
			}
			if y1 >= srcH {
				y1 = srcH - 1
			}

			fx := srcXf - float64(x0)
			fy := srcYf - float64(y0)

			c00 := img.At(bounds.Min.X+x0, bounds.Min.Y+y0)
			c10 := img.At(bounds.Min.X+x1, bounds.Min.Y+y0)
			c01 := img.At(bounds.Min.X+x0, bounds.Min.Y+y1)
			c11 := img.At(bounds.Min.X+x1, bounds.Min.Y+y1)

			r, g, b, a := bilinearBlend(c00, c10, c01, c11, fx, fy)
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}
	return dst
}

func bilinearBlend(c00, c10, c01, c11 color.Color, fx, fy float64) (uint8, uint8, uint8, uint8) {
	r00, g00, b00, a00 := c00.RGBA()
	r10, g10, b10, a10 := c10.RGBA()
	r01, g01, b01, a01 := c01.RGBA()
	r11, g11, b11, a11 := c11.RGBA()

	topR := lerp(int(r00), int(r10), fx)
	topG := lerp(int(g00), int(g10), fx)
	topB := lerp(int(b00), int(b10), fx)
	topA := lerp(int(a00), int(a10), fx)

	botR := lerp(int(r01), int(r11), fx)
	botG := lerp(int(g01), int(g11), fx)
	botB := lerp(int(b01), int(b11), fx)
	botA := lerp(int(a01), int(a11), fx)

	return uint8(lerp(topR, botR, fy) >> 8),
		uint8(lerp(topG, botG, fy) >> 8),
		uint8(lerp(topB, botB, fy) >> 8),
		uint8(lerp(topA, botA, fy) >> 8)
}

func lerp(a, b int, t float64) int {
	return int(float64(a)*(1-t) + float64(b)*t)
}

// boxBlur applies a separable box blur to an RGBA image.
func boxBlur(img *image.RGBA, radius int) *image.RGBA {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if w <= 0 || h <= 0 || radius <= 0 {
		return img
	}
	tmp := image.NewRGBA(image.Rect(0, 0, w, h))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			rs, gs, bs, as, n := 0, 0, 0, 0, 0
			for dy := -radius; dy <= radius; dy++ {
				yy := y + dy
				if yy < 0 || yy >= h {
					continue
				}
				c := img.RGBAAt(x, yy)
				rs += int(c.R)
				gs += int(c.G)
				bs += int(c.B)
				as += int(c.A)
				n++
			}
			tmp.SetRGBA(x, y, color.RGBA{R: uint8(rs / n), G: uint8(gs / n), B: uint8(bs / n), A: uint8(as / n)})
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rs, gs, bs, as, n := 0, 0, 0, 0, 0
			for dx := -radius; dx <= radius; dx++ {
				xx := x + dx
				if xx < 0 || xx >= w {
					continue
				}
				c := tmp.RGBAAt(xx, y)
				rs += int(c.R)
				gs += int(c.G)
				bs += int(c.B)
				as += int(c.A)
				n++
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(rs / n), G: uint8(gs / n), B: uint8(bs / n), A: uint8(as / n)})
		}
	}
	return dst
}

// GeneratePlaceholder produces a tiny, blurred, base64-encoded placeholder
// image used as a low-quality-image placeholder (LQIP) until the real image
// loads. PNG is used for sources with transparency, JPEG otherwise.
func GeneratePlaceholder(img image.Image) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}

	const pw = 16
	ph := int(float64(pw) * float64(h) / float64(w))
	if ph < 1 {
		ph = 1
	}
	tiny := ResizeBilinear(img, pw, ph).(*image.RGBA)
	blurred := boxBlur(tiny, 2)

	var data []byte
	var mime string
	if hasAlpha(img) {
		data, _ = EncodePNG(blurred)
		mime = "image/png"
	} else {
		data, _ = EncodeJPEG(blurred, 50)
		mime = "image/jpeg"
	}
	if len(data) == 0 {
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func ComputeSrcsetWidths(origWidth int) []int {
	breakpoints := []int{640, 768, 1024, 1280, 1536}
	var widths []int
	for _, bp := range breakpoints {
		if bp < origWidth {
			widths = append(widths, bp)
		}
	}
	if len(widths) == 0 || widths[len(widths)-1] != origWidth {
		widths = append(widths, origWidth)
	}
	return widths
}

func ComputeHeight(origW, origH, targetW int) int {
	if origW == 0 {
		return 0
	}
	return int(float64(targetW) * float64(origH) / float64(origW))
}

// writeFileAtomic writes data to path via a temp file + rename so concurrent
// page builds never observe a partially-written variant. Files that already
// exist with the same size are skipped (cacheKey is content-derived).
func writeFileAtomic(path string, data []byte) error {
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ProcessImage decodes the source image and generates WebP + original-format
// responsive variants plus an optional blur placeholder. Variants are written
// to <root>/.krate/cache/images/ (copied to the build output by the build step)
// and referenced by absolute /_krate/images/... URLs.
// vectorFormats are image formats that are passed through as-is rather than
// decoded, converted, or compressed (responsive variants only apply to raster
// sources).
// passthroughFormats are image formats that are passed through as-is rather
// than decoded, converted, or compressed (responsive variants only apply to
// raster sources we know how to process).
var passthroughFormats = map[string]string{
	".svg":  "image/svg+xml",
	".svgz": "image/svg+xml",
	".avif": "image/avif",
	".ico":  "image/x-icon",
	".cur":  "image/x-icon",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
}

// aspectRatio returns the width/height ratio, or 0 when height is unknown.
func aspectRatio(w, h int) float64 {
	if h <= 0 {
		return 0
	}
	return float64(w) / float64(h)
}

// isPassthroughSrc reports whether the source file is a passthrough format
// (e.g. SVG) that should not be decoded.
func isPassthroughSrc(srcPath string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(srcPath))
	mime, ok := passthroughFormats[ext]
	return ok, mime
}

func ProcessImage(root, srcPath string, reqW, reqH, quality int, wantPlaceholder bool) (*ImageResult, error) {
	if quality <= 0 {
		quality = 82
	}

	origW, origH := 0, 0
	if vector, mime := isPassthroughSrc(srcPath); vector {
		return &ImageResult{
			Width:        reqW,
			Height:       reqH,
			AspectRatio:  aspectRatio(reqW, reqH),
			FallbackMime: mime,
		}, nil
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("opening image %s: %w", srcPath, err)
	}
	defer f.Close()

	img, origFmt, err := DecodeImage(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image %s: %w", srcPath, err)
	}

	bounds := img.Bounds()
	origW = bounds.Dx()
	origH = bounds.Dy()

	outDir := getCacheDir(root)
	os.MkdirAll(outDir, 0755)

	data, _ := os.ReadFile(srcPath)
	cacheKey := ComputeCacheKey(data)

	alpha := hasAlpha(img)
	fallbackMime := "image/jpeg"
	fallbackExt := ".jpg"
	if alpha || origFmt == "png" || origFmt == "gif" {
		fallbackMime = "image/png"
		fallbackExt = ".png"
	}

	srcsetWidths := ComputeSrcsetWidths(origW)
	var webpSet, fallbackSet []SrcsetEntry

	for _, sw := range srcsetWidths {
		sh := ComputeHeight(origW, origH, sw)
		resized := ResizeBilinear(img, sw, sh)

		webpBytes, err := EncodeWebP(resized, quality)
		if err != nil {
			return nil, fmt.Errorf("encoding %dw webp: %w", sw, err)
		}
		webpName := fmt.Sprintf("%s_%d.webp", cacheKey, sw)
		if err := writeFileAtomic(filepath.Join(outDir, webpName), webpBytes); err != nil {
			return nil, fmt.Errorf("writing %s: %w", webpName, err)
		}
		webpSet = append(webpSet, SrcsetEntry{Width: sw, FilePath: outputPrefix + "/" + webpName})

		var fbBytes []byte
		if fallbackExt == ".png" {
			fbBytes, err = EncodePNG(resized)
		} else {
			fbBytes, err = EncodeJPEG(resized, quality)
		}
		if err != nil {
			return nil, fmt.Errorf("encoding %dw fallback: %w", sw, err)
		}
		fbName := fmt.Sprintf("%s_%d%s", cacheKey, sw, fallbackExt)
		if err := writeFileAtomic(filepath.Join(outDir, fbName), fbBytes); err != nil {
			return nil, fmt.Errorf("writing %s: %w", fbName, err)
		}
		fallbackSet = append(fallbackSet, SrcsetEntry{Width: sw, FilePath: outputPrefix + "/" + fbName})
	}

	// Display dimensions: honor the requested width (or height) but always
	// preserve the image's intrinsic aspect ratio so the layout box matches the
	// rendered pixels (no distortion) and CLS is mitigated via aspect-ratio.
	outW, outH := origW, origH
	if reqW > 0 {
		outW = reqW
		outH = ComputeHeight(origW, origH, reqW)
	} else if reqH > 0 {
		outH = reqH
		outW = ComputeHeight(origH, origW, reqH)
	}

	placeholder := ""
	if wantPlaceholder {
		placeholder = GeneratePlaceholder(img)
	}

	aspect := 1.0
	if outH > 0 {
		aspect = float64(outW) / float64(outH)
	}

	return &ImageResult{
		WebP:         webpSet,
		Fallback:     fallbackSet,
		Width:        outW,
		Height:       outH,
		AspectRatio:  aspect,
		FallbackMime: fallbackMime,
		Placeholder:  placeholder,
	}, nil
}

func ParseIntAttr(val string) int {
	val = strings.TrimSpace(val)
	val = strings.Trim(val, "\"'")
	v, _ := strconv.Atoi(val)
	return v
}
