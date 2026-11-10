package markdown

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// RenderToHTML converts Markdown source to HTML using the given config. The
// parser is goldmark (CommonMark) with GFM, footnotes and definition lists;
// Krate adds heading ids, syntax highlighting, emoji, math wrapping, GFM
// alerts and URL safety on top.
func RenderToHTML(src string, cfg Config) string {
	d := newDoc(src, cfg, false)
	var out bytes.Buffer
	for n := d.root.FirstChild(); n != nil; n = n.NextSibling() {
		out.WriteString(d.renderNode(n))
	}
	return out.String()
}

// doc is one parsed markdown document plus the renderer used to emit it.
type doc struct {
	md     goldmark.Markdown
	src    []byte
	root   ast.Node
	cfg    Config
	mdx    bool
	tokens *tokenTable
}

func newDoc(src string, cfg Config, mdx bool) *doc {
	return newDocTokens(src, cfg, mdx, nil)
}

func newDocTokens(src string, cfg Config, mdx bool, tokens *tokenTable) *doc {
	d := &doc{src: []byte(src), cfg: cfg, mdx: mdx, tokens: tokens}
	d.md = d.build()
	d.root = d.md.Parser().Parse(text.NewReader(d.src))
	return d
}

func (d *doc) build() goldmark.Markdown {
	var exts []goldmark.Extender
	if d.cfg.GFM {
		exts = append(exts, extension.Table, extension.Strikethrough, extension.TaskList,
			extension.NewLinkify(extension.WithLinkifyAllowedProtocols([]string{"http:", "https:", "ftp:"})))
	}
	exts = append(exts,
		extension.NewFootnote(
			extension.WithFootnoteBacklinkClass("krate-footnote-back"),
			extension.WithFootnoteBacklinkHTML("↩"),
			extension.WithFootnoteBacklinkTitle("Back to reference"),
		),
		extension.DefinitionList,
	)
	t := &transformer{d: d, used: map[string]bool{}}
	var opts []goldmark.Option
	if d.mdx {
		// MDX has no indented code blocks: indentation is JSX layout there.
		var bps []util.PrioritizedValue
		for _, bp := range parser.DefaultBlockParsers() {
			if bp.Priority == 500 {
				continue
			}
			bps = append(bps, bp)
		}
		opts = append(opts, goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(bps...),
			parser.WithInlineParsers(parser.DefaultInlineParsers()...),
			parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
		)))
	}
	opts = append(opts,
		goldmark.WithExtensions(exts...),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(t, 100))),
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(&krateRenderer{d: d}, 10)),
		),
	)
	return goldmark.New(opts...)
}

func (d *doc) renderNode(n ast.Node) string {
	var buf bytes.Buffer
	if err := d.md.Renderer().Render(&buf, d.src, n); err != nil {
		return ""
	}
	return d.post(buf.String())
}

var (
	footnoteIDRe = regexp.MustCompile(`(id|href)="(#?)(fn|fnref):`)
	mathBlockRe  = regexp.MustCompile(`\$\$([^$]+?)\$\$`)
	mathInlineRe = regexp.MustCompile(`\$([^\s$][^$\n]*?[^\s$]|[^\s$])\$`)
)

// post applies the string-level passes: footnote markup, emoji and math.
func (d *doc) post(s string) string {
	if strings.Contains(s, "doc-endnotes") || strings.Contains(s, "doc-noteref") {
		s = strings.Replace(s, `<div class="footnotes" role="doc-endnotes">`, `<section class="krate-footnotes" role="doc-endnotes">`, 1)
		if strings.Contains(s, "krate-footnotes") {
			if i := strings.LastIndex(s, "</div>"); i >= 0 {
				s = s[:i] + "</section>" + s[i+len("</div>"):]
			}
		}
		s = strings.ReplaceAll(s, `<sup id="fnref:`, `<sup class="krate-footnote-ref" id="fnref:`)
		s = footnoteIDRe.ReplaceAllString(s, `$1="$2$3-`)
	}
	if strings.Contains(s, ":") {
		s = mapHTMLText(s, emojiReplace)
	}
	if d.cfg.Math && strings.Contains(s, "$") {
		s = mapHTMLText(s, wrapMathInText)
	}
	return s
}

func wrapMathInText(text string) string {
	text = mathBlockRe.ReplaceAllString(text, `<span class="krate-math krate-math-block">$$$1$$</span>`)
	text = mathInlineRe.ReplaceAllString(text, `<span class="krate-math krate-math-inline">$$$1$$</span>`)
	return text
}

