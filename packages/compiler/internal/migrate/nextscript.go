package migrate

import (
	"regexp"
	"strings"
)

var nextScriptImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+([A-Za-z_$][\w$]*)[ \t]+from[ \t]+["']next/script["'][ \t]*;?[ \t]*\r?\n?`)

// scriptAttr is one attribute of a <Script> opening tag, as written.
type scriptAttr struct {
	name string
	raw  string
	// value is the attribute value with its quotes or braces removed.
	value string
	// static is true when the value is a plain string literal.
	static bool
}

// rewriteNextScript converts next/script <Script> elements to Krate's global
// <Script> (body end) or, for beforeInteractive, to a <script> in <Head>. It
// returns the notes for the report; manual lists what has no equivalent.
func rewriteNextScript(src string) (out string, rewrites, manual []string) {
	m := nextScriptImport.FindStringSubmatch(src)
	if m == nil {
		return src, nil, nil
	}
	local := m[1]
	tag := regexp.MustCompile(`<` + regexp.QuoteMeta(local) + `[\s/>]`)
	var b strings.Builder
	pos := 0
	converted := false
	failed := false
	for _, loc := range tag.FindAllStringIndex(src, -1) {
		start := loc[0]
		if start < pos {
			continue
		}
		attrs, end, selfClosing, ok := scanScriptOpening(src, start+1+len(local))
		if !ok {
			failed = true
			continue
		}
		children := ""
		if !selfClosing {
			closeRe := regexp.MustCompile(`</` + regexp.QuoteMeta(local) + `\s*>`)
			cl := closeRe.FindStringIndex(src[end:])
			if cl == nil {
				failed = true
				continue
			}
			children = src[end : end+cl[0]]
			end += cl[1]
		}
		el, notes, items := buildScriptElement(attrs, children)
		b.WriteString(src[pos:start])
		b.WriteString(el)
		pos = end
		converted = true
		rewrites = append(rewrites, notes...)
		manual = append(manual, items...)
	}
	if !converted {
		return src, nil, []string{"next/script <Script> could not be rewritten automatically; replace it with Krate's <Script> or a <script> in <Head>"}
	}
	b.WriteString(src[pos:])
	out = b.String()
	if failed {
		manual = append(manual, "some next/script <Script> elements could not be rewritten automatically; replace them with Krate's <Script>")
		return out, rewrites, manual
	}
	out = nextScriptImport.ReplaceAllString(out, "")
	return out, rewrites, manual
}

func buildScriptElement(attrs []scriptAttr, children string) (el string, rewrites, manual []string) {
	strategy := "afterInteractive"
	var kept []string
	hasSrc, hasLoadMode := false, false
	inner := children
	for _, a := range attrs {
		switch a.name {
		case "strategy":
			if a.static {
				strategy = a.value
			} else {
				strategy = "?"
			}
			continue
		case "onLoad", "onReady", "onError":
			manual = append(manual, "next/script "+a.name+" handler was dropped: a static <script> cannot run a JavaScript handler; attach it from a client component with a load listener")
			continue
		case "dangerouslySetInnerHTML":
			if h := htmlInnerExpr.FindStringSubmatch(a.value); h != nil {
				inner = "{" + strings.TrimSpace(h[1]) + "}"
			} else {
				manual = append(manual, "next/script dangerouslySetInnerHTML is not a literal {{ __html: ... }} and was dropped; pass the code as children")
			}
			continue
		case "src":
			hasSrc = true
		case "async", "defer":
			hasLoadMode = true
		}
		kept = append(kept, a.raw)
	}
	switch strategy {
	case "afterInteractive":
		if hasSrc && !hasLoadMode {
			kept = append(kept, "defer")
		}
		rewrites = append(rewrites, "converted next/script <Script> to Krate's <Script> (afterInteractive becomes defer)")
	case "lazyOnload":
		if hasSrc && !hasLoadMode {
			kept = append(kept, "async")
		}
		rewrites = append(rewrites, "converted next/script <Script> to Krate's <Script> (lazyOnload becomes async)")
	case "beforeInteractive":
		rewrites = append(rewrites, "converted next/script <Script> to a <script> in <Head> (beforeInteractive)")
		open := "<script" + joinAttrs(kept)
		if strings.TrimSpace(inner) == "" {
			return "<Head>" + open + " /></Head>", rewrites, manual
		}
		return "<Head>" + open + ">" + inner + "</script></Head>", rewrites, manual
	default:
		if hasSrc && !hasLoadMode {
			kept = append(kept, "defer")
		}
		reason := "next/script strategy " + strategy + " is not supported"
		if strategy == "?" {
			reason = "next/script strategy is not a string literal"
		}
		manual = append(manual, reason+"; the script was converted to a deferred Krate <Script> on the main thread, check that this is what you want")
	}
	open := "<Script" + joinAttrs(kept)
	if strings.TrimSpace(inner) == "" {
		return open + " />", rewrites, manual
	}
	return open + ">" + inner + "</Script>", rewrites, manual
}

var htmlInnerExpr = regexp.MustCompile(`(?s)^\{\s*__html\s*:\s*(.+?)\s*,?\s*\}$`)

func joinAttrs(attrs []string) string {
	var b strings.Builder
	for _, a := range attrs {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return b.String()
}

// scanScriptOpening reads the attributes of a JSX opening tag whose name ends
// at i. It returns the index after the closing ">" and whether the tag is
// self-closing. Comments between attributes are dropped.
func scanScriptOpening(src string, i int) (attrs []scriptAttr, end int, selfClosing, ok bool) {
	n := len(src)
	for i < n {
		switch c := src[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			for i < n && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, 0, false, false
			}
			i += j + 4
		case c == '/' && i+1 < n && src[i+1] == '>':
			return attrs, i + 2, true, true
		case c == '>':
			return attrs, i + 1, false, true
		case c == '{':
			j := skipBraces(src, i)
			if j < 0 {
				return nil, 0, false, false
			}
			attrs = append(attrs, scriptAttr{name: "...", raw: src[i:j]})
			i = j
		default:
			s := i
			for i < n && !strings.ContainsRune(" \t\r\n=/>{", rune(src[i])) {
				i++
			}
			if i == s {
				return nil, 0, false, false
			}
			a := scriptAttr{name: src[s:i]}
			j := i
			for j < n && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < n && src[j] == '=' {
				j++
				for j < n && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
					j++
				}
				if j >= n {
					return nil, 0, false, false
				}
				vs := j
				switch src[j] {
				case '"', '\'':
					k := strings.IndexByte(src[j+1:], src[j])
					if k < 0 {
						return nil, 0, false, false
					}
					j += k + 2
					a.value, a.static = src[vs+1:j-1], true
				case '{':
					k := skipBraces(src, j)
					if k < 0 {
						return nil, 0, false, false
					}
					j = k
					a.value = src[vs+1 : j-1]
					if lit, isLit := staticStringExpr(a.value); isLit {
						a.value, a.static = lit, true
					}
				default:
					return nil, 0, false, false
				}
				a.raw = src[s:j]
				i = j
			} else {
				a.raw = a.name
			}
			attrs = append(attrs, a)
		}
	}
	return nil, 0, false, false
}

// staticStringExpr reports whether expr is a single string literal (quotes or
// a template without interpolation) and returns its value.
func staticStringExpr(expr string) (string, bool) {
	e := strings.TrimSpace(expr)
	if len(e) < 2 {
		return "", false
	}
	q := e[0]
	if (q != '"' && q != '\'' && q != '`') || e[len(e)-1] != q {
		return "", false
	}
	body := e[1 : len(e)-1]
	if strings.ContainsRune(body, rune(q)) || strings.ContainsRune(body, '\\') || (q == '`' && strings.Contains(body, "${")) {
		return "", false
	}
	return body, true
}

// skipBraces returns the index after the "}" that closes the "{" at i, or -1.
// Strings and template literals inside are skipped.
func skipBraces(src string, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '"', '\'', '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				} else if c == '`' && src[i] == '$' && i+1 < len(src) && src[i+1] == '{' {
					j := skipBraces(src, i+1)
					if j < 0 {
						return -1
					}
					i = j - 1
				}
			}
		}
	}
	return -1
}
