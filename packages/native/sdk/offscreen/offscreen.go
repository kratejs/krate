// Package offscreen renders pages that no one is looking at and streams their
// frames: the closest honest equivalent of Electron's offscreen rendering.
//
// The system webviews cannot push frames the way Chromium's OSR does. What
// they can do is render a page in a window that is never shown (a
// GtkOffscreenWindow on Linux, a window parked off every display on Windows
// and macOS) and hand over a snapshot on request (webkit_web_view_get_snapshot,
// WKWebView takeSnapshot, WebView2 CapturePreview). Open creates such a
// window; Capture takes one frame of any window; Frames repeats that at a
// requested rate, one capture at a time: the next frame starts after the
// consumer took the last one, so a slow consumer makes the stream skip frames
// (counted in Frame.Dropped) instead of queueing them.
//
// Frames are PNG, or raw premultiplied BGRA (width*4 bytes per row) for
// consumers that want pixels. The snapshot rate is limited by the engine: see
// the offscreen section of docs/native/processes for measured numbers.
//
// Permission: "offscreen".
package offscreen

import (
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
)

// Permission is the app permission the UI needs.
const Permission = "offscreen"

// Host function ids.
const (
	IDOpen    = "krate/offscreen.open"
	IDCapture = "krate/offscreen.capture"
	IDFrames  = "krate/offscreen.frames"
)

// OpenOptions configure Open.
type OpenOptions struct {
	// Path is the route to render, "/" by default.
	Path   string `json:"path,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// CaptureOptions configure Capture and Frames.
type CaptureOptions struct {
	// Format is "png" (default) or "bgra".
	Format string `json:"format,omitempty"`
	// FullDocument captures the whole page, not just the visible area, where
	// the engine can (WebKitGTK).
	FullDocument bool `json:"fullDocument,omitempty"`
	// FPS is the frame rate of a stream (default 10, at most 60).
	FPS float64 `json:"fps,omitempty"`
	// MaxFrames ends a stream after this many frames; zero means no limit.
	MaxFrames int `json:"maxFrames,omitempty"`
}

// Frame is one captured frame. Data is the PNG file, or the raw pixels.
type Frame struct {
	Seq    int    `json:"seq"`
	TimeMs int64  `json:"timeMs"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	// Dropped counts the frames skipped since the previous one because the
	// capture or the consumer was too slow.
	Dropped int    `json:"dropped"`
	Data    []byte `json:"-"`
}

// Open creates a window that is never shown and returns its id. Close it with
// window.Close.
func Open(o OpenOptions) (string, error) { return jsonwire.Query[string](IDOpen, o) }

// Capture takes one frame of the window with this id ("" is the main window).
func Capture(window string, o CaptureOptions) (Frame, error) {
	return queryFrame(window, o)
}
