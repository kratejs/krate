package syntaxhighlight

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dlclark/regexp2/v2/syntax"
)

// Chroma compiles every lexer rule with regexp2 (a .NET-compatible engine), so
// the rules are parsed with regexp2's own parser and the resulting tree is
// written back out as a JavaScript RegExp source. The target is a sticky,
// Unicode-mode RegExp with no i, m or s flags: case folding, line anchors and
// "." are expanded explicitly so that they follow regexp2's rules and not
// JavaScript's.

// rxWordClass and rxSpaceClass are the bodies of the classes \w and \s as
// regexp2 defines them (Unicode letters, marks, digits and connector
// punctuation; Go's unicode.IsSpace), which differ from JavaScript's.
const (
	rxWordBody  = `\p{L}\p{Mn}\p{Nd}\p{Pc}\u200c\u200d`
	rxSpaceBody = `\t-\r \x85\xa0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000`
)

// rxResult is a rule pattern translated to JavaScript.
type rxResult struct {
	Src    string // RegExp source, to be compiled with the "yu" flags
	Groups int    // number of capture groups, not counting group 0
}

// transpileRule translates a chroma rule pattern, wrapped exactly as chroma
// compiles it, to JavaScript.
func transpileRule(pattern, flags string) (*rxResult, error) {
	if strings.Contains(pattern, "(?>") {
		return nil, fmt.Errorf("atomic groups are not supported")
	}
	full := "(?:" + pattern + ")"
	if flags != "" {
		full = "(?" + flags + ")" + full
	}
	full = `\G` + full
	tree, err := syntax.Parse(full, syntax.ParseOptions{})
	if err != nil {
		return nil, err
	}
	w := &rxWriter{}
	root := tree.Root
	if root.T != syntax.NtCapture || len(root.Children) != 1 {
		return nil, fmt.Errorf("unexpected tree root")
	}
	body := root.Children[0]
	// The leading \G becomes the sticky flag.
	var rest []*syntax.RegexNode
	if body.T == syntax.NtConcatenate && len(body.Children) > 0 && body.Children[0].T == syntax.NtStart {
		rest = body.Children[1:]
	} else if body.T == syntax.NtStart {
		rest = nil
	} else {
		return nil, fmt.Errorf("pattern does not start with \\G")
	}
	var b strings.Builder
	for _, n := range rest {
		s, prec, err := w.node(n)
		if err != nil {
			return nil, err
		}
		if prec < precConcat {
			s = "(?:" + s + ")"
		}
		b.WriteString(s)
	}
	// Capture groups must appear in numeric order for the JS numbering to match.
	for i, num := range w.captures {
		if num != i+1 {
			return nil, fmt.Errorf("capture groups are numbered out of order (%v)", w.captures)
		}
	}
	return &rxResult{Src: b.String(), Groups: len(w.captures)}, nil
}

const (
	precAlt = iota
	precConcat
	precAtom
)

type rxWriter struct {
	captures []int
}

