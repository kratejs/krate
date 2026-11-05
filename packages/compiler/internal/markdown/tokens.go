package markdown

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// tokenTable holds the JSX tags and {expressions} of an MDX document that were
// swapped for opaque tokens before the markdown parser ran.
type tokenTable struct {
	toks   []token
	fm     map[string]any
	consts map[string]string
}

type token struct {
	kind byte // 'T' inline tag, 'F' tag at line start, 'E' expression
	src  string
	off  int // byte offset in the protected chunk
}

type reportFn func(off int, warn bool, msg, hint string)

var (
	tokenRe     = regexp.MustCompile(`KRMDX([FTE])(\d+)X`)
	attrTokenRe = regexp.MustCompile(`=(")KRMDXE(\d+)X"`)
	constRe     = regexp.MustCompile(`^export\s+const\s+([A-Za-z_$][\w$]*)\s*=\s*(.+?);?\s*$`)
	numLitRe    = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)
	strLitRe    = regexp.MustCompile(`^(?:"([^"\\\n]*)"|'([^'\\\n]*)')$`)
	fmPathRe    = regexp.MustCompile(`^\s*frontmatter\.([A-Za-z_$][\w$]*)\s*$`)
	identRe     = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*)\s*$`)
)

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func newTokenTable(fm map[string]any, esm []string) *tokenTable {
	return &tokenTable{fm: fm, consts: constsFromESM(esm)}
}

func constsFromESM(esm []string) map[string]string {
	consts := map[string]string{}
	for _, s := range esm {
		m := constRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[2])
		switch {
		case numLitRe.MatchString(v), v == "true", v == "false":
			consts[m[1]] = v
		case strLitRe.MatchString(v):
			sm := strLitRe.FindStringSubmatch(v)
			consts[m[1]] = sm[1] + sm[2]
		}
	}
	return consts
}

func (t *tokenTable) add(kind byte, src string, off int) string {
	t.toks = append(t.toks, token{kind: kind, src: src, off: off})
	return fmt.Sprintf("KRMDX%c%dX", kind, len(t.toks)-1)
}

func (t *tokenTable) has(h string) bool { return tokenRe.MatchString(h) }

// staticValue resolves an expression that is known at build time: a JSX
// comment, frontmatter.key, or an exported literal constant.
func (t *tokenTable) staticValue(expr string) (string, bool) {
	e := strings.TrimSpace(expr)
	if strings.HasPrefix(e, "/*") && strings.HasSuffix(e, "*/") && !strings.Contains(e[2:len(e)-2], "*/") {
		return "", true
	}
	if m := fmPathRe.FindStringSubmatch(e); m != nil && t.fm != nil {
		switch v := t.fm[m[1]].(type) {
		case string:
			return v, true
		case bool:
			return strconv.FormatBool(v), true
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), true
		case int:
			return strconv.Itoa(v), true
		case int64:
			return strconv.FormatInt(v, 10), true
		}
		return "", false
	}
	if m := identRe.FindStringSubmatch(e); m != nil {
		if v, ok := t.consts[m[1]]; ok {
			return v, true
		}
	}
	return "", false
}

// plainText replaces tokens in heading text with their visible value.
func (t *tokenTable) plainText(s string) string {
	return tokenRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := tokenRe.FindStringSubmatch(m)
		i, _ := strconv.Atoi(sm[2])
		if i >= len(t.toks) || sm[1] != "E" {
			return ""
		}
		v, _ := t.staticValue(exprBody(t.toks[i].src))
		return v
	})
}

func exprBody(src string) string {
	return strings.TrimSuffix(strings.TrimPrefix(src, "{"), "}")
}

// resolveStatic substitutes build-time-known expressions in rendered HTML.
func (t *tokenTable) resolveStatic(h string) string {
	return tokenRe.ReplaceAllStringFunc(h, func(m string) string {
		sm := tokenRe.FindStringSubmatch(m)
		i, _ := strconv.Atoi(sm[2])
		if sm[1] != "E" || i >= len(t.toks) {
			return m
		}
		if v, ok := t.staticValue(exprBody(t.toks[i].src)); ok {
			return html.EscapeString(v)
		}
		return m
	})
}

// protect replaces JSX tags and {expressions} in text with tokens.
func (t *tokenTable) protect(text string, mdx bool, report reportFn) string {
	var out strings.Builder
	var mdOpen []string // md mode: open uppercase components at line start
	n := len(text)
	i := 0
	atLineStart := func(pos int) bool {
		for k := pos - 1; k >= 0 && text[k] != '\n'; k-- {
			if text[k] != ' ' && text[k] != '\t' {
				return false
			}
		}
		return true
	}
	for i < n {
		c := text[i]
		switch {
		case c == '\\' && i+1 < n:
			out.WriteString(text[i : i+2])
			i += 2
		case c == '`':
			j := i
			for j < n && text[j] == '`' {
				j++
			}
			run := text[i:j]
			if end := strings.Index(text[j:], run); end >= 0 && !strings.HasPrefix(text[j+end:], run+"`") {
				out.WriteString(text[i : j+end+len(run)])
				i = j + end + len(run)
			} else {
				out.WriteString(run)
				i = j
			}
		case c == '<':
			tg, ok := parseTag(text, i)
			if !ok {
				if mdx && i+1 < n && unicode.IsUpper(rune(text[i+1])) {
					report(i, true, "unrecognised JSX tag", "Check the tag for unbalanced quotes or braces; it will render as text.")
				}
				out.WriteByte(c)
				i++
				continue
			}
			flow := atLineStart(i)
			protect := false
			if mdx {
				protect = tg.component || tg.hasExpr
			} else if tg.component {
				if tg.closing {
					if len(mdOpen) > 0 && mdOpen[len(mdOpen)-1] == tg.name {
						mdOpen = mdOpen[:len(mdOpen)-1]
						protect = true
					}
				} else if flow && (tg.selfClose || strings.Contains(text[tg.end:], "</"+tg.name+">")) {
					// An unclosed tag at the start of a line is prose (`<Image> is
					// optimised ...`), not a component.
					protect = true
					if !tg.selfClose {
						mdOpen = append(mdOpen, tg.name)
					}
				}
			}
			if !protect {
				out.WriteByte(c)
				i++
				continue
			}
			kind := byte('T')
			if flow {
				kind = 'F'
			}
			if flow && tg.closing && strings.TrimSpace(restOfLine(text, tg.end)) == "" {
				separateFromContainer(&out)
			}
			out.WriteString(t.add(kind, text[i:tg.end], i))
			i = tg.end
		case c == '{' && mdx:
			end, ok := scanBraces(text, i)
			if !ok {
				report(i, false, "unterminated { expression } in MDX",
					"Close the expression with }, or escape the brace as \\{ to print it literally.")
				out.WriteByte(c)
				i++
				continue
			}
			out.WriteString(t.add('E', text[i:end], i))
			i = end
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func restOfLine(text string, from int) string {
	if nl := strings.IndexByte(text[from:], '\n'); nl >= 0 {
		return text[from : from+nl]
	}
	return text[from:]
}

