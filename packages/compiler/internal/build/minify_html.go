package build

import (
	"strconv"
	"strings"
)

func minifyHTML(html string) string {
	// Protect <script> and <style> content from all minification passes.
	rawBlocks := protectRawContent(html)
	s := rawBlocks.protected

	// Pass 1: strip HTML comments except hydration markers
	s = stripHTMLComments(s)

	// Pass 2: collapse whitespace between tags
	s = collapseWhitespaceBetweenTags(s)

	// Pass 3: collapse multiple spaces in text content to single space
	s = collapseTextWhitespace(s)

	// Pass 4: remove optional quotes around simple attribute values
	s = removeOptionalQuotes(s)

	s = strings.TrimSpace(s)

	return restoreRawContent(s, rawBlocks)
}

// ─── Raw content protection ──────────────────────────────────────────────────

type rawBlocks struct {
	protected string
	blocks    []string
}

func protectRawContent(html string) rawBlocks {
	var blocks []string
	var result strings.Builder
	lower := strings.ToLower(html)
	i := 0
	for i < len(html) {
		tagStart := -1
		tagLen := 0
		for _, tag := range []string{"<script", "<style", "<pre"} {
			if strings.HasPrefix(lower[i:], tag) {
				chAfter := html[i+len(tag)]
				if chAfter == '>' || chAfter == ' ' || chAfter == '\t' || chAfter == '\n' || chAfter == '\r' {
					tagStart = i
					tagLen = len(tag)
					break
				}
			}
		}
		if tagStart == -1 {
			result.WriteByte(html[i])
			i++
			continue
		}

		tagName := html[tagStart : tagStart+tagLen]

		// Find end of opening tag
		openEnd := strings.Index(html[tagStart:], ">")
		if openEnd == -1 {
			result.WriteString(html[tagStart:])
			break
		}
		openEnd += tagStart

		// Find matching closing tag
		closeTag := "</" + tagName[1:] + ">"
		closeIdx := strings.Index(lower[openEnd+1:], closeTag)
		if closeIdx == -1 {
			// No closing tag — write as-is
			result.WriteString(html[tagStart:])
			break
		}
		closeIdx += openEnd + 1

		fullEnd := closeIdx + len(closeTag)

		// Preserve the full block with a placeholder
		blockIdx := len(blocks)
		blocks = append(blocks, html[tagStart:fullEnd])
		result.WriteString("\x00BLOCK")
		result.WriteString(strconv.Itoa(blockIdx))
		result.WriteString("\x00")

		i = fullEnd
	}

	return rawBlocks{protected: result.String(), blocks: blocks}
}

func restoreRawContent(html string, rb rawBlocks) string {
	result := html
	for i, block := range rb.blocks {
		result = strings.Replace(result, "\x00BLOCK"+strconv.Itoa(i)+"\x00", block, 1)
	}
	return result
}

// ─── Stripped-down helpers

// stripHTMLComments removes HTML comments except hydration markers (<!--k:...-->).
func stripHTMLComments(html string) string {
	var b strings.Builder
	i := 0
	for i < len(html) {
		if i+4 <= len(html) && html[i:i+4] == "<!--" {
			end := strings.Index(html[i:], "-->")
			if end == -1 {
				b.WriteString(html[i:])
				break
			}
			comment := html[i : i+end+3]
			// Preserve hydration markers: <!--k:...--> and <!--/k:...-->
			if len(comment) > 5 && comment[4] == 'k' && comment[5] == ':' {
				b.WriteString(comment)
			}
			if len(comment) > 6 && comment[4] == '/' && comment[5] == 'k' && comment[6] == ':' {
				b.WriteString(comment)
			}
			// Preserve suspense markers
			if strings.HasPrefix(comment, "<!--suspense:") || strings.HasPrefix(comment, "<!--/suspense:") {
				b.WriteString(comment)
			}
			i += end + 3
		} else {
			b.WriteByte(html[i])
			i++
		}
	}
	return b.String()
}

// collapseWhitespaceBetweenTags collapses whitespace between HTML tags.
func collapseWhitespaceBetweenTags(s string) string {
	var b strings.Builder
	inTag := false
	inAttr := false
	attrQuote := byte(0)
	prevSpace := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '<' && !inAttr {
			inTag = true
			prevSpace = false
			b.WriteByte(ch)
			continue
		}

		if ch == '>' && inTag && !inAttr {
			inTag = false
			b.WriteByte(ch)
			continue
		}

		if inTag {
			if (ch == '"' || ch == '\'') && !inAttr {
				inAttr = true
				attrQuote = ch
			} else if inAttr && ch == attrQuote {
				inAttr = false
			}
			b.WriteByte(ch)
			continue
		}

		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteByte(ch)
			prevSpace = false
		}
	}
	return b.String()
}

func collapseTextWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteByte(ch)
			prevSpace = false
		}
	}

	return b.String()
}

func removeOptionalQuotes(s string) string {
	var b strings.Builder
	inTag := false
	inAttr := false
	attrQuote := byte(0)

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '<' && !inAttr {
			inTag = true
		}

		if ch == '>' && inTag && !inAttr {
			inTag = false
		}

		if inTag {
			if (ch == '"' || ch == '\'') && !inAttr {
				// Look ahead: if value is only alphanumeric, dash, underscore, we can drop quotes
				j := i + 1
				canDrop := true
				for j < len(s) && s[j] != ch {
					if s[j] == ' ' || s[j] == '\t' || s[j] == '>' || s[j] == '=' {
						canDrop = false
						break
					}
					j++
				}
				isEmpty := j < len(s) && s[j] == ch && j == i+1
				if canDrop && !isEmpty {
					inAttr = true
					attrQuote = ch
					continue // skip opening quote; closing quote will be skipped too
				}
				if !isEmpty {
					// Non-empty but can't drop (contains spaces, >, or =):
					// enter attr mode to prevent premature > detection inside value
					inAttr = true
					attrQuote = ch
				}
				// Empty value (="") — never drop quotes; dropping creates a bare name=
				// which the HTML parser may merge with the next attribute
				// (e.g. onerror="" onload="" → onerror=onload=)
			} else if inAttr && ch == attrQuote {
				inAttr = false
				continue // skip closing quote
			} else if inAttr && ch == ' ' {
				inAttr = false
			}
		}

		b.WriteByte(ch)
	}

	return b.String()
}
