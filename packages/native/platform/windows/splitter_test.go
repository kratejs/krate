package windows

import (
	"math"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func newSplit(dpi int) *splitter {
	return &splitter{
		layout: &caps.LayoutNode{Axis: caps.Horizontal, Ratio: 0.5,
			First: &caps.LayoutNode{View: 1, MinWidth: 100},
			Second: &caps.LayoutNode{Axis: caps.Vertical, Ratio: 0.5,
				First:  &caps.LayoutNode{View: 2, MinHeight: 50},
				Second: caps.Leaf(3)}},
		bounds: caps.Rect{W: 1006, H: 606},
		dpi:    dpi,
	}
}

func TestSplitterHitAndCursor(t *testing.T) {
	s := newSplit(96)
	a := s.arrange()
	if len(a.Panes) != 3 || len(a.Dividers) != 2 {
		t.Fatalf("%+v", a)
	}
	root := a.Dividers[0].Rect
	if got := s.cursor(root.X+2, 100); got != cursorSizeWE {
		t.Errorf("over the vertical divider: %v", got)
	}
	// The grab margin: 3 px outside still hits.
	if _, ok := s.hit(root.X-3, 100); !ok {
		t.Error("grab margin missing")
	}
	if _, ok := s.hit(root.X-10, 100); ok {
		t.Error("hit far from the divider")
	}
	nested := a.Dividers[1].Rect
	if got := s.cursor(nested.X+20, nested.Y+2); got != cursorSizeNS {
		t.Errorf("over the horizontal divider: %v", got)
	}
	if got := s.cursor(10, 10); got != cursorArrow {
		t.Errorf("over a pane: %v", got)
	}
}

func TestSplitterDrag(t *testing.T) {
	s := newSplit(96)
	root := s.arrange().Dividers[0].Rect
	if s.move(300, 300) {
		t.Error("move without a press changed the layout")
	}
	if !s.press(root.X+1, 50) || !s.dragging || s.path != "" {
		t.Fatal("press on the divider")
	}
	if got := s.cursor(0, 0); got != cursorSizeWE {
		t.Errorf("cursor while dragging: %v", got)
	}
	// Pointer at x=300: the first pane is 300-3 of the 1000 available.
	if !s.move(300, 50) {
		t.Fatal("move did not change the ratio")
	}
	if got := s.layout.Ratio; math.Abs(got-0.297) > 0.001 {
		t.Errorf("ratio %v", got)
	}
	if s.move(300, 90) {
		t.Error("the same position changed the ratio again")
	}
	// Dragging left of the minimum stops at 100 of 1000.
	s.move(5, 50)
	if got := s.layout.Ratio; math.Abs(got-0.1) > 0.001 {
		t.Errorf("min clamp: %v", got)
	}
	s.release()
	if s.move(500, 50) {
		t.Error("move after release")
	}
	if s.press(10, 10) {
		t.Error("press on a pane started a drag")
	}
}

func TestSplitterDPIScales(t *testing.T) {
	s := newSplit(192) // 200 percent
	if s.divider() != 12 || s.slop() != 6 {
		t.Fatalf("divider %d slop %d", s.divider(), s.slop())
	}
	a := s.arrange()
	if a.Dividers[0].Rect.W != 12 {
		t.Errorf("divider width %d", a.Dividers[0].Rect.W)
	}
	// The 100 DIP minimum is 200 device pixels, and the stored layout is
	// still in DIPs.
	s.press(a.Dividers[0].Rect.X+1, 50)
	s.move(5, 50)
	want := 200.0 / float64(1006-12)
	if got := s.layout.Ratio; math.Abs(got-want) > 0.002 {
		t.Errorf("ratio %v, want %v", got, want)
	}
	if s.layout.First.MinWidth != 100 {
		t.Errorf("stored minimum changed to %d", s.layout.First.MinWidth)
	}
	if scale(5, 0) != 5 {
		t.Error("dpi 0 should mean 96")
	}
}
