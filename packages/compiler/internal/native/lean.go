package native

import (
	"image"
	_ "image/gif"  // image.DecodeConfig formats
	_ "image/jpeg" // image.DecodeConfig formats
	_ "image/png"  // image.DecodeConfig formats
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Defaults of the lean-page checks (see config.NativeLean).
const (
	defaultDataURIKB  = 16
	defaultImageRatio = 2.0
	defaultListItems  = 100
	// minOversizePx keeps small icons out of the image check.
	minOversizePx = 256
	// manyWillChange is how many will-change declarations count as "everywhere".
	manyWillChange = 5
)

type leanOpts struct {
	dataBytes int
	ratio     float64
	listItems int
	ignore    map[string]bool
}

func leanOptions(cfg *config.Config) leanOpts {
	o := leanOpts{dataBytes: defaultDataURIKB << 10, ratio: defaultImageRatio, listItems: defaultListItems, ignore: map[string]bool{}}
	if cfg.Native == nil {
		return o
	}
	l := cfg.Native.Lean
	if l.DataURIKB > 0 {
		o.dataBytes = l.DataURIKB << 10
	}
	if l.ImageRatio > 0 {
		o.ratio = l.ImageRatio
	}
	if l.ListItems > 0 {
		o.listItems = l.ListItems
	}
	for _, c := range l.Ignore {
		o.ignore[strings.ToUpper(strings.TrimSpace(c))] = true
	}
	return o
}

// CheckLean reports memory-hygiene problems in the UI code of a native app:
// oversized images, large inlined data: URLs, web fonts, CSS effects that
// force compositing layers and long lists without windowing. Every result is
// a warning. placement may be nil; it lets the list check recognise values
// that come from host code.
func CheckLean(root string, cfg *config.Config, placement *Placement) ([]diag.Diagnostic, error) {
	o := leanOptions(cfg)
	l := &leaner{root: root, cfg: cfg, o: o, placement: placement, fonts: map[string]bool{}}
	files, err := leanFiles(root, cfg)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(data)
		rel := relPath(root, f)
		l.dataURIs(rel, src)
		switch {
		case filepath.Ext(f) == ".css":
			l.css(rel, src)
		case isScript(f):
			l.script(f, rel, src)
		}
	}
	l.flushWillChange()
	var out []diag.Diagnostic
	for _, d := range l.out {
		if o.ignore[string(d.Code)] {
			continue
		}
		d.Severity = diag.SeverityWarning
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// leanFiles lists the script and stylesheet files the lean checks read: src,
// the pages dir and the public dir.
func leanFiles(root string, cfg *config.Config) ([]string, error) {
	roots := []string{filepath.Join(root, "src")}
	add := func(d string) {
		if d == "" {
			return
		}
		if !filepath.IsAbs(d) {
			d = filepath.Join(root, d)
		}
		roots = append(roots, d)
	}
	add(cfg.PagesDir)
	add(cfg.PublicDir)
	seen := map[string]bool{}
	var files []string
	for _, r := range roots {
		if info, err := os.Stat(r); err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", ".krate", "dist", "dist-native", ".git":
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || seen[p] {
				return nil
			}
			if isScript(p) || filepath.Ext(p) == ".css" {
				seen[p] = true
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)
	return files, nil
}

type leaner struct {
	root      string
	cfg       *config.Config
	o         leanOpts
	placement *Placement
	out       []diag.Diagnostic
	fonts     map[string]bool

	willChange     int
	willChangeFile string
	willChangeLine int
	willChangeSrc  string
}

func (l *leaner) add(rel, src string, line, col int, code diag.Code, msg, hint string) {
	l.out = append(l.out, mkDiag(rel, src, line, col, code, msg, hint))
}

func lineOf(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return strings.Count(src[:off], "\n") + 1
}

// dataURIs reports base64 data: URLs above the threshold.
func (l *leaner) dataURIs(rel, src string) {
	const marker = ";base64,"
	from := 0
	for {
		i := strings.Index(src[from:], marker)
		if i < 0 {
			return
		}
		start := from + i + len(marker)
		end := start
		for end < len(src) && isB64(src[end]) {
			end++
		}
		from = end
		lo := max(0, start-len(marker)-80)
		if !strings.Contains(src[lo:start], "data:") {
			continue
		}
		size := (end - start) * 3 / 4
		if size <= l.o.dataBytes {
			continue
		}
		line := lineOf(src, start)
		l.add(rel, src, line, 1, diag.CodeLeanDataURI,
			"a data: URL of about "+kib(size)+" is inlined here; it is parsed with the file and decoded again on each use",
			"move the asset into the public directory and reference it by URL (the limit is native.lean.dataUriKb, now "+kib(l.o.dataBytes)+")")
	}
}

func isB64(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '-' || c == '_'
}

func kib(n int) string {
	if n >= 1<<20 {
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MiB"
	}
	return strconv.Itoa((n+512)>>10) + " KiB"
}

// ---- CSS ----

type cssBlock struct {
	kind     string // rule, keyframes, frame, fontface, group
	selector string
	line     int
	decls    map[string]string
	declLine map[string]int
}

var (
	iconFontRe    = regexp.MustCompile(`(?i)icon|symbol|awesome|material|glyph|emoji|codicon|feather|lucide|phosphor|remix|fontello|\bfa[- ]|ionicons`)
	fontServiceRe = regexp.MustCompile(`(?i)fonts\.googleapis\.com|fonts\.bunny\.net|use\.typekit\.net|fast\.fonts\.net|use\.fontawesome\.com|fonts\.adobe\.com`)
	blurRe        = regexp.MustCompile(`(?i)blur\(\s*([0-9.]+)\s*(px|rem|em)?\s*\)`)
	lenRe         = regexp.MustCompile(`(?i)(-?[0-9.]+)(px|rem|em)?`)
)

func (l *leaner) css(rel, src string) {
	clean := blankCSSComments(src)
	var stack []*cssBlock
	start := 0
	line := 1
	pos := 0
	top := func() *cssBlock {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	flushDecl := func(text string, declLine int) {
		b := top()
		t := strings.TrimSpace(text)
		if t == "" {
			return
		}
		if b == nil {
			if strings.HasPrefix(t, "@import") && fontServiceRe.MatchString(t) {
				l.webFont(rel, src, declLine, "a font service", "")
			}
			return
		}
		i := strings.Index(t, ":")
		if i < 0 {
			return
		}
		prop := strings.ToLower(strings.TrimSpace(t[:i]))
		val := strings.TrimSpace(t[i+1:])
		if b.decls == nil {
			b.decls = map[string]string{}
			b.declLine = map[string]int{}
		}
		if _, dup := b.decls[prop]; !dup {
			b.declLine[prop] = declLine
		}
		b.decls[prop] = val
		l.cssDecl(rel, src, b, prop, val, declLine)
	}
	declLineStart := 0
	reset := func(p int) {
		start = p
		declLineStart = 0
	}
	for pos < len(clean) {
		c := clean[pos]
		if c == '\n' {
			line++
		} else if declLineStart == 0 && c != ' ' && c != '\t' && c != '\r' && c != ';' && c != '}' {
			declLineStart = line
		}
		switch c {
		case '{':
			prelude := strings.TrimSpace(clean[start:pos])
			b := &cssBlock{line: max(declLineStart, 1), selector: prelude}
			parent := top()
			low := strings.ToLower(prelude)
			switch {
			case strings.HasPrefix(low, "@font-face"):
				b.kind = "fontface"
			case strings.HasPrefix(low, "@keyframes") || strings.HasPrefix(low, "@-webkit-keyframes"):
				b.kind = "keyframes"
			case parent != nil && parent.kind == "keyframes":
				b.kind = "frame"
			case strings.HasPrefix(low, "@"):
				b.kind = "group"
			default:
				b.kind = "rule"
			}
			stack = append(stack, b)
			reset(pos + 1)
		case ';':
			flushDecl(clean[start:pos], max(declLineStart, 1))
			reset(pos + 1)
		case '}':
			flushDecl(clean[start:pos], max(declLineStart, 1))
			if b := top(); b != nil {
				l.cssBlockDone(rel, src, b)
				stack = stack[:len(stack)-1]
			}
			reset(pos + 1)
		}
		pos++
	}
}

// blankCSSComments replaces comments with spaces, keeping newlines.
func blankCSSComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			j := strings.Index(s[i+2:], "*/")
			end := len(s)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			for k := i; k < end; k++ {
				if s[k] == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
			i = end - 1
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func cssPx(v string, unit string) float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "rem", "em":
		return f * 16
	}
	return f
}

// tagOnlySelector reports whether a selector list targets many elements by
// tag or universally: no class, id or attribute in some part.
func tagOnlySelector(sel string) bool {
	for _, part := range strings.Split(sel, ",") {
		p := strings.TrimSpace(part)
		if p == "" || strings.HasPrefix(p, "@") {
			continue
		}
		if !strings.ContainsAny(p, ".#[") {
			return true
		}
	}
	return false
}

func (l *leaner) cssDecl(rel, src string, b *cssBlock, prop, val string, line int) {
	low := strings.ToLower(val)
	none := low == "none" || low == "initial" || low == "unset" || low == "auto" || low == "inherit"
	switch b.kind {
	case "fontface":
		return
	}
	switch prop {
	case "backdrop-filter", "-webkit-backdrop-filter":
		if none {
			return
		}
		if prop == "-webkit-backdrop-filter" {
			if _, both := b.decls["backdrop-filter"]; both {
				return
			}
		}
		l.add(rel, src, line, 1, diag.CodeLeanCompositing,
			"backdrop-filter gives the element its own compositing layer and re-renders what is behind it",
			"use a solid or semi-transparent background, or limit the effect to one overlay that is not repeated or animated")
	case "filter", "-webkit-filter":
		for _, m := range blurRe.FindAllStringSubmatch(val, -1) {
			px := cssPx(m[1], m[2])
			if px >= 16 {
				l.add(rel, src, line, 1, diag.CodeLeanCompositing,
					"filter: blur("+m[1]+m[2]+") is a large blur; the element is rasterised into a separate layer",
					"keep blurs under 16px, pre-render the blurred image as a file, or drop the effect")
			} else if px > 0 && b.kind == "frame" {
				l.add(rel, src, line, 1, diag.CodeLeanCompositing,
					"an animated blur re-rasterises the element on every frame",
					"animate opacity or transform instead, or swap between two pre-blurred images")
			}
		}
	case "box-shadow":
		if none || b.kind == "frame" {
			return
		}
		if !tagOnlySelector(b.selector) || b.kind != "rule" {
			return
		}
		for _, sh := range splitTop(val) {
			nums := lenRe.FindAllStringSubmatch(sh, -1)
			var ls []float64
			for _, n := range nums {
				ls = append(ls, cssPx(n[1], n[2]))
			}
			if len(ls) >= 3 {
				blur := ls[2]
				spread := 0.0
				if len(ls) >= 4 {
					spread = ls[3]
				}
				if blur+abs(spread) >= 40 {
					l.add(rel, src, line, 1, diag.CodeLeanCompositing,
						"a big box-shadow ("+strings.TrimSpace(sh)+") on `"+b.selector+"` is painted for every matching element",
						"give the shadow to a class used on a few elements, or reduce the blur below 40px")
					return
				}
			}
		}
	case "will-change":
		if none {
			return
		}
		if strings.Contains(b.selector, "*") || (b.kind == "rule" && tagOnlySelector(b.selector)) {
			l.add(rel, src, line, 1, diag.CodeLeanCompositing,
				"will-change on `"+b.selector+"` promotes every matching element to its own layer",
				"set will-change only on the one element that is about to animate, and remove it afterwards")
			return
		}
		l.noteWillChange(rel, src, line)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// splitTop splits a value on commas outside parentheses.
func splitTop(v string) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, v[last:i])
				last = i + 1
			}
		}
	}
	return append(out, v[last:])
}