var containerLineRe = regexp.MustCompile(`^(?:\s*(?:[-*+]|\d{1,9}[.)])\s|\s*>|\s*\|| {2,}\S|\t)`)

// separateFromContainer ends the markdown block before a closing JSX tag that
// stands on its own line. Without a blank line, the opaque tag token is read as
// a lazy continuation of the last list item, quote or table row above it, and
// the component would close inside that block. Plain paragraphs are left
// alone so the tag stays in the same inline run as its text.
func separateFromContainer(out *strings.Builder) {
	cur := out.String()
	// Drop the indentation already written for this line.
	cur = strings.TrimRight(cur, " \t")
	if !strings.HasSuffix(cur, "\n") {
		return
	}
	body := strings.TrimRight(cur, "\n")
	if strings.HasSuffix(cur, "\n\n") || body == "" {
		return
	}
	prev := body[strings.LastIndexByte(body, '\n')+1:]
	if !containerLineRe.MatchString(prev) && !strings.HasSuffix(strings.TrimSpace(prev), "|") {
		return
	}
	out.Reset()
	out.WriteString(cur)
	out.WriteByte('\n')
}

type tagInfo struct {
	end       int
	name      string
	closing   bool
	selfClose bool
	component bool
	hasExpr   bool
}

func isNameStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9') || c == '.' || c == ':' || c == '-'
}

// parseTag parses a JSX tag starting at text[i] == '<'.
func parseTag(text string, i int) (tagInfo, bool) {
	var tg tagInfo
	n := len(text)
	j := i + 1
	if j < n && text[j] == '/' {
		tg.closing = true
		j++
	}
	if j < n && text[j] == '>' { // fragment
		tg.end = j + 1
		tg.component = true
		return tg, true
	}
	if j >= n || !isNameStart(text[j]) {
		return tg, false
	}
	s := j
	for j < n && isNameChar(text[j]) {
		j++
	}
	tg.name = text[s:j]
	first := tg.name[0]
	tg.component = (first >= 'A' && first <= 'Z') || first == '_' || strings.Contains(tg.name, ".")
	skipWS := func() {
		for j < n && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
			if text[j] == '\n' && strings.HasPrefix(text[j:], "\n\n") {
				return
			}
			j++
		}
	}
	for {
		skipWS()
		if j >= n {
			return tg, false
		}
		switch {
		case text[j] == '>':
			tg.end = j + 1
			return tg, true
		case text[j] == '/' && j+1 < n && text[j+1] == '>':
			tg.selfClose = true
			tg.end = j + 2
			return tg, true
		case tg.closing:
			return tg, false
		case text[j] == '{':
			e, ok := scanBraces(text, j)
			if !ok {
				return tg, false
			}
			tg.hasExpr = true
			j = e
		case isNameStart(text[j]):
			for j < n && isNameChar(text[j]) {
				j++
			}
			save := j
			skipWS()
			if j < n && text[j] == '=' {
				j++
				skipWS()
				if j >= n {
					return tg, false
				}
				switch text[j] {
				case '"', '\'':
					q := text[j]
					e := strings.IndexByte(text[j+1:], q)
					if e < 0 {
						return tg, false
					}
					j += e + 2
				case '{':
					e, ok := scanBraces(text, j)
					if !ok {
						return tg, false
					}
					tg.hasExpr = true
					j = e
				default:
					return tg, false
				}
			} else {
				j = save
			}
		default:
			return tg, false
		}
	}
}

