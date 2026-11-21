package migrate

import (
	"strings"
	"testing"
)

// Next.js swaps pages without a View Transition unless
// experimental.viewTransition is on, so the migrated config turns Krate's off
// unless the app opted in. Krate's transitions snapshot the page, and a
// backdrop-filter surface (the Genesis sidebar) composited dimmer for a frame.
func TestNextConfigViewTransitions(t *testing.T) {
	for _, c := range []struct {
		name, config string
		off          bool
	}{
		{"no next.config", "", true},
		{"default", `module.exports = { trailingSlash: false };`, true},
		{"opted in", `module.exports = { experimental: { viewTransition: true } };`, false},
		{"explicitly off", `module.exports = { experimental: { viewTransition: false } };`, true},
	} {
		files := map[string]string{}
		if c.config != "" {
			files["next.config.js"] = c.config
		}
		body := configBody(t, scanConfig(t, files))
		if got := strings.Contains(body, `"viewTransitions": "off"`); got != c.off {
			t.Errorf("%s: viewTransitions off = %v, want %v:\n%s", c.name, got, c.off, body)
		}
	}
	rep := scanConfig(t, map[string]string{"next.config.js": `module.exports = { experimental: { viewTransition: true } };`})
	for _, w := range rep.Warnings {
		if strings.Contains(w, "viewTransition") {
			t.Errorf("experimental.viewTransition is translated, not reported: %q", w)
		}
	}
}
