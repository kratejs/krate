package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// executeEmbeddedTSConfig evaluates a module-based config by bundling it with
// esbuild and running the bundle in the embedded QuickJS runtime — no Node or
// `npx tsx` subprocess. This is the fast, dependency-free path: the bundle is
// self-contained, and `import.meta.url` is preserved per source file so plugin
// and theme factories can still locate their own modules on disk.
func executeEmbeddedTSConfig(configPath string, cfg *Config) error {
	root := filepath.Dir(configPath)

	bundle, err := bundleConfigForEval(configPath, root)
	if err != nil {
		return err
	}

	rt, err := jsruntime.New()
	if err != nil {
		return fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()
	_ = rt.SetEnv(environ.Current)

	if _, err := rt.ExecuteModule(bundle); err != nil {
		if msg := err.Error(); strings.Contains(msg, validatePrefix) {
			idx := strings.Index(msg, validatePrefix) + len(validatePrefix)
			return &ConfigValidationError{Message: strings.TrimSpace(msg[idx:endOfLine(msg, idx)])}
		}
		return fmt.Errorf("evaluating config: %w", err)
	}
	res, err := rt.Execute(`JSON.stringify(globalThis.__krateConfig||null)`)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	out, _ := res.(string)
	if out == "" || out == "null" {
		return fmt.Errorf("config has no default export")
	}
	data := []byte(out)

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
// compiler can bundle them), runs validate(), and re-exports the config object.
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
globalThis.__krateConfig = config;
`, spec, validatePrefix)
}

// bundleConfigForEval esbuild-bundles the config bootstrap into a self-contained
// IIFE assigned to the `__krateConfig` global.
func bundleConfigForEval(configPath, root string) (string, error) {
	spec := "./" + filepath.Base(configPath)
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   configEvalBootstrap(spec),
			ResolveDir: root,
			Sourcefile: "krate-config-bootstrap.ts",
			Loader:     api.LoaderTS,
		},
		Bundle:   true,
		Format:   api.FormatESModule,
		Platform: api.PlatformNode,
		Write:    false,
		LogLevel: api.LogLevelSilent,
		Plugins:  []api.Plugin{importMetaPreserver()},
	})
	if len(result.Errors) > 0 {
		var sb strings.Builder
		for _, e := range result.Errors {
			if e.Location != nil {
				fmt.Fprintf(&sb, "%s:%d:%d: %s\n", e.Location.File, e.Location.Line, e.Location.Column, e.Text)
			} else {
				sb.WriteString(e.Text + "\n")
			}
		}
		return "", fmt.Errorf("bundling config:\n%s", sb.String())
	}
	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("config bundle produced no output")
	}
	return string(result.OutputFiles[0].Contents), nil
}

// importMetaPreserver rewrites, per source file, `import.meta.url` to a string
// literal of that file's own file:// URL, so bundling every module together does
// not collapse their URLs onto the bundle's location. Plugin and theme factories
// rely on this to point the compiler back at their real source on disk.
func importMetaPreserver() api.Plugin {
	return api.Plugin{
		Name: "krate-preserve-import-meta",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if args.Namespace != "file" {
					return api.OnLoadResult{}, nil
				}
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, nil
				}
				src := string(data)
				if strings.Contains(src, "import.meta.url") {
					src = strings.ReplaceAll(src, "import.meta.url", fmt.Sprintf("%q", fileURL(args.Path)))
				}
				contents := src
				return api.OnLoadResult{Contents: &contents, Loader: loaderForPath(args.Path)}, nil
			})
		},
	}
}

// fileURL converts an absolute filesystem path to a file:// URL (with a leading
// slash so Windows drive letters become file:///C:/...).
func fileURL(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func loaderForPath(p string) api.Loader {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".ts":
		return api.LoaderTS
	case ".tsx":
		return api.LoaderTSX
	case ".jsx":
		return api.LoaderJSX
	case ".json":
		return api.LoaderJSON
	case ".mjs", ".cjs", ".js":
		return api.LoaderJS
	default:
		return api.LoaderJS
	}
}
