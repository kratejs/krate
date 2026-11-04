package markdown

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/frontmatter"
)

// MDXResult holds the parsed result of an MDX file.
type MDXResult struct {
	Frontmatter map[string]any
	HTML        string
}

// ParseMDX splits an MDX source into frontmatter and rendered static HTML.
// Components and runtime expressions are dropped from HTML; use
// ParseMDXSegments for the full structure.
func ParseMDX(src string, cfg Config) *MDXResult {
	r := Compile(src, cfg, Options{MDX: true})
	return &MDXResult{Frontmatter: r.Frontmatter, HTML: r.StaticHTML(cfg)}
}

// Options select how a document is compiled.
type Options struct {
	// MDX enables inline JSX, {expressions} and MDX ESM semantics. Plain
	// markdown (.md) only recognises component tags at the start of a line.
	MDX bool
}

// Diagnostic is a located problem found while compiling a document. Line and
// Col are 1-based positions in the original source (frontmatter included).
type Diagnostic struct {
	Line, Col int
	Warning   bool
	Msg, Hint string
}

// Result is a compiled markdown/MDX document.
type Result struct {
	Frontmatter map[string]any
	// ESM holds the import/export statements hoisted out of the body.
	ESM      []string
	Segments []MDXSegment
	Diags    []Diagnostic
	// Uses lists the JSX tags and {expressions} found in the body.
	Uses []Use
}

// Use is one JSX tag ('T' or 'F' when it starts a line) or {expression} ('E').
type Use struct {
	Kind      byte
	Src       string
	Line, Col int
}

// StaticWarnings reports the constructs StaticHTML has to omit: components and
// expressions that only exist at render time.
func (r *Result) StaticWarnings() []Diagnostic {
	var out []Diagnostic
	for _, u := range r.Uses {
		switch u.Kind {
		case 'E':
			if _, ok := (&tokenTable{fm: r.Frontmatter, consts: constsFromESM(r.ESM)}).staticValue(exprBody(u.Src)); ok {
				continue
			}
			out = append(out, Diagnostic{Line: u.Line, Col: u.Col, Warning: true,
				Msg:  "MDX expression " + u.Src + " cannot be evaluated for static HTML",
				Hint: "Content-collection html is a plain string; use frontmatter values or exported literals, or render the entry through a page."})
		default:
			if strings.HasPrefix(u.Src, "</") {
				continue
			}
			out = append(out, Diagnostic{Line: u.Line, Col: u.Col, Warning: true,
				Msg:  "MDX element " + strings.SplitN(strings.Trim(u.Src, "<>/ \n"), " ", 2)[0] + " is not rendered in static HTML",
				Hint: "Content-collection html is a plain string, so components are dropped (their children are kept)."})
		}
	}
	return out
}

// ParseMDXSegments parses MDX (or plain Markdown) and returns ordered segments
// (HTML + JSX + code blocks + admonitions) suitable for generating TSX source.
func ParseMDXSegments(src string, cfg Config) (map[string]any, []MDXSegment) {
	r := Compile(src, cfg, Options{MDX: true})
	return r.Frontmatter, r.Segments
}

// ExtractImports returns the ESM statements (imports and exports) of an MDX
// source, in order. Multi-line statements are returned joined by newlines.
func ExtractImports(src string) []string {
	r := Compile(src, Config{}, Options{MDX: true})
	return r.ESM
}

// FrontmatterDecl returns the `const frontmatter = {...}` declaration that
// MDX expressions can reference, or "" when the module already exports one.
func FrontmatterDecl(fm map[string]any, esm []string) string {
	for _, s := range esm {
		if frontmatterDeclRe.MatchString(s) {
			return ""
		}
	}
	if fm == nil {
		fm = map[string]any{}
	}
	b, err := jsonMarshal(fm)
	if err != nil {
		b = "{}"
	}
	return "const frontmatter = " + b + ";\n"
}

var frontmatterDeclRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+frontmatter\b`)

// HTMLToJSX converts rendered HTML to JSX-compatible syntax.
// Specifically converts void elements to self-closing tags.
func HTMLToJSX(html string) string {
	for _, re := range voidTagRes {
		html = re.ReplaceAllStringFunc(html, func(match string) string {
			if strings.HasSuffix(strings.TrimSpace(match), "/>") {
				return match
			}
			return strings.TrimRight(match, ">") + " />"
		})
	}
	return html
}

var voidTagRes = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, tag := range []string{"hr", "img", "br", "input", "meta", "link", "col", "area", "base", "embed", "param", "source", "track", "wbr"} {
		out = append(out, regexp.MustCompile(`(?i)(<`+tag+`\b[^>]*?)\s*>`))
	}
	return out
}()

// MDXSegment represents either HTML text, a JSX block, or a code block for TSX generation.
type MDXSegment struct {
	HTML  string        // rendered HTML for markdown parts
	JSX   string        // JSX source (components, expressions, directives)
	Code  *CodeSegment  // fenced code block, rendered as <Code> component
	Aside *AsideSegment // admonition (:::note), rendered as <Aside> component
	// Static is the plain-HTML form of a JSX segment (used when the document
	// is rendered to a string, e.g. content collections). Empty for HTML.
	Static string
	Line   int // 1-based source line the segment starts on (0 when unknown)
}

// CodeSegment holds a fenced code block captured from markdown so it can be
// rendered as the <Code> component (with copy button + header) instead of a
// plain static <pre>.
type CodeSegment struct {
	Lang string // fenced code block language (may be empty)
	Meta string // text after the language (title="x", {1,3})
	Code string // raw code content
}

// AsideSegment holds an admonition (:::type ... :::) captured from markdown so
// it can be rendered as the <Aside> component instead of Go-generated markup.
type AsideSegment struct {
	Type      string // admonition type: note | tip | warning | danger | caution
	Title     string // resolved title (Note, Tip, ...) passed explicitly
	InnerHTML string // rendered markdown for the admonition body
}

// directiveRe matches a `:::component Name attrs...` block opener.
var directiveRe = regexp.MustCompile(`^:::\s*component\s+([A-Za-z_][A-Za-z0-9_.]*)\s*(.*)$`)

// containerDirectiveRe matches the built-in container directives that lower to
// @krate/components primitives: card, steps (+ nested ::step), tabs (+ ::tab),
// and code-group (fenced code blocks become tabs).
var containerDirectiveRe = regexp.MustCompile(`^:::\s*(card|steps|tabs|code-group)\b\s*(.*)$`)

// itemDirectiveRe matches a nested item marker inside :::steps / :::tabs.
var itemDirectiveRe = regexp.MustCompile(`^::\s*(step|tab)\b\s*(.*)$`)

var admonRe = regexp.MustCompile(`^:{3,}(\w+)\s*(.*)`)

// marker is a pre-pass construct (directive or admonition) replaced by a
// placeholder paragraph and re-expanded in document order.
type marker struct {
	jsx    string
	static string
	aside  *asideBlockT
}

type asideBlockT struct {
	Type      string
	Title     string
	InnerHTML string
}

// Compile parses a markdown or MDX document into ordered segments, hoisted ESM
// statements and located diagnostics.
func Compile(src string, cfg Config, opts Options) *Result {
	fm, body := frontmatter.Parse(src)
	res := &Result{Frontmatter: fm}
	lineOff := strings.Count(src, "\n") - strings.Count(body, "\n")
	if lineOff < 0 {
		lineOff = 0
	}
	diag := func(line, col int, warn bool, msg, hint string) {
		res.Diags = append(res.Diags, Diagnostic{Line: line + lineOff, Col: col, Warning: warn, Msg: msg, Hint: hint})
	}

	lines := strings.Split(body, "\n")
	inFence := make([]bool, len(lines))
	var markers []marker
	var out []string
	outFence := []bool{}
	outLine := []int{} // original 1-based line of each output line
	emit := func(s string, fence bool, orig int) {
		out = append(out, s)
		outFence = append(outFence, fence)
		outLine = append(outLine, orig)
	}

	var fs fenceState
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fs.active() {
			inFence[i] = true
			fs.feed(line)
			emit(line, true, i+1)
			continue
		}
		if fs.feed(line) {
			inFence[i] = true
			emit(line, true, i+1)
			continue
		}

		// ESM: import/export statements at column 0 end at a blank line.
		if esm, next, ok := scanESM(lines, i); ok {
			res.ESM = append(res.ESM, esm...)
			if strings.Contains(strings.Join(esm, "\n"), "export default") {
				diag(i+1, 1, false, "MDX `export default` is not supported",
					"Wrap the page in a layout component instead of exporting a default from MDX.")
			}
			for k := i; k < next; k++ {
				emit("", false, k+1)
			}
			i = next - 1
			continue
		}

		if m := containerDirectiveRe.FindStringSubmatch(trimmed); m != nil {
			inner, j := collectDirective(lines, i)
			jsx, static := renderContainerDirective(m[1], strings.TrimSpace(m[2]), inner, cfg)
			ph := fmt.Sprintf("KRMDXD%dX", len(markers))
			markers = append(markers, marker{jsx: jsx, static: static})
			emit("", false, i+1)
			emit(ph, false, i+1)
			emit("", false, i+1)
			i = j
			continue
		}
		if m := directiveRe.FindStringSubmatch(trimmed); m != nil {
			inner, j := collectDirective(lines, i)
			innerHTML := RenderToHTML(strings.Join(inner, "\n"), cfg)
			var jsx strings.Builder
			jsx.WriteString("<" + m[1])
			if a := strings.TrimSpace(m[2]); a != "" {
				jsx.WriteString(" " + a)
			}
			jsx.WriteString("><div class=\"krate-directive-body\" dangerouslySetInnerHTML={{__html: `")
			jsx.WriteString(escapeTemplateLiteral(innerHTML))
			jsx.WriteString("`}} /></" + m[1] + ">")
			ph := fmt.Sprintf("KRMDXD%dX", len(markers))
			markers = append(markers, marker{jsx: jsx.String(), static: innerHTML})
			emit("", false, i+1)
			emit(ph, false, i+1)
			emit("", false, i+1)
			i = j
			continue
		}
		if m := admonRe.FindStringSubmatch(trimmed); m != nil && cfg.Admonitions {
			adType := strings.ToLower(strings.TrimSpace(m[1]))
			inner, j := collectAdmonition(lines, i, len(m[0])-len(strings.TrimLeft(m[0], ":")))
			ph := fmt.Sprintf("KRMDXD%dX", len(markers))
			markers = append(markers, marker{aside: &asideBlockT{
				Type:      adType,
				Title:     asideTitle(adType, strings.TrimSpace(m[2])),
				InnerHTML: RenderToHTML(strings.Join(inner, "\n"), cfg),
			}})
			emit("", false, i+1)
			emit(ph, false, i+1)
			emit("", false, i+1)
			i = j
			continue
		}
		emit(line, false, i+1)
	}
	_ = inFence

	// Protect JSX tags and {expressions} behind opaque tokens so the markdown
	// parser never sees them.
	tt := newTokenTable(res.Frontmatter, res.ESM)
	var proc strings.Builder
	var chunk []string
	var chunkStart int
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		joined := strings.Join(chunk, "\n")
		start := chunkStart
		where := func(off int) (line, col int) {
			ln := start + strings.Count(joined[:off], "\n")
			if ln >= len(outLine) {
				ln = len(outLine) - 1
			}
			return outLine[ln], off - strings.LastIndex(joined[:off], "\n")
		}
		before := len(tt.toks)
		text := tt.protect(joined, opts.MDX, func(off int, warn bool, msg, hint string) {
			line, col := where(off)
			diag(line, col, warn, msg, hint)
		})
		for _, tk := range tt.toks[before:] {
			line, col := where(tk.off)
			res.Uses = append(res.Uses, Use{Kind: tk.kind, Src: tk.src, Line: line + lineOff, Col: col})
		}
		proc.WriteString(text)
		chunk = chunk[:0]
	}
	for i, l := range out {
		if outFence[i] {
			flush()
			if proc.Len() > 0 {
				proc.WriteByte('\n')
			}
			proc.WriteString(l)
			continue
		}
		if len(chunk) == 0 {
			chunkStart = i
			if proc.Len() > 0 {
				proc.WriteByte('\n')
			}
		}
		chunk = append(chunk, l)
	}
	flush()

	d := newDocTokens(proc.String(), cfg, opts.MDX, tt)

	var segs []MDXSegment
	var htmlBuf strings.Builder
	var jsxBuf, staticBuf strings.Builder
	flushHTML := func() {
		if htmlBuf.Len() > 0 {
			segs = append(segs, MDXSegment{HTML: htmlBuf.String()})
			htmlBuf.Reset()
		}
	}
	flushJSX := func() {
		if jsxBuf.Len() > 0 {
			segs = append(segs, MDXSegment{JSX: jsxBuf.String(), Static: staticBuf.String()})
			jsxBuf.Reset()
			staticBuf.Reset()
		}
	}
	for n := d.root.FirstChild(); n != nil; n = n.NextSibling() {
		switch v := n.(type) {
		case *ast.Paragraph:
			if mi, ok := markerIndex(nodeText(v, d.src)); ok && mi < len(markers) {
				flushHTML()
				flushJSX()
				mk := markers[mi]
				switch {
				case mk.aside != nil:
					segs = append(segs, MDXSegment{Aside: &AsideSegment{Type: mk.aside.Type, Title: mk.aside.Title, InnerHTML: mk.aside.InnerHTML}})
				default:
					segs = append(segs, MDXSegment{JSX: mk.jsx, Static: mk.static})
				}
				continue
			}
		case *ast.FencedCodeBlock:
			var info string
			if v.Info != nil {
				info = string(v.Info.Segment.Value(d.src))
			}
			lang, meta := splitInfo(info)
			var code strings.Builder
			for i := 0; i < v.Lines().Len(); i++ {
				seg := v.Lines().At(i)
				code.Write(seg.Value(d.src))
			}
			c := strings.TrimRight(code.String(), "\n")
			lang = unescapeInfo(lang)
			flushHTML()
			flushJSX()
			if cfg.Mermaid && strings.EqualFold(lang, "mermaid") {
				segs = append(segs, MDXSegment{
					JSX:    "<pre class=\"mermaid\">{`" + escapeTemplateLiteral(c) + "`}</pre>",
					Static: "<pre class=\"mermaid\">" + escape.HTML(c) + "</pre>\n",
				})
				continue
			}
			segs = append(segs, MDXSegment{Code: &CodeSegment{Lang: lang, Meta: meta, Code: c}})
			continue
		case *ast.Blockquote:
			if kind, ok := v.AttributeString(alertAttr); ok {
				var inner strings.Builder
				for c := v.FirstChild(); c != nil; c = c.NextSibling() {
					inner.WriteString(d.renderNode(c))
				}
				flushHTML()
				flushJSX()
				typ := AlertAsideType(kind.(string))
				segs = append(segs, MDXSegment{Aside: &AsideSegment{Type: typ, Title: asideTitle(typ, ""), InnerHTML: inner.String()}})
				continue
			}
		}
		h := d.renderNode(n)
		h = flowParaRe.ReplaceAllString(h, "$1")
		had := tt.has(h)
		h = tt.resolveStatic(h)
		if had {
			h = emptyParaRe.ReplaceAllString(h, "")
		}
		if !tt.has(h) {
			flushJSX()
			htmlBuf.WriteString(h)
			continue
		}
		jsx, static := tt.toJSX(h, func(off int, warn bool, msg, hint string) {
			diag(0, 0, warn, msg, hint)
		})
		if jsx == "" && static == "" {
			continue
		}
		if strings.TrimSpace(jsx) == "" {
			continue
		}
		flushHTML()
		jsxBuf.WriteString(jsx)
		staticBuf.WriteString(static)
	}
	flushHTML()
	flushJSX()
	res.Segments = segs
	_ = html.EscapeString
	return res
}

