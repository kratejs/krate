package css

import (
	"strconv"
	"strings"
)

// cssLiteralProtector replaces string literals, `url(...)` tokens, and `/*! ... */`
// license comments with opaque placeholders so the minifier's whitespace and
// value-level transforms cannot rewrite their contents. `restore` puts the
// originals back at the end.
type cssLiteralProtector struct {
	items []string
}

func (p *cssLiteralProtector) token(i int) string {
	return "\x00lit" + strconv.Itoa(i) + "\x00"
}

func (p *cssLiteralProtector) add(lit string) string {
	i := len(p.items)
	p.items = append(p.items, lit)
	return p.token(i)
}

func (p *cssLiteralProtector) protect(s string) string {
	var out strings.Builder
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '/' && strings.HasPrefix(s[i:], "/*!"):
			// License comment: tokenize so it survives comment stripping.
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				out.WriteString(s[i:])
				i = n
				continue
			}
			end := i + 2 + j + 2
			out.WriteString(p.add(s[i:end]))
			i = end
		case c == '/' && i+1 < n && s[i+1] == '*':
			// Ordinary comment: dropped without interpreting its contents. A
			// quote inside it (e.g. "doesn't") must not start a string. A
			// space keeps the tokens on either side apart (1px/**/2px).
			i = skipCSSComment(s, i)
			out.WriteByte(' ')
		case c == '\\':
			// Escape: keep the escaped byte literal so \' or \{ never opens
			// a string or block.
			out.WriteByte(c)
			if i+1 < n {
				out.WriteByte(s[i+1])
			}
			i += 2
		case c == '"' || c == '\'':
			j := cssSkipString(s, i)
			out.WriteString(p.add(s[i:j]))
			i = j
		case (c == 'u' || c == 'U') && hasPrefixFold(s[i:], "url(") && (i == 0 || !isIdentByte(s[i-1]) && s[i-1] != '-'):
			k := i + 4
			depth := 1
			for k < n {
				if s[k] == '\\' {
					k += 2
					continue
				}
				if s[k] == '(' {
					depth++
				} else if s[k] == ')' {
					depth--
					if depth == 0 {
						k++
						break
					}
				}
				k++
			}
			out.WriteString(p.add(s[i:k]))
			i = k
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func (p *cssLiteralProtector) restore(s string) string {
	for i, lit := range p.items {
		s = strings.ReplaceAll(s, p.token(i), lit)
	}
	return s
}

// collapseCSS collapses whitespace runs. Comments are already gone and
// literals protected. Escapes are copied whole so an escaped space survives.
func collapseCSS(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	n := len(css)
	wasSpace := false
	for i := 0; i < n; {
		ch := css[i]
		switch {
		case ch == '\\':
			b.WriteByte(ch)
			if i+1 < n {
				b.WriteByte(css[i+1])
			}
			i += 2
			wasSpace = false
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f':
			if !wasSpace {
				b.WriteByte(' ')
				wasSpace = true
			}
			i++
		default:
			b.WriteByte(ch)
			wasSpace = false
			i++
		}
	}
	return strings.TrimSpace(b.String())
}

// hasPrefixFold reports whether s begins with prefix, ASCII case-insensitively.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

// minifyItems rewrites one body: value transforms on declarations, duplicate
// folding in leaf rules, and removal of rules left empty.
func minifyItems(s string, ctx minCtx, top bool) string {
	items := splitCSSItems(s)
	hasBlock := false
	for _, it := range items {
		if it.block {
			hasBlock = true
			break
		}
	}
	if !hasBlock && !top {
		return minifyLeaf(items, ctx)
	}

	type piece struct {
		text  string
		block bool
		semi  bool
	}
	var pieces []piece
	for _, it := range items {
		if it.block {
			body := minifyItems(it.body, childCtx(ctx, it.head), false)
			if it.closed && strings.TrimSpace(body) == "" && canDropEmpty(it.head) {
				continue
			}
			text := it.head + "{" + body
			if it.closed {
				text += "}"
			}
			pieces = append(pieces, piece{text: text, block: true})
			continue
		}
		text := it.head
		if !top {
			text = rewriteDecl(text, ctx)
		}
		if strings.TrimSpace(text) == "" && it.closed {
			continue
		}
		pieces = append(pieces, piece{text: text, semi: it.closed})
	}
	if !top {
		// Trailing ';' and whitespace after the last statement are redundant.
		last := len(pieces) - 1
		for last >= 0 && !pieces[last].block && strings.TrimSpace(pieces[last].text) == "" {
			last--
		}
		if last >= 0 && !pieces[last].block {
			pieces[last].semi = false
			pieces = pieces[:last+1]
		}
	}
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(p.text)
		if p.semi {
			b.WriteByte(';')
		}
	}
	return b.String()
}

// rewriteDecl applies value transforms to a declaration statement in a body
// that also contains nested rules, keeping its surrounding text.
func rewriteDecl(head string, ctx minCtx) string {
	t := strings.TrimSpace(head)
	if t == "" || strings.HasPrefix(t, "@") {
		return head
	}
	colon := declColon(head)
	if colon < 0 {
		return head
	}
	prop := strings.TrimSpace(head[:colon])
	if prop == "" {
		return head
	}
	raw := head[colon+1:]
	val := strings.TrimSpace(raw)
	lead := raw[:len(raw)-len(strings.TrimLeft(raw, " \t\r\n\f"))]
	trail := raw[len(strings.TrimRight(raw, " \t\r\n\f")):]
	return head[:colon+1] + lead + minifyValue(prop, val, ctx) + trail
}

func minifyLeaf(items []cssItem, ctx minCtx) string {
	type seg struct {
		text string
		decl bool
		key  string
		val  string
	}
	var segs []seg
	var decls []declInfo
	var declAt []int
	for _, it := range items {
		t := strings.TrimSpace(it.head)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "@") {
			if prop, val, ok := parseDecl(t); ok && prop != "" {
				key := declKey(prop)
				val = minifyValue(prop, val, ctx)
				declAt = append(declAt, len(segs))
				decls = append(decls, declInfo{key: key, val: val})
				segs = append(segs, seg{decl: true, key: key, val: val})
				continue
			}
		}
		segs = append(segs, seg{text: t})
	}
	drop := planDrops(decls, ctx)
	dropped := make(map[int]bool)
	for k, d := range drop {
		if d {
			dropped[declAt[k]] = true
		}
	}
	var b strings.Builder
	first := true
	for i, sg := range segs {
		if dropped[i] {
			continue
		}
		if !first {
			b.WriteByte(';')
		}
		first = false
		if sg.decl {
			b.WriteString(sg.key)
			b.WriteByte(':')
			b.WriteString(sg.val)
		} else {
			b.WriteString(sg.text)
		}
	}
	return b.String()
}

// canDropEmpty reports whether a block with an empty body can be removed
// without changing meaning. Empty @layer and @keyframes blocks are kept: the
// first declares layer order, the second overrides an earlier animation.
func canDropEmpty(head string) bool {
	if !strings.HasPrefix(strings.TrimSpace(head), "@") {
		return true
	}
	switch preludeAtName(head) {
	case "media", "supports", "container", "scope", "starting-style", "font-face", "page", "document":
		return true
	}
	return false
}