func (l *leaner) noteWillChange(rel, src string, line int) {
	if l.willChange == 0 {
		l.willChangeFile, l.willChangeLine, l.willChangeSrc = rel, line, src
	}
	l.willChange++
}

func (l *leaner) flushWillChange() {
	if l.willChange >= manyWillChange {
		l.add(l.willChangeFile, l.willChangeSrc, l.willChangeLine, 1, diag.CodeLeanCompositing,
			"will-change is set in "+strconv.Itoa(l.willChange)+" places; each promoted element keeps its own layer",
			"set will-change only on the element that is about to animate, and remove it afterwards")
	}
}

func (l *leaner) cssBlockDone(rel, src string, b *cssBlock) {
	switch b.kind {
	case "fontface":
		family := strings.Trim(strings.TrimSpace(b.decls["font-family"]), `"'`)
		if family == "" || iconFontRe.MatchString(family) {
			return
		}
		srcVal := b.decls["src"]
		if !strings.Contains(strings.ToLower(srcVal), "url(") {
			return
		}
		l.webFont(rel, src, b.line, "the font \""+family+"\"", family)
	}
}

func (l *leaner) webFont(rel, src string, line int, what, family string) {
	key := family
	if key == "" {
		key = rel + ":" + strconv.Itoa(line)
	}
	if l.fonts[key] {
		return
	}
	l.fonts[key] = true
	l.add(rel, src, line, 1, diag.CodeLeanWebFont,
		what+" is loaded as a web font; every family and weight adds the font file, a parsed copy and glyph caches to the web process",
		"use a system stack such as `system-ui, -apple-system, \"Segoe UI\", sans-serif` unless the design needs this typeface (silence with native.lean.ignore)")
}

