package build

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// globalHeadHTML renders the site-wide <head> injection configured via `head`.
// It is the same for every page, so it is injected via a </head> replacement in
// the page-finish step (before CSP hashing). Tags the page's own head already
// declares (same meta key, link href, or script src) are skipped.
func (b *Builder) globalHeadHTML(existing string) string {
	h := b.Cfg.Head
	if len(h.Meta)+len(h.Links)+len(h.Scripts)+len(h.Raw) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range h.Meta {
		if headMetaPresent(existing, m) {
			continue
		}
		sb.WriteString(renderHeadMeta(m))
	}
	for _, l := range h.Links {
		if l.Href != "" && strings.Contains(existing, `href="`+escape.HTMLAttr(l.Href)+`"`) {
			continue
		}
		sb.WriteString(renderHeadLink(l))
	}
	for _, s := range h.Scripts {
		if s.Src != "" && strings.Contains(existing, `src="`+escape.HTMLAttr(s.Src)+`"`) {
			continue
		}
		sb.WriteString(renderHeadScript(s))
	}
	// Raw snippets are injected verbatim (they cannot be matched reliably).
	for _, raw := range h.Raw {
		sb.WriteString(raw)
		if !strings.HasSuffix(raw, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// headMetaPresent reports whether the existing document already declares a meta
// tag with the same identifying attribute (page-level declarations win).
func headMetaPresent(existing string, m config.HeadMeta) bool {
	if m.Charset != "" {
		return strings.Contains(existing, `charset="`+escape.HTMLAttr(m.Charset)+`"`)
	}
	key, val := "", ""
	switch {
	case m.Name != "":
		key, val = "name", m.Name
	case m.Property != "":
		key, val = "property", m.Property
	case m.HTTPEquiv != "":
		key, val = "http-equiv", m.HTTPEquiv
	default:
		return false
	}
	return strings.Contains(existing, key+`="`+escape.HTMLAttr(val)+`"`)
}

func writeHeadAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString("=\"")
	b.WriteString(escape.HTMLAttr(value))
	b.WriteByte('"')
}

func renderHeadMeta(m config.HeadMeta) string {
	var b strings.Builder
	b.WriteString("<meta")
	writeHeadAttr(&b, "charset", m.Charset)
	writeHeadAttr(&b, "name", m.Name)
	writeHeadAttr(&b, "property", m.Property)
	writeHeadAttr(&b, "http-equiv", m.HTTPEquiv)
	writeHeadAttr(&b, "content", m.Content)
	b.WriteString(">\n")
	return b.String()
}

func renderHeadLink(l config.HeadLink) string {
	var b strings.Builder
	b.WriteString("<link")
	writeHeadAttr(&b, "rel", l.Rel)
	writeHeadAttr(&b, "href", l.Href)
	writeHeadAttr(&b, "as", l.As)
	writeHeadAttr(&b, "type", l.Type)
	writeHeadAttr(&b, "crossorigin", l.CrossOrigin)
	writeHeadAttr(&b, "media", l.Media)
	writeHeadAttr(&b, "sizes", l.Sizes)
	b.WriteString(">\n")
	return b.String()
}

func renderHeadScript(s config.HeadScript) string {
	var b strings.Builder
	b.WriteString("<script")
	switch {
	case s.Module:
		b.WriteString(" type=\"module\"")
	case s.Type != "":
		writeHeadAttr(&b, "type", s.Type)
	}
	writeHeadAttr(&b, "src", s.Src)
	if s.Async {
		b.WriteString(" async")
	}
	if s.Defer {
		b.WriteString(" defer")
	}
	keys := make([]string, 0, len(s.Attrs))
	for k := range s.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeHeadAttr(&b, k, s.Attrs[k])
	}
	if s.Content != "" {
		b.WriteString(">")
		b.WriteString(escapeInlineScript(s.Content))
		b.WriteString("</script>\n")
	} else {
		b.WriteString("></script>\n")
	}
	return b.String()
}

// escapeInlineScript makes inline JS safe to embed in an HTML <script> by
// breaking up any `</script` (and `<!--`) sequences that would end the block.
func escapeInlineScript(js string) string {
	js = strings.ReplaceAll(js, "</", "<\\/")
	js = strings.ReplaceAll(js, "<!--", "<\\!--")
	return js
}
