package syntaxhighlight

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

type declMap map[string]string

// chromaClasses parses the stylesheet chroma itself writes for one theme into
// class -> declarations ("" is the .chroma block).
func chromaClasses(t *testing.T, theme string) map[string]declMap {
	t.Helper()
	var buf bytes.Buffer
	f := html.New(html.WithClasses(true), html.WithLineNumbers(false))
	if err := f.WriteCSS(&buf, styles.Registry[theme]); err != nil {
		t.Fatal(err)
	}
	out := map[string]declMap{}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.Index(line, "*/"); i >= 0 {
			line = strings.TrimSpace(line[i+2:])
		}
		open := strings.Index(line, "{")
		sel := strings.TrimSpace(line[:open])
		body := strings.Trim(line[open:], "{} ")
		switch {
		case sel == ".chroma":
			out[""] = parseDecls(body)
		case strings.HasPrefix(sel, ".chroma ."):
			class := strings.TrimPrefix(sel, ".chroma .")
			if skipClass[class] {
				continue
			}
			out[class] = parseDecls(body)
		}
	}
	return out
}

func parseDecls(body string) declMap {
	m := declMap{}
	for _, d := range strings.Split(body, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		k, v, _ := strings.Cut(d, ":")
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m
}

type cssRule struct {
	media string
	sel   string
	body  string
}

func parseRules(css string) []cssRule {
	var rules []cssRule
	i := 0
	readBlock := func() (head, body string) {
		open := strings.Index(css[i:], "{")
		head = strings.TrimSpace(css[i : i+open])
		depth, j := 0, i+open
		for ; j < len(css); j++ {
			if css[j] == '{' {
				depth++
			} else if css[j] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		body = css[i+open+1 : j]
		i = j + 1
		return
	}
	for i < len(css) {
		for i < len(css) && (css[i] == '\n' || css[i] == ' ') {
			i++
		}
		if i >= len(css) {
			break
		}
		head, body := readBlock()
		if strings.HasPrefix(head, "@media") {
			j := 0
			for j < len(body) {
				o := strings.Index(body[j:], "{")
				if o < 0 {
					break
				}
				sel := strings.TrimSpace(body[j : j+o])
				depth, k := 0, j+o
				for ; k < len(body); k++ {
					if body[k] == '{' {
						depth++
					} else if body[k] == '}' {
						depth--
						if depth == 0 {
							break
						}
					}
				}
				rules = append(rules, cssRule{media: head, sel: sel, body: body[j+o+1 : k]})
				j = k + 1
			}
			continue
		}
		rules = append(rules, cssRule{sel: head, body: body})
	}
	return rules
}

// evaluate resolves the stylesheet for one scenario and returns class ->
// declarations, treating a property that resolves to its "unset" fallback as
// absent (the token inherits).
func evaluate(t *testing.T, css string, systemDark bool, dataTheme string) map[string]declMap {
	t.Helper()
	vars := map[string]string{}
	apply := func(body string) {
		for k, v := range parseDecls(body) {
			vars[k] = v
		}
	}
	rules := parseRules(css)
	for _, r := range rules {
		if r.sel != ":where(:root)" {
			continue
		}
		if r.media != "" && !systemDark {
			continue
		}
		apply(r.body)
	}
	for _, r := range rules {
		if r.sel == ":where(:root[data-theme='"+dataTheme+"'])" {
			apply(r.body)
		}
	}
	var resolve func(prop, v string) (string, bool)
	resolve = func(prop, v string) (string, bool) {
		if !strings.HasPrefix(v, "var(") {
			return v, true
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(v, "var("), ")")
		name, fallback, hasFb := strings.Cut(inner, ",")
		if got, ok := vars[name]; ok && got != "initial" {
			return got, true
		}
		if !hasFb {
			t.Errorf("%s: %s has no value and no fallback", prop, v)
			return "", false
		}
		if strings.HasPrefix(fallback, "var(") {
			return resolve(prop, fallback)
		}
		if fallback == unsetFallback[prop] {
			return "", false
		}
		return fallback, true
	}
	out := map[string]declMap{}
	for _, r := range rules {
		if r.media != "" || !strings.HasPrefix(r.sel, ".chroma") || r.sel == ".chroma .line" {
			continue
		}
		var classes []string
		switch {
		case r.sel == ".chroma":
			classes = []string{""}
		case strings.HasPrefix(r.sel, ".chroma :is("):
			inner := strings.TrimSuffix(strings.TrimPrefix(r.sel, ".chroma :is("), ")")
			for _, c := range strings.Split(inner, ",") {
				classes = append(classes, strings.TrimPrefix(c, "."))
			}
		default:
			classes = []string{strings.TrimPrefix(r.sel, ".chroma .")}
		}
		d := declMap{}
		for k, v := range parseDecls(r.body) {
			if k == "-webkit-text-size-adjust" {
				continue
			}
			if val, ok := resolve(k, v); ok {
				d[k] = val
			}
		}
		for _, c := range classes {
			out[c] = d
		}
	}
	return out
}

func describe(m map[string]declMap) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		var props []string
		for p, v := range m[k] {
			props = append(props, p+":"+v)
		}
		sort.Strings(props)
		fmt.Fprintf(&sb, ".%s{%s}\n", k, strings.Join(props, ";"))
	}
	return sb.String()
}

// expected rewrites chroma's own output for a theme into the shape evaluate
// returns: font-weight/style/text-decoration names match, and the block gets
// its text colour defaulted the way ThemeCSS does.
func expected(t *testing.T, theme string) map[string]declMap {
	exp := chromaClasses(t, theme)
	bg, fg := baseColors(styles.Registry[theme])
	exp[""] = declMap{"background-color": bg, "color": fg}
	return exp
}

func compare(t *testing.T, label string, got, want map[string]declMap) {
	t.Helper()
	// -moz-tab-size and friends never apply (no tab width configured).
	for k := range got {
		if len(got[k]) == 0 {
			delete(got, k)
		}
	}
	for k := range want {
		if len(want[k]) == 0 {
			delete(want, k)
		}
	}
	if g, w := describe(got), describe(want); g != w {
		t.Errorf("%s differs from chroma\n--- got\n%s--- want\n%s", label, g, w)
	}
}

func TestThemeCSSMatchesChromaPerMode(t *testing.T) {
	names := styles.Names()
	for _, name := range names {
		other := DefaultDarkTheme
		if name == other {
			other = DefaultLightTheme
		}
		pairs := []Theme{{name, name}, {name, other}, {other, name}}
		for _, th := range pairs {
			css := ThemeCSS(th)
			compare(t, fmt.Sprintf("%v light forced", th), evaluate(t, css, true, "light"), expected(t, th.Light))
			compare(t, fmt.Sprintf("%v dark forced", th), evaluate(t, css, false, "dark"), expected(t, th.Dark))
			compare(t, fmt.Sprintf("%v system light", th), evaluate(t, css, false, ""), expected(t, th.Light))
			compare(t, fmt.Sprintf("%v system dark", th), evaluate(t, css, true, ""), expected(t, th.Dark))
			if t.Failed() {
				return
			}
		}
	}
}
