package ogimage

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// length is a CSS length: auto, pixels or a percentage of the parent.
type length struct {
	v    float64
	unit byte // 0 auto, 'p' px, '%' percent
}

func px(v float64) length { return length{v, 'p'} }

func (l length) auto() bool { return l.unit == 0 }

// resolve returns the pixel value against base (the parent's size, or -1 when unknown).
func (l length) resolve(base float64) (float64, bool) {
	switch l.unit {
	case 'p':
		return l.v, true
	case '%':
		if base >= 0 {
			return base * l.v / 100, true
		}
	}
	return 0, false
}

type gradientStop struct {
	c   rgba
	pos float64 // 0..1, -1 until resolved
}

type gradient struct {
	radial bool
	angle  float64 // degrees, CSS convention (0 = to top)
	stops  []gradientStop
}

type shadow struct {
	x, y, blur, spread float64
	c                  rgba
}

type style struct {
	display                               string
	flexDirection                         string
	flexWrap                              string
	justify, alignItems, alignSelf        string
	grow, shrink                          float64
	basis                                 length
	width, height, minW, minH, maxW, maxH length
	margin, padding                       [4]length // top, right, bottom, left
	border                                [4]float64
	borderColor                           rgba
	position                              string
	top, right, bottom, left              length
	rowGap, colGap                        float64
	overflow                              string
	bg                                    rgba
	bgImages                              []gradient
	opacity                               float64
	radius                                [4]float64 // tl, tr, br, bl
	shadows                               []shadow
	color                                 rgba
	fontFamily                            []string
	fontSize                              float64
	fontWeight                            int
	fontStyle                             string
	lineHeight                            float64 // multiplier when > 0 and lineHeightPx == 0
	lineHeightPx                          float64
	letterSpacing                         float64
	textAlign                             string
	textTransform                         string
	whiteSpace                            string
	textOverflow                          string
	objectFit                             string
}

func defaultStyle() style {
	return style{
		display: "flex", flexDirection: "row", flexWrap: "nowrap", justify: "flex-start", alignItems: "stretch",
		shrink: 1, position: "relative", opacity: 1, color: rgba{0, 0, 0, 1}, fontFamily: []string{"sans-serif"},
		fontSize: 16, fontWeight: 400, fontStyle: "normal", textAlign: "left", textTransform: "none", whiteSpace: "normal",
		objectFit: "fill", overflow: "visible",
	}
}

// inherit copies the properties that cascade into a child.
func (s style) inherit() style {
	c := defaultStyle()
	c.color, c.fontFamily, c.fontSize, c.fontWeight, c.fontStyle = s.color, s.fontFamily, s.fontSize, s.fontWeight, s.fontStyle
	c.lineHeight, c.lineHeightPx, c.letterSpacing, c.textAlign = s.lineHeight, s.lineHeightPx, s.letterSpacing, s.textAlign
	c.textTransform, c.whiteSpace, c.textOverflow = s.textTransform, s.whiteSpace, s.textOverflow
	return c
}

// ignoredProps are accepted and have no visual effect here.
var ignoredProps = map[string]bool{
	"boxSizing": true, "cursor": true, "pointerEvents": true, "userSelect": true, "wordBreak": true,
	"textRendering": true, "WebkitFontSmoothing": true, "fontSmooth": true, "fontKerning": true,
}

type parser struct {
	st *style
	is *issues
	at string
}

func (p *parser) bad(prop string, v any) {
	p.is.add("%s: %s: %v is not supported", p.at, prop, v)
}

func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// parseLength reads 12, "12px", "50%", "auto" (rem and em are 16 and the font size).
func (p *parser) parseLength(prop string, v any, fontSize float64) (length, bool) {
	if n, ok := num(v); ok {
		return px(n), true
	}
	s, ok := v.(string)
	if !ok {
		p.bad(prop, v)
		return length{}, false
	}
	s = strings.TrimSpace(s)
	switch {
	case s == "auto" || s == "":
		return length{}, true
	case strings.HasSuffix(s, "px"):
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64); err == nil {
			return px(f), true
		}
	case strings.HasSuffix(s, "%"):
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64); err == nil {
			return length{f, '%'}, true
		}
	case strings.HasSuffix(s, "rem"):
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "rem"), 64); err == nil {
			return px(f * 16), true
		}
	case strings.HasSuffix(s, "em"):
		if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "em"), 64); err == nil {
			return px(f * fontSize), true
		}
	default:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return px(f), true
		}
	}
	p.bad(prop, v)
	return length{}, false
}