// ---- scripts ----

var windowingImportRe = regexp.MustCompile(`(?i)virtual|virtuoso|virtua|react-window|windowed|window-list`)
var windowingNameRe = regexp.MustCompile(`^(Virtual|Windowed|Virtuoso)`)

func (l *leaner) script(path, rel, src string) {
	// Cheap pre-filters keep the parse to files that can matter.
	needImg := strings.Contains(src, "<img")
	needList := strings.Contains(src, ".map(")
	needStyle := strings.Contains(src, "backdrop") || strings.Contains(src, "Filter") || strings.Contains(src, "willChange") || strings.Contains(src, "className") || strings.Contains(src, "class=")
	needFont := strings.Contains(src, "@fontsource") || fontServiceRe.MatchString(src)
	if !needImg && !needList && !needStyle && !needFont {
		return
	}
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		return
	}
	s := &scriptScan{l: l, path: path, rel: rel, src: src, imports: map[string]string{}, arrays: map[string]int{}}
	for _, st := range prog.Body {
		imp, ok := st.(*ast.ImportStmt)
		if !ok {
			continue
		}
		imp.Source = strings.Trim(imp.Source, "\"'`")
		if strings.HasPrefix(imp.Source, "@fontsource") {
			l.webFont(rel, src, imp.Position.Line, "`"+imp.Source+"`", imp.Source)
		}
		if imp.Default != "" {
			s.imports[imp.Default] = imp.Source
		}
		for _, n := range imp.Named {
			s.imports[n.Local] = imp.Source
		}
		if imp.Namespace != "" {
			s.imports[imp.Namespace] = imp.Source
		}
		if windowingImportRe.MatchString(imp.Source) {
			s.windowed = true
		}
	}
	s.collectBindings(prog)
	visit(prog, func(n any) bool { s.node(n); return true })
	if needFont {
		for _, m := range fontServiceRe.FindAllStringIndex(src, 1) {
			l.webFont(rel, src, lineOf(src, m[0]), "a font service", "service:"+rel)
		}
	}
}

