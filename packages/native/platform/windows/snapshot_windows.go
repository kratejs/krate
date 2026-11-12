//go:build windows

package windows

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Page snapshots and offscreen windows. Everything the offscreen area needs
// from WebView2 is in this file: ICoreWebView2::CapturePreview into a memory
// IStream (a PNG, decoded here to BGRA), and a window parked far outside every
// display, tool window, never activated.
//
// Not run on a real Windows machine by the author: the CI native job runs
// TestGUIOffscreenSnapshot.

var (
	_ caps.ViewSnapshots    = (*App)(nil)
	_ caps.OffscreenWindows = (*App)(nil)
)

const (
	capturePreviewPNG = 0 // COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG
	streamSeekSet     = 0
	streamSeekEnd     = 2

	swShowNoActivate  = 4
	wsExNoActivate    = 0x08000000
	offscreenPosition = -20000
)

// OffscreenSupported is true: a window parked off every display renders.
func (a *App) OffscreenSupported() bool { return true }

// findView returns the view with this ref. Main thread.
func (a *App) findView(ref caps.ViewRef) *view {
	for _, w := range a.windows {
		if v := w.viewByRef(ref); v != nil {
			return v
		}
	}
	return nil
}

// newMemStream returns an empty, writable system IStream (refcount 1).
func newMemStream() unsafe.Pointer {
	if pSHCreateMemStream.Find() != nil {
		return nil
	}
	r, _, _ := pSHCreateMemStream.Call(0, 0)
	return ptr(r)
}

// readStream returns everything in a memory stream and releases it.
func readStream(s unsafe.Pointer) ([]byte, error) {
	defer comRelease(s)
	var size uint64
	if failed(vcall(s, streamSeek, 0, streamSeekEnd, uintptr(unsafe.Pointer(&size)))) {
		return nil, errors.New("windows: cannot size the capture stream")
	}
	if failed(vcall(s, streamSeek, 0, streamSeekSet, 0)) {
		return nil, errors.New("windows: cannot rewind the capture stream")
	}
	if size == 0 || size > 1<<30 {
		return nil, fmt.Errorf("windows: the capture is %d bytes", size)
	}
	buf := make([]byte, size)
	var read uint32
	if failed(vcall(s, streamRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&read)))) {
		return nil, errors.New("windows: cannot read the capture stream")
	}
	return buf[:read], nil
}

// SnapshotView renders a view with CapturePreview. It blocks until WebView2
// delivers the frame (up to ten seconds) and may be called from any goroutine.
func (a *App) SnapshotView(ref caps.ViewRef, o caps.SnapshotOptions) (caps.Snapshot, error) {
	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)
	var startErr error
	a.callMain(func() {
		v := a.findView(ref)
		switch {
		case v == nil:
			startErr = fmt.Errorf("windows: view %d does not exist", ref)
			return
		case v.w.released || v.w.destroyed:
			startErr = errReleased
			return
		}
		v.do(func() {
			stream := newMemStream()
			if stream == nil {
				done <- result{err: errors.New("windows: cannot create a memory stream")}
				return
			}
			h := newHandler(iidCapturePreviewHandler, true, func(hr, _ uintptr) uintptr {
				if failed(hr) {
					comRelease(stream)
					done <- result{err: fmt.Errorf("windows: CapturePreview: 0x%08x", uint32(hr))}
					return sOK
				}
				b, err := readStream(stream)
				done <- result{png: b, err: err}
				return sOK
			})
			if hr := vcall(v.webview, wvCapturePreview, capturePreviewPNG, uintptr(stream), uintptr(h)); failed(hr) {
				comRelease(stream)
				done <- result{err: fmt.Errorf("windows: CapturePreview: 0x%08x", uint32(hr))}
			}
		})
	})
	if startErr != nil {
		return caps.Snapshot{}, startErr
	}
	select {
	case r := <-done:
		if r.err != nil {
			return caps.Snapshot{}, r.err
		}
		return caps.SnapshotFromPNG(r.png)
	case <-time.After(10 * time.Second):
		return caps.Snapshot{}, errors.New("windows: the page snapshot did not arrive within 10 seconds")
	}
}