var emptyParaRe = regexp.MustCompile(`<p>\s*</p>\n?`)

var flowParaRe = regexp.MustCompile(`(?s)<p>(KRMDXF\d+X.*?)</p>\n?`)

func markerIndex(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "KRMDXD") || !strings.HasSuffix(s, "X") {
		return 0, false
	}
	n := 0
	for _, c := range s[len("KRMDXD") : len(s)-1] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func collectDirective(lines []string, i int) (inner []string, last int) {
	j := i + 1
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == ":::" {
			break
		}
		inner = append(inner, lines[j])
	}
	return inner, j
}

func collectAdmonition(lines []string, i, colons int) (inner []string, last int) {
	closer := strings.Repeat(":", colons)
	j := i + 1
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == closer {
			break
		}
		inner = append(inner, lines[j])
	}
	return inner, j
}

// fenceState tracks whether the scanner is inside a fenced code block, using
// the CommonMark rules (``` or ~~~, closer at least as long and of the same
// character).
type fenceState struct {
	ch  byte
	len int
}

func (f *fenceState) active() bool { return f.ch != 0 }

var fenceOpenRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// feed consumes one line. It returns true when the line opens a fence.
func (f *fenceState) feed(line string) bool {
	if f.ch != 0 {
		t := strings.TrimLeft(line, " ")
		if len(line)-len(t) <= 3 {
			n := 0
			for n < len(t) && t[n] == f.ch {
				n++
			}
			if n >= f.len && strings.TrimSpace(t[n:]) == "" {
				f.ch, f.len = 0, 0
			}
		}
		return false
	}
	m := fenceOpenRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	if m[1][0] == '`' && strings.Contains(m[2], "`") {
		return false
	}
	f.ch, f.len = m[1][0], len(m[1])
	return true
}