// mapHTMLText applies fn to every text run of an HTML string, leaving tags and
// the contents of pre/code/script/style elements untouched.
func mapHTMLText(s string, fn func(string) string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			out.WriteString(fn(s[i:]))
			break
		}
		if lt > 0 {
			out.WriteString(fn(s[i : i+lt]))
		}
		start := i + lt
		skipped := false
		for _, tag := range []string{"pre", "code", "script", "style"} {
			if hasTagPrefix(s[start:], tag) {
				closeTag := "</" + tag + ">"
				if end := strings.Index(s[start:], closeTag); end >= 0 {
					out.WriteString(s[start : start+end+len(closeTag)])
					i = start + end + len(closeTag)
					skipped = true
				}
				break
			}
		}
		if skipped {
			continue
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			out.WriteString(s[start:])
			break
		}
		out.WriteString(s[start : start+end+1])
		i = start + end + 1
	}
	return out.String()
}

func hasTagPrefix(s, tag string) bool {
	if !strings.HasPrefix(s, "<"+tag) || len(s) <= len(tag)+1 {
		return false
	}
	c := s[len(tag)+1]
	return c == '>' || c == ' ' || c == '\n' || c == '\t' || c == '/'
}

// --- AST transformer --------------------------------------------------------

var (
	kindTableWrap = ast.NewNodeKind("KrateTableWrap")
)

type tableWrap struct{ ast.BaseBlock }

func (*tableWrap) Kind() ast.NodeKind       { return kindTableWrap }
func (n *tableWrap) Dump(src []byte, l int) { ast.DumpHelper(n, src, l, nil, nil) }

var alertRe = regexp.MustCompile(`(?i)^\s*\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*$`)

const alertAttr = "krateAlert"

type transformer struct {
	d    *doc
	used map[string]bool
}

func (t *transformer) Transform(root *ast.Document, reader text.Reader, pc parser.Context) {
	src := reader.Source()
	var tables []ast.Node
	var unsafe []ast.Node
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Heading:
			if t.d.cfg.HeadingAnchors {
				if _, ok := v.AttributeString("id"); !ok {
					v.SetAttributeString("id", []byte(t.headingID(v, src)))
				}
			}
		case *east.Table:
			tables = append(tables, n)
		case *ast.Link:
			if !safeURL(string(v.Destination)) {
				unsafe = append(unsafe, n)
			} else if t.d.cfg.LinkURL != nil {
				if u, ok := t.d.cfg.LinkURL(string(v.Destination)); ok {
					v.Destination = []byte(u)
				}
			}
		case *ast.Image:
			if !safeURL(string(v.Destination)) {
				unsafe = append(unsafe, n)
			} else if t.d.cfg.ImageURL != nil {
				if u, ok := t.d.cfg.ImageURL(string(v.Destination)); ok {
					v.Destination = []byte(u)
				}
			}
		case *ast.AutoLink:
			if !safeURL(string(v.URL(src))) {
				unsafe = append(unsafe, n)
			}
		case *ast.Blockquote:
			if t.d.cfg.Admonitions {
				t.markAlert(v, src)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, n := range tables {
		parent := n.Parent()
		if parent == nil {
			continue
		}
		w := &tableWrap{}
		parent.ReplaceChild(parent, n, w)
		w.AppendChild(w, n)
	}
	for _, n := range unsafe {
		parent := n.Parent()
		if parent == nil {
			continue
		}
		// A script-capable destination keeps its label but loses the link.
		if _, isImg := n.(*ast.Image); isImg {
			parent.ReplaceChild(parent, n, ast.NewString([]byte(nodeText(n, src))))
			continue
		}
		if al, isAuto := n.(*ast.AutoLink); isAuto {
			parent.ReplaceChild(parent, n, ast.NewString(append([]byte(nil), al.Label(src)...)))
			continue
		}
		for c := n.FirstChild(); c != nil; {
			next := c.NextSibling()
			parent.InsertBefore(parent, n, c)
			c = next
		}
		parent.RemoveChild(parent, n)
	}
}

// markAlert tags a GFM alert blockquote (> [!NOTE]) and strips its marker.
func (t *transformer) markAlert(bq *ast.Blockquote, src []byte) {
	p, ok := bq.FirstChild().(*ast.Paragraph)
	if !ok || p.Lines().Len() == 0 {
		return
	}
	first := p.Lines().At(0)
	m := alertRe.FindStringSubmatch(string(first.Value(src)))
	if m == nil {
		return
	}
	bq.SetAttributeString(alertAttr, strings.ToLower(m[1]))
	// Drop inline children up to and including the first line break.
	cut := false
	for c := p.FirstChild(); c != nil && !cut; {
		next := c.NextSibling()
		if tx, ok := c.(*ast.Text); ok && (tx.SoftLineBreak() || tx.HardLineBreak()) {
			cut = true
		}
		p.RemoveChild(p, c)
		c = next
	}
	if p.FirstChild() == nil {
		bq.RemoveChild(bq, p)
	}
}

// headingID builds a GitHub-style slug from the heading's text content,
// deduplicated against earlier headings with -1, -2, ...
func (t *transformer) headingID(h *ast.Heading, src []byte) string {
	txt := nodeText(h, src)
	if t.d.tokens != nil {
		txt = t.d.tokens.plainText(txt)
	}
	base := Slugify(txt)
	if base == "" {
		base = "heading"
	}
	id := base
	for n := 1; t.used[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	t.used[id] = true
	return id
}

// Slugify turns heading text into an anchor id: Unicode letters and digits are
// kept and lowercased, whitespace runs collapse to one hyphen.
func Slugify(s string) string {
	s = html.UnescapeString(s)
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-':
			pendingDash = true
		}
	}
	return b.String()
}

