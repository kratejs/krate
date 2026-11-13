package caps

import (
	"math"
	"testing"
)

func three() *LayoutNode {
	// 1 | (2 over 3)
	return &LayoutNode{Axis: Horizontal, Ratio: 0.25,
		First: &LayoutNode{View: 1, MinWidth: 100},
		Second: &LayoutNode{Axis: Vertical, Ratio: 0.5,
			First:  &LayoutNode{View: 2, MinHeight: 50},
			Second: &LayoutNode{View: 3, MinHeight: 50}}}
}

func TestArrangeTwoPanes(t *testing.T) {
	root := &LayoutNode{Axis: Horizontal, Ratio: 0.25, First: Leaf(1), Second: Leaf(2)}
	a := Arrange(root, Rect{0, 0, 1006, 400}, 6)
	if len(a.Panes) != 2 || len(a.Dividers) != 1 {
		t.Fatalf("%+v", a)
	}
	if a.Panes[0].Rect != (Rect{0, 0, 250, 400}) || a.Panes[1].Rect != (Rect{256, 0, 750, 400}) {
		t.Fatalf("panes %+v", a.Panes)
	}
	if a.Dividers[0].Rect != (Rect{250, 0, 6, 400}) || a.Dividers[0].Path != "" {
		t.Fatalf("divider %+v", a.Dividers[0])
	}
}

func TestArrangeNestedCoversBounds(t *testing.T) {
	root := three()
	b := Rect{10, 20, 806, 606}
	a := Arrange(root, b, 6)
	if len(a.Panes) != 3 || len(a.Dividers) != 2 {
		t.Fatalf("%+v", a)
	}
	area := 0
	for _, p := range a.Panes {
		area += p.Rect.W * p.Rect.H
		if p.Rect.X < b.X || p.Rect.Y < b.Y || p.Rect.X+p.Rect.W > b.X+b.W || p.Rect.Y+p.Rect.H > b.Y+b.H {
			t.Fatalf("pane %+v outside bounds", p)
		}
	}
	for _, d := range a.Dividers {
		area += d.Rect.W * d.Rect.H
	}
	// Panes and dividers tile the bounds except for the corner where the
	// horizontal divider meets the vertical one's rows.
	if area > b.W*b.H {
		t.Fatalf("overlap: area %d > %d", area, b.W*b.H)
	}
	if a.Dividers[1].Path != "1" || a.Dividers[1].Axis != Vertical {
		t.Fatalf("nested divider %+v", a.Dividers[1])
	}
}

func TestSplitSizesMinimums(t *testing.T) {
	// Ratio asks for a tiny first half; the minimum wins.
	if f, s := SplitSizes(506, 6, 0.01, 100, 100); f != 100 || s != 400 {
		t.Fatalf("%d %d", f, s)
	}
	if f, s := SplitSizes(506, 6, 0.99, 100, 100); f != 400 || s != 100 {
		t.Fatalf("%d %d", f, s)
	}
	// Minimums do not fit: shared in proportion.
	if f, s := SplitSizes(106, 6, 0.5, 100, 300); f+s != 100 || f >= s {
		t.Fatalf("%d %d", f, s)
	}
	if f, s := SplitSizes(4, 6, 0.5, 0, 0); f != 0 || s != 0 {
		t.Fatalf("%d %d", f, s)
	}
}

func TestMinSizePropagates(t *testing.T) {
	w, h := MinSize(three(), 6)
	if w != 100+6+0 || h != 50+6+50 {
		t.Fatalf("%d x %d", w, h)
	}
}

func TestHitDividerAndDrag(t *testing.T) {
	root := three()
	b := Rect{0, 0, 1006, 606}
	a := Arrange(root, b, 6)
	d, ok := a.HitDivider(a.Dividers[0].Rect.X+3, 300, 2)
	if !ok || d.Path != "" {
		t.Fatalf("hit %+v %v", d, ok)
	}
	if _, ok := a.HitDivider(a.Dividers[0].Rect.X-10, 300, 2); ok {
		t.Fatal("hit far from the divider")
	}
	// Slop makes a miss a hit.
	if _, ok := a.HitDivider(a.Dividers[0].Rect.X-2, 300, 3); !ok {
		t.Fatal("slop did not widen the target")
	}
	// Drag the root divider to x=500: first half is 500-3=497 of 1000.
	r, err := root.Drag(b, 6, "", 500)
	if err != nil || math.Abs(r-0.497) > 0.001 || root.Ratio != r {
		t.Fatalf("drag: %v %v", r, err)
	}
	// Dragging into the minimum of the first half stops at 100/1000.
	r, _ = root.Drag(b, 6, "", 10)
	if math.Abs(r-0.1) > 0.001 {
		t.Fatalf("min clamp: %v", r)
	}
	// The nested split lives in the second region: y drag respects 50px minimums.
	r, err = root.Drag(b, 6, "1", 5000)
	if err != nil || math.Abs(r-float64(600-50)/600) > 0.001 {
		t.Fatalf("nested drag: %v %v", r, err)
	}
	if _, err := root.Drag(b, 6, "00", 10); err == nil {
		t.Fatal("drag on a leaf")
	}
	if _, err := root.Drag(b, 6, "x", 10); err == nil {
		t.Fatal("bad path")
	}
}

