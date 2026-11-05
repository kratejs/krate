package ogimage

import (
	"image"
	"math"
	"strings"
)

type node struct {
	tag      string
	text     string
	src      string
	st       style
	children []*node
	img      image.Image
	path     string

	// results: border-box position (relative to the parent's border box) and size
	x, y, w, h float64
	run        *textRun
}

// elementTags are the tags the renderer accepts; every one is a flex box.
var elementTags = map[string]bool{
	"div": true, "span": true, "p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"b": true, "strong": true, "i": true, "em": true, "section": true, "article": true, "header": true, "footer": true,
	"main": true, "nav": true, "ul": true, "ol": true, "li": true, "a": true, "label": true, "small": true, "img": true,
}

var headingSizes = map[string]float64{"h1": 32, "h2": 24, "h3": 19, "h4": 16, "h5": 13, "h6": 11}

type builder struct {
	is *issues
}

func (b *builder) build(n *Node, inherited style, path string) *node {
	if n == nil {
		return nil
	}
	if n.Type == "text" {
		return &node{text: n.Text, st: inherited.inherit(), path: path}
	}
	if !elementTags[n.Tag] {
		b.is.add("%s: <%s> is not supported (supported elements: div, span, p, h1-h6, b, strong, i, em, img and a few semantic aliases)", path, n.Tag)
		return nil
	}
	base := inherited
	switch n.Tag {
	case "b", "strong":
		base.fontWeight = 700
	case "i", "em":
		base.fontStyle = "italic"
	case "h1", "h2", "h3", "h4", "h5", "h6":
		base.fontWeight = 700
		base.fontSize = headingSizes[n.Tag]
	case "small":
		base.fontSize = inherited.fontSize * 0.8
	}
	st := computeStyle(n.Style, base, b.is, path)
	if st.display == "none" {
		return nil
	}
	nd := &node{tag: n.Tag, st: st, path: path, src: n.Src}
	if n.Tag == "img" {
		return nd
	}
	var pending []string
	flush := func() {
		if len(pending) > 0 {
			nd.children = append(nd.children, &node{text: strings.Join(pending, ""), st: st.inherit(), path: path + ">#text"})
			pending = nil
		}
	}
	for i, c := range n.Children {
		if c == nil {
			continue
		}
		if c.Type == "text" {
			pending = append(pending, c.Text)
			continue
		}
		flush()
		if child := b.build(c, st, describePath(path, c.Tag, i)); child != nil {
			nd.children = append(nd.children, child)
		}
	}
	flush()
	return nd
}

func (n *node) isText() bool { return n.tag == "" }

func (n *node) horizontal() bool { return !strings.HasPrefix(n.st.flexDirection, "column") }

func (n *node) outerSides(p *[4]length, base float64) (top, right, bottom, left float64) {
	g := func(l length) float64 { v, _ := l.resolve(base); return v }
	return g(p[0]), g(p[1]), g(p[2]), g(p[3])
}

// inset is padding plus border on each side.
func (n *node) inset(base float64) (top, right, bottom, left float64) {
	pt, pr, pb, pl := n.outerSides(&n.st.padding, base)
	return pt + n.st.border[0], pr + n.st.border[1], pb + n.st.border[2], pl + n.st.border[3]
}

func clampSize(v float64, lo, hi length, base float64) float64 {
	if m, ok := lo.resolve(base); ok {
		v = math.Max(v, m)
	}
	if m, ok := hi.resolve(base); ok {
		v = math.Min(v, m)
	}
	return v
}

type layouter struct {
	fonts *fontSet
	memo  map[measureKey][2]float64
}

type measureKey struct {
	n      *node
	availW float64
}

// measure returns the border-box size of n when its width is limited to availW
// (negative: unconstrained). Explicit width and height win.
func (l *layouter) measure(n *node, availW float64) (float64, float64) {
	key := measureKey{n, availW}
	if v, ok := l.memo[key]; ok {
		return v[0], v[1]
	}
	w, h := l.measureUncached(n, availW)
	l.memo[key] = [2]float64{w, h}
	return w, h
}