// nodeText returns the plain visible text of a node subtree.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Segment.Value(src))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			case *ast.AutoLink:
				b.Write(v.URL(src))
			case *ast.RawHTML:
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// --- Node renderer ----------------------------------------------------------

type krateRenderer struct{ d *doc }

func (r *krateRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.fenced)
	reg.Register(ast.KindBlockquote, r.blockquote)
	reg.Register(kindTableWrap, r.tableWrap)
}

// Scroll containers are made keyboard focusable and named only when they are
// likely to overflow, so short blocks do not add a tab stop and an announced
// group each.
const (
	codeFocusCols     = 60 // longest line, in characters
	tableFocusCols    = 4  // table columns
	tableFocusChars   = 80 // widest row, total characters
	tableLabelColumns = 3
)

func (r *krateRenderer) tableWrap(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</div>\n")
		return ast.WalkContinue, nil
	}
	cols, widest, headers := tableShape(n, src)
	if cols < tableFocusCols && widest <= tableFocusChars {
		_, _ = w.WriteString("<div class=\"table-wrap\">\n")
		return ast.WalkContinue, nil
	}
	label := "Table"
	if len(headers) > 0 {
		shown := headers
		more := ""
		if len(shown) > tableLabelColumns {
			shown, more = shown[:tableLabelColumns], ", and more"
		}
		label = "Table with columns " + strings.Join(shown, ", ") + more
	}
	_, _ = w.WriteString("<div class=\"table-wrap\" role=\"group\" aria-label=\"" + escape.HTMLAttr(label) + "\" tabindex=\"0\">\n")
	return ast.WalkContinue, nil
}

// tableShape reports a wrapped table's column count, the character width of
// its widest row, and its non-empty header cell texts.
func tableShape(n ast.Node, src []byte) (cols, widest int, headers []string) {
	tbl := n.FirstChild()
	if tbl == nil {
		return 0, 0, nil
	}
	for row := tbl.FirstChild(); row != nil; row = row.NextSibling() {
		width, count := 0, 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			txt := strings.TrimSpace(nodeText(cell, src))
			width += len([]rune(txt))
			count++
			if _, isHeader := row.(*east.TableHeader); isHeader && txt != "" {
				headers = append(headers, txt)
			}
		}
		if count > cols {
			cols = count
		}
		if width > widest {
			widest = width
		}
	}
	return cols, widest, headers
}