func TestValidate(t *testing.T) {
	good := three()
	if err := good.Validate([]ViewRef{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	bad := map[string]struct {
		n     *LayoutNode
		views []ViewRef
	}{
		"missing view":    {good, []ViewRef{1, 2, 3, 4}},
		"unknown view":    {good, []ViewRef{1, 2}},
		"duplicate":       {&LayoutNode{Axis: Horizontal, First: Leaf(1), Second: Leaf(1)}, []ViewRef{1}},
		"zero leaf":       {&LayoutNode{Axis: Horizontal, First: Leaf(1), Second: Leaf(0)}, []ViewRef{1}},
		"bad axis":        {&LayoutNode{Axis: "diag", First: Leaf(1), Second: Leaf(2)}, []ViewRef{1, 2}},
		"bad ratio":       {&LayoutNode{Axis: Horizontal, Ratio: 1, First: Leaf(1), Second: Leaf(2)}, []ViewRef{1, 2}},
		"nan ratio":       {&LayoutNode{Axis: Horizontal, Ratio: math.NaN(), First: Leaf(1), Second: Leaf(2)}, []ViewRef{1, 2}},
		"one half":        {&LayoutNode{Axis: Horizontal, First: Leaf(1)}, []ViewRef{1}},
		"split with view": {&LayoutNode{View: 9, Axis: Horizontal, First: Leaf(1), Second: Leaf(2)}, []ViewRef{1, 2}},
		"negative min":    {&LayoutNode{View: 1, MinWidth: -1}, []ViewRef{1}},
	}
	for name, c := range bad {
		if err := c.n.Validate(c.views); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	deep := Leaf(1)
	views := []ViewRef{1}
	for i := 0; i < MaxLayoutDepth+2; i++ {
		v := ViewRef(i + 2)
		views = append(views, v)
		deep = &LayoutNode{Axis: Horizontal, First: deep, Second: Leaf(v)}
	}
	if err := deep.Validate(views); err == nil {
		t.Error("too deep")
	}
}

func TestSplitLeafAndRemove(t *testing.T) {
	root := Leaf(1)
	r2, err := SplitLeaf(root, 1, 2, ViewOptions{Axis: Horizontal, Share: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if r2.First.View != 1 || r2.Second.View != 2 || math.Abs(r2.Ratio-0.7) > 1e-9 {
		t.Fatalf("%+v", r2)
	}
	if !root.IsLeaf() {
		t.Fatal("SplitLeaf changed its input")
	}
	r3, err := SplitLeaf(r2, 2, 3, ViewOptions{Axis: Vertical, NewFirst: true, MinHeight: 40})
	if err != nil {
		t.Fatal(err)
	}
	if got := r3.Leaves(); len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 2 {
		t.Fatalf("leaves %v", got)
	}
	if n := r3.At("10"); n == nil || n.View != 3 || n.MinHeight != 40 || r3.At("1").Ratio != 0.5 {
		t.Fatalf("%+v", r3.At("1"))
	}
	if err := r3.Validate([]ViewRef{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := SplitLeaf(r3, 99, 4, ViewOptions{}); err == nil {
		t.Fatal("split of an unknown view")
	}
	if _, err := SplitLeaf(r3, 1, 4, ViewOptions{Share: 1}); err == nil {
		t.Fatal("share 1")
	}
	if _, err := SplitLeaf(r3, 1, 4, ViewOptions{Axis: "x"}); err == nil {
		t.Fatal("bad axis")
	}

	r4, err := RemoveLeaf(r3, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := r4.Leaves(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("after remove %v", got)
	}
	if _, err := RemoveLeaf(r4, 99); err == nil {
		t.Fatal("remove unknown")
	}
	if _, err := RemoveLeaf(Leaf(1), 1); err == nil {
		t.Fatal("remove the only view")
	}
}

func TestRatiosAndMins(t *testing.T) {
	n := three()
	if r := n.Ratios(); r[""] != 0.25 || r["1"] != 0.5 || len(r) != 2 {
		t.Fatalf("%v", r)
	}
	if !n.SetMin(3, 10, 70) || n.At("11").MinHeight != 70 || n.SetMin(9, 1, 1) {
		t.Fatal("SetMin")
	}
	if err := n.SetRatio("", 0); err == nil {
		t.Fatal("ratio 0")
	}
	if err := n.SetRatio("0", 0.5); err == nil {
		t.Fatal("ratio on a leaf")
	}
}

func TestViewContext(t *testing.T) {
	ctx := t.Context()
	if _, ok := ViewFrom(ctx); ok {
		t.Fatal("untagged context has a view")
	}
	v, ok := ViewFrom(WithView(ctx, 7))
	if !ok || v != 7 {
		t.Fatal(v, ok)
	}
	if v, ok := ViewFrom(WithView(ctx, 0)); !ok || v != 0 {
		t.Fatal("a zero view must still count as native identity")
	}
}