func (w *rxWriter) node(n *syntax.RegexNode) (string, int, error) {
	switch n.T {
	case syntax.NtOne:
		return w.char(n.Ch, n.Options), precAtom, nil
	case syntax.NtNotone:
		return w.notChar(n.Ch, n.Options), precAtom, nil
	case syntax.NtSet:
		s, err := w.set(n.Set)
		return s, precAtom, err
	case syntax.NtMulti:
		var b strings.Builder
		for _, r := range n.Str {
			b.WriteString(w.char(r, n.Options))
		}
		if len(n.Str) == 1 {
			return b.String(), precAtom, nil
		}
		return b.String(), precConcat, nil
	case syntax.NtOneloop, syntax.NtOneloopatomic, syntax.NtOnelazy:
		return w.quant(w.char(n.Ch, n.Options), n.M, n.N, n.T == syntax.NtOnelazy)
	case syntax.NtNotoneloop, syntax.NtNotoneloopatomic, syntax.NtNotonelazy:
		return w.quant(w.notChar(n.Ch, n.Options), n.M, n.N, n.T == syntax.NtNotonelazy)
	case syntax.NtSetloop, syntax.NtSetloopatomic, syntax.NtSetlazy:
		s, err := w.set(n.Set)
		if err != nil {
			return "", 0, err
		}
		return w.quant(s, n.M, n.N, n.T == syntax.NtSetlazy)
	case syntax.NtRef:
		return `(?:\` + strconv.Itoa(n.M) + `)`, precAtom, nil
	case syntax.NtBol:
		return `(?<![^\n])`, precAtom, nil
	case syntax.NtEol:
		return `(?![^\n])`, precAtom, nil
	case syntax.NtBeginning:
		return `(?<![^])`, precAtom, nil
	case syntax.NtEnd:
		return `(?![^])`, precAtom, nil
	case syntax.NtEndZ:
		return `(?=\n?(?![^]))`, precAtom, nil
	case syntax.NtBoundary:
		return `(?:(?<=[` + rxWordBody + `])(?![` + rxWordBody + `])|(?<![` + rxWordBody + `])(?=[` + rxWordBody + `]))`, precAtom, nil
	case syntax.NtNonboundary:
		return `(?:(?<=[` + rxWordBody + `])(?=[` + rxWordBody + `])|(?<![` + rxWordBody + `])(?![` + rxWordBody + `]))`, precAtom, nil
	case syntax.NtNothing:
		return `[]`, precAtom, nil
	case syntax.NtEmpty, syntax.NtUpdateBumpalong:
		return ``, precConcat, nil
	case syntax.NtAlternate:
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			s, _, err := w.node(c)
			if err != nil {
				return "", 0, err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "|"), precAlt, nil
	case syntax.NtConcatenate:
		var b strings.Builder
		for _, c := range n.Children {
			s, prec, err := w.node(c)
			if err != nil {
				return "", 0, err
			}
			if prec < precConcat {
				s = "(?:" + s + ")"
			}
			b.WriteString(s)
		}
		return b.String(), precConcat, nil
	case syntax.NtLoop, syntax.NtLazyloop:
		child, prec, err := w.node(n.Children[0])
		if err != nil {
			return "", 0, err
		}
		if prec < precAtom || child == "" {
			child = "(?:" + child + ")"
		}
		return w.quant(child, n.M, n.N, n.T == syntax.NtLazyloop)
	case syntax.NtCapture:
		if n.M == 0 {
			return "", 0, fmt.Errorf("unexpected inner group 0")
		}
		w.captures = append(w.captures, n.M)
		child, _, err := w.node(n.Children[0])
		if err != nil {
			return "", 0, err
		}
		return "(" + child + ")", precAtom, nil
	case syntax.NtGroup, syntax.NtAtomic:
		// Atomic wrappers are only added by regexp2's optimiser, where they do
		// not change what matches (explicit (?>...) is rejected earlier).
		return w.node(n.Children[0])
	case syntax.NtPosLook, syntax.NtNegLook:
		child, _, err := w.node(n.Children[0])
		if err != nil {
			return "", 0, err
		}
		op := "="
		if n.T == syntax.NtNegLook {
			op = "!"
		}
		if n.Options&syntax.RightToLeft != 0 {
			return "(?<" + op + child + ")", precAtom, nil
		}
		return "(?" + op + child + ")", precAtom, nil
	}
	return "", 0, fmt.Errorf("unsupported regexp construct %v", n.T)
}

func (w *rxWriter) quant(atom string, min, max int, lazy bool) (string, int, error) {
	if atom == "" {
		return "", precConcat, nil
	}
	// A character-sized atom needs no group; longer ones are wrapped by callers.
	var q string
	switch {
	case min == 0 && max == math.MaxInt32:
		q = "*"
	case min == 1 && max == math.MaxInt32:
		q = "+"
	case min == 0 && max == 1:
		q = "?"
	case max == math.MaxInt32:
		q = "{" + strconv.Itoa(min) + ",}"
	case min == max:
		q = "{" + strconv.Itoa(min) + "}"
	default:
		q = "{" + strconv.Itoa(min) + "," + strconv.Itoa(max) + "}"
	}
	if lazy {
		q += "?"
	}
	return atom + q, precAtom, nil
}

// char writes one literal, expanding case-insensitive letters to their fold
// orbit (regexp2 uses unicode.SimpleFold for this).
func (w *rxWriter) char(r rune, opt syntax.RegexOptions) string {
	if opt&syntax.IgnoreCase != 0 {
		if orbit := foldOrbit(r); len(orbit) > 1 {
			var b strings.Builder
			b.WriteByte('[')
			for _, o := range orbit {
				b.WriteString(classChar(o))
			}
			b.WriteByte(']')
			return b.String()
		}
	}
	return litChar(r)
}

func (w *rxWriter) notChar(r rune, opt syntax.RegexOptions) string {
	if opt&syntax.IgnoreCase != 0 {
		if orbit := foldOrbit(r); len(orbit) > 1 {
			var b strings.Builder
			b.WriteString("[^")
			for _, o := range orbit {
				b.WriteString(classChar(o))
			}
			b.WriteByte(']')
			return b.String()
		}
	}
	return "[^" + classChar(r) + "]"
}

func foldOrbit(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func litChar(r rune) string {
	switch r {
	case '^', '$', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '/':
		return `\` + string(r)
	}
	return plainChar(r)
}

func classChar(r rune) string {
	switch r {
	case '\\', ']', '[', '^', '-':
		return `\` + string(r)
	}
	return plainChar(r)
}

// plainChar writes a rune that needs no regexp escaping, using an escape for
// characters that are awkward in source.
func plainChar(r rune) string {
	switch {
	case r == '\n':
		return `\n`
	case r == '\r':
		return `\r`
	case r == '\t':
		return `\t`
	case r < 0x20 || (r >= 0x7f && r < 0xa0) || r == 0x2028 || r == 0x2029 || r == 0xfeff || (r >= 0xd800 && r < 0xe000):
		return fmt.Sprintf(`\u{%x}`, r)
	case r > unicode.MaxRune:
		return fmt.Sprintf(`\u{%x}`, r)
	}
	return string(r)
}

// ---------------------------------------------------------------------------
// Character sets

type rxCat struct {
	name   string
	negate bool
}

type rxSet struct {
	negate   bool
	anything bool
	ranges   [][2]rune
	cats     []rxCat
	sub      *rxSet
}

func decodeSet(buf []byte) (*rxSet, error) {
	r := bytes.NewReader(buf)
	flags, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	s := &rxSet{negate: flags&1 != 0, anything: flags&2 != 0}
	var nr, nc int32
	if err := binary.Read(r, binary.LittleEndian, &nr); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &nc); err != nil {
		return nil, err
	}
	for i := int32(0); i < nr; i++ {
		a, _, err1 := r.ReadRune()
		b, _, err2 := r.ReadRune()
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("truncated set")
		}
		s.ranges = append(s.ranges, [2]rune{a, b})
	}
	for i := int32(0); i < nc; i++ {
		var l int8
		if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
			return nil, err
		}
		c := rxCat{}
		if l < 0 {
			c.negate = true
			l = -l
		}
		name := make([]byte, l)
		if _, err := r.Read(name); err != nil {
			return nil, err
		}
		c.name = string(name)
		s.cats = append(s.cats, c)
	}
	if r.Len() > 0 {
		rest := make([]byte, r.Len())
		_, _ = r.Read(rest)
		sub, err := decodeSet(rest)
		if err != nil {
			return nil, err
		}
		s.sub = sub
	}
	return s, nil
}

// set writes a regexp2 character set as a JavaScript class.
func (w *rxWriter) set(cs *syntax.CharSet) (string, error) {
	s, err := decodeSet(cs.Hash())
	if err != nil {
		return "", err
	}
	return setToJS(s)
}

func setToJS(s *rxSet) (string, error) {
	if s.anything && !s.negate && len(s.ranges) <= 1 && s.sub == nil {
		return `[^]`, nil
	}
	var body strings.Builder
	var standalone []string
	for _, r := range s.ranges {
		switch {
		case r[0] == r[1]:
			body.WriteString(classChar(r[0]))
		case r[1] == r[0]+1:
			body.WriteString(classChar(r[0]) + classChar(r[1]))
		default:
			body.WriteString(classChar(r[0]) + "-" + classChar(r[1]))
		}
	}
	// regexp2 tests the categories of a set in order and stops at the first
	// negated one: a rune outside it matches, a rune inside it does not, and
	// later categories are never consulted. So [^\W\d] is the same as \w there,
	// not "a letter". The positive categories before it are alternatives.
	for _, c := range s.cats {
		cb, err := categoryBody(c.name)
		if err != nil {
			return "", err
		}
		if c.negate {
			standalone = append(standalone, `[^`+cb+`]`)
			break
		}
		body.WriteString(cb)
	}
	var out string
	switch {
	case len(standalone) == 0:
		switch {
		case body.Len() == 0 && s.negate:
			out = `[^]`
		case body.Len() == 0:
			out = `[]`
		case s.negate:
			out = `[^` + body.String() + `]`
		default:
			out = `[` + body.String() + `]`
		}
	default:
		parts := standalone
		if body.Len() > 0 {
			parts = append([]string{`[` + body.String() + `]`}, standalone...)
		}
		union := `(?:` + strings.Join(parts, "|") + `)`
		if s.negate {
			out = `(?:(?!` + union + `)[^])`
		} else {
			out = union
		}
	}
	if s.sub != nil {
		sub, err := setToJS(s.sub)
		if err != nil {
			return "", err
		}
		out = `(?:(?!` + sub + `)` + out + `)`
	}
	return out, nil
}

// categoryBody is the class body that matches a category's runes.
func categoryBody(name string) (string, error) {
	switch name {
	case syntax.WordCategoryText:
		return rxWordBody, nil
	case syntax.SpaceCategoryText:
		return rxSpaceBody, nil
	}
	return categoryToJS(name, false)
}

// generalCategories are the Unicode general categories JS understands with
// \p{..} directly.
var generalCategories = map[string]bool{
	"L": true, "Lu": true, "Ll": true, "Lt": true, "Lm": true, "Lo": true,
	"M": true, "Mn": true, "Mc": true, "Me": true,
	"N": true, "Nd": true, "Nl": true, "No": true,
	"P": true, "Pc": true, "Pd": true, "Ps": true, "Pe": true, "Pi": true, "Pf": true, "Po": true,
	"S": true, "Sm": true, "Sc": true, "Sk": true, "So": true,
	"Z": true, "Zs": true, "Zl": true, "Zp": true,
	"C": true, "Cc": true, "Cf": true, "Cs": true, "Co": true,
}

func categoryToJS(name string, negate bool) (string, error) {
	if generalCategories[name] {
		if negate {
			return `\P{` + name + `}`, nil
		}
		return `\p{` + name + `}`, nil
	}
	// Scripts and other properties are written out as ranges from Go's tables
	// so the match does not depend on the browser's Unicode version.
	if negate {
		return "", fmt.Errorf("negated property %q is not supported", name)
	}
	var tab *unicode.RangeTable
	if t, ok := unicode.Scripts[name]; ok {
		tab = t
	} else if t, ok := unicode.Properties[name]; ok {
		tab = t
	}
	if tab == nil {
		return "", fmt.Errorf("unsupported Unicode category %q", name)
	}
	n := len(tab.R16) + len(tab.R32)
	if n > 200 {
		return "", fmt.Errorf("Unicode category %q is too large to expand (%d ranges)", name, n)
	}
	var b strings.Builder
	for _, r := range tab.R16 {
		writeRange(&b, rune(r.Lo), rune(r.Hi), rune(r.Stride))
	}
	for _, r := range tab.R32 {
		writeRange(&b, rune(r.Lo), rune(r.Hi), rune(r.Stride))
	}
	return b.String(), nil
}

func writeRange(b *strings.Builder, lo, hi, stride rune) {
	if stride != 1 {
		for r := lo; r <= hi; r += stride {
			b.WriteString(classChar(r))
		}
		return
	}
	if lo == hi {
		b.WriteString(classChar(lo))
		return
	}
	b.WriteString(classChar(lo) + "-" + classChar(hi))
}
