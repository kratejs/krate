package markdown

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// MDXResult holds the parsed result of an MDX file.
type MDXResult struct {
	Frontmatter map[string]string
	HTML        string
	JSXBlocks   []JSXBlock
}

// JSXBlock represents a JSX component embedded in MDX content.
type JSXBlock struct {
	Index    int    // position in the output order
	Tag      string // component name
	Attrs    string // raw attributes string
	Children string // content between opening and closing tags
}

// ParseMDX splits an MDX source into frontmatter, Markdown content, and JSX blocks.
// Returns the frontmatter map and the rendered HTML with JSX placeholders.
func ParseMDX(src string, cfg Config) *MDXResult {
	frontmatter, body := extractFrontmatter(src)

	// Replace JSX blocks with placeholders
	var jsxBlocks []JSXBlock
	var processed strings.Builder
	lines := strings.Split(body, "\n")
	i := 0
	idx := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Detect JSX opening tag: <ComponentName ...>
		if isJSXStart(trimmed) {
			tag, attrs, rest, endLine := extractJSXTag(lines, i)
			children := ""
			selfClose := strings.HasSuffix(strings.TrimSpace(lines[endLine]), "/>")
			if !selfClose {
				// Check if inline (rest contains closing tag)
				closeTag := "</" + tag + ">"
				if strings.Contains(rest, closeTag) {
					cIdx := strings.Index(rest, closeTag)
					children = strings.TrimSpace(rest[:cIdx])
					// Rest after closing tag is still part of this line
					afterClose := strings.TrimSpace(rest[cIdx+len(closeTag):])
					// Put it back into the current line for processing
					if afterClose != "" {
						lines[i] = afterClose
						i-- // will be incremented at end of else branch
					}
					block := JSXBlock{
						Index:    idx,
						Tag:      tag,
						Attrs:    attrs,
						Children: children,
					}
					jsxBlocks = append(jsxBlocks, block)
					processed.WriteString("__KRATE_MDX_")
					processed.WriteString(runeToStr(idx))
					processed.WriteString("__\n")
					idx++
					i++
					continue
				}

				// Multi-line: find closing tag in subsequent lines
				j := endLine + 1
				for j < len(lines) {
					if strings.Contains(lines[j], closeTag) {
						if endLine+1 <= j-1 {
							var childLines []string
							for k := endLine + 1; k < j; k++ {
								childLines = append(childLines, lines[k])
							}
							children = strings.Join(childLines, "\n")
						}
						break
					}
					j++
				}

				block := JSXBlock{
					Index:    idx,
					Tag:      tag,
					Attrs:    attrs,
					Children: children,
				}
				jsxBlocks = append(jsxBlocks, block)
				processed.WriteString("__KRATE_MDX_")
				processed.WriteString(runeToStr(idx))
				processed.WriteString("__\n")

				idx++
				if j < len(lines) {
					i = j + 1
				} else {
					i = endLine + 1
				}
			} else {
				block := JSXBlock{
					Index:    idx,
					Tag:      tag,
					Attrs:    attrs,
					Children: "",
				}
				jsxBlocks = append(jsxBlocks, block)
				processed.WriteString("__KRATE_MDX_")
				processed.WriteString(runeToStr(idx))
				processed.WriteString("__\n")

				idx++
				i++
			}
		} else {
			processed.WriteString(line)
			processed.WriteByte('\n')
			i++
		}
	}

	html := RenderToHTML(processed.String(), cfg)

	return &MDXResult{
		Frontmatter: frontmatter,
		HTML:        html,
		JSXBlocks:   jsxBlocks,
	}
}

// ReinsertJSXBlocks replaces placeholders in HTML with the actual JSX block content.
func (r *MDXResult) ReinsertJSXBlocks() string {
	result := r.HTML
	for _, jsx := range r.JSXBlocks {
		placeholder := "__KRATE_MDX_" + runeToStr(jsx.Index) + "__"
		// Build JSX string
		var jsxStr strings.Builder
		jsxStr.WriteString("<")
		jsxStr.WriteString(jsx.Tag)
		if jsx.Attrs != "" {
			jsxStr.WriteString(" ")
			jsxStr.WriteString(jsx.Attrs)
		}
		jsxStr.WriteString(">")
		if jsx.Children != "" {
			// Render children as Markdown too (recursive)
			childHTML := RenderToHTML(jsx.Children, DefaultConfig())
			jsxStr.WriteString(childHTML)
		}
		jsxStr.WriteString("</")
		jsxStr.WriteString(jsx.Tag)
		jsxStr.WriteString(">")
		result = strings.ReplaceAll(result, placeholder, jsxStr.String())
	}
	return result
}

