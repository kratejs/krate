package caps

import (
	"errors"
	"fmt"
	"math"
)

// Axis says how the two halves of a split sit.
type Axis string

// Split axes. Horizontal puts the halves side by side (first on the left, with
// a vertical divider between them); Vertical stacks them (first on top).
const (
	Horizontal Axis = "horizontal"
	Vertical   Axis = "vertical"
)

// MaxLayoutDepth bounds the depth of a layout tree.
const MaxLayoutDepth = 16

// LayoutNode is a node of a layout tree: a leaf that shows one view, or a
// split of two subtrees. The first subtree is the left or top one.
type LayoutNode struct {
	// View is the view of a leaf (non-zero). A split has no view.
	View ViewRef
	// MinWidth and MinHeight are the smallest size of a leaf's pane in pixels.
	MinWidth, MinHeight int

	// Axis, Ratio, First and Second describe a split. Ratio is the share of
	// the space (without the divider) that First gets, in (0,1); zero means
	// one half.
	Axis          Axis
	Ratio         float64
	First, Second *LayoutNode
}

// Leaf returns a leaf for v.
func Leaf(v ViewRef) *LayoutNode { return &LayoutNode{View: v} }

// IsLeaf reports whether n shows a view.
func (n *LayoutNode) IsLeaf() bool { return n != nil && n.First == nil && n.Second == nil }

// Clone returns a deep copy.
func (n *LayoutNode) Clone() *LayoutNode {
	if n == nil {
		return nil
	}
	c := *n
	c.First, c.Second = n.First.Clone(), n.Second.Clone()
	return &c
}

// Leaves lists the views of the tree, first half before second.
func (n *LayoutNode) Leaves() []ViewRef {
	var out []ViewRef
	var walk func(*LayoutNode)
	walk = func(n *LayoutNode) {
		if n == nil {
			return
		}
		if n.IsLeaf() {
			out = append(out, n.View)
			return
		}
		walk(n.First)
		walk(n.Second)
	}
	walk(n)
	return out
}

// Validate checks that the tree is well formed and names every view in views
// exactly once. Ratios of zero are allowed (one half).
func (n *LayoutNode) Validate(views []ViewRef) error {
	want := map[ViewRef]bool{}
	for _, v := range views {
		want[v] = true
	}
	seen := map[ViewRef]bool{}
	var walk func(n *LayoutNode, depth int) error
	walk = func(n *LayoutNode, depth int) error {
		if n == nil {
			return errors.New("layout: missing node")
		}
		if depth > MaxLayoutDepth {
			return fmt.Errorf("layout: deeper than %d splits", MaxLayoutDepth)
		}
		if n.MinWidth < 0 || n.MinHeight < 0 {
			return errors.New("layout: negative minimum size")
		}
		if n.IsLeaf() {
			if n.View == 0 {
				return errors.New("layout: a leaf needs a view")
			}
			if !want[n.View] {
				return fmt.Errorf("layout: view %d is not in this window", n.View)
			}
			if seen[n.View] {
				return fmt.Errorf("layout: view %d appears twice", n.View)
			}
			seen[n.View] = true
			return nil
		}
		if n.View != 0 {
			return errors.New("layout: a split cannot also show a view")
		}
		if n.First == nil || n.Second == nil {
			return errors.New("layout: a split needs two halves")
		}
		if n.Axis != Horizontal && n.Axis != Vertical {
			return fmt.Errorf("layout: axis %q must be %q or %q", n.Axis, Horizontal, Vertical)
		}
		if math.IsNaN(n.Ratio) || n.Ratio < 0 || n.Ratio >= 1 {
			return fmt.Errorf("layout: ratio %v must be between 0 and 1", n.Ratio)
		}
		if err := walk(n.First, depth+1); err != nil {
			return err
		}
		return walk(n.Second, depth+1)
	}
	if err := walk(n, 0); err != nil {
		return err
	}
	for _, v := range views {
		if !seen[v] {
			return fmt.Errorf("layout: view %d is missing from the layout", v)
		}
	}
	return nil
}

