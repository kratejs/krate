package build

import (
	"sort"
	"strings"
)

// Head tags of request-time renders. An async page's <Head> children render in
// place between comment markers; the renderer lifts them out of the body and
// sends them on the region frame (title and head). mergeHeadSection puts them
// into the head of the response so crawlers see them in the first bytes. The
// portable handler does the same (packages/runtime/src/head-tags.ts); the two
// must keep the same rules:
//
//   - A page tag replaces a baked tag with the same key: meta name, property
//     or http-equiv, meta charset, link rel=canonical, base.
//   - Among the page's own tags a repeated name, http-equiv, charset,
//     canonical or base keeps the last one; a repeated property (og:image)
//     keeps all of them.
//   - A tag identical to one already in the head is not added twice.
//   - Only meta, link, base, script, style and noscript are head content.
//   - The title replaces the baked title, or is added when there is none.

type headElement struct {
	name       string
	start, end int
	attrs      map[string]string
	inner      string
}

var headRawText = map[string]bool{"script": true, "style": true, "noscript": true, "title": true, "textarea": true, "template": true}

var headContent = map[string]bool{"meta": true, "link": true, "base": true, "script": true, "style": true, "noscript": true}

// scanHeadElements scans the elements of head markup: tag, attributes (quoted,
// unquoted and valueless, as the HTML tokenizer reads them) and the raw text of
// script, style and noscript. Comments and text between elements are skipped.
func scanHeadElements(s string) []headElement {
	var out []headElement
	lower := ""
	i := 0
	for i < len(s) {
		rel := strings.IndexByte(s[i:], '<')
		if rel < 0 {
			break
		}
		lt := i + rel
		if strings.HasPrefix(s[lt:], "<!--") {
			end := strings.Index(s[lt+4:], "-->")
			if end < 0 {
				i = len(s)
			} else {
				i = lt + 4 + end + 3
			}
			continue
		}
		if lt+1 >= len(s) || !isASCIIAlpha(s[lt+1]) {
			gt := strings.IndexByte(s[lt:], '>')
			if gt < 0 {
				i = len(s)
			} else {
				i = lt + gt + 1
			}
			continue
		}
		j := lt + 1
		for j < len(s) && !isHTMLSpace(s[j]) && s[j] != '/' && s[j] != '>' {
			j++
		}
		name := strings.ToLower(s[lt+1 : j])
		attrs := map[string]string{}
		closed := false
		for j < len(s) {
			for j < len(s) && (isHTMLSpace(s[j]) || s[j] == '/') {
				j++
			}
			if j < len(s) && s[j] == '>' {
				j++
				closed = true
				break
			}
			k := j
			for k < len(s) && !isHTMLSpace(s[k]) && s[k] != '=' && s[k] != '>' && s[k] != '/' {
				k++
			}
			if k == j {
				j++
				continue
			}
			an := strings.ToLower(s[j:k])
			j = k
			for j < len(s) && isHTMLSpace(s[j]) {
				j++
			}
			value := ""
			if j < len(s) && s[j] == '=' {
				j++
				for j < len(s) && isHTMLSpace(s[j]) {
					j++
				}
				if j < len(s) && (s[j] == '"' || s[j] == '\'') {
					q := s[j]
					e := strings.IndexByte(s[j+1:], q)
					if e < 0 {
						value = s[j+1:]
						j = len(s)
					} else {
						value = s[j+1 : j+1+e]
						j = j + 1 + e + 1
					}
				} else {
					e := j
					for e < len(s) && !isHTMLSpace(s[e]) && s[e] != '>' {
						e++
					}
					value = s[j:e]
					j = e
				}
			}
			if _, ok := attrs[an]; !ok {
				attrs[an] = value
			}
		}
		if !closed {
			break
		}
		end := j
		inner := ""
		if headRawText[name] {
			if lower == "" {
				lower = strings.ToLower(s)
			}
			close := strings.Index(lower[j:], "</"+name)
			if close < 0 {
				inner = s[j:]
				end = len(s)
			} else {
				close += j
				inner = s[j:close]
				gt := strings.IndexByte(s[close:], '>')
				if gt < 0 {
					end = len(s)
				} else {
					end = close + gt + 1
				}
			}
		}
		out = append(out, headElement{name: name, start: lt, end: end, attrs: attrs, inner: inner})
		i = end
	}
	return out
}

