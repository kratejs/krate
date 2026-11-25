package build

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// buildHydrationMap removes the source markers from generated hydration code
// and returns the cleaned code with a source map from it to the original TSX
// positions the markers name. Each marker starts a mapping that holds until the
// next one, so an error thrown inside a handler or expression binding is
// reported at that handler or expression in the .tsx file.
func buildHydrationMap(js string, marks []renderer.SrcMark, jsDir string) (string, string) {
	// src < 0 is an end marker: a segment of one field, which the source map
	// format reads as "no original position from here".
	var segs []mapSeg
	var sources []string
	srcIdx := map[string]int{}
	files := map[string]*mapSource{}
	source := func(file string) *mapSource {
		f := files[file]
		if f == nil {
			f = &mapSource{}
			if data, err := os.ReadFile(file); err == nil {
				f.text = string(data)
				f.ok = true
			}
			files[file] = f
		}
		return f
	}
	// lines is the statement whose generated lines follow its source lines
	// (SrcMark.Lines), while one is open.
	var lines *lineFollow
	var out strings.Builder
	out.Grow(len(js))
	line, col := 0, 0
	for i := 0; i < len(js); {
		if strings.HasPrefix(js[i:], renderer.SrcMarkEnd) {
			segs = append(segs, mapSeg{line, col, -1, 0, 0})
			lines = nil
			i += len(renderer.SrcMarkEnd)
			continue
		}
		if strings.HasPrefix(js[i:], renderer.SrcMarkPrefix) {
			end := strings.Index(js[i:], "*/")
			if end > 0 {
				if n, err := strconv.Atoi(js[i+len(renderer.SrcMarkPrefix) : i+end]); err == nil && n >= 0 && n < len(marks) {
					m := marks[n]
					rel := m.File
					if r, err := filepath.Rel(jsDir, m.File); err == nil {
						rel = r
					}
					rel = filepath.ToSlash(rel)
					idx, ok := srcIdx[m.File]
					if !ok {
						idx = len(sources)
						srcIdx[m.File] = idx
						sources = append(sources, rel)
					}
					segs = append(segs, mapSeg{line, col, idx, m.Line - 1, m.Col - 1})
					i += end + 2
					lines = nil
					if m.Lines {
						if f := source(m.File); f.ok {
							lines = &lineFollow{src: idx, lines: f.split(), at: m.Line - 1}
							// The rest of this line: its tokens map into the
							// statement's first line from its column on.
							segs = lines.tokens(segs, line, col, generatedLine(js[i:]), m.Line-1, m.Col-1)
						}
					}
					continue
				}
			}
		}
		r, size := utf8.DecodeRuneInString(js[i:])
		out.WriteString(js[i : i+size])
		i += size
		if r == '\n' {
			line++
			col = 0
			if lines != nil {
				segs = lines.next(segs, line, generatedLine(js[i:]))
			}
		} else if r >= 0x10000 {
			col += 2
		} else {
			col++
		}
	}
	sort.SliceStable(segs, func(a, b int) bool {
		if segs[a].genLine != segs[b].genLine {
			return segs[a].genLine < segs[b].genLine
		}
		return segs[a].genCol < segs[b].genCol
	})

	var mappings strings.Builder
	prevLine, prevSrc, prevSrcLine, prevSrcCol := 0, 0, 0, 0
	prevGenCol := 0
	for i, s := range segs {
		if i == 0 {
			mappings.WriteString(strings.Repeat(";", s.genLine))
		} else if s.genLine != segs[i-1].genLine {
			mappings.WriteString(strings.Repeat(";", s.genLine-prevLine))
			prevGenCol = 0
		} else {
			mappings.WriteString(",")
		}
		if s.src < 0 {
			mappings.WriteString(vlqEncode([]int{s.genCol - prevGenCol}))
			prevLine, prevGenCol = s.genLine, s.genCol
			continue
		}
		mappings.WriteString(vlqEncode([]int{s.genCol - prevGenCol, s.src - prevSrc, s.srcLine - prevSrcLine, s.srcCol - prevSrcCol}))
		prevLine, prevGenCol, prevSrc, prevSrcLine, prevSrcCol = s.genLine, s.genCol, s.src, s.srcLine, s.srcCol
	}

	contents := make([]string, len(sources))
	for file, idx := range srcIdx {
		if f := source(file); f.ok {
			contents[idx] = f.text
		}
	}
	sm := sourceMap{Version: 3, Sources: sources, SourcesContent: contents, Names: []string{}, Mappings: mappings.String()}
	data, err := json.Marshal(sm)
	if err != nil {
		return out.String(), ""
	}
	return out.String(), string(data)
}

// mapSeg is one mapping: a generated position and the original one.
type mapSeg struct{ genLine, genCol, src, srcLine, srcCol int }

