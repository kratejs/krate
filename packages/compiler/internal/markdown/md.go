package markdown

import (
	"fmt"
	"regexp"
	"strings"

	"krate-compiler/internal/escape"
	"krate-compiler/internal/icons"
)

// RenderToHTML converts Markdown source to HTML using the given config.
func RenderToHTML(src string, cfg Config) string {
	lines := strings.Split(src, "\n")
	blocks := parseBlocks(lines, cfg)
	var out strings.Builder
	for _, b := range blocks {
		out.WriteString(renderBlock(b, cfg))
	}
	return out.String()
}

type blockType int

const (
	bParagraph blockType = iota
	bHeading
	bCodeBlock
	bFencedCode
	bBlockquote
	bUnorderedList
	bOrderedList
	bTable
	bThematicBreak
	bAdmonition
	bHTML
	bBlank
)

type block struct {
	typ    blockType
	level  int       // heading level, list indent, etc.
	info   string    // fenced code language, admonition type
	lines  []string  // raw lines
	cells  [][]string // table cells
	items  []block   // list items, blockquote children
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*)`)
	fenceRe    = regexp.MustCompile("^`{3,}" + `(\w*)\s*$`)
	taskRe     = regexp.MustCompile(`^(\s*[-*+]\s+)\[([ xX])\]\s+(.*)`)
	tableSepRe = regexp.MustCompile(`^\|[\s:-]+\|`)
	admonRe    = regexp.MustCompile(`^:{3,}(\w+)\s*(.*)`)
	hrRe       = regexp.MustCompile(`^[-*_]{3,}\s*$`)
	linkRe     = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	imageRe    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
)