var (
	importStartRe = regexp.MustCompile(`^import\s+(?:type\s+)?(?:["'][^"']+["']|[\w$*{][^"']*?\s*from\s*["'][^"']+["'])\s*;?$`)
	exportStartRe = regexp.MustCompile(`^export\s+(?:const|let|var|function|async|class|default|type|interface|enum|\{|\*)`)
	importHeadRe  = regexp.MustCompile(`^import\s+(?:type\s+)?(?:["']|[\w$*{])`)
)

// scanESM recognises an ESM block starting at lines[i] (column 0): the
// contiguous non-blank lines form one or more statements. It returns the
// statements and the index just past the block.
func scanESM(lines []string, i int) ([]string, int, bool) {
	l := lines[i]
	if !strings.HasPrefix(l, "import") && !strings.HasPrefix(l, "export") {
		return nil, 0, false
	}
	end := i
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
		end++
	}
	block := lines[i:end]
	var stmts []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			stmts = append(stmts, strings.TrimSpace(strings.Join(cur, "\n")))
			cur = nil
		}
	}
	depth := 0
	for k, ln := range block {
		t := strings.TrimSpace(ln)
		if depth == 0 && len(cur) == 0 {
			switch {
			case strings.HasPrefix(ln, "import"):
				if !importHeadRe.MatchString(ln) {
					return nil, 0, false
				}
			case strings.HasPrefix(ln, "export"):
				if !exportStartRe.MatchString(ln) {
					return nil, 0, false
				}
			default:
				return nil, 0, false
			}
		}
		cur = append(cur, ln)
		depth += strings.Count(t, "{") + strings.Count(t, "(") + strings.Count(t, "[") -
			strings.Count(t, "}") - strings.Count(t, ")") - strings.Count(t, "]")
		if depth <= 0 {
			depth = 0
			joined := strings.Join(cur, "\n")
			if strings.HasPrefix(joined, "import") && !importStartRe.MatchString(strings.Join(strings.Fields(joined), " ")) {
				return nil, 0, false
			}
			flush()
		}
		_ = k
	}
	if depth != 0 || len(cur) != 0 {
		return nil, 0, false
	}
	return stmts, end, len(stmts) > 0
}

