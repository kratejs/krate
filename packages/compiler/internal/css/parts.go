package css

import "strings"

// A page's raw CSS is assembled from many sources (layouts, packages, the
// page's own imports) by plain string concatenation. Each source is preceded
// by a part marker comment naming it, so the build can later write one
// stylesheet per source. Markers are comments, so text that still carries
// them is valid CSS; SplitParts removes them.
const partMarkerOpen = "/*krate:css-part "

// PartMarker returns the marker that starts the CSS of source key. An empty
// key marks CSS that belongs to the page itself (generated rules, plugin
// additions).
func PartMarker(key string) string {
	key = strings.ReplaceAll(key, "*/", "*_/")
	return partMarkerOpen + key + "*/\n"
}

// StartsWithPart reports whether text begins (after whitespace) with a part
// marker.
func StartsWithPart(text string) bool {
	return strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), partMarkerOpen)
}

// Part is the CSS of one source, in cascade order.
type Part struct {
	Key string
	CSS string
}

// SplitParts splits text at its part markers. Text before the first marker
// belongs to the page itself (Key ""). Parts whose CSS is only whitespace are
// dropped; the order of the rest is the order of the text.
func SplitParts(text string) []Part {
	var out []Part
	add := func(key, body string) {
		if strings.TrimSpace(body) != "" {
			out = append(out, Part{Key: key, CSS: body})
		}
	}
	key := ""
	for {
		i := strings.Index(text, partMarkerOpen)
		if i < 0 {
			add(key, text)
			return out
		}
		add(key, text[:i])
		rest := text[i+len(partMarkerOpen):]
		end := strings.Index(rest, "*/")
		if end < 0 {
			// A truncated marker: keep the remaining text with the current part.
			add(key, text[i:])
			return out
		}
		key = rest[:end]
		text = strings.TrimPrefix(rest[end+2:], "\n")
	}
}

// StripPartMarkers removes every part marker from text.
func StripPartMarkers(text string) string {
	if !strings.Contains(text, partMarkerOpen) {
		return text
	}
	var sb strings.Builder
	for _, p := range SplitParts(text) {
		sb.WriteString(p.CSS)
	}
	return sb.String()
}

// MapParts applies fn to the CSS of every part and keeps the markers, so a
// rule-level pass (such as unused-rule pruning) never sees a marker glued to
// the first rule of a part.
func MapParts(text string, fn func(string) string) string {
	if !strings.Contains(text, partMarkerOpen) {
		return fn(text)
	}
	var sb strings.Builder
	for _, p := range SplitParts(text) {
		sb.WriteString(PartMarker(p.Key))
		sb.WriteString(fn(p.CSS))
	}
	return sb.String()
}