// scanBraces returns the index just past the } matching the { at text[i].
func scanBraces(text string, i int) (int, bool) {
	depth := 0
	n := len(text)
	for j := i; j < n; j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j + 1, true
			}
		case '"', '\'':
			q := text[j]
			j++
			for j < n && text[j] != q && text[j] != '\n' {
				if text[j] == '\\' {
					j++
				}
				j++
			}
		case '`':
			e, ok := scanTemplate(text, j)
			if !ok {
				return 0, false
			}
			j = e - 1
		case '/':
			if j+1 < n && text[j+1] == '*' {
				e := strings.Index(text[j+2:], "*/")
				if e < 0 {
					return 0, false
				}
				j += e + 3
			} else if j+1 < n && text[j+1] == '/' {
				e := strings.IndexByte(text[j:], '\n')
				if e < 0 {
					return 0, false
				}
				j += e
			}
		}
	}
	return 0, false
}

func scanTemplate(text string, i int) (int, bool) {
	n := len(text)
	for j := i + 1; j < n; j++ {
		switch text[j] {
		case '\\':
			j++
		case '`':
			return j + 1, true
		case '$':
			if j+1 < n && text[j+1] == '{' {
				e, ok := scanBraces(text, j+1)
				if !ok {
					return 0, false
				}
				j = e - 1
			}
		}
	}
	return 0, false
}

// toJSX converts rendered HTML that still carries tokens into JSX source, and
// a static HTML twin with components and runtime expressions removed.
func (t *tokenTable) toJSX(h string, report reportFn) (jsx, static string) {
	h = HTMLToJSX(h)
	h = attrTokenRe.ReplaceAllStringFunc(h, func(m string) string {
		sm := attrTokenRe.FindStringSubmatch(m)
		i, _ := strconv.Atoi(sm[2])
		if i >= len(t.toks) {
			return m
		}
		return "=" + t.toks[i].src
	})
	var jb, sb strings.Builder
	i := 0
	for i < len(h) {
		lt := strings.IndexByte(h[i:], '<')
		textEnd := len(h)
		if lt >= 0 {
			textEnd = i + lt
		}
		t.emitText(&jb, &sb, h[i:textEnd], report)
		if lt < 0 {
			break
		}
		end := tagEnd(h, textEnd)
		tag := h[textEnd:end]
		jb.WriteString(t.expandTokens(tag, false, nil))
		sb.WriteString(tokenRe.ReplaceAllString(tag, ""))
		i = end
	}
	return jb.String(), sb.String()
}

// tagEnd returns the index just past the HTML tag starting at h[i].
func tagEnd(h string, i int) int {
	var q byte
	for j := i + 1; j < len(h); j++ {
		c := h[j]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return j + 1
		}
	}
	return len(h)
}

func (t *tokenTable) expandTokens(s string, text bool, report reportFn) string {
	return tokenRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := tokenRe.FindStringSubmatch(m)
		i, _ := strconv.Atoi(sm[2])
		if i >= len(t.toks) {
			return ""
		}
		return t.toks[i].src
	})
}

// emitText writes a run of HTML text: tokens expand to their JSX source, the
// remaining text becomes JSX-safe.
func (t *tokenTable) emitText(jb, sb *strings.Builder, s string, report reportFn) {
	for len(s) > 0 {
		loc := tokenRe.FindStringSubmatchIndex(s)
		plain := s
		if loc != nil {
			plain = s[:loc[0]]
		}
		if plain != "" {
			jb.WriteString(jsxText(plain))
			sb.WriteString(plain)
		}
		if loc == nil {
			return
		}
		kind := s[loc[2]]
		idx, _ := strconv.Atoi(s[loc[4]:loc[5]])
		if idx < len(t.toks) {
			tk := t.toks[idx]
			jb.WriteString(tk.src)
			if kind == 'E' && report != nil {
				report(tk.off, true, "runtime expression is omitted from static HTML output", "Use a page or docs route to render dynamic MDX expressions.")
			}
		}
		s = s[loc[1]:]
	}
}

// jsxText converts HTML text (entities, braces, newlines) to JSX children.
func jsxText(s string) string {
	if !strings.ContainsAny(s, "&{}\n\r<>\\") {
		return s
	}
	return "{" + escape.JSStringDQ(html.UnescapeString(s)) + "}"
}