// HasCodeSegments reports whether any segment is a <Code> block (used to decide
// whether to auto-import the Code component).
func HasCodeSegments(segments []MDXSegment) bool {
	for _, seg := range segments {
		if seg.Code != nil {
			return true
		}
	}
	return false
}

// HasAsideSegments reports whether any segment is an <Aside> block (used to
// decide whether to auto-import the Aside component).
func HasAsideSegments(segments []MDXSegment) bool {
	for _, seg := range segments {
		if seg.Aside != nil {
			return true
		}
	}
	return false
}

// RenderSegmentsToHTML renders parsed segments back into a single HTML string,
// suitable for a search index or prerendered fallback content. Code blocks
// render as plain <pre>, and admonitions as <aside>-structure markup matching
// the <Aside> component (without client-side icons).
func RenderSegmentsToHTML(segments []MDXSegment) string {
	return renderSegmentsHTML(segments, nil)
}

func renderSegmentsHTML(segments []MDXSegment, cfg *Config) string {
	var sb strings.Builder
	for _, seg := range segments {
		sb.WriteString(seg.HTML)
		if seg.JSX != "" {
			if cfg != nil {
				sb.WriteString(seg.Static)
			} else {
				sb.WriteString(seg.JSX)
			}
		}
		if seg.Code != nil {
			if cfg != nil {
				sb.WriteString(renderCode(seg.Code.Code, seg.Code.Lang, seg.Code.Meta, *cfg))
			} else {
				sb.WriteString("<pre>")
				sb.WriteString(html.EscapeString(seg.Code.Code))
				sb.WriteString("</pre>")
			}
		}
		if seg.Aside != nil {
			sb.WriteString("<div class=\"krate-aside krate-aside-")
			sb.WriteString(seg.Aside.Type)
			sb.WriteString("\"><div class=\"krate-aside-title\">")
			sb.WriteString(seg.Aside.Title)
			sb.WriteString("</div><div class=\"krate-aside-content\">")
			sb.WriteString(seg.Aside.InnerHTML)
			sb.WriteString("</div></div>")
		}
	}
	return sb.String()
}