type scriptScan struct {
	l        *leaner
	path     string
	rel      string
	src      string
	imports  map[string]string // local name -> import source
	arrays   map[string]int    // module-level array literal sizes
	host     map[string]bool   // names that hold host code values
	windowed bool
}

func (s *scriptScan) collectBindings(prog *ast.Program) {
	s.host = map[string]bool{}
	for name, source := range s.imports {
		if s.isHostImport(source, name) {
			s.host[name] = true
		}
	}
	// Names assigned from a host call or a host signal, anywhere in the file.
	for pass := 0; pass < 3; pass++ {
		visit(prog, func(n any) bool {
			d, ok := n.(*ast.VarDecl)
			if !ok || d.Init == nil {
				return true
			}
			names := d.Names
			if d.Name != "" && d.Name != "{...}" && !d.IsDestructuring {
				names = []string{d.Name}
			}
			if s.hostDerived(d.Init) {
				for _, nm := range names {
					s.host[nm] = true
				}
			}
			return true
		})
	}
	for _, st := range prog.Body {
		vs, ok := st.(*ast.VarStmt)
		if !ok {
			if ex, ok := st.(*ast.ExportStmt); ok {
				vs, _ = ex.Declaration.(*ast.VarStmt)
			}
		}
		if vs == nil {
			continue
		}
		for _, d := range vs.Decls {
			if d == nil || d.Init == nil || d.Name == "" {
				continue
			}
			if n, ok := s.staticCount(d.Init); ok {
				s.arrays[d.Name] = n
			}
		}
	}
}

