package runtimes

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SourceMap is a decoded version 3 source map: enough to find the original
// position of a generated one.
type SourceMap struct {
	Sources []string
	lines   [][]segment // by generated line (0-based)
}

type segment struct {
	genCol, src, line, col int
}

// ParseSourceMap decodes the "mappings" of a source map.
func ParseSourceMap(data []byte) (*SourceMap, error) {
	var raw struct {
		Sources  []string `json:"sources"`
		Mappings string   `json:"mappings"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	sm := &SourceMap{Sources: raw.Sources}
	src, line, col := 0, 0, 0
	var cur []segment
	genCol := 0
	var fields []int
	i := 0
	flush := func() {
		sm.lines = append(sm.lines, cur)
		cur, genCol = nil, 0
	}
	m := raw.Mappings
	for i <= len(m) {
		if i == len(m) {
			flush()
			break
		}
		switch m[i] {
		case ';':
			flush()
			i++
			continue
		case ',':
			i++
			continue
		}
		fields = fields[:0]
		for i < len(m) && m[i] != ',' && m[i] != ';' {
			v, n, err := vlq(m[i:])
			if err != nil {
				return nil, err
			}
			fields = append(fields, v)
			i += n
		}
		if len(fields) == 0 {
			continue
		}
		genCol += fields[0]
		if len(fields) >= 4 {
			src += fields[1]
			line += fields[2]
			col += fields[3]
			cur = append(cur, segment{genCol, src, line, col})
		}
	}
	return sm, nil
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func vlq(s string) (val, n int, err error) {
	shift := 0
	for n < len(s) {
		d := -1
		for k := 0; k < len(b64); k++ {
			if b64[k] == s[n] {
				d = k
				break
			}
		}
		if d < 0 {
			return 0, 0, fmt.Errorf("bad source map character %q", s[n])
		}
		n++
		val |= (d & 31) << shift
		if d&32 == 0 {
			if val&1 == 1 {
				return -(val >> 1), n, nil
			}
			return val >> 1, n, nil
		}
		shift += 5
	}
	return 0, 0, fmt.Errorf("truncated source map value")
}

// Original maps a generated 1-based line and column to the original source
// name and 1-based line and column.
func (sm *SourceMap) Original(line, col int) (source string, l, c int, ok bool) {
	if line < 1 || line > len(sm.lines) {
		return "", 0, 0, false
	}
	segs := sm.lines[line-1]
	if len(segs) == 0 {
		return "", 0, 0, false
	}
	zero := max(col-1, 0)
	i := sort.Search(len(segs), func(i int) bool { return segs[i].genCol > zero }) - 1
	if i < 0 {
		i = 0
	}
	s := segs[i]
	if s.src < 0 || s.src >= len(sm.Sources) {
		return "", 0, 0, false
	}
	return sm.Sources[s.src], s.line + 1, s.col + 1, true
}
