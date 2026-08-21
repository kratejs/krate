package markdown

import (
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
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
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

// MDXSegment represents either HTML text or a JSX block for TSX generation.
type MDXSegment struct {
	HTML string // rendered HTML for markdown parts
	JSX  string // JSX source for JSX blocks (e.g., "<CustomComponent>test</CustomComponent>")
}

// ParseMDXSegments parses MDX and returns ordered segments (HTML + JSX blocks)
// suitable for generating TSX source code. JSX blocks are returned as raw JSX strings
// that can be embedded directly in TSX output.
func ParseMDXSegments(src string, cfg Config) (frontmatter map[string]string, segments []MDXSegment) {
	frontmatter, body := extractFrontmatter(src)

	// Strip import lines from the body so they don't render as markdown text.
	bodyLines := strings.Split(body, "\n")
	var bodyWithoutImports []string
	inFrontmatter := false
	for _, line := range bodyLines {
		trimmed := strings.TrimSpace(line)
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

	type jsxInfo struct {
		Index    int
		Tag      string
		Attrs    string
		Children string
		SelfClose bool
	}
	var jsxBlocks []jsxInfo
	var processed strings.Builder
	lines := strings.Split(body, "\n")
	i := 0
	idx := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

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
				info := jsxInfo{
					Index:     idx,
					Tag:       tag,
					Attrs:     attrs,
					Children:  children,
					SelfClose: false,
				}
				jsxBlocks = append(jsxBlocks, info)
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
				info := jsxInfo{
					Index:     idx,
					Tag:       tag,
					Attrs:     attrs,
					Children:  children,
					SelfClose: false,
				}
				jsxBlocks = append(jsxBlocks, info)
				processed.WriteString("__KRATE_MDX_")
				processed.WriteString(runeToStr(idx))
				processed.WriteString("__\n")
				idx++
				i = j + 1
				if j >= len(lines) {
					i = endLine + 1
				}
			} else {
				// Self-closing: strip trailing " /" from attrs if present
				cleanAttrs := attrs
				cleanAttrs = strings.TrimSuffix(strings.TrimSpace(cleanAttrs), "/")
				cleanAttrs = strings.TrimSpace(cleanAttrs)
				info := jsxInfo{
					Index:     idx,
					Tag:       tag,
					Attrs:     cleanAttrs,
					Children:  "",
					SelfClose: true,
				}
				jsxBlocks = append(jsxBlocks, info)
				processed.WriteString("__KRATE_MDX_")
				processed.WriteString(runeToStr(idx))
				processed.WriteString("__\n")
				idx++
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
	// inline content in <p> tags, but JSX block placeholders should be block-level.
	// e.g., <p>__KRATE_MDX_0__</p> → __KRATE_MDX_0__
	for i := 0; i < len(jsxBlocks); i++ {
		placeholder := "__KRATE_MDX_" + runeToStr(i) + "__"
		html = strings.ReplaceAll(html, "<p>"+placeholder+"</p>", placeholder)
	}

	// Split HTML at placeholders to create segments
	var segs []MDXSegment
	remaining := html
	for _, jsx := range jsxBlocks {
		placeholder := "__KRATE_MDX_" + runeToStr(jsx.Index) + "__"
		pos := strings.Index(remaining, placeholder)
		if pos < 0 {
			continue
		}
		if pos > 0 {
			segs = append(segs, MDXSegment{HTML: remaining[:pos]})
		}
		// Build the JSX string for this block
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
				childHTML := RenderToHTML(jsx.Children, DefaultConfig())
				jsxStr.WriteString(childHTML)
			}
			jsxStr.WriteString("</")
			jsxStr.WriteString(jsx.Tag)
			jsxStr.WriteString(">")
		}
		segs = append(segs, MDXSegment{JSX: jsxStr.String()})
		remaining = remaining[pos+len(placeholder):]
	}
	if remaining != "" {
		segs = append(segs, MDXSegment{HTML: remaining})
	}

	return frontmatter, segs
}

