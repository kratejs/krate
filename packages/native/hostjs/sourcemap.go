package hostjs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// sourceMap is a decoded source map (version 3). Index maps (sections) are
// not supported.
type sourceMap struct {
	sources []string
	names   []string
	// lines[i] holds the segments of generated line i, sorted by column.
	lines [][]segment
}

type segment struct {
	col    int
	source int // -1 when the segment has no source
	line   int
	srcCol int
	name   int // -1 when none
}

// position is an original source location, 1-based line and column.
type position struct {
	Source string
	Line   int
	Col    int
	Name   string
}

func parseSourceMap(data []byte) (*sourceMap, error) {
	var raw struct {
		Version    int      `json:"version"`
		SourceRoot string   `json:"sourceRoot"`
		Sources    []string `json:"sources"`
		Names      []string `json:"names"`
		Mappings   string   `json:"mappings"`
		Sections   []any    `json:"sections"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("source map: %w", err)
	}
	if raw.Version != 3 {
		return nil, fmt.Errorf("source map: unsupported version %d", raw.Version)
	}
	if len(raw.Sections) > 0 {
		return nil, fmt.Errorf("source map: index maps are not supported")
	}
	m := &sourceMap{names: raw.Names}
	for _, s := range raw.Sources {
		if raw.SourceRoot != "" && !strings.Contains(s, "://") && !strings.HasPrefix(s, "/") {
			s = strings.TrimSuffix(raw.SourceRoot, "/") + "/" + s
		}
		m.sources = append(m.sources, s)
	}
	if err := m.decode(raw.Mappings); err != nil {
		return nil, err
	}
	return m, nil
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

// vlq reads one base64 VLQ value from s at *i.
func vlq(s string, i *int) (int, error) {
	result, shift := 0, 0
	for {
		if *i >= len(s) {
			return 0, fmt.Errorf("source map: truncated mappings")
		}
		d := b64Index[s[*i]]
		if d < 0 {
			return 0, fmt.Errorf("source map: bad mappings character %q", s[*i])
		}
		*i++
		result += int(d&31) << shift
		if d&32 == 0 {
			break
		}
		shift += 5
		if shift > 30 {
			return 0, fmt.Errorf("source map: mappings value too large")
		}
	}
	v := result >> 1
	if result&1 == 1 {
		v = -v
	}
	return v, nil
}

func (m *sourceMap) decode(mappings string) error {
	var srcIdx, srcLine, srcCol, nameIdx int
	for _, lineText := range strings.Split(mappings, ";") {
		var segs []segment
		genCol := 0
		for _, text := range strings.Split(lineText, ",") {
			if text == "" {
				continue
			}
			var f [5]int
			n := 0
			for i := 0; i < len(text); {
				if n == len(f) {
					return fmt.Errorf("source map: too many fields in a segment")
				}
				v, err := vlq(text, &i)
				if err != nil {
					return err
				}
				f[n] = v
				n++
			}
			if n != 1 && n != 4 && n != 5 {
				return fmt.Errorf("source map: a segment has %d fields", n)
			}
			genCol += f[0]
			seg := segment{col: genCol, source: -1, name: -1}
			if n >= 4 {
				srcIdx += f[1]
				srcLine += f[2]
				srcCol += f[3]
				if srcIdx < 0 || srcIdx >= len(m.sources) || srcLine < 0 || srcCol < 0 {
					return fmt.Errorf("source map: segment out of range")
				}
				seg.source, seg.line, seg.srcCol = srcIdx, srcLine, srcCol
			}
			if n == 5 {
				nameIdx += f[4]
				if nameIdx < 0 || nameIdx >= len(m.names) {
					return fmt.Errorf("source map: name index out of range")
				}
				seg.name = nameIdx
			}
			segs = append(segs, seg)
		}
		m.lines = append(m.lines, segs)
	}
	return nil
}

// lookup maps a 1-based generated line and column to the original position of
// the nearest segment at or before col on that line.
func (m *sourceMap) lookup(line, col int) (position, bool) {
	if m == nil || line < 1 || line > len(m.lines) {
		return position{}, false
	}
	segs := m.lines[line-1]
	zero := col - 1
	i := sort.Search(len(segs), func(i int) bool { return segs[i].col > zero })
	for i--; i >= 0; i-- {
		s := segs[i]
		if s.source < 0 {
			continue
		}
		p := position{Source: m.sources[s.source], Line: s.line + 1, Col: s.srcCol + 1}
		if s.name >= 0 {
			p.Name = m.names[s.name]
		}
		return p, true
	}
	return position{}, false
}
