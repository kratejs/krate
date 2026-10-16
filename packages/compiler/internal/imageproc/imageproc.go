package imageproc

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	webpenc "github.com/skrashevich/go-webp"
	xdraw "golang.org/x/image/draw"
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
	// Computed per call - the cache must be scoped to the current project root.
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
// Uses a pure-Go VP8 encoder - no cgo, no libwebp dependency.
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

// resize scales img to targetW×targetH using Catmull-Rom resampling
// (golang.org/x/image/draw). This replaces a hand-rolled bilinear loop with a
// faster, higher-quality filter.
func resize(img image.Image, targetW, targetH int) *image.RGBA {
	if targetW < 1 {
		targetW = 1
	}
	if targetH < 1 {
		targetH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return dst
}

// variantWorkers bounds per-image resize/encode concurrency. Page builds already
// run in parallel, so keep this modest to avoid oversubscribing the CPU.
func variantWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

// widthQuality lowers the encode quality for small variants (which are shown on
// small screens and hide artifacts well), keeping larger variants at base.
func widthQuality(base, width int) int {
	if width < 768 {
		if q := base - 8; q >= 50 {
			return q
		}
	}
	return base
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
	tiny := resize(img, pw, ph)
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

// imageMeta is the cached outcome of processing one source image, stored beside
// the variants so a later build can return the result without decoding, resizing
// or re-encoding. Variants are named by (cacheKey, quality, width); the meta by
// (cacheKey, quality).
type imageMeta struct {
	OrigW          int           `json:"origW"`
	OrigH          int           `json:"origH"`
	FallbackMime   string        `json:"fallbackMime"`
	HasPlaceholder bool          `json:"hasPlaceholder"`
	Placeholder    string        `json:"placeholder,omitempty"`
	WebP           []SrcsetEntry `json:"webp"`
	Fallback       []SrcsetEntry `json:"fallback"`
}

func imageMetaPath(outDir, cacheKey string, quality int) string {
	return filepath.Join(outDir, fmt.Sprintf("%s_q%d.meta.json", cacheKey, quality))
}

// loadImageMeta returns a cached result for the source when the meta exists, the
// requested placeholder is present, and every referenced variant file still
// exists on disk.
func loadImageMeta(outDir, cacheKey string, quality int, wantPlaceholder bool) (*ImageResult, bool) {
	data, err := os.ReadFile(imageMetaPath(outDir, cacheKey, quality))
	if err != nil {
		return nil, false
	}
	var m imageMeta
	if json.Unmarshal(data, &m) != nil || m.OrigW <= 0 || m.OrigH <= 0 {
		return nil, false
	}
	if wantPlaceholder && !m.HasPlaceholder {
		return nil, false
	}
	for _, e := range append(append([]SrcsetEntry{}, m.WebP...), m.Fallback...) {
		if _, err := os.Stat(filepath.Join(outDir, filepath.Base(e.FilePath))); err != nil {
			return nil, false
		}
	}
	return &ImageResult{
		WebP:         m.WebP,
		Fallback:     m.Fallback,
		Width:        m.OrigW,
		Height:       m.OrigH,
		FallbackMime: m.FallbackMime,
		Placeholder:  m.Placeholder,
	}, true
}

// displayDims applies a requested width/height while preserving aspect ratio.
func displayDims(origW, origH, reqW, reqH int) (int, int) {
	switch {
	case reqW > 0:
		return reqW, ComputeHeight(origW, origH, reqW)
	case reqH > 0:
		return ComputeHeight(origH, origW, reqH), reqH
	default:
		return origW, origH
	}
}

func ProcessImage(root, srcPath string, reqW, reqH, quality int, wantPlaceholder bool) (*ImageResult, error) {
	if quality <= 0 {
		quality = 82
	}

	if vector, mime := isPassthroughSrc(srcPath); vector {
		return &ImageResult{
			Width:        reqW,
			Height:       reqH,
			AspectRatio:  aspectRatio(reqW, reqH),
			FallbackMime: mime,
		}, nil
	}

	outDir := getCacheDir(root)

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, fmt.Errorf("reading image %s: %w", srcPath, err)
	}
	cacheKey := ComputeCacheKey(data)

	// Cache hit: reuse the previous result without decoding/resizing/encoding.
	if res, ok := loadImageMeta(outDir, cacheKey, quality, wantPlaceholder); ok {
		outW, outH := displayDims(res.Width, res.Height, reqW, reqH)
		res.Width, res.Height = outW, outH
		res.AspectRatio = aspectRatio(outW, outH)
		// The placeholder is caller-gated: a source cached with a placeholder must
		// not leak it into a usage that opted out (placeholder="empty").
		if !wantPlaceholder {
			res.Placeholder = ""
		}
		return res, nil
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("opening image %s: %w", srcPath, err)
	}
	img, origFmt, err := DecodeImage(f)
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("decoding image %s: %w", srcPath, err)
	}

	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()

	_ = os.MkdirAll(outDir, 0755)

	alpha := hasAlpha(img)
	fallbackMime := "image/jpeg"
	fallbackExt := ".jpg"
	if alpha || origFmt == "png" || origFmt == "gif" {
		fallbackMime = "image/png"
		fallbackExt = ".png"
	}

	srcsetWidths := ComputeSrcsetWidths(origW)

	// Generate variants in parallel (bounded): decode once, then resize + encode
	// each width concurrently. img is only read, so concurrent Scale is safe.
	type variantResult struct {
		webp SrcsetEntry
		fb   SrcsetEntry
		err  error
	}
	results := make([]variantResult, len(srcsetWidths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, variantWorkers())
	for i, sw := range srcsetWidths {
		i, sw := i, sw
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			sh := ComputeHeight(origW, origH, sw)
			resized := resize(img, sw, sh)
			q := widthQuality(quality, sw)

			webpBytes, werr := EncodeWebP(resized, q)
			if werr != nil {
				results[i].err = fmt.Errorf("encoding %dw webp: %w", sw, werr)
				return
			}
			webpName := fmt.Sprintf("%s_q%d_%d.webp", cacheKey, quality, sw)
			if werr := writeFileAtomic(filepath.Join(outDir, webpName), webpBytes); werr != nil {
				results[i].err = fmt.Errorf("writing %s: %w", webpName, werr)
				return
			}
			results[i].webp = SrcsetEntry{Width: sw, FilePath: outputPrefix + "/" + webpName}

			var fbBytes []byte
			var ferr error
			if fallbackExt == ".png" {
				fbBytes, ferr = EncodePNG(resized)
			} else {
				fbBytes, ferr = EncodeJPEG(resized, q)
			}
			if ferr != nil {
				results[i].err = fmt.Errorf("encoding %dw fallback: %w", sw, ferr)
				return
			}
			fbName := fmt.Sprintf("%s_q%d_%d%s", cacheKey, quality, sw, fallbackExt)
			if ferr := writeFileAtomic(filepath.Join(outDir, fbName), fbBytes); ferr != nil {
				results[i].err = fmt.Errorf("writing %s: %w", fbName, ferr)
				return
			}
			results[i].fb = SrcsetEntry{Width: sw, FilePath: outputPrefix + "/" + fbName}
		}()
	}
	wg.Wait()

	var webpSet, fallbackSet []SrcsetEntry
	for i := range results {
		if results[i].err != nil {
			return nil, results[i].err
		}
		webpSet = append(webpSet, results[i].webp)
		fallbackSet = append(fallbackSet, results[i].fb)
	}

	placeholder := ""
	if wantPlaceholder {
		placeholder = GeneratePlaceholder(img)
	}

	// Persist the result so a later build skips decode/resize/encode.
	meta := imageMeta{
		OrigW:          origW,
		OrigH:          origH,
		FallbackMime:   fallbackMime,
		HasPlaceholder: placeholder != "",
		Placeholder:    placeholder,
		WebP:           webpSet,
		Fallback:       fallbackSet,
	}
	if metaBytes, merr := json.Marshal(meta); merr == nil {
		_ = writeFileAtomic(imageMetaPath(outDir, cacheKey, quality), metaBytes)
	}

	outW, outH := displayDims(origW, origH, reqW, reqH)

	return &ImageResult{
		WebP:         webpSet,
		Fallback:     fallbackSet,
		Width:        outW,
		Height:       outH,
		AspectRatio:  aspectRatio(outW, outH),
		FallbackMime: fallbackMime,
		Placeholder:  placeholder,
	}, nil
}