func (p *parser) pxValue(prop string, v any, fontSize float64) (float64, bool) {
	l, ok := p.parseLength(prop, v, fontSize)
	if !ok {
		return 0, false
	}
	if l.unit == '%' {
		p.bad(prop, v)
		return 0, false
	}
	return l.v, true
}

// boxValues expands a 1 to 4 value shorthand to top, right, bottom, left.
func boxValues(vals []string) ([4]string, bool) {
	switch len(vals) {
	case 1:
		return [4]string{vals[0], vals[0], vals[0], vals[0]}, true
	case 2:
		return [4]string{vals[0], vals[1], vals[0], vals[1]}, true
	case 3:
		return [4]string{vals[0], vals[1], vals[2], vals[1]}, true
	case 4:
		return [4]string{vals[0], vals[1], vals[2], vals[3]}, true
	}
	return [4]string{}, false
}

func fieldsOf(v any) []string {
	if n, ok := num(v); ok {
		return []string{strconv.FormatFloat(n, 'f', -1, 64)}
	}
	if s, ok := v.(string); ok {
		return strings.Fields(s)
	}
	return nil
}

// computeStyle reads the style object of an element over the inherited style.
func computeStyle(raw map[string]any, inherited style, is *issues, at string) style {
	st := inherited.inherit()
	p := &parser{st: &st, is: is, at: at}
	// font size first: em units depend on it.
	if v, ok := raw["fontSize"]; ok {
		if f, ok := p.pxValue("fontSize", v, inherited.fontSize); ok {
			st.fontSize = f
		}
	}
	for prop, v := range raw {
		if prop == "fontSize" || ignoredProps[prop] {
			continue
		}
		p.apply(prop, v)
	}
	return st
}

func (p *parser) color(prop string, v any) (rgba, bool) {
	s, ok := v.(string)
	if !ok {
		p.bad(prop, v)
		return rgba{}, false
	}
	if c, ok := parseColor(s); ok {
		return c, true
	}
	if strings.EqualFold(strings.TrimSpace(s), "currentcolor") {
		return p.st.color, true
	}
	p.bad(prop, v)
	return rgba{}, false
}

func (p *parser) enum(prop string, v any, allowed ...string) (string, bool) {
	s, _ := v.(string)
	for _, a := range allowed {
		if s == a {
			return s, true
		}
	}
	p.bad(prop, v)
	return "", false
}

