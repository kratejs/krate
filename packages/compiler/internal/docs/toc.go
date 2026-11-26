package docs

import (
	"fmt"
	"html"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// TOCItem represents a single heading entry in the table of contents.
type TOCItem struct {
	Title string `json:"title"`          // plain text (search, simple themes)
	HTML  string `json:"html,omitempty"` // sanitized inline HTML of the heading
	ID    string `json:"id"`             // anchor id
	Depth int    `json:"depth"`          // 2 for h2, 3 for h3
}

// ExtractTOC parses rendered HTML and extracts the default heading range
// (h2-h3) for the TOC sidebar.
func ExtractTOC(html string) []TOCItem {
	return ExtractTOCLevels(html, 2, 3)
}

// ExtractTOCLevels parses rendered HTML and extracts headings with levels in
// the inclusive [minLevel, maxLevel] range (2-6). Levels outside 1-6 and an
// empty range are clamped to the defaults (2-3).
func ExtractTOCLevels(html string, minLevel, maxLevel int) []TOCItem {
	if minLevel < 2 {
		minLevel = 2
	}
	if minLevel > 6 {
		minLevel = 6
	}
	if maxLevel < minLevel {
		maxLevel = minLevel
	}
	if maxLevel > 6 {
		maxLevel = 6
	}

	var items []TOCItem
	pos := 0
	for pos < len(html) {
		nextIdx := -1
		depth := 0
		for d := minLevel; d <= maxLevel; d++ {
			needle := fmt.Sprintf("<h%d", d)
			if idx := strings.Index(html[pos:], needle); idx >= 0 && (nextIdx < 0 || idx < nextIdx) {
				nextIdx = pos + idx
				depth = d
			}
		}
		if nextIdx < 0 || nextIdx >= len(html) {
			break
		}

		tagEnd := strings.Index(html[nextIdx:], ">")
		if tagEnd < 0 {
			break
		}
		openTag := html[nextIdx : nextIdx+tagEnd+1]
		idAttr := extractAttr(openTag, "id")
		if idAttr == "" {
			pos = nextIdx + tagEnd + 1
			continue
		}

		contentStart := nextIdx + tagEnd + 1
		closeTag := "</h"
		closeIdx := strings.Index(html[contentStart:], closeTag)
		if closeIdx < 0 {
			break
		}
		text := html[contentStart : contentStart+closeIdx]
		text = stripInlineHTML(text)

		items = append(items, TOCItem{
			Title: text,
			HTML:  tocInlineHTML(html[contentStart : contentStart+closeIdx]),
			ID:    idAttr,
			Depth: depth,
		})
		pos = contentStart + closeIdx + 5
	}
	return items
}

// InjectHeadingAnchors post-processes rendered HTML to add anchor link buttons
// to headings that have id attributes. The link comes after the heading text,
// labelled "Link to section: <heading>", with the "#" glyph aria-hidden.
func InjectHeadingAnchors(html string) string {
	var out strings.Builder
	pos := 0
	for pos < len(html) {
		start := strings.IndexAny(html[pos:], "<")
		if start < 0 {
			out.WriteString(html[pos:])
			break
		}
		start += pos
		out.WriteString(html[pos:start])

		if start+2 < len(html) && (html[start+1] == 'h' || html[start+1] == 'H') {
			tagEnd := strings.IndexByte(html[start:], '>')
			if tagEnd < 0 {
				out.WriteString(html[start:])
				break
			}
			tagEnd += start
			openTag := html[start : tagEnd+1]

			idAttr := extractAttr(openTag, "id")
			if idAttr != "" {
				contentStart := tagEnd + 1
				closeTag := "</h"
				closeIdx := strings.Index(html[contentStart:], closeTag)
				if closeIdx >= 0 {
					closeIdx += contentStart
					content := html[contentStart:closeIdx]

					escapedID := escape.HTMLAttr(idAttr)
					// The heading carries its text as aria-label so the link does not
					// leak into its name. The link is named after the heading and the "#" glyph is hidden from assistive technology. The
					// class and position are unchanged (the theme places it
					// absolutely in the margin).
					text := stripInlineHTML(content)
					label := escape.HTMLAttr("Link to section: " + text)
					// The heading is named by its own text, so the link inside
					// it does not become part of the heading's accessible name.
					openTag := html[start : tagEnd+1]
					if extractAttr(openTag, "aria-label") == "" && extractAttr(openTag, "aria-labelledby") == "" {
						openTag = openTag[:len(openTag)-1] + " aria-label=\"" + escape.HTMLAttr(text) + "\">"
					}
					out.WriteString(openTag)
					out.WriteString(content)
					out.WriteString("<a href=\"#")
					out.WriteString(escapedID)
					out.WriteString("\" class=\"heading-anchor\" data-pagefind-ignore aria-label=\"")
					out.WriteString(label)
					out.WriteString("\"><span aria-hidden=\"true\">#</span></a>")
					out.WriteString(html[closeIdx : closeIdx+5])
					pos = closeIdx + 5
					continue
				}
			}
		}

		out.WriteString(html[start : start+1])
		pos = start + 1
	}
	return out.String()
}

func extractAttr(tag, name string) string {
	search := name + "=\""
	idx := strings.Index(tag, search)
	if idx < 0 {
		return ""
	}
	start := idx + len(search)
	end := strings.IndexByte(tag[start:], '"')
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
}

func stripInlineHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	// Decode entities so a heading `Components &amp; helpers` yields the plain
	// text `Components & helpers` in the TOC/search index (not `&amp;`).
	return strings.TrimSpace(html.UnescapeString(out.String()))
}

// tocInlineAllowed lists the inline tags kept in a TOC entry's html field.
var tocInlineAllowed = map[string]bool{
	"code": true, "em": true, "strong": true, "kbd": true,
	"sub": true, "sup": true, "s": true,
}

// tocInlineHTML reduces heading inner HTML to a safe subset: the heading
// anchor link is dropped, the allowed inline tags are kept without attributes,
// other tags are removed (their text stays) and text is re-escaped. It returns
// "" when the result has no markup beyond the plain title.
func tocInlineHTML(s string) string {
	var out strings.Builder
	var stack []string // open allowed tags, innermost last
	marked := false
	skipAnchor := 0
	for i := 0; i < len(s); {
		if s[i] != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			if skipAnchor == 0 {
				out.WriteString(escape.HTML(html.UnescapeString(s[i : i+j])))
			}
			i += j
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			break
		}
		tag := s[i+1 : i+end]
		i += end + 1
		closing := strings.HasPrefix(tag, "/")
		name := strings.TrimPrefix(tag, "/")
		if k := strings.IndexAny(name, " \t\r\n/"); k >= 0 {
			name = name[:k]
		}
		name = strings.ToLower(name)
		if name == "a" {
			if closing {
				if skipAnchor > 0 {
					skipAnchor--
				}
			} else if strings.Contains(tag, "heading-anchor") || skipAnchor > 0 {
				skipAnchor++
			}
			continue
		}
		if skipAnchor > 0 || !tocInlineAllowed[name] {
			continue
		}
		if closing {
			if n := len(stack); n > 0 && stack[n-1] == name {
				stack = stack[:n-1]
				out.WriteString("</" + name + ">")
			}
			continue
		}
		if strings.HasSuffix(tag, "/") {
			continue
		}
		stack = append(stack, name)
		marked = true
		out.WriteString("<" + name + ">")
	}
	for k := len(stack) - 1; k >= 0; k-- {
		out.WriteString("</" + stack[k] + ">")
	}
	if !marked {
		return ""
	}
	return strings.TrimSpace(out.String())
}
