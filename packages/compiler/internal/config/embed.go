package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// executeEmbeddedTSConfig evaluates a module-based config in-process via
// jseval (esbuild bundle + embedded QuickJS) — no Node or `npx tsx` subprocess.
func executeEmbeddedTSConfig(configPath string, cfg *Config) error {
	root := filepath.Dir(configPath)

	data, err := jseval.Eval(jseval.Options{
		Source:     configEvalBootstrap("./" + filepath.Base(configPath)),
		ResolveDir: root,
		Sourcefile: "krate-config-bootstrap.ts",
		Env:        environ.Current,
	})
	if err != nil {
		if msg := err.Error(); strings.Contains(msg, validatePrefix) {
			idx := strings.Index(msg, validatePrefix) + len(validatePrefix)
			return &ConfigValidationError{Message: strings.TrimSpace(msg[idx:endOfLine(msg, idx)])}
		}
		return fmt.Errorf("evaluating config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing config output: %w", err)
	}
	Warnings = append(Warnings, UnknownKeyWarnings(data)...)

	// Resolve plugin module paths relative to the config directory.
	for i, p := range cfg.Plugins {
		if p.Module != "" && !filepath.IsAbs(p.Module) {
			cfg.Plugins[i].Module = filepath.Join(root, p.Module)
		}
	}
	return nil
}

// configEvalBootstrap is the synthetic entry module. It imports the user config,
// normalizes plugin/theme `module` file:// URLs to filesystem paths (so the
// compiler can bundle them), runs validate(), and assigns the config object to
// jseval.ResultGlobal.
func configEvalBootstrap(spec string) string {
	return fmt.Sprintf(`import cfg from '%s';

function __krateFileURLToPath(u) {
  var p = String(u).replace(/^file:\/\//, '');
  try { p = decodeURIComponent(p); } catch (e) {}
  if (p.charAt(0) === '/' && p.charAt(2) === ':' && /[A-Za-z]/.test(p.charAt(1))) { p = p.slice(1); }
  return p;
}

var config = (cfg && cfg.default) ? cfg.default : cfg;
if (Array.isArray(config.plugins)) {
  for (var i = 0; i < config.plugins.length; i++) {
    var p = config.plugins[i];
    if (p && typeof p.module === 'string' && p.module.indexOf('file://') === 0) { p.module = __krateFileURLToPath(p.module); }
    var theme = (p && p.options && typeof p.options === 'object') ? p.options.theme : null;
    if (theme && typeof theme === 'object' && typeof theme.module === 'string' && theme.module.indexOf('file://') === 0) { theme.module = __krateFileURLToPath(theme.module); }
  }
}
if (typeof config.validate === 'function') {
  try {
    config.validate(config);
  } catch (e) {
    throw new Error('%s' + ((e && e.message) ? e.message : String(e)));
  }
}
delete config.validate;
globalThis.%s = config;
`, spec, validatePrefix, jseval.ResultGlobal)
}