func (p *parser) apply(prop string, v any) {
	st := p.st
	fs := st.fontSize
	switch prop {
	case "display":
		if s, ok := p.enum(prop, v, "flex", "none"); ok {
			st.display = s
		}
	case "flexDirection":
		if s, ok := p.enum(prop, v, "row", "column", "row-reverse", "column-reverse"); ok {
			st.flexDirection = s
		}
	case "flexWrap":
		if s, ok := p.enum(prop, v, "nowrap", "wrap", "wrap-reverse"); ok {
			st.flexWrap = s
		}
	case "justifyContent":
		if s, ok := p.enum(prop, v, "flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly"); ok {
			st.justify = s
		}
	case "alignItems":
		if s, ok := p.enum(prop, v, "stretch", "flex-start", "flex-end", "center", "baseline"); ok {
			st.alignItems = s
		}
	case "alignSelf":
		if s, ok := p.enum(prop, v, "auto", "stretch", "flex-start", "flex-end", "center", "baseline"); ok {
			st.alignSelf = s
		}
	case "flexGrow":
		if f, ok := num(v); ok {
			st.grow = f
		} else {
			p.bad(prop, v)
		}
	case "flexShrink":
		if f, ok := num(v); ok {
			st.shrink = f
		} else {
			p.bad(prop, v)
		}
	case "flexBasis":
		if l, ok := p.parseLength(prop, v, fs); ok {
			st.basis = l
		}
	case "flex":
		p.flexShorthand(v)
	case "width":
		st.width, _ = p.parseLength(prop, v, fs)
	case "height":
		st.height, _ = p.parseLength(prop, v, fs)
	case "minWidth":
		st.minW, _ = p.parseLength(prop, v, fs)
	case "minHeight":
		st.minH, _ = p.parseLength(prop, v, fs)
	case "maxWidth":
		st.maxW, _ = p.parseLength(prop, v, fs)
	case "maxHeight":
		st.maxH, _ = p.parseLength(prop, v, fs)
	case "margin", "padding":
		vals, ok := boxValues(fieldsOf(v))
		if !ok {
			p.bad(prop, v)
			return
		}
		for i, s := range vals {
			l, _ := p.parseLength(prop, s, fs)
			if prop == "margin" {
				st.margin[i] = l
			} else {
				st.padding[i] = l
			}
		}
	case "marginTop", "marginRight", "marginBottom", "marginLeft", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft":
		side := map[string]int{"Top": 0, "Right": 1, "Bottom": 2, "Left": 3}[strings.TrimPrefix(strings.TrimPrefix(prop, "margin"), "padding")]
		l, _ := p.parseLength(prop, v, fs)
		if strings.HasPrefix(prop, "margin") {
			st.margin[side] = l
		} else {
			st.padding[side] = l
		}
	case "position":
		if s, ok := p.enum(prop, v, "relative", "absolute"); ok {
			st.position = s
		}
	case "top":
		st.top, _ = p.parseLength(prop, v, fs)
	case "right":
		st.right, _ = p.parseLength(prop, v, fs)
	case "bottom":
		st.bottom, _ = p.parseLength(prop, v, fs)
	case "left":
		st.left, _ = p.parseLength(prop, v, fs)
	case "gap":
		vals := fieldsOf(v)
		if len(vals) == 0 || len(vals) > 2 {
			p.bad(prop, v)
			return
		}
		r, _ := p.pxValue(prop, vals[0], fs)
		c := r
		if len(vals) == 2 {
			c, _ = p.pxValue(prop, vals[1], fs)
		}
		st.rowGap, st.colGap = r, c
	case "rowGap":
		st.rowGap, _ = p.pxValue(prop, v, fs)
	case "columnGap":
		st.colGap, _ = p.pxValue(prop, v, fs)
	case "overflow":
		if s, ok := p.enum(prop, v, "visible", "hidden"); ok {
			st.overflow = s
		}
	case "backgroundColor":
		if c, ok := p.color(prop, v); ok {
			st.bg = c
		}
	case "background":
		p.background(v)
	case "backgroundImage":
		p.backgroundImage(prop, v)
	case "opacity":
		if f, ok := num(v); ok {
			st.opacity = math.Max(0, math.Min(1, f))
		} else {
			p.bad(prop, v)
		}
	case "borderRadius":
		vals, ok := boxValues(fieldsOf(v))
		if !ok {
			p.bad(prop, v)
			return
		}
		for i, s := range vals {
			st.radius[i], _ = p.pxValue(prop, s, fs)
		}
	case "borderTopLeftRadius", "borderTopRightRadius", "borderBottomRightRadius", "borderBottomLeftRadius":
		idx := map[string]int{"borderTopLeftRadius": 0, "borderTopRightRadius": 1, "borderBottomRightRadius": 2, "borderBottomLeftRadius": 3}[prop]
		st.radius[idx], _ = p.pxValue(prop, v, fs)
	case "border":
		p.borderShorthand(v, [4]bool{true, true, true, true})
	case "borderTop":
		p.borderShorthand(v, [4]bool{true})
	case "borderRight":
		p.borderShorthand(v, [4]bool{false, true})
	case "borderBottom":
		p.borderShorthand(v, [4]bool{false, false, true})
	case "borderLeft":
		p.borderShorthand(v, [4]bool{false, false, false, true})
	case "borderWidth":
		vals, ok := boxValues(fieldsOf(v))
		if !ok {
			p.bad(prop, v)
			return
		}
		for i, s := range vals {
			st.border[i], _ = p.pxValue(prop, s, fs)
		}
	case "borderColor":
		if c, ok := p.color(prop, v); ok {
			st.borderColor = c
		}
	case "borderStyle":
		p.enum(prop, v, "solid")
	case "boxShadow":
		p.boxShadow(v)
	case "color":
		if c, ok := p.color(prop, v); ok {
			st.color = c
		}
	case "fontFamily":
		if s, ok := v.(string); ok {
			var fams []string
			for _, f := range strings.Split(s, ",") {
				fams = append(fams, strings.Trim(strings.TrimSpace(f), `"'`))
			}
			st.fontFamily = fams
		} else {
			p.bad(prop, v)
		}
	case "fontWeight":
		if f, ok := num(v); ok {
			st.fontWeight = int(f)
			return
		}
		switch w := v.(type) {
		case string:
			switch w {
			case "normal":
				st.fontWeight = 400
			case "bold":
				st.fontWeight = 700
			default:
				if n, err := strconv.Atoi(w); err == nil {
					st.fontWeight = n
				} else {
					p.bad(prop, v)
				}
			}
		default:
			p.bad(prop, v)
		}
	case "fontStyle":
		if s, ok := p.enum(prop, v, "normal", "italic"); ok {
			st.fontStyle = s
		}
	case "lineHeight":
		if f, ok := num(v); ok {
			st.lineHeight, st.lineHeightPx = f, 0
		} else if s, ok := v.(string); ok && s == "normal" {
			st.lineHeight, st.lineHeightPx = 0, 0
		} else if l, ok := p.parseLength(prop, v, fs); ok && l.unit == 'p' {
			st.lineHeight, st.lineHeightPx = 0, l.v
		}
	case "letterSpacing":
		st.letterSpacing, _ = p.pxValue(prop, v, fs)
	case "textAlign":
		if s, ok := p.enum(prop, v, "left", "center", "right", "start", "end"); ok {
			st.textAlign = map[string]string{"start": "left", "end": "right"}[s]
			if st.textAlign == "" {
				st.textAlign = s
			}
		}
	case "textTransform":
		if s, ok := p.enum(prop, v, "none", "uppercase", "lowercase", "capitalize"); ok {
			st.textTransform = s
		}
	case "whiteSpace":
		if s, ok := p.enum(prop, v, "normal", "nowrap", "pre", "pre-wrap"); ok {
			st.whiteSpace = s
		}
	case "textOverflow":
		if s, ok := p.enum(prop, v, "clip", "ellipsis"); ok {
			st.textOverflow = s
		}
	case "objectFit":
		if s, ok := p.enum(prop, v, "fill", "contain", "cover"); ok {
			st.objectFit = s
		}
	default:
		p.is.add("%s: the style property %q is not supported", p.at, prop)
	}
}