func (l *layouter) measureUncached(n *node, availW float64) (float64, float64) {
	if n.isText() {
		run := l.fonts.layoutText(n.text, n.st, availW, n.path)
		return run.w, run.h
	}
	st := n.st
	if n.tag == "img" {
		w, _ := st.width.resolve(availW)
		h, _ := st.height.resolve(-1)
		if n.img != nil {
			b := n.img.Bounds()
			iw, ih := float64(b.Dx()), float64(b.Dy())
			switch {
			case st.width.auto() && st.height.auto():
				w, h = iw, ih
			case st.width.auto():
				w = h * iw / ih
			case st.height.auto():
				h = w * ih / iw
			}
		}
		return clampSize(w, st.minW, st.maxW, availW), clampSize(h, st.minH, st.maxH, -1)
	}
	it, ir, ib, il := n.inset(availW)
	var innerAvail float64 = -1
	fixedW, hasW := st.width.resolve(availW)
	if hasW {
		innerAvail = fixedW - il - ir
	} else if availW >= 0 {
		mt, mr, mb, ml := n.outerSides(&st.margin, availW)
		_ = mt
		_ = mb
		innerAvail = availW - il - ir - mr - ml
	}
	horizontal := n.horizontal()
	var flow []*node
	for _, c := range n.children {
		if c.st.position != "absolute" {
			flow = append(flow, c)
		}
	}
	var cw, ch float64
	gap := st.colGap
	if !horizontal {
		gap = st.rowGap
	}
	if st.flexWrap != "nowrap" && horizontal && innerAvail >= 0 {
		// Wrapped row: lines of items, each as wide as it fits.
		lineW, lineH := 0.0, 0.0
		first := true
		for _, c := range flow {
			w, h := l.outerSize(c, innerAvail)
			if !first && lineW+gap+w > innerAvail+0.01 {
				cw = math.Max(cw, lineW)
				ch += lineH + st.rowGap
				lineW, lineH, first = 0, 0, true
			}
			if !first {
				lineW += gap
			}
			lineW += w
			lineH = math.Max(lineH, h)
			first = false
		}
		cw = math.Max(cw, lineW)
		ch += lineH
	} else if horizontal {
		// A row: items take their natural width, shrink together when they do
		// not fit, and their text wraps at the width they end up with.
		widths := make([]float64, len(flow))
		sum := 0.0
		shrinkTotal := 0.0
		for i, c := range flow {
			w, _ := l.outerSize(c, -1)
			widths[i] = w
			sum += w
			shrinkTotal += c.st.shrink * w
		}
		sum += gap * float64(maxInt(len(flow)-1, 0))
		if innerAvail >= 0 && sum > innerAvail && shrinkTotal > 0 {
			for i, c := range flow {
				widths[i] = math.Max(widths[i]+(innerAvail-sum)*c.st.shrink*widths[i]/shrinkTotal, 0)
			}
			sum = innerAvail
		}
		for i, c := range flow {
			_, h := l.outerSize(c, widths[i])
			ch = math.Max(ch, h)
		}
		cw = sum
	} else {
		for i, c := range flow {
			w, h := l.outerSize(c, innerAvail)
			if i > 0 {
				ch += gap
			}
			ch += h
			cw = math.Max(cw, w)
		}
	}
	w := cw + il + ir
	h := ch + it + ib
	if hasW {
		w = fixedW
	}
	if fh, ok := st.height.resolve(-1); ok {
		h = fh
	}
	return clampSize(w, st.minW, st.maxW, availW), clampSize(h, st.minH, st.maxH, -1)
}

// outerSize is the margin-box size of a child.
func (l *layouter) outerSize(c *node, availW float64) (float64, float64) {
	w, h := l.measure(c, availW)
	mt, mr, mb, ml := c.outerSides(&c.st.margin, availW)
	return w + ml + mr, h + mt + mb
}

type flexItem struct {
	n                 *node
	mt, mr, mb, ml    float64
	base, main, cross float64
	minMain, maxMain  float64
	crossDefinite     bool
	grow, shrink      float64
	frozen            bool
}

