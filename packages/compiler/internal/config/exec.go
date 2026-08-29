package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"krate-compiler/internal/tsexec"
)

// configExecTimeout bounds how long the npx tsx config bootstrap may run. A
// user config that hangs (blocking network call, infinite loop) must not block
// the build indefinitely.
const configExecTimeout = 30 * time.Second

// validatePrefix is the marker printed to stderr by the bootstrap when the
// user-supplied validate() function throws. The Go code uses this to surface
// a clear "config validation failed" message instead of a generic execution error.
const validatePrefix = "KRATE_CONFIG_VALIDATION_ERROR: "

// executeTSConfig runs krate.config.ts via npx tsx and parses the JSON output.
// A temporary bootstrap file imports the config module and prints the result as JSON.
// If the config exports a validate() function, it is called with the config object
// before serialization; errors are surfaced as ConfigValidationError.
// configPath should be the absolute path to the config file.
// The working directory for the subprocess is the directory containing the config.
func executeTSConfig(configPath string, cfg *Config) error {
	if _, err := os.Stat(configPath); err != nil {
		return err
	}

	root := filepath.Dir(configPath)

	output, stderr, err := tsexec.RunBootstrap("krate-config-bootstrap", configBootstrapContent(configPath), root, configExecTimeout)
	if err != nil {
		if idx := strings.Index(stderr, validatePrefix); idx >= 0 {
			return &ConfigValidationError{Message: stderr[idx+len(validatePrefix) : endOfLine(stderr, idx)]}
		}
		return err
	}

	if err := json.Unmarshal(output, cfg); err != nil {
		return fmt.Errorf("parsing config output: %w", err)
	}

	// Resolve plugin module paths relative to the config directory
	for i, p := range cfg.Plugins {
		if p.Module != "" && !filepath.IsAbs(p.Module) {
			cfg.Plugins[i].Module = filepath.Join(root, p.Module)
		}
	}

	return nil
}

// ConfigValidationError is returned when the user's validate() function throws.
type ConfigValidationError struct {
	Message string
}

func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("config validation failed: %s", e.Message)
}

// configBootstrapContent returns the ESM bootstrap script that imports a user
// krate.config.ts, resolves community plugin module paths, runs validate(), and
// prints the config as JSON.
func configBootstrapContent(configPath string) string {
	return fmt.Sprintf(
		`import cfg from '%s';
import { fileURLToPath } from 'url';
const config = cfg.default || cfg;
// Resolve community plugin module paths. Plugin factories return the module's
// own location via import.meta.url (a file:// URL); convert it to a filesystem
// path so the compiler can bundle it.
if (Array.isArray(config.plugins)) {
  for (const p of config.plugins) {
    if (p && typeof p.module === 'string' && p.module.startsWith('file://')) {
      p.module = fileURLToPath(p.module);
    }
  }
}
if (typeof config.validate === 'function') {
	try {
		const result = config.validate(config);
		if (result && typeof result.then === 'function') await result;
	} catch (e) {
		process.stderr.write('KRATE_CONFIG_VALIDATION_ERROR: ' + (e.message || e) + '\\n');
		process.exit(1);
	}
}
delete config.validate;
console.log(JSON.stringify(config));`,
		tsexec.ImportPath(configPath),
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

// configUsesModules reports whether a config source relies on imports/requires
// that the static parser cannot handle. When true, the only viable way to load
// the config is via JS execution.
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
// could not be executed. It explains the likely dependency problem while
// preserving the underlying execution error for context.
func configNotExecutableError(tsPath string, err error) error {
	return fmt.Errorf(
		"parsing config %s: config uses imports/requires but could not be executed. "+
			"This usually means packages aren't installed correctly — run `npm install` in the project root. "+
			"Underlying error: %w",
		tsPath, err,
	)
}
