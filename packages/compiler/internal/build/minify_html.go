package build

import (
	"strings"
)

// minifyHTML shrinks rendered page HTML in a single tokenizing pass:
//   - HTML comments are dropped except runtime markers (k:, suspense:, region:)
//   - whitespace runs in text (outside <pre> and raw-text elements) collapse
//     to one space
//   - start/end tags are re-emitted with normalized attribute spacing, and
//     quotes are dropped from attribute values only when the HTML spec allows
//     an unquoted value
//
// Attribute values are never altered apart from quoting. The tokenizer follows
// the HTML spec closely enough that anything it does not understand (EOF
// inside a tag, unterminated raw text) is copied through verbatim.
func minifyHTML(src string) string {
	m := &minifier{s: src}
	m.run()
	out := m.out.String()
	out = strings.TrimLeft(out, htmlSpace)
	// Trailing whitespace is only dropped after a complete tag; after text it
	// can be significant (for example before an unterminated "<").
	if t := strings.TrimRight(out, htmlSpace); !m.keepTail && m.preDepth == 0 && strings.HasSuffix(t, ">") {
		out = t
	}
	return out
}

const htmlSpace = " \t\n\f\r"

// rawTextTags are elements whose content is not ordinary markup (raw text,
// RCDATA, or whitespace-significant) and is copied verbatim.
var rawTextTags = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true,
	"xmp": true, "iframe": true, "noembed": true, "noframes": true,
	"noscript": true, "plaintext": true,
}

type minifier struct {
	s         string
	out       strings.Builder
	prevSpace bool
	preDepth  int
	keepTail  bool
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

func isASCIIAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func (m *minifier) writeRaw(s string) {
	m.out.WriteString(s)
	m.prevSpace = false
}

func (m *minifier) text(c byte) {
	if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
		if m.preDepth > 0 {
			m.out.WriteByte(c)
			return
		}
		if !m.prevSpace {
			m.out.WriteByte(' ')
			m.prevSpace = true
		}
		return
	}
	m.out.WriteByte(c)
	m.prevSpace = false
}

func (m *minifier) run() {
	s := m.s
	i := 0
	for i < len(s) {
		if s[i] != '<' {
			m.text(s[i])
			i++
			continue
		}
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := commentEnd(s, i)
			if end < 0 {
				m.writeRaw(rest)
				m.keepTail = true
				return
			}
			if keepComment(s[i:end]) {
				m.writeRaw(s[i:end])
			}
			i = end
		case strings.HasPrefix(rest, "<![CDATA["):
			idx := strings.Index(rest, "]]>")
			if idx < 0 {
				m.writeRaw(rest)
				m.keepTail = true
				return
			}
			m.writeRaw(rest[:idx+3])
			i += idx + 3
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?' || rest[1] == '/' && (len(rest) < 3 || !isASCIIAlpha(rest[2]))):
			// Doctype, bogus comment or processing instruction: up to '>'.
			idx := strings.IndexByte(rest, '>')
			if idx < 0 {
				m.writeRaw(rest)
				m.keepTail = true
				return
			}
			m.writeRaw(rest[:idx+1])
			i += idx + 1
		case len(rest) > 1 && (isASCIIAlpha(rest[1]) || rest[1] == '/'):
			var ok bool
			i, ok = m.tag(i)
			if !ok {
				return
			}
		default:
			m.text('<')
			i++
		}
	}
}

// commentEnd returns the index just past the comment starting at i, or -1.
func commentEnd(s string, i int) int {
	start := i + 4
	if strings.HasPrefix(s[start:], ">") {
		return start + 1
	}
	if strings.HasPrefix(s[start:], "->") {
		return start + 2
	}
	best := -1
	if k := strings.Index(s[start:], "-->"); k >= 0 {
		best = start + k + 3
	}
	if k := strings.Index(s[start:], "--!>"); k >= 0 && (best < 0 || start+k+4 < best) {
		best = start + k + 4
	}
	return best
}