// headTagKey is the dedupe key of an element ("" when it never replaces another).
func headTagKey(el headElement) string {
	a := el.attrs
	has := func(k string) bool { _, ok := a[k]; return ok }
	switch el.name {
	case "meta":
		switch {
		case has("charset"):
			return "meta:charset"
		case has("name"):
			return "meta:name:" + strings.ToLower(a["name"])
		case has("property"):
			return "meta:property:" + strings.ToLower(a["property"])
		case has("http-equiv"):
			return "meta:http-equiv:" + strings.ToLower(a["http-equiv"])
		}
	case "link":
		for _, r := range strings.Fields(strings.ToLower(a["rel"])) {
			if r == "canonical" {
				return "link:canonical"
			}
		}
	case "base":
		return "base"
	}
	return ""
}

// singleHeadKey: a repeated property (og:image) is legitimate; every other key
// is single.
func singleHeadKey(key string) bool {
	return key != "" && !strings.HasPrefix(key, "meta:property:")
}

func headSignature(el headElement) string {
	keys := make([]string, 0, len(el.attrs))
	for k := range el.attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(el.name)
	b.WriteByte('|')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(el.attrs[k])
	}
	b.WriteByte('|')
	b.WriteString(el.inner)
	return b.String()
}

type headTag struct {
	html string
	key  string
	el   headElement
}

// parseHeadTags returns the head tags of a <Head> fragment: only head content,
// the last of a repeated single key.
func parseHeadTags(fragment string) []headTag {
	var tags []headTag
	for _, el := range scanHeadElements(fragment) {
		if !headContent[el.name] {
			continue
		}
		tags = append(tags, headTag{html: fragment[el.start:el.end], key: headTagKey(el), el: el})
	}
	last := map[string]int{}
	for i, t := range tags {
		if singleHeadKey(t.key) {
			last[t.key] = i
		}
	}
	kept := tags[:0:0]
	for i, t := range tags {
		if !singleHeadKey(t.key) || last[t.key] == i {
			kept = append(kept, t)
		}
	}
	return kept
}

// mergeHeadSection merges page head content into the head section of a
// document: head is the text from the start of the document up to (not
// including) </head>. It returns the new head section.
func mergeHeadSection(head, title, fragment string) string {
	var tags []headTag
	if fragment != "" {
		tags = parseHeadTags(fragment)
	}
	if title == "" && len(tags) == 0 {
		return head
	}
	type edit struct {
		start, end int
		repl       string
	}
	var edits []edit
	pageKeys := map[string]bool{}
	for _, t := range tags {
		if t.key != "" {
			pageKeys[t.key] = true
		}
	}
	kept := map[string]bool{}
	titleDone := false
	for _, el := range scanHeadElements(head) {
		if el.name == "title" && title != "" {
			if !titleDone {
				edits = append(edits, edit{el.start, el.end, "<title>" + title + "</title>"})
				titleDone = true
			} else {
				edits = append(edits, edit{el.start, el.end, ""})
			}
			continue
		}
		if key := headTagKey(el); key != "" && pageKeys[key] {
			edits = append(edits, edit{el.start, el.end, ""})
			continue
		}
		kept[headSignature(el)] = true
	}
	var add strings.Builder
	if title != "" && !titleDone {
		add.WriteString("<title>" + title + "</title>")
	}
	for _, t := range tags {
		sig := headSignature(t.el)
		if kept[sig] {
			continue
		}
		kept[sig] = true
		add.WriteString(t.html)
	}
	var out strings.Builder
	at := 0
	for _, e := range edits {
		out.WriteString(head[at:e.start])
		out.WriteString(e.repl)
		at = e.end
	}
	out.WriteString(head[at:])
	out.WriteString(add.String())
	return out.String()
}

// headEndOf returns where the head section of a document ends (</head>, else
// <body), or -1.
func headEndOf(doc string) int {
	lower := strings.ToLower(doc)
	if h := strings.Index(lower, "</head"); h >= 0 {
		return h
	}
	return strings.Index(lower, "<body")
}