// StaticHTML renders the document to one HTML string with highlighted code
// blocks, for consumers that cannot host components (content collections).
// Components and runtime expressions are omitted; Compile reports them.
func (r *Result) StaticHTML(cfg Config) string { return renderSegmentsHTML(r.Segments, &cfg) }

// asideTitle resolves an admonition title the same way the <Aside> component
// does, so it can be passed explicitly (the component's own defaulting isn't
// evaluated by the SSR compiler). A title supplied on the opening line
// (e.g. :::tip My Title) takes precedence.
func asideTitle(adType, titleLine string) string {
	title := strings.TrimSpace(titleLine)
	if title != "" {
		return title
	}
	switch adType {
	case "tip":
		return "Tip"
	case "warning":
		return "Warning"
	case "danger":
		return "Danger"
	case "caution":
		return "Caution"
	default:
		return "Note"
	}
}

// BuildAsideJSX renders an admonition as an <Aside> component JSX string. The
// rendered markdown body is passed as a dangerouslySetInnerHTML div so it is
// emitted as raw HTML (unescaped) inside the Aside.
func BuildAsideJSX(ad *AsideSegment) string {
	var sb strings.Builder
	// Attribute values are emitted as JS string EXPRESSIONS (`{ "..." }`) so
	// quotes/ampersands survive the lexer/parser round-trip verbatim.
	sb.WriteString("<Aside type={")
	sb.WriteString(escape.JSStringDQ(ad.Type))
	sb.WriteString("} title={")
	sb.WriteString(escape.JSStringDQ(ad.Title))
	sb.WriteString("}><div dangerouslySetInnerHTML={{__html:`")
	sb.WriteString(escapeTemplateLiteral(ad.InnerHTML))
	sb.WriteString("`}} /></Aside>")
	return sb.String()
}

// BuildCodeJSX renders a fenced code block as a <Code> component JSX string,
// passing the raw code as template-literal children so build-time chroma
// highlighting and the client-side copy/header UI both apply.
func BuildCodeJSX(lang, code string) string { return BuildCodeJSXMeta(lang, "", code) }

// BuildCodeJSXMeta is BuildCodeJSX with the fence meta (title="x") applied.
func BuildCodeJSXMeta(lang, meta, code string) string {
	var sb strings.Builder
	sb.WriteString("<Code")
	if lang != "" {
		sb.WriteString(" lang={")
		sb.WriteString(escape.JSStringDQ(lang))
		sb.WriteString("}")
	}
	if title, _ := codeMeta(meta); title != "" {
		sb.WriteString(" title={")
		sb.WriteString(escape.JSStringDQ(title))
		sb.WriteString("}")
	}
	sb.WriteString(">{`")
	sb.WriteString(escapeTemplateLiteral(code))
	sb.WriteString("`}</Code>")
	return sb.String()
}