// isHostImport reports whether name imported from source is host code.
func (s *scriptScan) isHostImport(source, name string) bool {
	if source == "@krate/app" || strings.HasPrefix(source, "@krate/app/") {
		return name == "signal" || name == "hostSignal" || name == "call" || name == "stream"
	}
	if !strings.HasPrefix(source, ".") {
		return false
	}
	base := filepath.Join(filepath.Dir(s.path), filepath.FromSlash(source))
	cands := []string{base}
	for _, e := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts", ".go"} {
		cands = append(cands, base+e, filepath.Join(base, "index"+e))
	}
	for _, c := range cands {
		info, err := os.Stat(c)
		if err != nil || info.IsDir() {
			continue
		}
		if filepath.Ext(c) == ".go" || IsHostFileName(c) {
			return true
		}
		if s.l.placement != nil {
			if m := s.l.placement.Module(c); m != nil {
				if m.Whole {
					return true
				}
				for _, f := range m.Funcs {
					if f.Name == name {
						return true
					}
				}
			}
		}
		return false
	}
	return false
}

// hostDerived reports whether an expression is a value read from host code.
func (s *scriptScan) hostDerived(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Identifier:
		return s.host[x.Name]
	case *ast.CallExpr:
		return s.hostDerived(x.Callee)
	case *ast.AwaitExpr:
		return s.hostDerived(x.Arg)
	case *ast.ParenExpr:
		return s.hostDerived(x.Expr)
	case *ast.MemberExpr:
		return s.hostDerived(x.Object)
	}
	return false
}

// staticCount returns the length of an array the build can see.
func (s *scriptScan) staticCount(e ast.Expr) (int, bool) {
	switch x := e.(type) {
	case *ast.ArrayExpr:
		n := 0
		for _, el := range x.Elements {
			if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
				if c, ok := s.staticCount(u.Arg); ok {
					n += c
					continue
				}
			}
			n++
		}
		return n, true
	case *ast.ParenExpr:
		return s.staticCount(x.Expr)
	case *ast.TypeAssertion:
		return s.staticCount(x.Expr)
	case *ast.NewExpr:
		if id, ok := x.Callee.(*ast.Identifier); ok && id.Name == "Array" && len(x.Args) == 1 {
			return numLit(x.Args[0])
		}
	case *ast.CallExpr:
		if id, ok := x.Callee.(*ast.Identifier); ok && id.Name == "Array" && len(x.Args) == 1 {
			return numLit(x.Args[0])
		}
		if m, ok := x.Callee.(*ast.MemberExpr); ok && !m.Computed {
			obj, _ := m.Object.(*ast.Identifier)
			prop, _ := m.Property.(*ast.Identifier)
			if obj != nil && prop != nil && obj.Name == "Array" && prop.Name == "from" && len(x.Args) >= 1 {
				if o, ok := x.Args[0].(*ast.ObjectExpr); ok {
					for _, p := range o.Properties {
						if p.Key == "length" {
							return numLit(p.Value)
						}
					}
				}
				return s.staticCount(x.Args[0])
			}
			// [..].fill / .keys on a sized array keep the size
			if prop != nil && (prop.Name == "fill" || prop.Name == "keys") {
				return s.staticCount(m.Object)
			}
		}
	}
	return 0, false
}

