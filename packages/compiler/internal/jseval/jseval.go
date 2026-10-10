// Package jseval evaluates TypeScript/JavaScript in-process: it bundles an entry
// module (resolving imports) with esbuild and runs it in the embedded QuickJS
// runtime - no Node, no tsx, no subprocess. It backs build-time evaluation of
// user TS such as krate.config.ts and tailwind.config.ts.
package jseval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// ResultGlobal is the global the entry assigns its result to. Eval returns its
// JSON encoding, so entries end with e.g.
// `globalThis.__krateEvalResult = config;`.
const ResultGlobal = "__krateEvalResult"

// Options configures Eval.
type Options struct {
	// Source is the entry module source (TS/TSX/JS). It may import real files,
	// which esbuild resolves and bundles relative to ResolveDir.
	Source string
	// ResolveDir is the esbuild resolve/working directory for the entry.
	ResolveDir string
	// Sourcefile names the entry in diagnostics (default "__krate_entry.ts").
	Sourcefile string
	// Loader is the entry loader (default TS).
	Loader api.Loader
	// Env, when non-empty, is exposed to the script as process.env.
	Env map[string]string
}

// Eval bundles the entry (resolving imports) and runs it in the embedded QuickJS
// runtime, returning the JSON value assigned to globalThis.__krateEvalResult.
// It returns an error when the source cannot be bundled or executed, so callers
// can fall back to a static path.
func Eval(opts Options) (json.RawMessage, error) {
	bundle, err := bundle(opts)
	if err != nil {
		return nil, err
	}

	rt, err := jsruntime.New()
	if err != nil {
		return nil, fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()
	if len(opts.Env) > 0 {
		_ = rt.SetEnv(opts.Env)
	}

	if _, err := rt.ExecuteModule(bundle); err != nil {
		return nil, err
	}
	res, err := rt.Execute(`JSON.stringify(globalThis.` + ResultGlobal + ` ?? null)`)
	if err != nil {
		return nil, err
	}
	s, _ := res.(string)
	if s == "" || s == "null" {
		return nil, fmt.Errorf("entry produced no %s", ResultGlobal)
	}
	return json.RawMessage(s), nil
}

func bundle(opts Options) (string, error) {
	sourcefile := opts.Sourcefile
	if sourcefile == "" {
		sourcefile = "__krate_entry.ts"
	}
	loader := opts.Loader
	if loader == api.LoaderNone {
		loader = api.LoaderTS
	}
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   opts.Source,
			ResolveDir: opts.ResolveDir,
			Sourcefile: sourcefile,
			Loader:     loader,
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
		return "", fmt.Errorf("bundling entry:\n%s", sb.String())
	}
	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("bundle produced no output")
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