// escapeTemplateLiteral escapes backticks and ${ so raw code can be embedded
// safely inside a JSX template-literal expression.
func escapeTemplateLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

// collectFencedCode collects the lines of a fenced code block starting at line
// i (which must open a fence). Returns the language, the code lines, and the
// index just past the closing fence.
func collectFencedCode(lines []string, i int) (lang string, code []string, next int) {
	var fs fenceState
	fs.feed(lines[i])
	m := fenceOpenRe.FindStringSubmatch(lines[i])
	if m != nil {
		lang, _ = splitInfo(m[2])
	}
	i++
	for i < len(lines) {
		if fs.active() {
			fs.feed(lines[i])
			if !fs.active() {
				return lang, code, i + 1
			}
		}
		code = append(code, lines[i])
		i++
	}
	return lang, code, i
}

// --- Built-in container directives ------------------------------------------

type directiveItem struct {
	title    string
	bodyHTML string
}

// renderContainerDirective lowers :::card / :::steps / :::tabs / :::code-group
// to @krate/components JSX. Nested items use ::step / ::tab markers; code-group
// items are fenced code blocks.
func renderContainerDirective(kind, arg string, lines []string, cfg Config) (jsx, static string) {
	switch kind {
	case "card":
		inner := RenderToHTML(strings.Join(lines, "\n"), cfg)
		st := "<div class=\"krate-card\">"
		if arg != "" {
			st += "<div class=\"krate-card-title\">" + escape.HTML(arg) + "</div>"
		}
		return wrapDirectiveComponent("Card", arg, inner), st + inner + "</div>\n"
	case "steps":
		items := splitDirectiveItems(lines, "step", cfg)
		var b, sb strings.Builder
		b.WriteString("<Steps>")
		sb.WriteString("<ol class=\"krate-steps\">")
		for _, it := range items {
			b.WriteString("<Step")
			sb.WriteString("<li>")
			if it.title != "" {
				b.WriteString(" title=" + jsxQuote(it.title))
				sb.WriteString("<strong>" + escape.HTML(it.title) + "</strong>")
			}
			b.WriteString(">")
			b.WriteString(directiveInner(it.bodyHTML))
			b.WriteString("</Step>")
			sb.WriteString(it.bodyHTML + "</li>")
		}
		b.WriteString("</Steps>")
		sb.WriteString("</ol>\n")
		return b.String(), sb.String()
	case "tabs":
		items := splitDirectiveItems(lines, "tab", cfg)
		var sb strings.Builder
		sb.WriteString("<div class=\"krate-tabs\">")
		for i, it := range items {
			label := it.title
			if label == "" {
				label = fmt.Sprintf("Tab %d", i+1)
			}
			sb.WriteString("<section><h4>" + escape.HTML(label) + "</h4>" + it.bodyHTML + "</section>")
		}
		sb.WriteString("</div>\n")
		return renderTabsJSX(items), sb.String()
	case "code-group":
		items := parseCodeGroup(lines)
		var sb strings.Builder
		for _, it := range items {
			sb.WriteString(renderCode(it.code, it.lang, "", cfg))
		}
		return renderCodeGroupJSX(items), sb.String()
	}
	return "", ""
}

func splitDirectiveItems(lines []string, marker string, cfg Config) []directiveItem {
	var items []directiveItem
	var body []string
	var cur *directiveItem
	flush := func() {
		if cur != nil {
			cur.bodyHTML = RenderToHTML(strings.Join(body, "\n"), cfg)
			items = append(items, *cur)
			body = nil
		}
	}
	for _, ln := range lines {
		if m := itemDirectiveRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil && m[1] == marker {
			flush()
			cur = &directiveItem{title: strings.TrimSpace(m[2])}
			continue
		}
		if cur != nil {
			body = append(body, ln)
		}
	}
	flush()
	if len(items) == 0 {
		items = append(items, directiveItem{bodyHTML: RenderToHTML(strings.Join(lines, "\n"), cfg)})
	}
	return items
}