func extractFrontmatter(src string) (map[string]string, string) {
	// Strip a leading UTF-8 BOM so files saved with one still parse their
	// frontmatter and body correctly.
	src = strings.TrimPrefix(src, "\ufeff")
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, src
	}

	fm := make(map[string]string)
	i := 1
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "---" {
			i++
			break
		}
		line := lines[i]
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, "\"'")
			fm[key] = val
		}
		i++
	}

	body := strings.Join(lines[i:], "\n")
	return fm, body
}

func isJSXStart(s string) bool {
	if !strings.HasPrefix(s, "<") {
		return false
	}
	if strings.HasPrefix(s, "</") || strings.HasPrefix(s, "<!") || strings.HasPrefix(s, "<?") {
		return false
	}
	// Only uppercase component names are JSX blocks
	for _, r := range s[1:] {
		if r == ' ' || r == '>' || r == '/' {
			return false
		}
		if unicode.IsUpper(r) || r == '_' {
			return true
		}
		return false
	}
	return false
}

func extractJSXTag(lines []string, start int) (tag, attrs, rest string, endLine int) {
	line := lines[start]
	// Remove leading '<'
	content := strings.TrimSpace(line[1:])

	// Extract tag name - it's the first word before space, >, or /
	tagEnd := len(content)
	for i, r := range content {
		if r == ' ' || r == '>' || r == '/' {
			tagEnd = i
			break
		}
	}
	tag = content[:tagEnd]

	// If line has ">test</Tag>" pattern, inline children
	attrStart := tagEnd
	// Find the closing > of the opening tag
	for attrStart < len(content) {
		if content[attrStart] == '>' {
			// Check if this is a self-closing tag: />
			if attrStart > 0 && content[attrStart-1] == '/' {
				attrs = strings.TrimSpace(content[tagEnd : attrStart-1])
			} else {
				attrs = strings.TrimSpace(content[tagEnd:attrStart])
			}
			// Remaining content after > is either inline children or rest of line
			rest = strings.TrimSpace(content[attrStart+1:])
			return tag, attrs, rest, start
		}
		attrStart++
	}

	// No > found on this line - multi-line attributes
	attrs = strings.TrimSpace(content[tagEnd:])
	return tag, attrs, "", start
}

func runeToStr(i int) string {
	if i < 26 {
		return string(rune('a' + i))
	}
	return string(rune('A' + (i - 26)))
}

// ExtractImports pulls import statements from MDX/TSX source lines.
// Returns only lines that start with "import " (after trimming whitespace).
func ExtractImports(src string) []string {
	lines := strings.Split(src, "\n")
	var imports []string
	inFrontmatter := false
	inCodeFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fenceRe.MatchString(trimmed) {
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			imports = append(imports, trimmed)
		}
	}
	return imports
}

// HTMLToJSX converts rendered HTML to JSX-compatible syntax.
// Specifically converts void elements to self-closing tags.
func HTMLToJSX(html string) string {
	voidElements := []string{"hr", "img", "br", "input", "meta", "link", "col", "area", "base", "embed", "param", "source", "track", "wbr"}
	for _, tag := range voidElements {
		// Match opening tag without self-closing slash: <tag ...> (but not <tag .../>)
		re := regexp.MustCompile(`(?i)(<` + tag + `\b[^>]*?)\s*>`)
		html = re.ReplaceAllStringFunc(html, func(match string) string {
			// If already self-closing or has a closing tag right after, skip
			if strings.HasSuffix(strings.TrimSpace(match), "/>") {
				return match
			}
			return strings.TrimRight(match, ">") + " />"
		})
	}
	return html
}