// Rect is a rectangle in pixels.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

func (r Rect) inflate(d int) Rect { return Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d} }

// ratioOf returns the effective ratio of a split.
func ratioOf(n *LayoutNode) float64 {
	if n.Ratio <= 0 || n.Ratio >= 1 {
		return 0.5
	}
	return n.Ratio
}

// MinSize is the smallest size the subtree can be laid out in, with dividers
// of the given thickness.
func MinSize(n *LayoutNode, divider int) (w, h int) {
	if n == nil {
		return 0, 0
	}
	if n.IsLeaf() {
		return n.MinWidth, n.MinHeight
	}
	fw, fh := MinSize(n.First, divider)
	sw, sh := MinSize(n.Second, divider)
	if n.Axis == Horizontal {
		return fw + divider + sw, max(fh, sh)
	}
	return max(fw, sw), fh + divider + sh
}

// SplitSizes divides total pixels between two halves separated by a divider.
// ratio is the first half's share of the space left after the divider.
// A half is not made smaller than its minimum while the room allows it; when
// the minimums do not fit, the room is shared in proportion to them.
func SplitSizes(total, divider int, ratio float64, minFirst, minSecond int) (first, second int) {
	avail := total - divider
	if avail <= 0 {
		return 0, 0
	}
	first = int(math.Round(ratio * float64(avail)))
	lo, hi := minFirst, avail-minSecond
	if lo > hi {
		if s := minFirst + minSecond; s > 0 {
			first = int(math.Round(float64(avail) * float64(minFirst) / float64(s)))
		}
	} else {
		first = max(lo, min(first, hi))
	}
	first = max(0, min(first, avail))
	return first, avail - first
}

// Pane is where a view goes.
type Pane struct {
	View ViewRef
	Rect Rect
}

// Divider is a draggable divider. Path names the split it belongs to: the
// empty path is the root, and each "0" or "1" descends into the first or
// second half.
type Divider struct {
	Path string
	Axis Axis
	Rect Rect
}

// Arrangement is a layout resolved to rectangles.
type Arrangement struct {
	Panes    []Pane
	Dividers []Divider
}

// Arrange resolves the tree inside bounds. Dividers are divider pixels thick.
func Arrange(root *LayoutNode, bounds Rect, divider int) Arrangement {
	var out Arrangement
	var walk func(n *LayoutNode, r Rect, path string)
	walk = func(n *LayoutNode, r Rect, path string) {
		if n == nil {
			return
		}
		if n.IsLeaf() {
			out.Panes = append(out.Panes, Pane{n.View, r})
			return
		}
		fw, fh := MinSize(n.First, divider)
		sw, sh := MinSize(n.Second, divider)
		if n.Axis == Horizontal {
			a, b := SplitSizes(r.W, divider, ratioOf(n), fw, sw)
			out.Dividers = append(out.Dividers, Divider{path, n.Axis, Rect{r.X + a, r.Y, min(divider, r.W), r.H}})
			walk(n.First, Rect{r.X, r.Y, a, r.H}, path+"0")
			walk(n.Second, Rect{r.X + a + divider, r.Y, b, r.H}, path+"1")
			return
		}
		a, b := SplitSizes(r.H, divider, ratioOf(n), fh, sh)
		out.Dividers = append(out.Dividers, Divider{path, n.Axis, Rect{r.X, r.Y + a, r.W, min(divider, r.H)}})
		walk(n.First, Rect{r.X, r.Y, r.W, a}, path+"0")
		walk(n.Second, Rect{r.X, r.Y + a + divider, r.W, b}, path+"1")
	}
	walk(root, bounds, "")
	return out
}