func renderTabsJSX(items []directiveItem) string {
	type norm struct{ label, value, html string }
	var list []norm
	used := map[string]bool{}
	for i, it := range items {
		label := it.title
		if label == "" {
			label = fmt.Sprintf("Tab %d", i+1)
		}
		val := budgetSlug(label, i)
		for used[val] {
			val += "-" + runeToStr(i)
		}
		used[val] = true
		list = append(list, norm{label, val, it.bodyHTML})
	}
	var b strings.Builder
	b.WriteString("<Tabs defaultValue=" + jsxQuote(list[0].value) + ">")
	b.WriteString("<TabsList>")
	for _, n := range list {
		b.WriteString("<TabsTrigger value=" + jsxQuote(n.value) + ">" + escape.HTML(n.label) + "</TabsTrigger>")
	}
	b.WriteString("</TabsList>")
	for _, n := range list {
		b.WriteString("<TabsContent value=" + jsxQuote(n.value) + ">" + directiveInner(n.html) + "</TabsContent>")
	}
	b.WriteString("</Tabs>")
	return b.String()
}

type codeGroupItem struct {
	lang string
	code string
}

func parseCodeGroup(lines []string) []codeGroupItem {
	var items []codeGroupItem
	for i := 0; i < len(lines); {
		if fenceOpenRe.MatchString(lines[i]) {
			lang, codeLines, next := collectFencedCode(lines, i)
			items = append(items, codeGroupItem{lang: lang, code: strings.Join(codeLines, "\n")})
			i = next
			continue
		}
		i++
	}
	return items
}

func renderCodeGroupJSX(items []codeGroupItem) string {
	if len(items) == 0 {
		return ""
	}
	val := func(i int) string {
		if items[i].lang == "" {
			return "code-" + runeToStr(i)
		}
		return items[i].lang + "-" + runeToStr(i)
	}
	var b strings.Builder
	b.WriteString("<Tabs defaultValue=" + jsxQuote(val(0)) + ">")
	b.WriteString("<TabsList>")
	for i, it := range items {
		label := it.lang
		if label == "" {
			label = fmt.Sprintf("Code %d", i+1)
		}
		b.WriteString("<TabsTrigger value=" + jsxQuote(val(i)) + ">" + escape.HTML(label) + "</TabsTrigger>")
	}
	b.WriteString("</TabsList>")
	for i, it := range items {
		b.WriteString("<TabsContent value=" + jsxQuote(val(i)) + ">" + BuildCodeJSX(it.lang, it.code) + "</TabsContent>")
	}
	b.WriteString("</Tabs>")
	return b.String()
}

func wrapDirectiveComponent(name, title, innerHTML string) string {
	var b strings.Builder
	b.WriteString("<" + name)
	if title != "" {
		b.WriteString(" title=" + jsxQuote(title))
	}
	b.WriteString(">")
	b.WriteString(directiveInner(innerHTML))
	b.WriteString("</" + name + ">")
	return b.String()
}

// directiveInner wraps rendered markdown as dangerouslySetInnerHTML.
func directiveInner(html string) string {
	return "<div class=\"krate-directive-body\" dangerouslySetInnerHTML={{__html: `" +
		escapeTemplateLiteral(html) + "`}} />"
}

func jsxQuote(s string) string { return "\"" + escape.HTMLAttr(s) + "\"" }

// budgetSlug slugifies a label for use as a tab value (deterministic).
func budgetSlug(label string, idx int) string {
	var b strings.Builder
	prev := false
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = false
		} else if !prev && b.Len() > 0 {
			b.WriteByte('-')
			prev = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "tab-" + runeToStr(idx)
	}
	return s
}

// DirectiveComponents returns the @krate/components names used by built-in
// container directives in src, so the docs plugin can auto-import them.
func DirectiveComponents(src string) []string {
	used := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			used[n] = true
		}
	}
	for _, ln := range strings.Split(src, "\n") {
		m := containerDirectiveRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		switch m[1] {
		case "card":
			add("Card")
		case "steps":
			add("Steps", "Step")
		case "tabs":
			add("Tabs", "TabsList", "TabsTrigger", "TabsContent")
		case "code-group":
			add("Tabs", "TabsList", "TabsTrigger", "TabsContent", "Code")
		}
	}
	out := make([]string, 0, len(used))
	for n := range used {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func runeToStr(i int) string {
	if i < 26 {
		return string(rune('a' + i))
	}
	return string(rune('A' + (i - 26)))
}