func numLit(e ast.Expr) (int, bool) {
	if l, ok := e.(*ast.Literal); ok && l.Kind == ast.NumberLit {
		f, err := strconv.ParseFloat(l.Value, 64)
		if err == nil && f >= 0 {
			return int(f), true
		}
	}
	return 0, false
}

func (s *scriptScan) node(n any) {
	switch x := n.(type) {
	case *ast.JSXElement:
		name := x.Opening.Name
		if windowingNameRe.MatchString(name) {
			s.windowed = true
		}
		if name == "img" {
			s.image(x)
		}
		for _, a := range x.Opening.Attributes {
			if a == nil || a.Spread {
				continue
			}
			switch a.Name {
			case "className", "class":
				s.tailwind(a)
			case "style":
				if o, ok := a.Value.(*ast.ObjectExpr); ok {
					s.styleObject(o, a.Position)
				}
			}
		}
	case *ast.CallExpr:
		s.mapCall(x)
	}
}

// ---- images ----

var numPxRe = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*(?:px)?\s*$`)

func attrSize(e ast.Expr) float64 {
	switch x := e.(type) {
	case *ast.Literal:
		if m := numPxRe.FindStringSubmatch(x.Value); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			return f
		}
	case *ast.ParenExpr:
		return attrSize(x.Expr)
	}
	return 0
}

func propExpr(o *ast.ObjectExpr, key string) ast.Expr {
	for _, p := range o.Properties {
		if p != nil && !p.Spread && !p.Computed && p.Key == key {
			return p.Value
		}
	}
	return nil
}

func (s *scriptScan) image(el *ast.JSXElement) {
	var srcVal ast.Expr
	var w, h float64
	for _, a := range el.Opening.Attributes {
		if a == nil || a.Spread {
			continue
		}
		switch a.Name {
		case "src":
			srcVal = a.Value
		case "width":
			w = attrSize(a.Value)
		case "height":
			h = attrSize(a.Value)
		case "style":
			if o, ok := a.Value.(*ast.ObjectExpr); ok {
				if v := propExpr(o, "width"); v != nil && w == 0 {
					w = attrSize(v)
				}
				if v := propExpr(o, "height"); v != nil && h == 0 {
					h = attrSize(v)
				}
			}
		}
	}
	if srcVal == nil || (w == 0 && h == 0) {
		return
	}
	ref := ""
	switch v := srcVal.(type) {
	case *ast.Literal:
		ref = v.Value
	case *ast.Identifier:
		ref = s.imports[v.Name]
	}
	file := s.imageFile(ref)
	if file == "" {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	iw, ih := imageSize(f)
	if iw == 0 {
		return
	}
	over := func(intrinsic int, rendered float64) bool {
		return rendered > 0 && intrinsic >= minOversizePx && float64(intrinsic) > s.l.o.ratio*rendered
	}
	if !over(iw, w) && !over(ih, h) {
		return
	}
	rw, rh := "auto", "auto"
	if w > 0 {
		rw = strconv.Itoa(int(w))
	}
	if h > 0 {
		rh = strconv.Itoa(int(h))
	}
	s.l.add(s.rel, s.src, el.Position.Line, el.Position.Col, diag.CodeLeanImageOversize,
		relPath(s.l.root, file)+" is "+strconv.Itoa(iw)+"x"+strconv.Itoa(ih)+" px but is shown at "+rw+"x"+rh+"; the webview decodes the full bitmap (about "+kib(iw*ih*4)+" of pixels)",
		"resize the file to at most "+strconv.FormatFloat(s.l.o.ratio, 'f', -1, 64)+"x its rendered size (native.lean.imageRatio sets the factor)")
}

// imageFile resolves an image reference to a local file, or "".
func (s *scriptScan) imageFile(ref string) string {
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "data:") {
		return ""
	}
	switch strings.ToLower(filepath.Ext(ref)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		return ""
	}
	ref = strings.SplitN(strings.SplitN(ref, "?", 2)[0], "#", 2)[0]
	var cands []string
	if strings.HasPrefix(ref, "/") {
		pub := s.l.cfg.PublicDir
		if pub == "" {
			pub = "public"
		}
		if !filepath.IsAbs(pub) {
			pub = filepath.Join(s.l.root, pub)
		}
		cands = append(cands, filepath.Join(pub, filepath.FromSlash(ref)))
	} else if strings.HasPrefix(ref, ".") {
		cands = append(cands, filepath.Join(filepath.Dir(s.path), filepath.FromSlash(ref)))
	}
	for _, c := range cands {
		if within(s.l.root, c) {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c
			}
		}
	}
	return ""
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && !strings.HasPrefix(r, "..")
}

func imageSize(f *os.File) (int, int) {
	if strings.HasSuffix(strings.ToLower(f.Name()), ".webp") {
		return webpSize(f)
	}
	c, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return c.Width, c.Height
}

// webpSize reads the canvas size from a WebP header (VP8, VP8L, VP8X).
func webpSize(f *os.File) (int, int) {
	var b [30]byte
	n, _ := f.Read(b[:])
	if n < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8X":
		w := 1 + int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := 1 + int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w, h
	case "VP8L":
		bits := uint32(b[21]) | uint32(b[22])<<8 | uint32(b[23])<<16 | uint32(b[24])<<24
		return int(bits&0x3fff) + 1, int(bits>>14&0x3fff) + 1
	case "VP8 ":
		return int(b[26]) | int(b[27]&0x3f)<<8, int(b[28]) | int(b[29]&0x3f)<<8
	}
	return 0, 0
}

// ---- inline styles and utility classes ----

var (
	twBackdropRe = regexp.MustCompile(`(?:^|\s)(?:[a-z-]+:)*backdrop-blur(?:-([\w\[\].]+))?(?:\s|$)`)
	twBlurRe     = regexp.MustCompile(`(?:^|\s)(?:[a-z-]+:)*blur-(lg|xl|2xl|3xl)(?:\s|$)`)
	twWillRe     = regexp.MustCompile(`(?:^|\s)(?:[a-z-]+:)*will-change-(?:transform|scroll|contents|\[[^\]]+\])(?:\s|$)`)
)

func classText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Literal:
		return x.Value
	case *ast.TemplateExpr:
		return strings.Join(x.Raw, " ")
	}
	return ""
}

func (s *scriptScan) tailwind(a *ast.JSXAttr) {
	t := classText(a.Value)
	if t == "" {
		return
	}
	if m := twBackdropRe.FindStringSubmatch(t); m != nil && m[1] != "none" {
		s.l.add(s.rel, s.src, a.Position.Line, a.Position.Col, diag.CodeLeanCompositing,
			"the backdrop-blur utility is a backdrop-filter: the element gets its own compositing layer and re-renders what is behind it",
			"use a solid or semi-transparent background, or limit the effect to one overlay that is not repeated")
	}
	if m := twBlurRe.FindStringSubmatch(t); m != nil {
		s.l.add(s.rel, s.src, a.Position.Line, a.Position.Col, diag.CodeLeanCompositing,
			"the blur-"+m[1]+" utility is a large blur (16px or more); the element is rasterised into a separate layer",
			"keep blurs under 16px, pre-render the blurred image as a file, or drop the effect")
	}
	if twWillRe.MatchString(t) {
		s.l.noteWillChange(s.rel, s.src, a.Position.Line)
	}
}

func strLit(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Literal:
		if x.Kind == ast.StringLit {
			return x.Value, true
		}
	case *ast.TemplateExpr:
		return strings.Join(x.Raw, ""), true
	}
	return "", false
}

func (s *scriptScan) styleObject(o *ast.ObjectExpr, pos ast.Pos) {
	for _, p := range o.Properties {
		if p == nil || p.Spread || p.Computed {
			continue
		}
		v, ok := strLit(p.Value)
		if !ok {
			continue
		}
		low := strings.ToLower(v)
		switch p.Key {
		case "backdropFilter", "WebkitBackdropFilter":
			if low != "none" && low != "" {
				s.l.add(s.rel, s.src, pos.Line, pos.Col, diag.CodeLeanCompositing,
					"backdropFilter gives the element its own compositing layer and re-renders what is behind it",
					"use a solid or semi-transparent background, or limit the effect to one overlay that is not repeated")
				return
			}
		case "filter", "WebkitFilter":
			for _, m := range blurRe.FindAllStringSubmatch(v, -1) {
				if cssPx(m[1], m[2]) >= 16 {
					s.l.add(s.rel, s.src, pos.Line, pos.Col, diag.CodeLeanCompositing,
						"filter: blur("+m[1]+m[2]+") is a large blur; the element is rasterised into a separate layer",
						"keep blurs under 16px, pre-render the blurred image as a file, or drop the effect")
					return
				}
			}
		case "willChange":
			if low != "auto" && low != "" {
				s.l.noteWillChange(s.rel, s.src, pos.Line)
			}
		}
	}
}

// ---- lists ----

func (s *scriptScan) mapCall(c *ast.CallExpr) {
	if s.windowed {
		return
	}
	m, ok := c.Callee.(*ast.MemberExpr)
	if !ok || m.Computed || len(c.Args) < 1 {
		return
	}
	if p, ok := m.Property.(*ast.Identifier); !ok || p.Name != "map" {
		return
	}
	fn, ok := c.Args[0].(*ast.ArrowFn)
	if !ok || !returnsJSX(fn) {
		return
	}
	recv := m.Object
	for {
		switch x := recv.(type) {
		case *ast.ParenExpr:
			recv = x.Expr
			continue
		case *ast.TypeAssertion:
			recv = x.Expr
			continue
		case *ast.CallExpr:
			if mm, ok := x.Callee.(*ast.MemberExpr); ok && !mm.Computed {
				if pn, ok := mm.Property.(*ast.Identifier); ok {
					switch pn.Name {
					case "slice", "splice", "toSpliced", "with":
						return // paged by the author
					case "filter", "sort", "toSorted", "reverse", "toReversed", "concat", "flat", "flatMap", "map":
						recv = mm.Object
						continue
					}
				}
			}
		}
		break
	}
	line, col := c.Position.Line, c.Position.Col
	if n, ok := s.countOf(recv); ok {
		if n > s.l.o.listItems {
			s.l.add(s.rel, s.src, line, col, diag.CodeLeanLongList,
				"this .map() renders "+strconv.Itoa(n)+" items into the DOM with no windowing; all of them stay in memory",
				"render only the visible window with a virtual list, page the data with .slice(), or lower the list (native.lean.listItems is "+strconv.Itoa(s.l.o.listItems)+")")
		}
		return
	}
	if s.hostDerived(recv) {
		s.l.add(s.rel, s.src, line, col, diag.CodeLeanLongList,
			"this .map() renders every item host code returns into the DOM, and the UI does not bound the length",
			"have host code return one page at a time, .slice() the list to a page, or render it with a virtual list")
	}
}

func (s *scriptScan) countOf(e ast.Expr) (int, bool) {
	if id, ok := e.(*ast.Identifier); ok {
		n, ok := s.arrays[id.Name]
		return n, ok
	}
	return s.staticCount(e)
}

// returnsJSX reports whether a callback produces JSX.
func returnsJSX(fn *ast.ArrowFn) bool {
	found := false
	visit(fn.Body, func(n any) bool {
		switch n.(type) {
		case *ast.JSXElement, *ast.JSXFragment:
			found = true
		}
		return !found
	})
	return found
}

// visit walks an AST by reflection, calling fn on every node pointer. It
// descends while fn returns true.
func visit(n any, fn func(any) bool) {
	walkValue(reflect.ValueOf(n), fn, 0)
}

var posType = reflect.TypeOf(ast.Pos{})

func walkValue(v reflect.Value, fn func(any) bool, depth int) {
	if depth > 400 {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkValue(v.Elem(), fn, depth+1)
		}
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		if v.Type().Elem().Kind() == reflect.Struct && v.CanInterface() {
			if !fn(v.Interface()) {
				return
			}
		}
		walkValue(v.Elem(), fn, depth+1)
	case reflect.Struct:
		if v.Type() == posType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkValue(v.Field(i), fn, depth+1)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), fn, depth+1)
		}
	}
}