func parseBlocks(lines []string, cfg Config) []block {
	var blocks []block
	i := 0
	for i < len(lines) {
		line := lines[i]

		// Blank line
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		// Thematic break
		if hrRe.MatchString(line) {
			blocks = append(blocks, block{typ: bThematicBreak})
			i++
			continue
		}

		// Admonition
		if cfg.Admonitions {
		if m := admonRe.FindStringSubmatch(line); m != nil {
			typ := m[1]
			var adLines []string
			i++
			// Extract only the colon prefix from the match for the closer
			cols := len(m[0]) - len(strings.TrimLeft(m[0], ":"))
			closer := strings.Repeat(":", cols)
			for i < len(lines) {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == closer {
					i++
					break
				}
					adLines = append(adLines, lines[i])
					i++
				}
				blocks = append(blocks, block{
					typ:   bAdmonition,
					info:  typ,
					lines: adLines,
				})
				continue
			}
		}

	// Fenced code block
	if m := fenceRe.FindStringSubmatch(line); m != nil {
		lang := m[1]
		var code []string
		i++
		// The closer is just the backtick sequence, NOT including the language tag
		// e.g. ```tsx opens with ``` and should close with ``` (not ```tsx)
		closeRe := regexp.MustCompile("^`{3,}")
		closer := closeRe.FindString(m[0])
		if closer == "" {
			closer = m[0]
		}
		for i < len(lines) {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == closer || (len(trimmed) >= len(closer) && strings.TrimRight(trimmed, "`") == "") {
				i++
				break
			}
			code = append(code, lines[i])
			i++
		}
			blocks = append(blocks, block{
				typ:   bFencedCode,
				info:  lang,
				lines: code,
			})
			continue
		}

		// Heading
		if m := headingRe.FindStringSubmatch(line); m != nil {
			blocks = append(blocks, block{
				typ:   bHeading,
				level: len(m[1]),
				lines: []string{strings.TrimSpace(m[2])},
			})
			i++
			continue
		}

		// Blockquote
		if strings.HasPrefix(line, "> ") || line == ">" {
			var qLines []string
			for i < len(lines) {
				l := lines[i]
				if strings.HasPrefix(l, "> ") {
					qLines = append(qLines, strings.TrimPrefix(l, "> "))
					i++
				} else if l == ">" {
					qLines = append(qLines, "")
					i++
				} else {
					break
				}
			}
			blocks = append(blocks, block{
				typ:   bBlockquote,
				lines: qLines,
			})
			continue
		}

		// Unordered list
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
			var items []block
			var itemLines []string
			for i < len(lines) {
				l := lines[i]
				if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "+ ") {
					if len(itemLines) > 0 {
						items = append(items, block{typ: bParagraph, lines: itemLines})
					}
					itemLines = []string{l[2:]}
					i++
				} else if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") && !strings.HasPrefix(l, "+ ") && !strings.HasPrefix(l, "#") {
					itemLines = append(itemLines, l)
					i++
				} else {
					break
				}
			}
			if len(itemLines) > 0 {
				items = append(items, block{typ: bParagraph, lines: itemLines})
			}
			blocks = append(blocks, block{typ: bUnorderedList, items: items})
			continue
		}

		// Ordered list
		if matched, _ := regexp.MatchString(`^\d+\.\s+`, line); matched {
			var items []block
			var itemLines []string
			for i < len(lines) {
				l := lines[i]
				if matched2, _ := regexp.MatchString(`^\d+\.\s+`, l); matched2 {
					if len(itemLines) > 0 {
						items = append(items, block{typ: bParagraph, lines: itemLines})
					}
					parts := strings.SplitN(l, ". ", 2)
					if len(parts) == 2 {
						itemLines = []string{parts[1]}
					} else {
						itemLines = []string{l}
					}
					i++
				} else if strings.TrimSpace(l) != "" {
					itemLines = append(itemLines, l)
					i++
				} else {
					break
				}
			}
			if len(itemLines) > 0 {
				items = append(items, block{typ: bParagraph, lines: itemLines})
			}
			blocks = append(blocks, block{typ: bOrderedList, items: items})
			continue
		}

		// Table
		if strings.HasPrefix(line, "|") && i+1 < len(lines) && tableSepRe.MatchString(lines[i+1]) {
			var rows [][]string
			rows = append(rows, parseTableRow(line))
			i++
			i++ // skip separator
			for i < len(lines) && strings.HasPrefix(lines[i], "|") {
				rows = append(rows, parseTableRow(lines[i]))
				i++
			}
			blocks = append(blocks, block{typ: bTable, cells: rows})
			continue
		}

		// Raw HTML block (line starts with <)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "</") {
			var htmlLines []string
			htmlLines = append(htmlLines, trimmed)
			i++
			for i < len(lines) {
				l := strings.TrimSpace(lines[i])
				if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "+ ") {
					break
				}
				if strings.HasPrefix(l, "<") && strings.HasPrefix(l, "</") {
					htmlLines = append(htmlLines, l)
					i++
					continue
				}
				if strings.HasPrefix(l, "<") {
					htmlLines = append(htmlLines, l)
					i++
					continue
				}
				htmlLines = append(htmlLines, lines[i])
				i++
			}
			blocks = append(blocks, block{typ: bHTML, lines: htmlLines})
			continue
		}

		// Paragraph (collect consecutive non-blank, non-heading, non-list lines)
		var paraLines []string
		for i < len(lines) {
			l := lines[i]
			trimmed := strings.TrimSpace(l)
			if trimmed == "" {
				break
			}
			if strings.HasPrefix(l, "#") {
				break
			}
			if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "+ ") {
				break
			}
			paraLines = append(paraLines, l)
			i++
		}
		if len(paraLines) > 0 {
			blocks = append(blocks, block{typ: bParagraph, lines: paraLines})
		} else {
			i++
		}
	}
	return blocks
}

func parseTableRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	cells := strings.Split(trimmed, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func renderBlock(b block, cfg Config) string {
	switch b.typ {
	case bHeading:
		return renderHeading(b, cfg)
	case bParagraph:
		return renderParagraph(b, cfg)
	case bFencedCode:
		return renderCodeBlock(b, cfg)
	case bBlockquote:
		return renderBlockquote(b, cfg)
	case bUnorderedList:
		return renderList(b, "ul", cfg)
	case bOrderedList:
		return renderList(b, "ol", cfg)
	case bTable:
		return renderTable(b, cfg)
	case bThematicBreak:
		return "<hr>\n"
	case bAdmonition:
		return renderAdmonition(b, cfg)
	case bHTML:
		return strings.Join(b.lines, "\n") + "\n"
	default:
		return ""
	}
}

func renderHeading(b block, cfg Config) string {
	text := renderInline(b.lines[0], cfg)
	id := ""
	if cfg.HeadingAnchors {
		id = slugify(text)
	}
	if id != "" {
		return fmt.Sprintf("<h%d id=\"%s\">%s</h%d>\n", b.level, id, text, b.level)
	}
	return fmt.Sprintf("<h%d>%s</h%d>\n", b.level, text, b.level)
}

func renderParagraph(b block, cfg Config) string {
	text := renderInline(strings.Join(b.lines, " "), cfg)
	return fmt.Sprintf("<p>%s</p>\n", text)
}

func renderCodeBlock(b block, cfg Config) string {
	code := strings.Join(b.lines, "\n")
	code = escape.HTML(code)
	if b.info != "" {
		return fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>\n", b.info, code)
	}
	return fmt.Sprintf("<pre><code>%s</code></pre>\n", code)
}

func renderBlockquote(b block, cfg Config) string {
	inner := RenderToHTML(strings.Join(b.lines, "\n"), cfg)
	return fmt.Sprintf("<blockquote>\n%s</blockquote>\n", inner)
}

func renderList(b block, tag string, cfg Config) string {
	var out strings.Builder
	out.WriteString(fmt.Sprintf("<%s>\n", tag))
	for _, item := range b.items {
		out.WriteString("<li>")
		text := renderInline(strings.Join(item.lines, " "), cfg)
		if taskRe.MatchString(text) {
			m := taskRe.FindStringSubmatch(text)
			checked := m[2] == "x" || m[2] == "X"
			text = m[3]
			check := "<input type=\"checkbox\" disabled"
			if checked {
				check += " checked"
			}
			check += "> "
			text = check + renderInline(text, cfg)
		}
		out.WriteString(text)
		out.WriteString("</li>\n")
	}
	out.WriteString(fmt.Sprintf("</%s>\n", tag))
	return out.String()
}

func renderTable(b block, cfg Config) string {
	if len(b.cells) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<table>\n")
	for i, row := range b.cells {
		tag := "td"
		if i == 0 {
			tag = "th"
			out.WriteString("<thead>\n")
		}
		out.WriteString("<tr>")
		for _, cell := range row {
			out.WriteString(fmt.Sprintf("<%s>%s</%s>", tag, renderInline(cell, cfg), tag))
		}
		out.WriteString("</tr>\n")
		if i == 0 {
			out.WriteString("</thead>\n<tbody>\n")
		}
	}
	out.WriteString("</tbody>\n</table>\n")
	return out.String()
}

func renderAdmonition(b block, cfg Config) string {
	typ := b.info

	var title = b.info
	if (title == "" || title == "note" || title == "tip" || title == "warning" || title == "danger" || title == "caution") {
		if (typ == "tip") {
			title = "Tip";
		} else if (typ == "warning") {
			title = "Warning";
		} else if (typ == "danger") {
			title = "Danger";
		} else if (typ == "caution") {
			title = "Caution";
		} else {
			title = "Note";
		}
	}
	var icon = "tabler:info-circle";
	if (typ == "tip") {
		icon = "tabler:bulb";
	} else if (typ == "warning") {
		icon = "tabler:alert-triangle";
	} else if (typ == "danger") {
		icon = "tabler:skull";
	} else if (typ == "caution") {
		icon = "tabler:alert-circle";
	}
	iconBody, err := icons.GetIconContent(cfg.Root, icon)
	if err != nil || iconBody == "" {
		iconBody = ""
	}
	ic := fmt.Sprintf("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"24\" height=\"24\" viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\">%s</svg>", iconBody)
	content := RenderToHTML(strings.Join(b.lines, "\n"), cfg)
	return fmt.Sprintf("<div class=\"krate-aside krate-aside-%s\"><div class=\"krate-aside-title\">%s%s</div><div class=\"krate-aside-content\">%s</div></div>\n", typ, ic, title, content)
}

func slugify(text string) string {
	lower := strings.ToLower(text)
	re := regexp.MustCompile(`[^a-z0-9\s-]`)
	clean := re.ReplaceAllString(lower, "")
	spaces := regexp.MustCompile(`\s+`)
	clean = spaces.ReplaceAllString(clean, "-")
	clean = strings.Trim(clean, "-")
	return clean
}
