package csssignals

import "strings"

// StyleMode selects how CSS-signal stylesheets are emitted for browser
// compatibility.
type StyleMode int

const (
	// StyleModern emits :has()-based selectors (the zero-JS default).
	StyleModern StyleMode = iota
	// StyleLegacy emits class-based selectors driven by the scoped shim, for
	// browsers without :has().
	StyleLegacy
	// StyleHybrid emits both, wrapped in @supports selector(:has(*)), so modern
	// browsers keep the :has() path and legacy browsers use the class path.
	StyleHybrid
)

// ParseStyleMode maps a config string ("modern"|"legacy"|"hybrid") to a
// StyleMode, defaulting to StyleModern.
func ParseStyleMode(s string) StyleMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "legacy":
		return StyleLegacy
	case "hybrid":
		return StyleHybrid
	default:
		return StyleModern
	}
}

// String returns the config token for the mode.
func (m StyleMode) String() string {
	switch m {
	case StyleLegacy:
		return "legacy"
	case StyleHybrid:
		return "hybrid"
	default:
		return "modern"
	}
}

// NeedsShim reports whether the mode requires the legacy scoped shim to drive
// the class-based selectors (every mode except modern).
func (m StyleMode) NeedsShim() bool { return m == StyleLegacy || m == StyleHybrid }

// Sentinels wrapped inside the hybrid @supports blocks. The build-time
// per-variant split (internal/css) matches these prefixes (which include the
// opening brace and the sentinel comment), so it only ever transforms
// Krate-generated blocks and never an author's own `@supports selector(...)`.
const (
	CompatModernPrefix = "@supports selector(:has(*)){/*krate:modern*/"
	CompatLegacyPrefix = "@supports not selector(:has(*)){/*krate:legacy*/"
)
