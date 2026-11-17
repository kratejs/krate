package build

import (
	"errors"
	"math/bits"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// Page stylesheets are split by source. The raw CSS of a page carries a part
// marker before each source (see css.PartMarker): every stylesheet the
// layouts and the page import, generated rules, and the syntax-highlighting
// sheet the build adds. Each source is processed on its own and the sources
// are written as content-hashed files, so a source many pages share (a docs
// theme, a component's stylesheet) is downloaded once and then cached. The
// files are linked in source order, so the cascade is the one a single
// combined sheet had.
const (
	// cssRequestCost is what one more stylesheet request is taken to cost,
	// in gzip bytes: a source used by the same pages as its neighbour and
	// smaller than this is not worth a request of its own.
	cssRequestCost = 1024
	// maxPageSheets caps the stylesheets a page links, besides the site-global
	// (Tailwind) sheet.
	maxPageSheets = 5
)

// cssPartOut is one processed source of a page stylesheet.
type cssPartOut struct {
	raw   string // the source text, split again for the UA variants
	pre   string // processed text before minification, scanned for fonts
	data  []byte // processed output
	gz    int
	tw    twPageInfo
	users pageSet // the pages that link this source
}

// pageSet is a set of page indexes.
type pageSet []uint64

func (s *pageSet) add(i int) {
	for len(*s) <= i/64 {
		*s = append(*s, 0)
	}
	(*s)[i/64] |= 1 << (i % 64)
}

func (s pageSet) and(o pageSet) pageSet {
	out := make(pageSet, min(len(s), len(o)))
	for i := range out {
		out[i] = s[i] & o[i]
	}
	return out
}

func (s pageSet) equal(o pageSet) bool {
	for i := 0; i < max(len(s), len(o)); i++ {
		var a, b uint64
		if i < len(s) {
			a = s[i]
		}
		if i < len(o) {
			b = o[i]
		}
		if a != b {
			return false
		}
	}
	return true
}

// count returns how many pages are in s.
func (s pageSet) count() int {
	n := 0
	for _, w := range s {
		n += bits.OnesCount64(w)
	}
	return n
}

// cssUnit is a candidate stylesheet: a run of adjacent sources.
type cssUnit struct {
	end   int     // index after the last source of the run
	gz    int     // gzip size of the sources
	users pageSet // pages that link every source of the run
}

// mergeCost estimates, in gzip bytes over the whole site, what merging two
// adjacent files costs: a page that uses only one of them keeps linking that
// file alone, so a visitor who also sees a page that uses both downloads its
// bytes twice; every page that uses both saves a request, counted as
// cssRequestCost bytes. Merging pays off when the cost is negative.
func mergeCost(a, b cssUnit) int {
	both := a.users.and(b.users).count()
	onlyA := a.users.count() - both
	onlyB := b.users.count() - both
	return a.gz*onlyA + b.gz*onlyB - cssRequestCost*both
}

// planCSSSheets groups a page's sources (in cascade order, each with its gzip
// size and the set of pages that use it) into stylesheets and returns the end
// index (exclusive) of each group. A group is always a run of adjacent
// sources, so the cascade never changes. Starting from one file per source:
//
//  1. Adjacent sources used by exactly the same pages are one file: no page
//     downloads anything it does not use, and there are fewer requests.
//  2. The adjacent pair with the lowest mergeCost is merged while that cost
//     is negative. This folds small sources into the files of the pages that
//     use them, but never folds a small source into a file that many more
//     pages use: those pages would download that file a second time.
//  3. While there are more than maxPageSheets files, the adjacent pair with
//     the lowest mergeCost is merged anyway.
func planCSSSheets(gz []int, users []pageSet) []int {
	var us []cssUnit
	for i := range gz {
		if n := len(us); n > 0 && us[n-1].users.equal(users[i]) {
			us[n-1].end = i + 1
			us[n-1].gz += gz[i]
			continue
		}
		us = append(us, cssUnit{end: i + 1, gz: gz[i], users: users[i]})
	}
	cheapest := func() (int, int) {
		best, bestCost := -1, 0
		for i := 0; i+1 < len(us); i++ {
			if c := mergeCost(us[i], us[i+1]); best < 0 || c < bestCost {
				best, bestCost = i, c
			}
		}
		return best, bestCost
	}
	for len(us) > 1 {
		i, c := cheapest()
		if c >= 0 && len(us) <= maxPageSheets {
			break
		}
		us[i+1].gz += us[i].gz
		us[i+1].users = us[i].users.and(us[i+1].users)
		us = append(us[:i], us[i+1:]...)
	}
	ends := make([]int, len(us))
	for i, u := range us {
		ends[i] = u.end
	}
	return ends
}

// pageSheets is the stylesheet plan of one distinct page CSS input.
type pageSheets struct {
	srcs   []*cssPartOut
	files  []string
	modern []string
	legacy []string
	fonts  []fontFace
	tw     twPageInfo
	// twCtx is the Tailwind directives of all of the page's sources, the
	// context `@apply` expands in (nil when nothing needs it).
	twCtx *css.TailwindDirectives
}

// writePageCSS writes the stylesheets of every page (grouped by
// planCSSSheets) and records them on the page results. The syntax-highlighting
// sheet is added only to pages that render code. A dev build writes one
// stylesheet per page: a rebuild sees only the changed pages, and the dev
// overlay swaps changed stylesheets without keeping their order. Returns the
// set of files written.
func (b *Builder) writePageCSS(results []*PageResult) (map[string]bool, error) {
	written := make(map[string]bool)
	var writeErrs []error
	write := func(file string, data []byte) {
		if file == "" || written[file] {
			return
		}
		if err := writeOutput(filepath.Join(b.Cfg.OutDir, file), data); err != nil {
			writeErrs = append(writeErrs, err)
		}
		written[file] = true
	}
	sheetName := func(data []byte, suffix string) string {
		return "styles." + hashContent(data) + suffix + ".css"
	}

	var chromaCSS string
	chromaLoaded := false
	var codeTheme syntaxhighlight.Theme
	if b.Cfg.Markdown.CodeHighlight {
		// Resolve up front so an unknown theme name is reported even on a
		// build that renders no code.
		codeTheme = b.codeTheme()
	}

	// Per-variant stylesheets: in hybrid+userAgent mode the UA sibling should
	// download only the legacy signal rules (and the default document only the
	// modern rules), so split the hybrid stylesheet per variant.
	variants := b.legacyDocEnabled()
	split := !b.DevMode

	// The inline+directive+minify pipeline is memoized per distinct source
	// and per distinct page input: the docs theme alone is on every page.
	parts := make(map[string]*cssPartOut)
	byData := make(map[string]*cssPartOut)
	// processPart processes one source. ctx holds the Tailwind directives of
	// all of the page's sources, so `@apply` resolves a utility, variant or
	// theme value another source defines. Only text that expands directives
	// depends on it; a source whose output is the same on two pages is one
	// part, so the pages share its file.
	processPart := func(text string, ctx *css.TailwindDirectives, ctxKey string) *cssPartOut {
		key := text
		if ctx != nil && needsTailwindContext(text) {
			key = text + "\x00" + ctxKey
		} else {
			ctx = nil
		}
		if p, ok := parts[key]; ok {
			return p
		}
		p := &cssPartOut{raw: text}
		var out string
		p.pre, out, p.tw = b.processPageCSS(text, ctx)
		if out != "" {
			if q, ok := byData[out]; ok {
				parts[key] = q
				return q
			}
			p.data = []byte(out)
			p.gz = int(gzipLen(p.data))
			byData[out] = p
		}
		parts[key] = p
		return p
	}

	// Pass 1: the sources of every page, and which pages use each source.
	plans := make(map[string]*pageSheets)
	pagePlan := make([]*pageSheets, len(results))
	for i, r := range results {
		r.CSSFiles, r.CSSModernFiles, r.CSSLegacyFiles = nil, nil, nil
		if r.CSS == "" && !pageRendersCode(r) {
			b.setTailwindPage(r.Page, twPageInfo{})
			continue
		}
		pageCSS := r.CSS
		if b.Cfg.Markdown.CodeHighlight && pageRendersCode(r) {
			if !chromaLoaded {
				chromaCSS = syntaxhighlight.ThemeCSS(codeTheme)
				chromaLoaded = true
			}
			if chromaCSS != "" {
				pageCSS = css.PartMarker("syntax-highlighting") + chromaCSS + "\n" + pageCSS
			}
		}
		key := pageCSS
		if variants && r.HasCSSSignals {
			key = "variants\x00" + pageCSS
		}
		plan, ok := plans[key]
		if !ok {
			plan = &pageSheets{}
			plan.twCtx = b.pageTailwindContext(pageCSS)
			var srcs []*cssPartOut
			if split {
				for _, part := range css.SplitParts(pageCSS) {
					srcs = append(srcs, processPart(part.CSS, plan.twCtx, key))
				}
			} else {
				srcs = append(srcs, processPart(css.StripPartMarkers(pageCSS), plan.twCtx, key))
			}
			var pre []string
			for _, p := range srcs {
				pre = append(pre, p.pre)
				plan.tw = mergeTwPageInfo(b.Root, plan.tw, p.tw)
				if p.data != nil {
					plan.srcs = append(plan.srcs, p)
				}
			}
			if b.Cfg.Fonts.PreloadEnabled() {
				plan.fonts = collectFontFaces(strings.Join(pre, "\n"), b.Cfg.Fonts.PreloadLimit)
			}
			plans[key] = plan
		}
		for _, p := range plan.srcs {
			p.users.add(i)
		}
		pagePlan[i] = plan
	}

	// Pass 2: group each distinct page's sources into files.
	planned := make(map[*pageSheets]bool)
	for i, r := range results {
		plan := pagePlan[i]
		if plan == nil {
			continue
		}
		if !planned[plan] {
			planned[plan] = true
			gz := make([]int, len(plan.srcs))
			users := make([]pageSet, len(plan.srcs))
			for j, p := range plan.srcs {
				gz[j], users[j] = p.gz, p.users
			}
			withVariants := variants && r.HasCSSSignals
			start := 0
			for _, end := range planCSSSheets(gz, users) {
				g := plan.srcs[start:end]
				start = end
				chunks := make([][]byte, len(g))
				raw := make([]string, len(g))
				for j, p := range g {
					chunks[j], raw[j] = p.data, p.raw
				}
				data := joinBytes(chunks)
				name := sheetName(data, "")
				write(name, data)
				plan.files = append(plan.files, name)
				if !withVariants {
					continue
				}
				// A group whose variant output equals its default output
				// keeps the default file.
				modernRaw, legacyRaw := splitVariantCSS(strings.Join(raw, "\n"))
				for _, v := range []struct {
					raw, suffix string
					files       *[]string
				}{{modernRaw, ".modern", &plan.modern}, {legacyRaw, ".legacy", &plan.legacy}} {
					_, out, _ := b.processPageCSS(v.raw, plan.twCtx)
					switch {
					case out == "":
					case out == string(data):
						*v.files = append(*v.files, name)
					default:
						vname := sheetName([]byte(out), v.suffix)
						write(vname, []byte(out))
						*v.files = append(*v.files, vname)
					}
				}
			}
		}
		r.CSSFiles = plan.files
		r.CSSModernFiles = plan.modern
		r.CSSLegacyFiles = plan.legacy
		r.FontURLs = plan.fonts
		b.setTailwindPage(r.Page, plan.tw)
	}
	return written, errors.Join(writeErrs...)
}

// processPageCSS runs one stylesheet source through the page pipeline:
// @import inlining, Tailwind directives, font-display, base-path URL
// prefixing, then minification. It returns the text before minification (for
// the font scan), the output ("" when nothing is left), and the Tailwind
// state the source contributes.
func (b *Builder) processPageCSS(text string, ctx *css.TailwindDirectives) (pre, out string, info twPageInfo) {
	pre = b.inlineImports(text)
	pre, info = b.processCSSDirectives(pre, ctx)
	pre = injectFontDisplay(pre, b.Cfg.Fonts.DisplayValue())
	pre = b.prefixCSSURLs(pre)
	if b.Cfg.ShouldMinifyCSS() {
		out = css.Minify(pre)
	} else {
		out = css.RemoveDuplicateDeclarations(pre)
	}
	if strings.TrimSpace(out) == "" {
		out = ""
	}
	return pre, out, info
}

// needsTailwindContext reports whether processing text expands Tailwind
// directives, which can name what another source defines.
func needsTailwindContext(text string) bool {
	return strings.Contains(text, "@apply") || strings.Contains(text, "@variant")
}

// pageTailwindContext parses the Tailwind directives of all of a page's CSS
// sources once, for the `@apply` expansion of each source. It is nil when
// Tailwind is off or no source expands directives.
func (b *Builder) pageTailwindContext(pageCSS string) *css.TailwindDirectives {
	if !b.Cfg.Tailwind.Enabled || !needsTailwindContext(pageCSS) {
		return nil
	}
	return b.parseTailwindDirectives(b.inlineImports(css.StripPartMarkers(pageCSS)))
}

// mergeTwPageInfo combines the Tailwind state of two sources of one page.
func mergeTwPageInfo(root string, a, b twPageInfo) twPageInfo {
	if b.dirs != nil {
		if a.dirs == nil {
			a.dirs = b.dirs
		} else {
			merged := css.NewTailwindDirectives(root)
			merged.MergeFrom(a.dirs)
			merged.MergeFrom(b.dirs)
			a.dirs = merged
		}
	}
	if len(b.extras) > 0 {
		a.extras = append(append([]string(nil), a.extras...), b.extras...)
	}
	return a
}

func joinBytes(chunks [][]byte) []byte {
	if len(chunks) == 1 {
		return chunks[0]
	}
	n := 0
	for _, c := range chunks {
		n += len(c) + 1
	}
	out := make([]byte, 0, n)
	for i, c := range chunks {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, c...)
	}
	return out
}

// joinCSSParts appends the CSS text b to a. Text of b that does not start with
// a part marker (plugin output, compiled island styles) is marked as the
// page's own, so it never joins the last source of a.
func joinCSSParts(a, b string) string {
	if strings.TrimSpace(b) == "" {
		return a
	}
	if !css.StartsWithPart(b) {
		b = css.PartMarker("") + b
	}
	if a == "" {
		return b
	}
	return a + "\n" + b
}