// MDXSegment represents either HTML text, a JSX block, or a code block for TSX generation.
type MDXSegment struct {
	HTML  string        // rendered HTML for markdown parts
	JSX   string        // JSX source for JSX blocks (e.g., "<CustomComponent>test</CustomComponent>")
	Code  *CodeSegment  // fenced code block, rendered as <Code> component
	Aside *AsideSegment // admonition (:::note), rendered as <Aside> component
}

// CodeSegment holds a fenced code block captured from markdown so it can be
// rendered as the <Code> component (with copy button + header) instead of a
// plain static <pre>.
type CodeSegment struct {
	Lang string // fenced code block language (may be empty)
	Code string // raw code content
}

// AsideSegment holds an admonition (:::type ... :::) captured from markdown so
// it can be rendered as the <Aside> component instead of Go-generated markup.
type AsideSegment struct {
	Type      string // admonition type: note | tip | warning | danger | caution
	Title     string // resolved title (Note, Tip, ...) passed explicitly
	InnerHTML string // rendered markdown for the admonition body
}

// seqMarker is a single entry (in document order) marking where a JSX block,
// code block, or admonition sits within the rendered HTML, so segments can be
// interleaved in the correct position.
type seqMarker struct {
	placeholder string       // marker used to locate position in rendered HTML
	jsx         *jsxBlockT   // set for JSX blocks
	code        *codeBlockT  // set for code blocks
	aside       *asideBlockT // set for admonitions
}

type jsxBlockT struct {
	Tag      string
	Attrs    string
	Children string
	SelfClose bool
}

type codeBlockT struct {
	Lang string
	Code string
}

type asideBlockT struct {
	Type      string
	Title     string
	InnerHTML string
}

// makePlaceholder generates a unique marker token for the given numeric index.
func makePlaceholder(prefix string, idx int) string {
	return "__KRATE_" + prefix + "_" + runeToStr(idx) + "__"
}