func (p *parser) flexShorthand(v any) {
	st := p.st
	if f, ok := num(v); ok {
		st.grow, st.shrink, st.basis = f, 1, px(0)
		return
	}
	s, ok := v.(string)
	if !ok {
		p.bad("flex", v)
		return
	}
	switch strings.TrimSpace(s) {
	case "none":
		st.grow, st.shrink, st.basis = 0, 0, length{}
		return
	case "auto":
		st.grow, st.shrink, st.basis = 1, 1, length{}
		return
	}
	parts := strings.Fields(s)
	st.grow, st.shrink, st.basis = 1, 1, px(0)
	for i, part := range parts {
		if f, err := strconv.ParseFloat(part, 64); err == nil && i < 2 {
			if i == 0 {
				st.grow = f
			} else {
				st.shrink = f
			}
			continue
		}
		if l, ok := p.parseLength("flex", part, st.fontSize); ok {
			st.basis = l
		}
	}
}

func (p *parser) borderShorthand(v any, sides [4]bool) {
	st := p.st
	s, ok := v.(string)
	if n, isNum := num(v); isNum {
		s, ok = strconv.FormatFloat(n, 'f', -1, 64)+"px solid black", true
	}
	if !ok {
		p.bad("border", v)
		return
	}
	width, col := 1.0, st.color
	for _, part := range splitSpaces(s) {
		switch {
		case part == "solid":
		case part == "none" || part == "hidden":
			width = 0
		case part == "dashed" || part == "dotted" || part == "double":
			p.bad("border", part)
		default:
			if c, ok := parseColor(part); ok {
				col = c
			} else if l, ok := p.parseLength("border", part, st.fontSize); ok && l.unit == 'p' {
				width = l.v
			}
		}
	}
	for i, on := range sides {
		if on {
			st.border[i] = width
		}
	}
	st.borderColor = col
}

