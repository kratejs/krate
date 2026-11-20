package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// evaluateEmbeddedTSConfig evaluates a module-based config in-process via jseval
// (esbuild bundle + embedded QuickJS) and returns its JSON. No Node subprocess.
func evaluateEmbeddedTSConfig(configPath string, opts LoadOptions) ([]byte, error) {
	return evalConfigModule(configPath, "./"+filepath.Base(configPath), nil, environ.ScriptEnv(), opts)
}

// evalConfigModule evaluates the config module spec (resolved from the config's
// directory) and returns its JSON. virtual holds in-memory files by absolute
// path; env is what process.env shows.
func evalConfigModule(configPath, spec string, virtual map[string]string, env map[string]string, opts LoadOptions) ([]byte, error) {
	root := filepath.Dir(configPath)

	data, err := jseval.Eval(jseval.Options{
		Source:       configEvalBootstrap(spec, opts),
		ResolveDir:   root,
		Sourcefile:   "krate-config-bootstrap.ts",
		Env:          env,
		Await:        true,
		NodeShims:    true,
		VirtualFiles: virtual,
	})
	if err != nil {
		var thrown *jseval.ThrownError
		if errors.As(err, &thrown) && !strings.Contains(thrown.Text, validatePrefix) {
			return nil, &ConfigThrownError{Text: thrown.Text}
		}
		if msg := err.Error(); strings.Contains(msg, validatePrefix) {
			idx := strings.Index(msg, validatePrefix) + len(validatePrefix)
			return nil, &ConfigValidationError{Message: strings.TrimSpace(msg[idx:endOfLine(msg, idx)])}
		}
		return nil, fmt.Errorf("evaluating config: %w", err)
	}
	return data, nil
}

// configEvalBootstrap is the synthetic entry module. It imports the user config,
// supports the object or function form of defineConfig (calling the function with
// { command, mode, isDev }), normalizes plugin/theme `module` file:// URLs to
// filesystem paths (so the compiler can bundle them), runs validate(), and
// assigns the config object to jseval.ResultGlobal.
func configEvalBootstrap(spec string, opts LoadOptions) string {
	commandJSON, _ := json.Marshal(opts.Command)
	modeJSON, _ := json.Marshal(opts.Mode)
	isDev := opts.Command == "dev" || opts.Mode == "development"
	specJSON, _ := json.Marshal(spec)
	// The user module is imported dynamically inside try/catch so a throw while
	// it evaluates (or a rejected top-level await) is reported with its
	// message, not as a missing result. Awaiting covers async defineConfig.
	return fmt.Sprintf(`function __krateFileURLToPath(u) {
  var p = String(u).replace(/^file:\/\//, '');
  try { p = decodeURIComponent(p); } catch (e) {}
  if (p.charAt(0) === '/' && p.charAt(2) === ':' && /[A-Za-z]/.test(p.charAt(1))) { p = p.slice(1); }
  return p;
}
function __krateDescribe(e) {
  if (e && typeof e === 'object') {
    var m = e.message ? String(e.message) : String(e);
    var name = e.name && e.name !== 'Error' ? e.name + ': ' : '';
    return name + m + (e.stack ? '\n' + String(e.stack) : '');
  }
  return String(e);
}
try {
  var __krateEnv = { command: %s, mode: %s, isDev: %t, dev: %t };
  var cfg = await import(%s);
  var config = (cfg && cfg.default) ? cfg.default : cfg;
  // A config that mixes imports with module.exports = cfg exports nothing as
  // ESM; take the CommonJS value instead.
  if (!(cfg && (cfg.default || Object.keys(cfg).length)) && globalThis.module && globalThis.module.exports && Object.keys(globalThis.module.exports).length) {
    config = globalThis.module.exports.default || globalThis.module.exports;
  }
  if (typeof config === 'function') {
    config = config(__krateEnv);
  }
  config = (await config) || {};
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
} catch (e) {
  globalThis.%s = __krateDescribe(e);
}
`, string(commandJSON), string(modeJSON), isDev, isDev, string(specJSON), validatePrefix, jseval.ResultGlobal, jseval.ErrorGlobal)
}

// EvaluateSource evaluates src as the project's krate.config.ts, with env as
// process.env, and returns the resulting config. It runs the same evaluator as
// Load (a build-command, production-mode context) but never falls back to the
// static parser, so any failure to run the module is returned. The file need
// not exist on disk: the migrator uses this to check a config it has not
// written yet.
func EvaluateSource(root, src string, env map[string]string) (*Config, error) {
	warningsMu.Lock()
	defer warningsMu.Unlock()
	path, err := filepath.Abs(filepath.Join(root, "krate.config.ts"))
	if err != nil {
		return nil, err
	}
	opts := LoadOptions{Command: "build", Mode: "production"}
	raw, err := evalConfigModule(path, path, map[string]string{filepath.Clean(path): src}, env, opts)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, describeUnmarshalError("krate.config.ts", err)
	}
	if err := applyNormalizedKeys(raw, cfg); err != nil {
		return nil, fmt.Errorf("config krate.config.ts: %w", err)
	}
	if err := cfg.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}
