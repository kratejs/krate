package build

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
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
	type seg struct{ genLine, genCol, src, srcLine, srcCol int }
	var segs []seg
	var sources []string
	srcIdx := map[string]int{}
	var out strings.Builder
	out.Grow(len(js))
	line, col := 0, 0
	for i := 0; i < len(js); {
		if strings.HasPrefix(js[i:], renderer.SrcMarkEnd) {
			segs = append(segs, seg{line, col, -1, 0, 0})
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
					segs = append(segs, seg{line, col, idx, m.Line - 1, m.Col - 1})
					i += end + 2
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
		} else if r >= 0x10000 {
			col += 2
		} else {
			col++
		}
	}

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
		if data, err := os.ReadFile(file); err == nil {
			contents[idx] = string(data)
		}
	}
	sm := sourceMap{Version: 3, Sources: sources, SourcesContent: contents, Names: []string{}, Mappings: mappings.String()}
	data, err := json.Marshal(sm)
	if err != nil {
		return out.String(), ""
	}
	return out.String(), string(data)
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
