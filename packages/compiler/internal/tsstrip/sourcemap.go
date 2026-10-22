package tsstrip

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// segment is one decoded source-map mapping: a generated position (UTF-16
// column on a generated line) and the original position it came from.
type segment struct {
	genCol  int
	origLn  int // 0-based original line (source-map line rules)
	origCol int // 0-based original UTF-16 column
}

// sourceMap holds decoded mappings indexed by 0-based generated line.
type sourceMap struct {
	lines [][]segment
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var b64Index = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b64); i++ {
		t[b64[i]] = int8(i)
	}
	return t
}()

var errBadVLQ = errors.New("tsstrip: malformed source map mappings")

// decodeMappings decodes a source map v3 "mappings" string. Only single-source
// maps are produced by Transform, so the source index is read and ignored.
func decodeMappings(mappings string) (*sourceMap, error) {
	sm := &sourceMap{}
	var line []segment
	origLn, origCol, srcIdx, nameIdx := 0, 0, 0, 0
	genCol := 0
	i := 0
	readVLQ := func() (int, error) {
		shift, result := 0, 0
		for {
			if i >= len(mappings) {
				return 0, errBadVLQ
			}
			d := b64Index[mappings[i]]
			i++
			if d < 0 {
				return 0, errBadVLQ
			}
			result += int(d&31) << shift
			if d&32 == 0 {
				break
			}
			shift += 5
			if shift > 30 {
				return 0, errBadVLQ
			}
		}
		if result&1 != 0 {
			return -(result >> 1), nil
		}
		return result >> 1, nil
	}
	for i < len(mappings) {
		switch mappings[i] {
		case ';':
			sm.lines = append(sm.lines, line)
			line = nil
			genCol = 0
			i++
			continue
		case ',':
			i++
			continue
		}
		v, err := readVLQ()
		if err != nil {
			return nil, err
		}
		genCol += v
		if i < len(mappings) && mappings[i] != ',' && mappings[i] != ';' {
			var d [3]int
			for k := 0; k < 3; k++ {
				if d[k], err = readVLQ(); err != nil {
					return nil, err
				}
			}
			srcIdx += d[0]
			origLn += d[1]
			origCol += d[2]
			if i < len(mappings) && mappings[i] != ',' && mappings[i] != ';' {
				n, err := readVLQ()
				if err != nil {
					return nil, err
				}
				nameIdx += n
			}
			line = append(line, segment{genCol: genCol, origLn: origLn, origCol: origCol})
		}
	}
	sm.lines = append(sm.lines, line)
	_ = srcIdx
	_ = nameIdx
	for _, l := range sm.lines {
		sort.SliceStable(l, func(a, b int) bool { return l[a].genCol < l[b].genCol })
	}
	return sm, nil
}

// lookup returns the segment covering (genLine, genCol) as (line, index): the
// last segment at or before genCol on that line, else the last segment of an
// earlier line. ok is false when no segment precedes the position.
func (sm *sourceMap) lookup(genLine, genCol int) (int, int, bool) {
	if genLine >= len(sm.lines) {
		genLine = len(sm.lines) - 1
		genCol = 1 << 30
	}
	for ln := genLine; ln >= 0; ln-- {
		segs := sm.lines[ln]
		if len(segs) == 0 {
			continue
		}
		if ln < genLine {
			return ln, len(segs) - 1, true
		}
		k := sort.Search(len(segs), func(j int) bool { return segs[j].genCol > genCol })
		if k > 0 {
			return ln, k - 1, true
		}
	}
	return 0, 0, false
}

// next returns the segment following (ln, k), if any.
func (sm *sourceMap) next(ln, k int) (segment, int, bool) {
	if k+1 < len(sm.lines[ln]) {
		return sm.lines[ln][k+1], ln, true
	}
	for l := ln + 1; l < len(sm.lines); l++ {
		if len(sm.lines[l]) > 0 {
			return sm.lines[l][0], l, true
		}
	}
	return segment{}, 0, false
}

// textIndex converts between byte offsets and the two line/column systems in
// play: Krate's lexer (lines split on '\n' only, columns counted in runes) and
// source maps / esbuild (lines split on \n, \r, \r\n, U+2028, U+2029; columns
// in UTF-16 code units).
type textIndex struct {
	s       string
	nl      []int // byte offset of each '\n'-delimited line start
	es      []int // byte offset of each source-map line start
	esASCII []bool
}

func newTextIndex(s string) *textIndex {
	t := &textIndex{s: s, nl: []int{0}, es: []int{0}}
	ascii := true
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch c {
			case '\n':
				t.nl = append(t.nl, i)
				t.esASCII = append(t.esASCII, ascii)
				t.es = append(t.es, i)
				ascii = true
			case '\r':
				if i < len(s) && s[i] == '\n' {
					continue
				}
				t.esASCII = append(t.esASCII, ascii)
				t.es = append(t.es, i)
				ascii = true
			}
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		ascii = false
		if r == ' ' || r == ' ' {
			t.esASCII = append(t.esASCII, false)
			t.es = append(t.es, i)
			ascii = true
		}
	}
	t.esASCII = append(t.esASCII, ascii)
	return t
}

// krateOffset converts a Krate (1-based line, rune column) position to a byte
// offset, clamped to the line.
func (t *textIndex) krateOffset(line, col int) int {
	if line < 1 {
		return 0
	}
	if line > len(t.nl) {
		return len(t.s)
	}
	off := t.nl[line-1]
	end := len(t.s)
	if line < len(t.nl) {
		end = t.nl[line]
	}
	for k := 0; k < col && off < end; k++ {
		_, n := utf8.DecodeRuneInString(t.s[off:])
		off += n
	}
	return off
}

// kratePos converts a byte offset to a Krate (1-based line, rune column).
func (t *textIndex) kratePos(off int) (int, int) {
	if off > len(t.s) {
		off = len(t.s)
	}
	ln := sort.Search(len(t.nl), func(j int) bool { return t.nl[j] > off }) - 1
	return ln + 1, utf8.RuneCountInString(t.s[t.nl[ln]:off])
}

// esPos converts a byte offset to a 0-based source-map (line, UTF-16 column).
func (t *textIndex) esPos(off int) (int, int) {
	ln := sort.Search(len(t.es), func(j int) bool { return t.es[j] > off }) - 1
	start := t.es[ln]
	if t.esASCII[ln] {
		return ln, off - start
	}
	col := 0
	for _, r := range t.s[start:off] {
		col += utf16Len(r)
	}
	return ln, col
}

// esOffset converts a 0-based source-map (line, UTF-16 column) to a byte
// offset, clamped to the end of that line's content.
func (t *textIndex) esOffset(line, col int) int {
	if line < 0 {
		return 0
	}
	if line >= len(t.es) {
		return len(t.s)
	}
	start := t.es[line]
	end := len(t.s)
	if line+1 < len(t.es) {
		end = t.es[line+1]
		for end > start && (t.s[end-1] == '\n' || t.s[end-1] == '\r') {
			end--
		}
	}
	if t.esASCII[line] {
		if start+col > end {
			return end
		}
		return start + col
	}
	off := start
	for off < end && col > 0 {
		r, n := utf8.DecodeRuneInString(t.s[off:])
		col -= utf16Len(r)
		off += n
	}
	return off
}

func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}
