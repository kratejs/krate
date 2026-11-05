package ogimage

import (
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type fontEntry struct {
	family string
	weight int
	italic bool
	f      *sfnt.Font
}

// fontSet resolves a style to a font and caches faces by size.
type fontSet struct {
	entries []fontEntry
	faces   map[faceKey]font.Face
	is      *issues
}

type faceKey struct {
	f    *sfnt.Font
	size float64
}

func newFontSet(specs []FontSpec, is *issues) *fontSet {
	fs := &fontSet{faces: map[faceKey]font.Face{}, is: is}
	for _, sp := range specs {
		data, err := base64.StdEncoding.DecodeString(sp.Data)
		if err != nil {
			is.add("font %q: unreadable data", sp.Name)
			continue
		}
		f, err := sfnt.Parse(data)
		if err != nil {
			is.add("font %q: only TrueType and OpenType (.ttf, .otf) fonts are supported (%v)", sp.Name, err)
			continue
		}
		w := sp.Weight
		if w == 0 {
			w = 400
		}
		fs.entries = append(fs.entries, fontEntry{family: strings.ToLower(sp.Name), weight: w, italic: sp.Style == "italic", f: f})
	}
	for _, d := range []struct {
		data   []byte
		weight int
		italic bool
	}{{goregular.TTF, 400, false}, {gobold.TTF, 700, false}, {goitalic.TTF, 400, true}, {gobolditalic.TTF, 700, true}} {
		if f, err := sfnt.Parse(d.data); err == nil {
			fs.entries = append(fs.entries, fontEntry{family: "", weight: d.weight, italic: d.italic, f: f})
		}
	}
	return fs
}

// resolve picks the font for a style: the first requested family that exists
// (the closest weight and style), else the built-in sans.
func (fs *fontSet) resolve(st style) *sfnt.Font {
	italic := st.fontStyle == "italic"
	for _, fam := range st.fontFamily {
		fam = strings.ToLower(fam)
		best := -1
		bestScore := math.MaxFloat64
		for i, e := range fs.entries {
			if e.family == "" || e.family != fam {
				continue
			}
			score := math.Abs(float64(e.weight - st.fontWeight))
			if e.italic != italic {
				score += 1000
			}
			if score < bestScore {
				best, bestScore = i, score
			}
		}
		if best >= 0 {
			return fs.entries[best].f
		}
	}
	best, bestScore := 0, math.MaxFloat64
	for i, e := range fs.entries {
		if e.family != "" {
			continue
		}
		bold := st.fontWeight >= 600
		score := 0.0
		if (e.weight >= 600) != bold {
			score += 10
		}
		if e.italic != italic {
			score += 5
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	return fs.entries[best].f
}

func (fs *fontSet) face(f *sfnt.Font, size float64) font.Face {
	k := faceKey{f, size}
	if face, ok := fs.faces[k]; ok {
		return face
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		face, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingNone})
	}
	fs.faces[k] = face
	return face
}

// textRun is a laid out block of text.
type textRun struct {
	lines  []string
	widths []float64
	face   font.Face
	lineH  float64
	ascent float64
	w, h   float64
}

func applyTransform(s, mode string) string {
	switch mode {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		prev := ' '
		return strings.Map(func(r rune) rune {
			out := r
			if unicode.IsSpace(prev) {
				out = unicode.ToUpper(r)
			}
			prev = r
			return out
		}, s)
	}
	return s
}

func (fs *fontSet) measureString(face font.Face, s string, spacing float64) float64 {
	w := float64(font.MeasureString(face, s)) / 64
	if spacing != 0 {
		w += spacing * float64(len([]rune(s)))
	}
	return w
}

// layoutText breaks text into lines for the style. maxW < 0 means unconstrained.
func (fs *fontSet) layoutText(text string, st style, maxW float64, path string) *textRun {
	f := fs.resolve(st)
	face := fs.face(f, st.fontSize)
	m := face.Metrics()
	ascent := float64(m.Ascent) / 64
	natural := float64(m.Height) / 64
	lineH := st.lineHeightFor(natural)
	text = applyTransform(text, st.textTransform)

	// Characters the font lacks would draw as empty boxes: report them.
	var buf sfnt.Buffer
	var missing []string
	seen := map[rune]bool{}
	for _, r := range text {
		if r == '\n' || r == '\t' || unicode.IsSpace(r) || seen[r] {
			continue
		}
		seen[r] = true
		if idx, err := f.GlyphIndex(&buf, r); err != nil || idx == 0 {
			missing = append(missing, string(r))
		}
	}
	if len(missing) > 0 {
		fs.is.add("%s: the font has no glyph for %s; supply a font that has them in ImageResponse options.fonts", path, strings.Join(missing, " "))
	}

	pre := st.whiteSpace == "pre" || st.whiteSpace == "pre-wrap"
	nowrap := st.whiteSpace == "nowrap" || st.whiteSpace == "pre"
	var paragraphs []string
	if pre {
		paragraphs = strings.Split(text, "\n")
	} else {
		paragraphs = []string{strings.Join(strings.Fields(text), " ")}
	}
	run := &textRun{face: face, lineH: lineH, ascent: ascent + (lineH-natural)/2}
	for _, para := range paragraphs {
		words := strings.Split(para, " ")
		if pre {
			words = []string{para}
		}
		cur := ""
		for _, word := range words {
			try := word
			if cur != "" {
				try = cur + " " + word
			}
			if !nowrap && maxW >= 0 && cur != "" && fs.measureString(face, try, st.letterSpacing) > maxW+0.01 {
				run.lines = append(run.lines, cur)
				cur = word
				continue
			}
			cur = try
		}
		run.lines = append(run.lines, cur)
	}
	// Ellipsis for a single line that overflows.
	if st.textOverflow == "ellipsis" && maxW >= 0 && len(run.lines) == 1 && fs.measureString(face, run.lines[0], st.letterSpacing) > maxW {
		rs := []rune(run.lines[0])
		for len(rs) > 0 && fs.measureString(face, string(rs)+"…", st.letterSpacing) > maxW {
			rs = rs[:len(rs)-1]
		}
		run.lines[0] = string(rs) + "…"
	}
	for _, l := range run.lines {
		w := fs.measureString(face, l, st.letterSpacing)
		run.widths = append(run.widths, w)
		run.w = math.Max(run.w, w)
	}
	run.h = lineH * float64(len(run.lines))
	return run
}

// drawString draws s at baseline origin (x, y) with the given face and spacing.
func drawString(dst drawTarget, face font.Face, s string, x, y float64, c rgba, spacing float64) {
	d := &font.Drawer{Dst: dst.img, Src: dst.uniform(c), Face: face}
	if spacing == 0 {
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
		d.DrawString(s)
		return
	}
	cx := x
	for _, r := range s {
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(cx * 64), Y: fixed.Int26_6(y * 64)}
		d.DrawString(string(r))
		adv, _ := face.GlyphAdvance(r)
		cx += float64(adv)/64 + spacing
	}
}

func (r *textRun) String() string { return fmt.Sprintf("%d lines %.0fx%.0f", len(r.lines), r.w, r.h) }
