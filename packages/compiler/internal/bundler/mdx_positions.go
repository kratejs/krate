package bundler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
)

// The parser reads the TSX an MDX file compiles to, so its positions are in
// that generated text. The compiler knows where each JSX tag and `{expression}`
// of the document came from (markdown.Use), and the generated text repeats
// each of them verbatim, so a position on a line that holds one maps back to
// the .mdx line it was written on.

type placedUse struct {
	use      markdown.Use
	genLine  int // 1-based line in the generated TSX
	genStart int // 0-based column of the first character
}

// placeMDXUses finds each use, in document order, in the generated TSX.
func placeMDXUses(tsx string, uses []markdown.Use) []placedUse {
	lines := strings.Split(tsx, "\n")
	var placed []placedUse
	line, col := 0, 0
	for _, u := range uses {
		needle, _, _ := strings.Cut(u.Src, "\n")
		if needle == "" {
			continue
		}
		for l, c := line, col; l < len(lines); l, c = l+1, 0 {
			if i := strings.Index(lines[l][min(c, len(lines[l])):], needle); i >= 0 {
				start := min(c, len(lines[l])) + i
				placed = append(placed, placedUse{use: u, genLine: l + 1, genStart: start})
				line, col = l, start+len(needle)
				break
			}
		}
	}
	return placed
}

// mapMDXDiagnostics reports parser errors against the .mdx file. A position on
// a line that holds a known tag or expression becomes that construct's source
// line and column; any other position is reported as "near" the last known
// construct before it, since the text between constructs is rendered markdown
// with no position of its own.
func mapMDXDiagnostics(path, src, tsx string, uses []markdown.Use, errs []error) (string, bool) {
	placed := placeMDXUses(tsx, uses)
	srcLines := strings.Split(src, "\n")
	sourceLine := func(n int) string {
		if n >= 1 && n <= len(srcLines) {
			return strings.TrimRight(srcLines[n-1], "\r")
		}
		return ""
	}
	var out []error
	allExact := true
	for _, e := range errs {
		d, ok := e.(diag.Diagnostic)
		if !ok {
			return "", false
		}
		on := -1
		for i, p := range placed {
			if p.genLine == d.Line && p.genStart <= d.Col-1 {
				on = i
			}
		}
		if on < 0 {
			for i, p := range placed {
				if p.genLine == d.Line {
					on = i
					break
				}
			}
		}
		if on >= 0 {
			p := placed[on]
			d.File = path
			d.Line = p.use.Line
			if off := d.Col - 1 - p.genStart; off > 0 && !strings.Contains(p.use.Src, "\n") {
				d.Col = p.use.Col + off
			} else {
				d.Col = p.use.Col
			}
			d.Source = sourceLine(d.Line)
			out = append(out, d)
			continue
		}
		allExact = false
		// Nearest construct before the generated line.
		idx := sort.Search(len(placed), func(i int) bool { return placed[i].genLine > d.Line }) - 1
		d.File = path
		if idx >= 0 {
			d.Message += fmt.Sprintf(" (near line %d of the .mdx file)", placed[idx].use.Line)
			d.Line, d.Col = placed[idx].use.Line, placed[idx].use.Col
			d.Source = sourceLine(d.Line)
		} else {
			d.Line, d.Col, d.Source = 0, 0, ""
		}
		out = append(out, d)
	}
	return diag.FormatDiagnostics(out), allExact
}
