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
	"github.com/kratejs/krate/packages/compiler/internal/safe"
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
	// ResolveImport, when set, is consulted before esbuild for every import
	// specifier. Returning ok=true resolves it to an absolute file path (used to
	// map `krate/content` and tsconfig path aliases, which esbuild can't see).
	ResolveImport func(importer, spec string) (string, bool)
	// Await drains the QuickJS job queue after the module evaluates, so a result
	// assigned from a resolved Promise becomes visible.
	Await bool
}

// Eval bundles the entry (resolving imports) and runs it in the embedded QuickJS
// runtime, returning the JSON value assigned to globalThis.__krateEvalResult.
// It returns an error when the source cannot be bundled or executed, so callers
// can fall back to a static path.
func Eval(opts Options) (out json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = safe.Recover(r)
		}
	}()

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
	if opts.Await {
		rt.DrainJobs()
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
	plugins := []api.Plugin{}
	if opts.ResolveImport != nil {
		plugins = append(plugins, resolveForwarder(opts.ResolveImport))
	}
	plugins = append(plugins, krateResolver(), importMetaPreserver())
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
		Plugins:  plugins,
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
				if strings.Contains(src, importMetaURL) {
					src = rewriteImportMetaURL(src, fmt.Sprintf("%q", fileURL(args.Path)))
				}
				contents := src
				return api.OnLoadResult{Contents: &contents, Loader: loaderForPath(args.Path)}, nil
			})
		},
	}
}

// importMetaURL is the meta-property importMetaPreserver rewrites.
const importMetaURL = "import.meta.url"

// rewriteImportMetaURL returns src with every code-context occurrence of
// import.meta.url replaced by lit (a quoted file:// URL). Occurrences inside
// string literals, template-literal text, and comments are left untouched, so
// source that merely mentions the text (for example a docs code sample held in
// a string) is not corrupted.
func rewriteImportMetaURL(src, lit string) string {
	var b strings.Builder
	b.Grow(len(src))
	writeCode(&b, src, 0, len(src), lit, false)
	return b.String()
}

// writeCode copies src[begin:end] in code context, replacing import.meta.url
// with lit. When stopAtBrace is set it returns just past the '}' that closes the
// enclosing template expression; otherwise it consumes the whole range.
func writeCode(b *strings.Builder, src string, begin, end int, lit string, stopAtBrace bool) int {
	depth := 0
	i := begin
	for i < end {
		c := src[i]
		switch {
		case c == '{':
			depth++
			b.WriteByte(c)
			i++
		case c == '}' && stopAtBrace && depth == 0:
			b.WriteByte(c)
			return i + 1
		case c == '}':
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
			i++
		case c == '/' && i+1 < end && src[i+1] == '/':
			i = writeLineComment(b, src, i, end)
		case c == '/' && i+1 < end && src[i+1] == '*':
			i = writeBlockComment(b, src, i, end)
		case c == '\'' || c == '"':
			i = writeQuoted(b, src, i, end)
		case c == '`':
			i = writeTemplate(b, src, i, end, lit)
		case c == 'i' && end-i >= len(importMetaURL) && src[i:i+len(importMetaURL)] == importMetaURL && identifierBoundary(src, i, i+len(importMetaURL)):
			b.WriteString(lit)
			i += len(importMetaURL)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return i
}

func writeLineComment(b *strings.Builder, src string, i, end int) int {
	j := i + 2
	for j < end && src[j] != '\n' {
		j++
	}
	b.WriteString(src[i:j])
	return j
}

func writeBlockComment(b *strings.Builder, src string, i, end int) int {
	stop := end
	if j := strings.Index(src[i+2:end], "*/"); j >= 0 {
		stop = i + 2 + j + 2
	}
	b.WriteString(src[i:stop])
	return stop
}

func writeQuoted(b *strings.Builder, src string, i, end int) int {
	q := src[i]
	j := i + 1
	for j < end {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case q:
			j++
			b.WriteString(src[i:j])
			return j
		case '\n':
			b.WriteString(src[i:j])
			return j
		}
		j++
	}
	b.WriteString(src[i:end])
	return end
}

// writeTemplate copies a template literal starting at the backtick src[i],
// rewriting import.meta.url inside each ${...} substitution and leaving the
// literal text untouched.
func writeTemplate(b *strings.Builder, src string, i, end int, lit string) int {
	j := i + 1
	for j < end {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case '`':
			j++
			b.WriteString(src[i:j])
			return j
		case '$':
			if j+1 < end && src[j+1] == '{' {
				b.WriteString(src[i : j+2])
				i = writeCode(b, src, j+2, end, lit, true)
				j = i
				continue
			}
			j++
		default:
			j++
		}
	}
	b.WriteString(src[i:end])
	return end
}

// identifierBoundary reports whether src[start:end] is a standalone token (not
// adjacent to an identifier character or a property access dot).
func identifierBoundary(src string, start, end int) bool {
	if start > 0 {
		if p := src[start-1]; isIdentByte(p) || p == '.' {
			return false
		}
	}
	if end < len(src) {
		if n := src[end]; isIdentByte(n) || n == '.' {
			return false
		}
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
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