func keepComment(c string) bool {
	for _, p := range []string{"<!--k:", "<!--/k:", "<!--suspense:", "<!--/suspense:", "<!--region:", "<!--/region:"} {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
}

// tag re-emits the start or end tag at i and handles raw-text content that
// follows a start tag. It returns the next index and false if the rest of the
// input was consumed verbatim.
func (m *minifier) tag(i int) (int, bool) {
	s := m.s
	j := i + 1
	isEnd := s[j] == '/'
	if isEnd {
		j++
	}
	nameStart := j
	for j < len(s) && !isHTMLSpace(s[j]) && s[j] != '/' && s[j] != '>' {
		j++
	}
	name := s[nameStart:j]

	var tb strings.Builder
	tb.WriteString(s[i:j])
	lastUnquoted := false
	prevValueless := false
	slash := false
	for {
		slash = false
		for j < len(s) && (isHTMLSpace(s[j]) || s[j] == '/') {
			slash = s[j] == '/'
			j++
		}
		if j >= len(s) {
			m.writeRaw(s[i:])
			m.keepTail = true
			return len(s), false
		}
		if s[j] == '>' {
			j++
			break
		}
		ns := j
		if s[j] == '=' {
			j++
		}
		for j < len(s) && !isHTMLSpace(s[j]) && s[j] != '/' && s[j] != '>' && s[j] != '=' {
			j++
		}
		aname := s[ns:j]
		k := j
		for k < len(s) && isHTMLSpace(s[k]) {
			k++
		}
		lastUnquoted = false
		if aname[0] == '=' && prevValueless {
			// A space would let the '=' bind as a value to the previous name.
			tb.WriteByte('/')
		} else {
			tb.WriteByte(' ')
		}
		tb.WriteString(aname)
		prevValueless = k >= len(s) || s[k] != '='
		if prevValueless {
			continue
		}
		k++
		for k < len(s) && isHTMLSpace(s[k]) {
			k++
		}
		if k >= len(s) {
			m.writeRaw(s[i:])
			m.keepTail = true
			return len(s), false
		}
		tb.WriteByte('=')
		switch q := s[k]; q {
		case '"', '\'':
			end := strings.IndexByte(s[k+1:], q)
			if end < 0 {
				m.writeRaw(s[i:])
				m.keepTail = true
				return len(s), false
			}
			val := s[k+1 : k+1+end]
			j = k + 2 + end
			if canUnquote(val) {
				tb.WriteString(val)
				lastUnquoted = true
			} else {
				tb.WriteByte(q)
				tb.WriteString(val)
				tb.WriteByte(q)
			}
		case '>':
			tb.WriteString(`""`)
			j = k
		default:
			vs := k
			for k < len(s) && !isHTMLSpace(s[k]) && s[k] != '>' {
				k++
			}
			tb.WriteString(s[vs:k])
			lastUnquoted = true
			j = k
		}
	}
	if slash {
		if lastUnquoted {
			tb.WriteByte(' ')
		}
		tb.WriteByte('/')
	}
	tb.WriteByte('>')
	m.writeRaw(tb.String())

	lname := strings.ToLower(name)
	switch {
	case lname == "pre":
		if isEnd {
			if m.preDepth > 0 {
				m.preDepth--
			}
		} else {
			m.preDepth++
		}
	case !isEnd && rawTextTags[lname]:
		if lname == "plaintext" {
			m.writeRaw(s[j:])
			m.keepTail = true
			return len(s), false
		}
		end := rawTextEnd(s, j, lname)
		if end < 0 {
			m.writeRaw(s[j:])
			m.keepTail = true
			return len(s), false
		}
		m.writeRaw(s[j:end])
		j = end
	}
	return j, true
}

// canUnquote reports whether v may be written as an unquoted attribute value
// per the HTML spec. Values ending in '/' keep their quotes so a following
// '>' can never be mistaken for part of a self-closing marker.
func canUnquote(v string) bool {
	if v == "" || v[len(v)-1] == '/' {
		return false
	}
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case ' ', '\t', '\n', '\f', '\r', '"', '\'', '=', '<', '>', '`':
			return false
		}
	}
	return true
}

// rawTextEnd returns the index of the end tag that closes a raw-text element
// whose content starts at from, or -1 when unterminated.
func rawTextEnd(s string, from int, name string) int {
	if name == "script" {
		return scriptEnd(s, from)
	}
	for k := from; k < len(s); {
		idx := strings.Index(s[k:], "</")
		if idx < 0 {
			return -1
		}
		k += idx
		if isEndTagAt(s, k, name) {
			return k
		}
		k += 2
	}
	return -1
}

// isEndTagAt reports whether s[k:] is "</name" followed by whitespace, '/' or '>'.
func isEndTagAt(s string, k int, name string) bool {
	e := k + 2 + len(name)
	if e >= len(s) || s[k] != '<' || s[k+1] != '/' || !strings.EqualFold(s[k+2:e], name) {
		return false
	}
	return isHTMLSpace(s[e]) || s[e] == '/' || s[e] == '>'
}

// scriptEnd finds the closing </script> honoring the script data escaped and
// double-escaped states ("<!--" ... "<script>" ... "-->").
func scriptEnd(s string, from int) int {
	escaped, double := false, false
	for k := from; k < len(s); k++ {
		if s[k] != '<' && s[k] != '-' {
			continue
		}
		switch {
		case s[k] == '-':
			if escaped && strings.HasPrefix(s[k:], "-->") {
				escaped, double = false, false
				k += 2
			}
		case strings.HasPrefix(s[k:], "<!--"):
			escaped = true
			if strings.HasPrefix(s[k+2:], "-->") {
				escaped, double = false, false
				k += 4
			} else {
				k += 3
			}
		case isEndTagAt(s, k, "script"):
			if !double {
				return k
			}
			double = false
			k += 7
		case escaped && isStartTagAt(s, k, "script"):
			double = true
			k += 6
		}
	}
	return -1
}

func isStartTagAt(s string, k int, name string) bool {
	e := k + 1 + len(name)
	if e >= len(s) || !strings.EqualFold(s[k+1:e], name) {
		return false
	}
	return isHTMLSpace(s[e]) || s[e] == '/' || s[e] == '>'
}