// ParseMDXSegments parses MDX (or plain Markdown) and returns ordered segments
// (HTML + JSX blocks + code blocks) suitable for generating TSX source code.
// JSX blocks are returned as raw JSX strings, and fenced code blocks are
// returned as Code segments that render the <Code> component — both are
// embedded directly in TSX output.
func ParseMDXSegments(src string, cfg Config) (frontmatter map[string]string, segments []MDXSegment) {
	frontmatter, body := extractFrontmatter(src)

	// Strip import lines from the body so they don't render as markdown text.
	// Lines inside fenced code blocks are preserved — an "import ..." line used
	// as a code sample must not be hoisted or removed from its block.
	bodyLines := strings.Split(body, "\n")
	var bodyWithoutImports []string
	inFrontmatter := false
	inCodeFence := false
	for _, line := range bodyLines {
		trimmed := strings.TrimSpace(line)
		if fenceRe.MatchString(trimmed) {
			inCodeFence = !inCodeFence
		}
		if inCodeFence {
			bodyWithoutImports = append(bodyWithoutImports, line)
			continue
		}
		if trimmed == "---" {
			inFrontmatter = !inFrontmatter
		}
		if inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			continue
		}
		bodyWithoutImports = append(bodyWithoutImports, line)
	}
	body = strings.Join(bodyWithoutImports, "\n")

	var markers []seqMarker
	var processed strings.Builder
	lines := strings.Split(body, "\n")
	i := 0
	jsxIdx := 0
	codeIdx := 0
	asideIdx := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Admonition (:::type ... / :::::: ) → <Aside> segment.
		if m := admonRe.FindStringSubmatch(trimmed); m != nil && strings.HasPrefix(trimmed, ":") {
			adType := strings.ToLower(strings.TrimSpace(m[1]))
			adTitleLine := strings.TrimSpace(m[2])
			var inner []string
			closer := strings.Repeat(":", len(m[0])-len(strings.TrimLeft(m[0], ":")))
			j := i + 1
			for ; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == closer {
					break
				}
				inner = append(inner, lines[j])
			}
			title := asideTitle(adType, adTitleLine)
			innerHTML := RenderToHTML(strings.Join(inner, "\n"), cfg)
			placeholder := makePlaceholder("ASIDE", asideIdx)
			processed.WriteString(placeholder)
			processed.WriteByte('\n')
			markers = append(markers, seqMarker{
				placeholder: placeholder,
				aside: &asideBlockT{
					Type:      adType,
					Title:     title,
					InnerHTML: innerHTML,
				},
			})
			asideIdx++
			i = j + 1
			continue
		}

		// Fenced code block → <Code> segment.
		if fenceRe.MatchString(trimmed) {
			lang, codeLines, next := collectFencedCode(lines, i)
			placeholder := makePlaceholder("CODE", codeIdx)
			processed.WriteString(placeholder)
			processed.WriteByte('\n')
			markers = append(markers, seqMarker{
				placeholder: placeholder,
				code:        &codeBlockT{Lang: lang, Code: strings.Join(codeLines, "\n")},
			})
			codeIdx++
			i = next
			continue
		}

		if isJSXStart(trimmed) {
			tag, attrs, rest, endLine := extractJSXTag(lines, i)
			selfClose := strings.HasSuffix(strings.TrimSpace(lines[endLine]), "/>")

			children := ""
			if !selfClose {
				closeTag := "</" + tag + ">"
				// Check if inline (rest contains closing tag)
				if strings.Contains(rest, closeTag) {
					cIdx := strings.Index(rest, closeTag)
					children = strings.TrimSpace(rest[:cIdx])
					afterClose := strings.TrimSpace(rest[cIdx+len(closeTag):])
					if afterClose != "" {
						lines[i] = afterClose
						i--
					}
					info := &jsxBlockT{
						Tag:      tag,
						Attrs:    attrs,
						Children: children,
						SelfClose: false,
					}
					placeholder := makePlaceholder("MDX", jsxIdx)
					processed.WriteString(placeholder)
					processed.WriteByte('\n')
					markers = append(markers, seqMarker{placeholder: placeholder, jsx: info})
					jsxIdx++
					i++
					continue
				}

				// Multi-line: find closing tag in subsequent lines
				j := endLine + 1
				for j < len(lines) {
					if strings.Contains(lines[j], closeTag) {
						if endLine+1 <= j-1 {
							var childLines []string
							for k := endLine + 1; k < j; k++ {
								childLines = append(childLines, lines[k])
							}
							children = strings.Join(childLines, "\n")
						}
						break
					}
					j++
				}
				info := &jsxBlockT{
					Tag:      tag,
					Attrs:    attrs,
					Children: children,
					SelfClose: false,
				}
				placeholder := makePlaceholder("MDX", jsxIdx)
				processed.WriteString(placeholder)
				processed.WriteByte('\n')
				markers = append(markers, seqMarker{placeholder: placeholder, jsx: info})
				jsxIdx++
				i = j + 1
				if j >= len(lines) {
					i = endLine + 1
				}
			} else {
				// Self-closing: strip trailing " /" from attrs if present
				cleanAttrs := attrs
				cleanAttrs = strings.TrimSuffix(strings.TrimSpace(cleanAttrs), "/")
				cleanAttrs = strings.TrimSpace(cleanAttrs)
				info := &jsxBlockT{
					Tag:      tag,
					Attrs:    cleanAttrs,
					Children: "",
					SelfClose: true,
				}
				placeholder := makePlaceholder("MDX", jsxIdx)
				processed.WriteString(placeholder)
				processed.WriteByte('\n')
				markers = append(markers, seqMarker{placeholder: placeholder, jsx: info})
				jsxIdx++
				i = endLine + 1
			}
		} else {
			processed.WriteString(line)
			processed.WriteByte('\n')
			i++
		}
	}

	html := RenderToHTML(processed.String(), cfg)

	// Clean up <p> wrappers around placeholders. The markdown renderer wraps
	// inline content in <p> tags, but block placeholders should be block-level.
	// e.g., <p>__KRATE_..._0__</p> → __KRATE_..._0__
	for _, m := range markers {
		html = strings.ReplaceAll(html, "<p>"+m.placeholder+"</p>", m.placeholder)
	}

	// Split HTML at markers to create segments in document order.
	var segs []MDXSegment
	remaining := html
	for _, m := range markers {
		pos := strings.Index(remaining, m.placeholder)
		if pos < 0 {
			continue
		}
		if pos > 0 {
			segs = append(segs, MDXSegment{HTML: remaining[:pos]})
		}
		if m.jsx != nil {
			segs = append(segs, MDXSegment{JSX: buildJSXString(m.jsx)})
		} else if m.code != nil {
			segs = append(segs, MDXSegment{Code: &CodeSegment{Lang: m.code.Lang, Code: m.code.Code}})
		} else if m.aside != nil {
			segs = append(segs, MDXSegment{Aside: &AsideSegment{
				Type:      m.aside.Type,
				Title:     m.aside.Title,
				InnerHTML: m.aside.InnerHTML,
			}})
		}
		remaining = remaining[pos+len(m.placeholder):]
	}
	if remaining != "" {
		segs = append(segs, MDXSegment{HTML: remaining})
	}

	return frontmatter, segs
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
	var sb strings.Builder
	for _, seg := range segments {
		sb.WriteString(seg.HTML)
		if seg.JSX != "" {
			sb.WriteString(seg.JSX)
		}
		if seg.Code != nil {
			sb.WriteString("<pre>")
			sb.WriteString(html.EscapeString(seg.Code.Code))
			sb.WriteString("</pre>")
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
	sb.WriteString("<Aside type=\"")
	sb.WriteString(escapeJSXString(ad.Type))
	sb.WriteString("\" title=\"")
	sb.WriteString(escapeJSXString(ad.Title))
	sb.WriteString("\"><div dangerouslySetInnerHTML={{__html:`")
	sb.WriteString(escapeTemplateLiteral(ad.InnerHTML))
	sb.WriteString("`}} /></Aside>")
	return sb.String()
}

func buildJSXString(jsx *jsxBlockT) string {
	var jsxStr strings.Builder
	jsxStr.WriteString("<")
	jsxStr.WriteString(jsx.Tag)
	if jsx.Attrs != "" {
		jsxStr.WriteString(" ")
		jsxStr.WriteString(jsx.Attrs)
	}
	if jsx.SelfClose {
		jsxStr.WriteString(" />")
	} else {
		jsxStr.WriteString(">")
		if jsx.Children != "" {
			// JSX children are kept as raw text — not processed as markdown.
			// MDX treats content inside JSX components as JSX, not markdown.
			jsxStr.WriteString(jsx.Children)
		}
		jsxStr.WriteString("</")
		jsxStr.WriteString(jsx.Tag)
		jsxStr.WriteString(">")
	}
	return jsxStr.String()
}

// BuildCodeJSX renders a fenced code block as a <Code> component JSX string,
// passing the raw code as template-literal children so build-time chroma
// highlighting and the client-side copy/header UI both apply.
func BuildCodeJSX(lang, code string) string {
	var sb strings.Builder
	sb.WriteString("<Code")
	if lang != "" {
		sb.WriteString(" lang=\"")
		sb.WriteString(escapeJSXString(lang))
		sb.WriteString("\"")
	}
	sb.WriteString(">{`")
	sb.WriteString(escapeTemplateLiteral(code))
	sb.WriteString("`}</Code>")
	return sb.String()
}

func escapeJSXString(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
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
// i (which must match fenceRe). Returns the language tag, the code lines, and
// the index just past the closing fence.
func collectFencedCode(lines []string, i int) (lang string, code []string, next int) {
	// The caller matches against the trimmed line, so the raw line here may
	// carry leading whitespace (indented fences). fenceRe is ^-anchored, so
	// always match against the trimmed opening line.
	open := strings.TrimSpace(lines[i])
	lang = fenceRe.FindStringSubmatch(open)[1]
	i++
	closeRe := regexp.MustCompile("^`{3,}")
	closer := closeRe.FindString(fenceRe.FindString(open))
	if closer == "" {
		closer = "```"
	}
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == closer || (len(trimmed) >= len(closer) && strings.TrimRight(trimmed, "`") == "") {
			i++
			return lang, code, i
		}
		code = append(code, lines[i])
		i++
	}
	return lang, code, i
}

