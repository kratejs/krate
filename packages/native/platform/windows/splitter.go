//go:build windows

package windows

import "github.com/kratejs/krate/packages/native/platform/caps"

// The split-pane arithmetic of a window with several WebView2 controllers.
// This file has no Windows dependencies; the window procedure in
// views_windows.go feeds it mouse messages and applies the rectangles it
// computes to the controllers.

const (
	// dividerDIP is the thickness of a divider in device-independent pixels.
	dividerDIP = 6
	// grabDIP is how far outside the divider a press still grabs it.
	grabDIP = 3
)

// cursorKind says which mouse cursor the pointer is over.
type cursorKind int

const (
	cursorArrow cursorKind = iota
	cursorSizeWE
	cursorSizeNS
)

// splitter holds the layout of one window and the state of a divider drag.
type splitter struct {
	layout *caps.LayoutNode
	bounds caps.Rect
	// dpi is the window's DPI; sizes below are scaled from device-independent
	// pixels by dpi/96.
	dpi int

	dragging bool
	path     string
}

func scale(v, dpi int) int {
	if dpi <= 0 {
		dpi = 96
	}
	return (v*dpi + 48) / 96
}

func (s *splitter) divider() int { return scale(dividerDIP, s.dpi) }
func (s *splitter) slop() int    { return scale(grabDIP, s.dpi) }

// scaled returns the layout with its minimum sizes in device pixels.
func (s *splitter) scaled() *caps.LayoutNode {
	if s.layout == nil {
		return nil
	}
	c := s.layout.Clone()
	var walk func(n *caps.LayoutNode)
	walk = func(n *caps.LayoutNode) {
		if n == nil {
			return
		}
		n.MinWidth, n.MinHeight = scale(n.MinWidth, s.dpi), scale(n.MinHeight, s.dpi)
		walk(n.First)
		walk(n.Second)
	}
	walk(c)
	return c
}

// arrange returns the rectangle of every pane and divider.
func (s *splitter) arrange() caps.Arrangement {
	return caps.Arrange(s.scaled(), s.bounds, s.divider())
}

// hit returns the divider under the point.
func (s *splitter) hit(x, y int) (caps.Divider, bool) {
	return s.arrange().HitDivider(x, y, s.slop())
}

// cursor is the cursor to show at the point: a resize cursor over a divider or
// while dragging one.
func (s *splitter) cursor(x, y int) cursorKind {
	var axis caps.Axis
	if s.dragging {
		if n := s.layout.At(s.path); n != nil && !n.IsLeaf() {
			axis = n.Axis
		}
	} else if d, ok := s.hit(x, y); ok {
		axis = d.Axis
	}
	switch axis {
	case caps.Horizontal:
		return cursorSizeWE
	case caps.Vertical:
		return cursorSizeNS
	}
	return cursorArrow
}

// press starts a drag when the point is on a divider.
func (s *splitter) press(x, y int) bool {
	d, ok := s.hit(x, y)
	if !ok {
		return false
	}
	s.dragging, s.path = true, d.Path
	return true
}

// move continues a drag: the divider follows the pointer, limited by the
// minimum sizes. It reports whether the ratio changed and the panes need to be
// placed again.
func (s *splitter) move(x, y int) bool {
	if !s.dragging {
		return false
	}
	n := s.layout.At(s.path)
	if n == nil || n.IsLeaf() {
		return false
	}
	pos := x
	if n.Axis == caps.Vertical {
		pos = y
	}
	ratio, err := caps.DragTo(s.scaled(), s.bounds, s.divider(), s.path, pos)
	if err != nil || ratio == n.Ratio {
		return false
	}
	return s.layout.SetRatio(s.path, ratio) == nil
}

// release ends a drag.
func (s *splitter) release() { s.dragging = false }