func (r *krateRenderer) blockquote(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if v, ok := n.AttributeString(alertAttr); ok {
		typ, _ := v.(string)
		if entering {
			_, _ = w.WriteString("<div class=\"krate-aside krate-aside-" + AlertAsideType(typ) + "\"><div class=\"krate-aside-title\">" +
				asideTitle(AlertAsideType(typ), "") + "</div><div class=\"krate-aside-content\">\n")
		} else {
			_, _ = w.WriteString("</div></div>\n")
		}
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<blockquote>\n")
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

// AlertAsideType maps a GFM alert kind onto the admonition types.
func AlertAsideType(kind string) string {
	switch strings.ToLower(kind) {
	case "important":
		return "note"
	case "warning":
		return "warning"
	case "caution":
		return "danger"
	case "tip":
		return "tip"
	}
	return "note"
}

// splitInfo separates a fence info string into language and meta.
func splitInfo(info string) (lang, meta string) {
	info = strings.TrimSpace(info)
	if i := strings.IndexAny(info, " \t{"); i >= 0 {
		return info[:i], strings.TrimSpace(info[i:])
	}
	return info, ""
}

var (
	metaTitleRe = regexp.MustCompile(`(?:^|\s)title=(?:"([^"]*)"|'([^']*)')`)
	metaLinesRe = regexp.MustCompile(`\{([0-9,\- ]+)\}`)
)

// codeMeta extracts title="..." and {1,3-5} highlight ranges from fence meta.
func codeMeta(meta string) (title, lines string) {
	if m := metaTitleRe.FindStringSubmatch(meta); m != nil {
		title = m[1]
		if title == "" {
			title = m[2]
		}
	}
	if m := metaLinesRe.FindStringSubmatch(meta); m != nil {
		lines = strings.ReplaceAll(m[1], " ", "")
	}
	return
}

func (r *krateRenderer) fenced(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fc := n.(*ast.FencedCodeBlock)
	var info string
	if fc.Info != nil {
		info = string(fc.Info.Segment.Value(src))
	}
	lang, meta := splitInfo(info)
	var code bytes.Buffer
	for i := 0; i < fc.Lines().Len(); i++ {
		seg := fc.Lines().At(i)
		code.Write(seg.Value(src))
	}
	_, _ = w.WriteString(renderCode(code.String(), lang, meta, r.d.cfg))
	return ast.WalkSkipChildren, nil
}

// renderCode emits one fenced code block as HTML.
func renderCode(code, lang, meta string, cfg Config) string {
	lang = unescapeInfo(lang)
	if cfg.Mermaid && strings.EqualFold(lang, "mermaid") {
		return "<pre class=\"mermaid\">" + escape.HTML(strings.TrimRight(code, "\n")) + "</pre>\n"
	}
	title, hl := codeMeta(meta)
	var attrs string
	if title != "" {
		attrs += " data-title=\"" + escape.HTMLAttr(title) + "\""
	}
	if hl != "" {
		attrs += " data-highlight-lines=\"" + escape.HTMLAttr(hl) + "\""
	}
	class := ""
	if lang != "" {
		class = " class=\"language-" + escape.HTMLAttr(lang) + "\""
	}
	// A <pre> that is likely to scroll must be keyboard focusable and named.
	if longestLine(code) > codeFocusCols {
		label := "Code"
		if title != "" {
			label = title
		} else if lang != "" {
			label = lang + " code"
		}
		attrs += " role=\"group\" aria-label=\"" + escape.HTMLAttr(label) + "\" tabindex=\"0\""
	}
	var pre string
	if cfg.CodeHighlight && lang != "" {
		code = strings.TrimRight(code, "\n")
		hlCode := syntaxhighlight.HighlightThemeLines(code, syntaxhighlight.NormalizeLanguage(lang), cfg.CodeTheme, hl)
		pre = "<pre class=\"chroma\"" + attrs + "><code" + class + ">" + hlCode + "</code></pre>\n"
	} else {
		pre = "<pre" + attrs + "><code" + class + ">" + escape.HTML(code) + "</code></pre>\n"
	}
	if title == "" {
		return pre
	}
	// Static fences keep their title as a header, matching the <Code> component.
	return "<div class=\"krate-code-block\"><div class=\"krate-code-header\"><div class=\"krate-code-title\">" +
		escape.HTML(title) + "</div></div>" + pre + "</div>\n"
}

// longestLine returns the length in runes of the longest line of code.
func longestLine(code string) int {
	longest := 0
	for _, l := range strings.Split(code, "\n") {
		if n := len([]rune(l)); n > longest {
			longest = n
		}
	}
	return longest
}

var infoEscapeRe = regexp.MustCompile("\\\\([!-/:-@\\[-`{-~])")

// unescapeInfo resolves backslash escapes and entities in a fence language.
func unescapeInfo(s string) string {
	return html.UnescapeString(infoEscapeRe.ReplaceAllString(s, "$1"))
}