// HitDivider returns the divider under the point, allowing slop extra pixels
// on each side so that a thin divider is easy to grab.
func (a Arrangement) HitDivider(x, y, slop int) (Divider, bool) {
	for _, d := range a.Dividers {
		if d.Rect.inflate(slop).Contains(x, y) {
			return d, true
		}
	}
	return Divider{}, false
}

// At returns the node at path ("" is the root), or nil.
func (n *LayoutNode) At(path string) *LayoutNode {
	for _, c := range path {
		if n == nil || n.IsLeaf() {
			return nil
		}
		switch c {
		case '0':
			n = n.First
		case '1':
			n = n.Second
		default:
			return nil
		}
	}
	return n
}

// regionOf returns the rectangle the node at path occupies.
func regionOf(root *LayoutNode, bounds Rect, divider int, path string) (Rect, bool) {
	r := bounds
	n := root
	for _, c := range path {
		if n == nil || n.IsLeaf() {
			return Rect{}, false
		}
		fw, fh := MinSize(n.First, divider)
		sw, sh := MinSize(n.Second, divider)
		var a, b int
		horiz := n.Axis == Horizontal
		if horiz {
			a, b = SplitSizes(r.W, divider, ratioOf(n), fw, sw)
		} else {
			a, b = SplitSizes(r.H, divider, ratioOf(n), fh, sh)
		}
		switch c {
		case '0':
			n = n.First
			if horiz {
				r = Rect{r.X, r.Y, a, r.H}
			} else {
				r = Rect{r.X, r.Y, r.W, a}
			}
		case '1':
			n = n.Second
			if horiz {
				r = Rect{r.X + a + divider, r.Y, b, r.H}
			} else {
				r = Rect{r.X, r.Y + a + divider, r.W, b}
			}
		default:
			return Rect{}, false
		}
	}
	return r, true
}

// DragTo computes the ratio that puts the divider of the split at path under
// the pointer coordinate pos (x for a horizontal split, y for a vertical one,
// in the same space as bounds), keeping both halves at least as large as
// their minimums. It does not change the tree; see SetRatio.
func DragTo(root *LayoutNode, bounds Rect, divider int, path string, pos int) (float64, error) {
	n := root.At(path)
	if n == nil || n.IsLeaf() {
		return 0, fmt.Errorf("layout: no split at %q", path)
	}
	r, ok := regionOf(root, bounds, divider, path)
	if !ok {
		return 0, fmt.Errorf("layout: no split at %q", path)
	}
	fw, fh := MinSize(n.First, divider)
	sw, sh := MinSize(n.Second, divider)
	var start, total, minF, minS int
	if n.Axis == Horizontal {
		start, total, minF, minS = r.X, r.W, fw, sw
	} else {
		start, total, minF, minS = r.Y, r.H, fh, sh
	}
	avail := total - divider
	if avail <= 0 {
		return ratioOf(n), nil
	}
	first := pos - start - divider/2
	lo, hi := minF, avail-minS
	if lo > hi {
		return ratioOf(n), nil
	}
	first = max(lo, min(first, hi))
	return float64(first) / float64(avail), nil
}

// SetRatio sets the ratio of the split at path.
func (n *LayoutNode) SetRatio(path string, ratio float64) error {
	s := n.At(path)
	if s == nil || s.IsLeaf() {
		return fmt.Errorf("layout: no split at %q", path)
	}
	if math.IsNaN(ratio) || ratio <= 0 || ratio >= 1 {
		return fmt.Errorf("layout: ratio %v must be between 0 and 1", ratio)
	}
	s.Ratio = ratio
	return nil
}

// Drag moves the divider of the split at path under the pointer coordinate
// pos and stores the resulting ratio in the tree.
func (n *LayoutNode) Drag(bounds Rect, divider int, path string, pos int) (float64, error) {
	r, err := DragTo(n, bounds, divider, path, pos)
	if err != nil {
		return 0, err
	}
	return r, n.SetRatio(path, r)
}

