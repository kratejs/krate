// CSSARIAJS is the keyboard and ARIA enhancer for CSS-signal scopes that carry
// a widget role (tabs, listbox, disclosure, dialog, popover, menu). The pointer
// path is pure CSS; this script makes the label triggers focusable, maps
// Enter/Space/Escape/arrow keys onto the hidden controller inputs, manages
// focus (move into an opened overlay, trap it when modal, restore it on close),
// links dialog titles, and keeps aria-selected/aria-expanded in sync with the
// checked state the :has() stylesheet already tracks. It is injected ONLY on
// pages that use such a role, so every other CSS-signal page stays zero-JS.
package renderer

import (
	_ "embed"
	"strings"
)

//go:embed cssaria.js
var cssARIASource string

// CSSARIAJS is the enhancer with indentation and newlines removed. Every source
// line ends in a statement or block delimiter, so joining is safe.
var CSSARIAJS = minifyLines(cssARIASource)

func minifyLines(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		b.WriteString(strings.TrimSpace(line))
	}
	return b.String()
}
