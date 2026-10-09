package config

import (
	"fmt"
	"strings"
)

// validatePrefix is the marker thrown by the embedded config bootstrap when the
// user-supplied validate() function throws. The Go code uses this to surface a
// clear "config validation failed" message instead of a generic execution error.
const validatePrefix = "KRATE_CONFIG_VALIDATION_ERROR: "

// ConfigValidationError is returned when the user's validate() function throws.
type ConfigValidationError struct {
	Message string
}

func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("config validation failed: %s", e.Message)
}

// configUsesModules reports whether a config source relies on imports/requires
// that the static parser cannot handle. When true, the only viable way to load
// the config is JS execution (esbuild + the embedded QuickJS runtime).
func configUsesModules(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "import ") {
			return true
		}
		// require(...) is a common way to pull in packages; it can appear
		// mid-line (e.g. `const cfg = require('@krate/config')`).
		if strings.Contains(line, "require(") {
			return true
		}
	}
	return false
}

// configNotExecutableError builds the error surfaced when a module-based config
// could not be executed by the embedded runtime.
func configNotExecutableError(tsPath string, err error) error {
	return fmt.Errorf(
		"parsing config %s: config uses imports/requires but could not be evaluated. "+
			"This usually means a dependency isn't installed — run `npm install` in the project root. "+
			"Underlying error: %w",
		tsPath, err,
	)
}

func endOfLine(s string, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return i
		}
	}
	return len(s)
}
