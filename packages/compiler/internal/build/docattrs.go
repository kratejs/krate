package build

import (
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// Attributes of the document's <html> and <body> elements.
//
// A <Head> may hold lowercase <html> and <body> elements. They render nothing
// in place; their attributes are set on the document element instead:
//
//	<Head><html lang="fr" class="dark" /><body class="font-sans" /></Head>
//
// Sources, lowest to highest priority: site.htmlAttributes and
// site.bodyAttributes, the layouts' <Head>, the page's <Head> (a later <Head>
// replaces an earlier one attribute by attribute), and for lang the page's
// `export const lang`. The same rules apply to request-time <Head> content;
// packages/runtime/src/head-tags.ts holds the portable copy.

// docAttrs is a set of element attributes with decoded values; "" is a boolean
// attribute.
type docAttrs map[string]string

var attrNameRe = regexp.MustCompile(`^[A-Za-z_:][-A-Za-z0-9_:.]*$`)

// docTagName returns "html" or "body" when tag is the start tag of that element.
func docTagName(tag string) string {
	if len(tag) < 5 || tag[0] != '<' {
		return ""
	}
	for _, name := range []string{"html", "body"} {
		if len(tag) > len(name)+1 && strings.EqualFold(tag[1:len(name)+1], name) {
			if c := tag[len(name)+1]; c == ' ' || c == '>' || c == '/' || c == '\t' || c == '\n' {
				return name
			}
		}
	}
	return ""
}

// isDocCloseTag reports whether tag is </html> or </body>.
func isDocCloseTag(tag string) bool {
	l := strings.ToLower(tag)
	return strings.HasPrefix(l, "</html") || strings.HasPrefix(l, "</body")
}

// parseTagAttrs reads the attributes of a start tag. Names are lowercased;
// the first occurrence of a name wins, as the HTML tokenizer has it.
func parseTagAttrs(tag string) docAttrs {
	out := docAttrs{}
	els := scanHeadElements(tag)
	if len(els) == 0 {
		return out
	}
	for k, v := range els[0].attrs {
		if attrNameRe.MatchString(k) {
			out[k] = html.UnescapeString(v)
		}
	}
	return out
}

// renderTagAttrs renders attributes sorted by name, values escaped.
func renderTagAttrs(a docAttrs) string {
	keys := make([]string, 0, len(a))
	for k := range a {
		if attrNameRe.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(k)
		if v := a[k]; v != "" {
			b.WriteString(`="`)
			b.WriteString(escape.HTMLAttr(v))
			b.WriteByte('"')
		}
	}
	return b.String()
}

// mergeDocAttrs overlays over onto base.
func mergeDocAttrs(base, over docAttrs) docAttrs {
	out := docAttrs{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// splitDocTags removes the <html> and <body> elements from head markup and
// returns their attributes. Later elements replace earlier ones attribute by
// attribute when last is true, else earlier ones win.
func splitDocTags(head string, last bool) (rest string, htmlA, body docAttrs) {
	if !strings.Contains(head, "<") {
		return head, nil, nil
	}
	lower := strings.ToLower(head)
	if !strings.Contains(lower, "<html") && !strings.Contains(lower, "<body") {
		return head, nil, nil
	}
	toks, tail := tokenizeHead(head)
	var out strings.Builder
	for _, t := range toks {
		out.WriteString(t.pre)
		switch name := docTagName(t.tag); {
		case name != "":
			attrs := parseTagAttrs(t.tag)
			target := &htmlA
			if name == "body" {
				target = &body
			}
			if last {
				*target = mergeDocAttrs(*target, attrs)
			} else {
				*target = mergeDocAttrs(attrs, *target)
			}
		case isDocCloseTag(t.tag):
		default:
			out.WriteString(t.tag)
		}
	}
	out.WriteString(tail)
	return out.String(), htmlA, body
}

// mergeDocTags folds the <html> and <body> elements of head markup into one
// element each, placed where the first one was.
func mergeDocTags(head string, last bool) string {
	rest, htmlA, body := splitDocTags(head, last)
	if htmlA == nil && body == nil {
		return head
	}
	var b strings.Builder
	if htmlA != nil {
		b.WriteString("<html" + renderTagAttrs(htmlA) + "></html>")
	}
	if body != nil {
		b.WriteString("<body" + renderTagAttrs(body) + "></body>")
	}
	return b.String() + rest
}

// startTagIndex returns the index of the first start tag of name in doc, or -1.
func startTagIndex(doc, name string) int {
	lower := strings.ToLower(doc)
	for i := 0; ; {
		rel := strings.Index(lower[i:], "<"+name)
		if rel < 0 {
			return -1
		}
		at := i + rel
		if end := at + 1 + len(name); end < len(doc) && strings.IndexByte(" >/\t\n", doc[end]) >= 0 {
			return at
		}
		i = at + 1
	}
}

// setStartTagAttrs sets attrs on the first start tag of name in doc, keeping
// the attributes already there for names attrs does not mention.
func setStartTagAttrs(doc, name string, attrs docAttrs) string {
	if len(attrs) == 0 {
		return doc
	}
	at := startTagIndex(doc, name)
	if at < 0 {
		return doc
	}
	end := htmlTagEnd(doc, at)
	merged := mergeDocAttrs(parseTagAttrs(doc[at:end]), attrs)
	return doc[:at] + "<" + name + renderTagAttrs(merged) + ">" + doc[end:]
}

// configDocAttrs converts a config attribute map.
func configDocAttrs(m config.AttrMap) docAttrs {
	if len(m) == 0 {
		return nil
	}
	out := docAttrs{}
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}