// layoutNode places the children of n, whose border box is already w by h.
func (l *layouter) layoutNode(n *node) {
	if n.isText() || n.tag == "img" {
		if n.isText() {
			n.run = l.fonts.layoutText(n.text, n.st, n.w+0.01, n.path)
		}
		return
	}
	st := n.st
	it, ir, ib, il := n.inset(n.w)
	innerW := n.w - il - ir
	innerH := n.h - it - ib
	horizontal := n.horizontal()
	reverse := strings.HasSuffix(st.flexDirection, "reverse")
	mainAvail, crossAvail := innerW, innerH
	mainGap, crossGap := st.colGap, st.rowGap
	if !horizontal {
		mainAvail, crossAvail = innerH, innerW
		mainGap, crossGap = st.rowGap, st.colGap
	}

	var items []*flexItem
	var abs []*node
	for _, c := range n.children {
		if c.st.position == "absolute" {
			abs = append(abs, c)
			continue
		}
		mt, mr, mb, ml := c.outerSides(&c.st.margin, innerW)
		it := &flexItem{n: c, mt: mt, mr: mr, mb: mb, ml: ml, grow: c.st.grow, shrink: c.st.shrink}
		// Base main size.
		var mainDef length
		if horizontal {
			mainDef = c.st.width
		} else {
			mainDef = c.st.height
		}
		basis := c.st.basis
		if basis.auto() {
			basis = mainDef
		}
		if v, ok := basis.resolve(mainAvail); ok {
			it.base = v
		} else {
			mw, mh := l.measure(c, innerW)
			if horizontal {
				// Natural width: measure unconstrained for rows.
				mw, _ = l.measure(c, -1)
				it.base = mw
			} else {
				it.base = mh
			}
		}
		minL, maxL := c.st.minW, c.st.maxW
		if !horizontal {
			minL, maxL = c.st.minH, c.st.maxH
		}
		it.minMain, it.maxMain = 0, math.Inf(1)
		if v, ok := minL.resolve(mainAvail); ok {
			it.minMain = v
		}
		if v, ok := maxL.resolve(mainAvail); ok {
			it.maxMain = v
		}
		it.base = math.Min(math.Max(it.base, it.minMain), it.maxMain)
		items = append(items, it)
	}

	// Lines.
	var lines [][]*flexItem
	if st.flexWrap != "nowrap" {
		var cur []*flexItem
		sum := 0.0
		for _, it := range items {
			outer := it.base + it.marginMain(horizontal)
			if len(cur) > 0 && sum+mainGap+outer > mainAvail+0.01 {
				lines = append(lines, cur)
				cur, sum = nil, 0
			}
			if len(cur) > 0 {
				sum += mainGap
			}
			sum += outer
			cur = append(cur, it)
		}
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
	} else if len(items) > 0 {
		lines = [][]*flexItem{items}
	}

	type lineBox struct {
		items []*flexItem
		cross float64
	}
	var boxes []lineBox
	for _, line := range lines {
		// Resolve flexible lengths.
		used := 0.0
		for i, it := range line {
			used += it.base + it.marginMain(horizontal)
			if i > 0 {
				used += mainGap
			}
		}
		free := mainAvail - used
		for _, it := range line {
			it.main = it.base
		}
		if free > 0 {
			total := 0.0
			for _, it := range line {
				total += it.grow
			}
			if total > 0 {
				for _, it := range line {
					it.main = math.Min(it.base+free*it.grow/total, it.maxMain)
				}
			}
		} else if free < 0 {
			total := 0.0
			for _, it := range line {
				total += it.shrink * it.base
			}
			if total > 0 {
				for _, it := range line {
					it.main = math.Max(it.base+free*(it.shrink*it.base)/total, it.minMain)
				}
			}
		}
		// Cross sizes.
		lineCross := 0.0
		for _, it := range line {
			c := it.n
			var crossDef length
			if horizontal {
				crossDef = c.st.height
			} else {
				crossDef = c.st.width
			}
			if v, ok := crossDef.resolve(crossAvail); ok {
				it.cross, it.crossDefinite = v, true
			} else {
				w := it.main
				if !horizontal {
					w = innerW - it.ml - it.mr
				}
				mw, mh := l.measure(c, w+it.ml+it.mr)
				if horizontal {
					_ = mw
					it.cross = mh
				} else {
					it.cross = mw
				}
			}
			lineCross = math.Max(lineCross, it.cross+it.marginCross(horizontal))
		}
		boxes = append(boxes, lineBox{line, lineCross})
	}
	// A single line of a container with a definite cross size fills it.
	if len(boxes) == 1 {
		boxes[0].cross = crossAvail
	} else if len(boxes) > 1 {
		total := 0.0
		for _, b := range boxes {
			total += b.cross
		}
		total += crossGap * float64(len(boxes)-1)
		if extra := crossAvail - total; extra > 0 {
			for i := range boxes {
				boxes[i].cross += extra / float64(len(boxes))
			}
		}
	}

	crossPos := 0.0
	for _, lb := range boxes {
		line := lb.items
		used := 0.0
		for i, it := range line {
			used += it.main + it.marginMain(horizontal)
			if i > 0 {
				used += mainGap
			}
		}
		free := math.Max(mainAvail-used, 0)
		pos, between := 0.0, mainGap
		switch st.justify {
		case "flex-end":
			pos = free
		case "center":
			pos = free / 2
		case "space-between":
			if len(line) > 1 {
				between = mainGap + free/float64(len(line)-1)
			}
		case "space-around":
			share := free / float64(len(line))
			pos, between = share/2, mainGap+share
		case "space-evenly":
			share := free / float64(len(line)+1)
			pos, between = share, mainGap+share
		}
		for idx, it := range line {
			c := it.n
			ordered := idx
			if reverse {
				ordered = len(line) - 1 - idx
			}
			_ = ordered
			align := st.alignItems
			if c.st.alignSelf != "" && c.st.alignSelf != "auto" {
				align = c.st.alignSelf
			}
			crossSize := it.cross
			if align == "stretch" && !it.crossDefinite {
				crossSize = lb.cross - it.marginCross(horizontal)
				if maxL := c.st.maxH; horizontal {
					if v, ok := maxL.resolve(-1); ok {
						crossSize = math.Min(crossSize, v)
					}
				}
			}
			offset := 0.0
			switch align {
			case "flex-end":
				offset = lb.cross - crossSize - it.marginCross(horizontal)
			case "center":
				offset = (lb.cross - crossSize - it.marginCross(horizontal)) / 2
			}
			mainStart := pos + it.mainStartMargin(horizontal)
			crossStart := crossPos + offset + it.crossStartMargin(horizontal)
			if horizontal {
				c.x, c.y, c.w, c.h = il+mainStart, it_(it, n)+crossStart, it.main, crossSize
			} else {
				c.x, c.y, c.w, c.h = il+crossStart, it_(it, n)+mainStart, crossSize, it.main
			}
			pos += it.main + it.marginMain(horizontal) + between
		}
		if reverse {
			// Mirror along the main axis.
			for _, it := range line {
				c := it.n
				if horizontal {
					c.x = il + (innerW - (c.x - il) - c.w)
				} else {
					c.y = it_(it, n) + (innerH - (c.y - it_(it, n)) - c.h)
				}
			}
		}
		crossPos += lb.cross + crossGap
	}
	if st.flexWrap == "wrap-reverse" && len(boxes) > 1 {
		for _, lb := range boxes {
			for _, it := range lb.items {
				c := it.n
				if horizontal {
					c.y = it_(it, n) + (innerH - (c.y - it_(it, n)) - c.h)
				} else {
					c.x = il + (innerW - (c.x - il) - c.w)
				}
			}
		}
	}
	for _, c := range n.children {
		if c.st.position == "absolute" {
			l.placeAbsolute(n, c, il, it_top(n, innerW), innerW, innerH)
		}
	}
	for _, c := range n.children {
		l.layoutNode(c)
	}
}

