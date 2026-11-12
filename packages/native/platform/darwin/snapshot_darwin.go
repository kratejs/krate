//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Page snapshots and offscreen windows. WKWebView takeSnapshot returns an
// NSImage; it is turned into a PNG with NSBitmapImageRep and decoded in Go
// (caps.SnapshotFromPNG). An offscreen window is a borderless window parked far
// outside every display: ordered in, so its page renders, but never visible.
//
// Written and compiled, not run on a real Mac by the author: the CI native
// job runs TestGUIOffscreenSnapshot.

var (
	_ caps.ViewSnapshots    = (*App)(nil)
	_ caps.OffscreenWindows = (*App)(nil)
)

const (
	offscreenOrigin       = -30000.0
	nsBitmapImageFileType = 4 // NSBitmapImageFileTypePNG
)

// OffscreenSupported is true on every macOS this package runs on.
func (a *App) OffscreenSupported() bool { return true }

// parkOffscreen moves a borderless window far off screen and orders it in
// without activating the app.
func parkOffscreen(win objc.ID, width, height int) {
	win.Send(sel("setFrame:display:"), nsRect{
		Origin: nsPoint{X: offscreenOrigin, Y: offscreenOrigin},
		Size:   nsSize{Width: float64(width), Height: float64(height)},
	}, true)
	win.Send(sel("setHasShadow:"), false)
	win.Send(sel("setIgnoresMouseEvents:"), true)
	win.Send(sel("setExcludedFromWindowsMenu:"), true)
	win.Send(sel("orderFront:"), objc.ID(0))
}

// SnapshotView renders a view with takeSnapshot. It blocks until WebKit
// delivers the image (up to ten seconds) and may be called from any goroutine.
func (a *App) SnapshotView(ref caps.ViewRef, o caps.SnapshotOptions) (caps.Snapshot, error) {
	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)
	var startErr error
	runOnMain(func() {
		withPool(func() {
			var web objc.ID
			for _, w := range a.windows {
				if v := w.viewByRef(ref); v != nil {
					web = v.web
					break
				}
			}
			if web == 0 {
				startErr = fmt.Errorf("darwin: view %d does not exist", ref)
				return
			}
			s := sel("takeSnapshotWithConfiguration:completionHandler:")
			if !respondsTo(web, s) {
				startErr = errors.New("darwin: this WKWebView cannot take snapshots")
				return
			}
			// The completion block runs on the main thread, once. NSImage to
			// PNG data: TIFFRepresentation, NSBitmapImageRep, PNG.
			block := objc.NewBlock(func(_ objc.Block, image, nsErr objc.ID) {
				if image == 0 {
					msg := "WebKit returned no snapshot"
					if nsErr != 0 {
						msg = goString(nsErr.Send(sel("localizedDescription")))
					}
					done <- result{err: errors.New("darwin: page snapshot failed: " + msg)}
					return
				}
				tiff := image.Send(sel("TIFFRepresentation"))
				rep := cls("NSBitmapImageRep").Send(sel("imageRepWithData:"), tiff)
				if rep == 0 {
					done <- result{err: errors.New("darwin: cannot convert the snapshot")}
					return
				}
				data := rep.Send(sel("representationUsingType:properties:"), uintptr(nsBitmapImageFileType), objc.ID(0))
				if data == 0 {
					done <- result{err: errors.New("darwin: cannot encode the snapshot as PNG")}
					return
				}
				done <- result{png: append([]byte(nil), dataBytes(data)...)}
			})
			web.Send(s, objc.ID(0), objc.ID(block))
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
		return caps.Snapshot{}, errors.New("darwin: the page snapshot did not arrive within 10 seconds")
	}
}