// SplitLeaf returns a copy of root in which the leaf for beside is replaced
// by a split of that leaf and a new leaf for nv, placed as o says.
func SplitLeaf(root *LayoutNode, beside, nv ViewRef, o ViewOptions) (*LayoutNode, error) {
	if o.Share < 0 || o.Share >= 1 || math.IsNaN(o.Share) {
		return nil, fmt.Errorf("layout: share %v must be between 0 and 1", o.Share)
	}
	axis := o.Axis
	if axis == "" {
		axis = Horizontal
	}
	if axis != Horizontal && axis != Vertical {
		return nil, fmt.Errorf("layout: axis %q must be %q or %q", axis, Horizontal, Vertical)
	}
	share := o.Share
	if share == 0 {
		share = 0.5
	}
	out := root.Clone()
	done := false
	var walk func(n *LayoutNode, depth int) *LayoutNode
	walk = func(n *LayoutNode, depth int) *LayoutNode {
		if n == nil || done {
			return n
		}
		if n.IsLeaf() {
			if n.View != beside {
				return n
			}
			done = true
			fresh := &LayoutNode{View: nv, MinWidth: o.MinWidth, MinHeight: o.MinHeight}
			if o.NewFirst {
				return &LayoutNode{Axis: axis, Ratio: share, First: fresh, Second: n}
			}
			return &LayoutNode{Axis: axis, Ratio: 1 - share, First: n, Second: fresh}
		}
		n.First = walk(n.First, depth+1)
		n.Second = walk(n.Second, depth+1)
		return n
	}
	out = walk(out, 0)
	if !done {
		return nil, fmt.Errorf("layout: view %d is not in the layout", beside)
	}
	if d := depthOf(out); d > MaxLayoutDepth {
		return nil, fmt.Errorf("layout: deeper than %d splits", MaxLayoutDepth)
	}
	return out, nil
}

func depthOf(n *LayoutNode) int {
	if n == nil || n.IsLeaf() {
		return 0
	}
	return 1 + max(depthOf(n.First), depthOf(n.Second))
}

// RemoveLeaf returns a copy of root without the leaf for v; the leaf's sibling
// takes the place of their parent split. The only remaining leaf cannot be
// removed.
func RemoveLeaf(root *LayoutNode, v ViewRef) (*LayoutNode, error) {
	if root.IsLeaf() {
		return nil, errors.New("layout: cannot remove the only view")
	}
	found := false
	var walk func(n *LayoutNode) *LayoutNode
	walk = func(n *LayoutNode) *LayoutNode {
		if n == nil || n.IsLeaf() {
			return n
		}
		if n.First.IsLeaf() && n.First.View == v {
			found = true
			return n.Second
		}
		if n.Second.IsLeaf() && n.Second.View == v {
			found = true
			return n.First
		}
		n.First = walk(n.First)
		n.Second = walk(n.Second)
		return n
	}
	out := walk(root.Clone())
	if !found {
		return nil, fmt.Errorf("layout: view %d is not in the layout", v)
	}
	return out, nil
}

// Ratios maps the path of every split to its effective ratio.
func (n *LayoutNode) Ratios() map[string]float64 {
	out := map[string]float64{}
	var walk func(n *LayoutNode, path string)
	walk = func(n *LayoutNode, path string) {
		if n == nil || n.IsLeaf() {
			return
		}
		out[path] = ratioOf(n)
		walk(n.First, path+"0")
		walk(n.Second, path+"1")
	}
	walk(n, "")
	return out
}

// SetMin sets the minimum size of the leaf for v. It reports whether the
// leaf exists.
func (n *LayoutNode) SetMin(v ViewRef, w, h int) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		if n.View == v {
			n.MinWidth, n.MinHeight = w, h
			return true
		}
		return false
	}
	return n.First.SetMin(v, w, h) || n.Second.SetMin(v, w, h)
}