// mapSource is a source file a map names, read once per map.
type mapSource struct {
	text  string
	ok    bool
	lines []string
}

func (f *mapSource) split() []string {
	if f.lines == nil {
		f.lines = strings.Split(f.text, "\n")
	}
	return f.lines
}

// generatedLine is the generated code from s to the end of its line, without
// source markers (which are not in the output).
func generatedLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if strings.Contains(s, renderer.SrcMarkPrefix) {
		s = srcMarkCommentRe.ReplaceAllString(s, "")
	}
	return s
}

var (
	srcMarkCommentRe = regexp.MustCompile(`/\*@k:[^*]*\*/`)
	mapTokenRe       = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)
)

// lineFollow maps the generated lines of one statement to its source lines.
// The generated code is a reprint of the source (types removed, some
// expressions rewritten), so each generated line is matched to the first
// following source line that holds most of its identifiers; its identifiers
// then map to their columns there. A line that matches none keeps the
// mapping before it.
type lineFollow struct {
	src   int
	lines []string
	at    int // 0-based source line the last generated line matched
}

// lineFollowWindow is how many source lines ahead a generated line is looked
// for (blank lines and comments the reprint dropped).
const lineFollowWindow = 12

func (lf *lineFollow) next(segs []mapSeg, genLine int, gen string) []mapSeg {
	toks := mapTokenRe.FindAllString(gen, -1)
	if len(toks) == 0 {
		return segs
	}
	for l := lf.at + 1; l < len(lf.lines) && l <= lf.at+lineFollowWindow; l++ {
		if tokenShare(toks, lf.lines[l]) {
			lf.at = l
			return lf.tokens(segs, genLine, 0, gen, l, 0)
		}
	}
	return segs
}

// tokenShare reports whether most identifiers of a generated line are on a
// source line.
func tokenShare(toks []string, src string) bool {
	have := map[string]bool{}
	for _, t := range mapTokenRe.FindAllString(src, -1) {
		have[t] = true
	}
	n := 0
	for _, t := range toks {
		if have[t] {
			n++
		}
	}
	return n*5 >= len(toks)*3 && n > 0
}

// tokens maps each identifier of gen (which starts at generated column
// genCol0) to the same identifier on source line srcLine at or after
// srcCol0, in order. The line's first code maps to the start of the source
// line's code.
func (lf *lineFollow) tokens(segs []mapSeg, genLine, genCol0 int, gen string, srcLine, srcCol0 int) []mapSeg {
	if srcLine < 0 || srcLine >= len(lf.lines) {
		return segs
	}
	src := strings.TrimRight(lf.lines[srcLine], "\r")
	if srcCol0 < 0 || srcCol0 > len(src) {
		srcCol0 = 0
	}
	if genCol0 == 0 {
		lead := len(gen) - len(strings.TrimLeft(gen, " \t"))
		srcLead := len(src) - len(strings.TrimLeft(src, " \t"))
		segs = append(segs, mapSeg{genLine, utf16Len(gen[:lead]), lf.src, srcLine, utf16Len(src[:srcLead])})
	}
	cursor := srcCol0
	for _, loc := range mapTokenRe.FindAllStringIndex(gen, -1) {
		tok := gen[loc[0]:loc[1]]
		at := indexWord(src, tok, cursor)
		if at < 0 {
			continue
		}
		segs = append(segs, mapSeg{genLine, genCol0 + utf16Len(gen[:loc[0]]), lf.src, srcLine, utf16Len(src[:at])})
		cursor = at + len(tok)
	}
	return segs
}

// indexWord finds tok as a whole identifier in s at or after from, -1 when
// it is not there.
func indexWord(s, tok string, from int) int {
	for from <= len(s) {
		i := strings.Index(s[from:], tok)
		if i < 0 {
			return -1
		}
		i += from
		before := i == 0 || !isIdentByte(s[i-1])
		after := i+len(tok) >= len(s) || !isIdentByte(s[i+len(tok)])
		if before && after {
			return i
		}
		from = i + 1
	}
	return -1
}

// utf16Len is the length of s in UTF-16 code units (source map columns).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// minifyJSWithInputMap minifies js and composes the result with an input source
// map (generated -> original), so the minified file maps straight to the
// original sources. On any esbuild error it returns the input and no map.
func minifyJSWithInputMap(js, inputMap string) (string, string) {
	in := js + "\n//# sourceMappingURL=data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(inputMap))
	result := api.Transform(in, api.TransformOptions{
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Charset:           api.CharsetUTF8,
		Sourcefile:        "krate-hydration.js",
		SourcesContent:    api.SourcesContentInclude,
		Sourcemap:         api.SourceMapExternal,
	})
	if len(result.Errors) > 0 {
		return js, ""
	}
	return string(result.Code), string(result.Map)
}