// splitSpaces splits on spaces outside parentheses ("1px solid rgb(0, 0, 0)").
func splitSpaces(s string) []string {
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
		}
		if c == ' ' && depth == 0 {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func (p *parser) background(v any) {
	s, ok := v.(string)
	if !ok {
		p.bad("background", v)
		return
	}
	if c, ok := parseColor(s); ok {
		p.st.bg = c
		return
	}
	p.backgroundImage("background", v)
}

func (p *parser) backgroundImage(prop string, v any) {
	s, ok := v.(string)
	if !ok {
		p.bad(prop, v)
		return
	}
	// Layers are separated by top-level commas, but commas also separate
	// gradient arguments, so split on "), " boundaries.
	var layers []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				layers = append(layers, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	layers = append(layers, strings.TrimSpace(s[start:]))
	for _, l := range layers {
		g, ok := p.gradient(l)
		if !ok {
			p.bad(prop, l)
			continue
		}
		p.st.bgImages = append(p.st.bgImages, g)
	}
}

func (p *parser) gradient(s string) (gradient, bool) {
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return gradient{}, false
	}
	fn := strings.TrimSpace(s[:open])
	args := splitArgs(s[open+1 : len(s)-1])
	g := gradient{angle: 180}
	switch fn {
	case "linear-gradient", "repeating-linear-gradient":
		if fn != "linear-gradient" {
			return gradient{}, false
		}
	case "radial-gradient":
		g.radial = true
	default:
		return gradient{}, false
	}
	if len(args) > 0 {
		first := strings.TrimSpace(args[0])
		if !g.radial && strings.HasSuffix(first, "deg") {
			if a, err := strconv.ParseFloat(strings.TrimSuffix(first, "deg"), 64); err == nil {
				g.angle = a
				args = args[1:]
			}
		} else if !g.radial && strings.HasPrefix(first, "to ") {
			dirs := map[string]float64{"to top": 0, "to right": 90, "to bottom": 180, "to left": 270, "to top right": 45, "to right top": 45, "to bottom right": 135, "to right bottom": 135, "to bottom left": 225, "to left bottom": 225, "to top left": 315, "to left top": 315}
			a, ok := dirs[first]
			if !ok {
				return gradient{}, false
			}
			g.angle = a
			args = args[1:]
		} else if g.radial && (strings.HasPrefix(first, "circle") || strings.HasPrefix(first, "ellipse") || strings.HasPrefix(first, "closest") || strings.HasPrefix(first, "farthest") || strings.HasPrefix(first, "at ")) {
			args = args[1:]
		}
	}
	for _, a := range args {
		parts := splitSpaces(a)
		if len(parts) == 0 {
			return gradient{}, false
		}
		c, ok := parseColor(parts[0])
		if !ok {
			return gradient{}, false
		}
		stop := gradientStop{c: c, pos: -1}
		if len(parts) > 1 {
			if !strings.HasSuffix(parts[1], "%") {
				return gradient{}, false
			}
			f, err := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
			if err != nil {
				return gradient{}, false
			}
			stop.pos = f / 100
		}
		g.stops = append(g.stops, stop)
	}
	if len(g.stops) < 2 {
		return gradient{}, false
	}
	// Unplaced stops spread evenly between their neighbours.
	if g.stops[0].pos < 0 {
		g.stops[0].pos = 0
	}
	if g.stops[len(g.stops)-1].pos < 0 {
		g.stops[len(g.stops)-1].pos = 1
	}
	for i := 1; i < len(g.stops)-1; i++ {
		if g.stops[i].pos >= 0 {
			continue
		}
		j := i
		for g.stops[j].pos < 0 {
			j++
		}
		lo, hi := g.stops[i-1].pos, g.stops[j].pos
		for k := i; k < j; k++ {
			g.stops[k].pos = lo + (hi-lo)*float64(k-i+1)/float64(j-i+1)
		}
	}
	return g, true
}

func (p *parser) boxShadow(v any) {
	s, ok := v.(string)
	if !ok {
		p.bad("boxShadow", v)
		return
	}
	if strings.TrimSpace(s) == "none" {
		return
	}
	for _, layer := range splitArgs(s) {
		var sh shadow
		sh.c = rgba{0, 0, 0, 1}
		var lens []float64
		for _, part := range splitSpaces(layer) {
			if part == "inset" {
				p.bad("boxShadow", "inset")
				return
			}
			if c, ok := parseColor(part); ok {
				sh.c = c
				continue
			}
			l, ok := p.parseLength("boxShadow", part, p.st.fontSize)
			if !ok || l.unit != 'p' {
				return
			}
			lens = append(lens, l.v)
		}
		if len(lens) < 2 || len(lens) > 4 {
			p.bad("boxShadow", layer)
			return
		}
		sh.x, sh.y = lens[0], lens[1]
		if len(lens) > 2 {
			sh.blur = lens[2]
		}
		if len(lens) > 3 {
			sh.spread = lens[3]
		}
		p.st.shadows = append(p.st.shadows, sh)
	}
}

func (s style) lineHeightFor(metricsHeight float64) float64 {
	switch {
	case s.lineHeightPx > 0:
		return s.lineHeightPx
	case s.lineHeight > 0:
		return s.lineHeight * s.fontSize
	}
	return metricsHeight
}

func describePath(parent, tag string, index int) string {
	return fmt.Sprintf("%s>%s[%d]", parent, tag, index)
}
