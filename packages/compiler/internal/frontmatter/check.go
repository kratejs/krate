package frontmatter

import "strings"

// Issue is a syntax error in a frontmatter block that Parse would otherwise
// turn into a silently wrong value (for example `title: "bad` becoming the
// string `"bad`). Line and Col are 1-based positions in the whole file.
type Issue struct {
	Line, Col int
	Msg, Hint string
}

const (
	hintQuote = "close the quote, or wrap the value in the other kind of quote; a value containing `: ` or `#` should be quoted"
	hintFlow  = "close the bracket, or write the list one item per line"
)

// Check validates the frontmatter block at the top of src and returns the
// errors Parse cannot represent. It reports nothing for a file without a
// closed `---` block (see Detect for that), and accepts everything Parse reads
// correctly: quoted colons, multi-line quoted and block scalars, plain
// multi-line scalars, flow collections and empty blocks.
func Check(src string) []Issue {
	src = strings.TrimPrefix(src, "\ufeff")
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return nil
	}
	block := make([]string, 0, closeIdx-1)
	for _, l := range lines[1:closeIdx] {
		block = append(block, strings.TrimRight(l, "\r"))
	}

	var issues []Issue
	report := func(i, col int, msg, hint string) {
		issues = append(issues, Issue{Line: i + 2, Col: col, Msg: msg, Hint: hint})
	}
	// contIndent is the indent of the last key or item that may continue onto
	// deeper plain lines (a multi-line plain scalar); -1 when none may.
	contIndent := -1

	for i := 0; i < len(block); i++ {
		line := block[i]
		t := strings.TrimSpace(line)
		if t == "" || t[0] == '#' {
			continue
		}
		ind := indentation(line)

		item := t
		isItem := false
		for isListItem(item) {
			item = strings.TrimSpace(strings.TrimPrefix(item, "-"))
			isItem = true
		}
		if isItem && item == "" {
			contIndent = -1
			continue
		}

		var value string
		switch {
		case isItem:
			if _, v, ok := splitSimpleKeyValue(item); ok {
				value = v
			} else {
				value = item
			}
		default:
			_, v, ok := splitKeyValue(item)
			if !ok {
				if contIndent >= 0 && ind > contIndent {
					continue
				}
				report(i, ind+1, "expected `key: value`, found `"+shorten(t)+"`",
					"put each frontmatter entry on its own line as `key: value`")
				continue
			}
			value = v
		}

		col := strings.LastIndex(line, value) + 1
		if value == "" || col <= 0 {
			col = ind + 1
		}
		if value == "" {
			contIndent = ind
			continue
		}
		if value[0] == '#' {
			contIndent = ind
			continue
		}
		if _, _, ok := blockScalarHeader(value); ok {
			for i+1 < len(block) && (strings.TrimSpace(block[i+1]) == "" || indentation(block[i+1]) > ind) {
				i++
			}
			contIndent = -1
			continue
		}
		switch value[0] {
		case '"', '\'':
			contIndent = -1
			end := quoteEnd(value)
			if end < 0 {
				joined, n, ok := multiLineQuoted(value, block[i+1:])
				_ = joined
				if !ok {
					report(i, col, "quoted value is never closed: "+shorten(value), hintQuote)
					continue
				}
				i += n
				continue
			}
			if rest := strings.TrimSpace(value[end+1:]); rest != "" && rest[0] != '#' {
				report(i, col+end+1, "unexpected text after the closing quote: "+shorten(rest), hintQuote)
			}
		case '[', '{':
			contIndent = -1
			if flowDepth(value) > 0 {
				closed := false
				text := value
				for j := i + 1; j < len(block); j++ {
					text += " " + strings.TrimSpace(block[j])
					if flowDepth(text) <= 0 {
						i = j
						closed = true
						break
					}
				}
				if !closed {
					report(i, col, "`"+string(value[0])+"` is never closed: "+shorten(value), hintFlow)
				}
			}
		default:
			contIndent = ind
		}
	}
	return issues
}

// flowDepth returns how many flow collections (`[` or `{`) remain open at the
// end of s, ignoring quoted text. A closed collection returns 0.
func flowDepth(s string) int {
	depth := 0
	var q byte
	var prev byte // previous non-space byte outside quotes
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"' && c == '\\':
			i++
		case q != 0:
			if c == q {
				q = 0
				prev = c
			}
		case (c == '"' || c == '\'') && (prev == 0 || prev == '[' || prev == '{' || prev == ',' || prev == ':'):
			// A quote starts a quoted scalar only where a value begins, so an
			// apostrophe inside a plain word (`don't`) is ordinary text.
			q = c
		case c == '[' || c == '{':
			depth++
			prev = c
		case c == ']' || c == '}':
			depth--
			prev = c
		case c == '#' && i > 0 && s[i-1] == ' ':
			return depth
		case c != ' ' && c != '\t':
			prev = c
		}
	}
	return depth
}

func shorten(s string) string {
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}