func it_(_ *flexItem, n *node) float64 {
	t, _, _, _ := n.inset(n.w)
	return t
}

func it_top(n *node, _ float64) float64 {
	t, _, _, _ := n.inset(n.w)
	return t
}

func (it *flexItem) marginMain(horizontal bool) float64 {
	if horizontal {
		return it.ml + it.mr
	}
	return it.mt + it.mb
}

func (it *flexItem) marginCross(horizontal bool) float64 {
	if horizontal {
		return it.mt + it.mb
	}
	return it.ml + it.mr
}

func (it *flexItem) mainStartMargin(horizontal bool) float64 {
	if horizontal {
		return it.ml
	}
	return it.mt
}

func (it *flexItem) crossStartMargin(horizontal bool) float64 {
	if horizontal {
		return it.mt
	}
	return it.ml
}

// placeAbsolute sizes and positions an absolutely positioned child against the
// padding box of its parent (ox, oy is the content origin).
func (l *layouter) placeAbsolute(parent, c *node, ox, oy, innerW, innerH float64) {
	st := c.st
	left, hasL := st.left.resolve(innerW)
	right, hasR := st.right.resolve(innerW)
	top, hasT := st.top.resolve(innerH)
	bottom, hasB := st.bottom.resolve(innerH)
	w, hasW := st.width.resolve(innerW)
	h, hasH := st.height.resolve(innerH)
	if !hasW {
		if hasL && hasR {
			w, hasW = innerW-left-right, true
		} else {
			mw, _ := l.measure(c, -1)
			w = mw
		}
	}
	if !hasH {
		if hasT && hasB {
			h, hasH = innerH-top-bottom, true
		} else {
			_, mh := l.measure(c, w)
			h = mh
		}
	}
	x, y := 0.0, 0.0
	switch {
	case hasL:
		x = left
	case hasR:
		x = innerW - right - w
	}
	switch {
	case hasT:
		y = top
	case hasB:
		y = innerH - bottom - h
	}
	c.x, c.y, c.w, c.h = ox+x, oy+y, w, h
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
