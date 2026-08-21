// Package escape provides the canonical HTML, attribute, and JavaScript string
// escaping used across the compiler. Every emitter that writes user content into
// HTML or generated JS must route through these functions so escaping stays
// consistent (one shared implementation, no drift).
package escape

import "strings"

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
	// The \x1f unit separator is the internal array-item delimiter used by
	// SSREval's array evaluation. It is not content and must never reach HTML.
	"\x1f", "",
)

// HTML escapes a string for safe inclusion in HTML text content or attribute
// values. The full set (& < > " ') is escaped and the internal \x1f array
// separator is stripped.
func HTML(s string) string {
	return htmlReplacer.Replace(s)
}

// HTMLAttr is an alias for HTML: both escape the full set, so the result is
// safe in double-quoted attributes, single-quoted attributes, and text nodes.
func HTMLAttr(s string) string {
	return HTML(s)
}

// JSString escapes a string for safe embedding in a JS single-quoted string
// literal. The caller is responsible for adding the surrounding quotes.
func JSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	s = strings.ReplaceAll(s, "\x00", `\0`)
	return s
}

// JSStringDQ escapes a string for embedding as a double-quoted JS string
// literal and returns it wrapped in double quotes. Used when interpolating a
// JSON payload (e.g. props) directly into generated JS.
func JSStringDQ(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}
