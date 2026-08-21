package docs

import (
	"strings"
	"unicode"

	"krate-compiler/internal/escape"
)

// TOCItem represents a single heading entry in the table of contents.
type TOCItem struct {
	Title string `json:"title"` // displayed text
	ID    string `json:"id"`    // anchor id
	Depth int    `json:"depth"` // 2 for h2, 3 for h3
}

// ExtractTOC parses rendered HTML and extracts heading structure for the TOC sidebar.
func ExtractTOC(html string) []TOCItem {
	var items []TOCItem
	pos := 0
	for pos < len(html) {
		h2Idx := strings.Index(html[pos:], "<h2")
		h3Idx := strings.Index(html[pos:], "<h3")
		nextIdx := -1
		depth := 0
		if h2Idx >= 0 && (h3Idx < 0 || h2Idx < h3Idx) {
			nextIdx = pos + h2Idx
			depth = 2
		} else if h3Idx >= 0 {
			nextIdx = pos + h3Idx
			depth = 3
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
			ID:    idAttr,
			Depth: depth,
		})
		pos = contentStart + closeIdx + 5
	}
	return items
}

// InjectHeadingAnchors post-processes rendered HTML to add anchor link buttons
// to headings that have id attributes.
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
					out.WriteString(html[start : tagEnd+1])
					out.WriteString("<a href=\"#")
					out.WriteString(escapedID)
					out.WriteString("\" class=\"heading-anchor\" aria-label=\"Link to this section\">#</a>")
					out.WriteString(content)
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
	return strings.TrimSpace(out.String())
}

// HasLetters checks if a string contains any letter characters.
func HasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
