package caps

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
)

// Snapshot is one rendered frame of a view: premultiplied BGRA pixels, row
// by row, Stride bytes apart (the byte order of a little-endian ARGB32 value,
// which is what cairo, CoreGraphics and WebView2 bitmaps share).
//
// This file is the whole platform-facing surface of the page capture used by
// the offscreen area (sdk/offscreen). It is kept apart so that another page
// capture API can replace it without touching the rest.
type Snapshot struct {
	Width, Height int
	Stride        int
	Pix           []byte
}

// SnapshotOptions tune ViewSnapshots.SnapshotView.
type SnapshotOptions struct {
	// FullDocument captures the whole page instead of the visible area, where
	// the engine supports it (WebKitGTK does; the others ignore it).
	FullDocument bool
}

// ViewSnapshots is implemented by Apps that can render a view to pixels
// (WebKitGTK webkit_web_view_get_snapshot, WKWebView takeSnapshot, WebView2
// CapturePreview). SnapshotView blocks until the engine delivers the frame and
// may be called from any goroutine.
type ViewSnapshots interface {
	SnapshotView(v ViewRef, o SnapshotOptions) (Snapshot, error)
}

// OffscreenWindows is implemented by Apps whose NewWindow honors
// platform.WindowOptions.Offscreen: a window that is never mapped on screen
// but whose page still renders, so SnapshotView returns real frames.
type OffscreenWindows interface {
	OffscreenSupported() bool
}

// SnapshotFromPNG decodes a PNG file into a Snapshot (premultiplied BGRA). The
// engines whose capture API produces an image file (WebView2 CapturePreview,
// WKWebView takeSnapshot through NSBitmapImageRep) use it.
func SnapshotFromPNG(b []byte) (Snapshot, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return Snapshot{}, err
	}
	r := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, r.Min, draw.Src)
	// image.RGBA is premultiplied RGBA; swap R and B for BGRA.
	for i := 0; i+3 < len(rgba.Pix); i += 4 {
		rgba.Pix[i], rgba.Pix[i+2] = rgba.Pix[i+2], rgba.Pix[i]
	}
	return Snapshot{Width: r.Dx(), Height: r.Dy(), Stride: rgba.Stride, Pix: rgba.Pix}, nil
}
