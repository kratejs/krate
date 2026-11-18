package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/routesource"
)

// nextConfigData is the subset of next.config.* that maps cleanly onto Krate.
type nextConfigData struct {
	BasePath  string            `json:"basePath,omitempty"`
	Redirects []nextRedirect    `json:"redirects,omitempty"`
	Rewrites  []nextRewrite     `json:"rewrites,omitempty"`
	Headers   []nextHeaderRule  `json:"headers,omitempty"`
	Images    map[string]any    `json:"images,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Site      map[string]any    `json:"site,omitempty"`
	// Head holds raw <head> tags written to the config head array.
	Head []string `json:"head,omitempty"`
	// ServerExternalPackages and OutputFileTracingIncludes carry Next's keys of
	// the same name to the Krate config keys of the same name.
	ServerExternalPackages    []string            `json:"serverExternalPackages,omitempty"`
	OutputFileTracingIncludes map[string][]string `json:"outputFileTracingIncludes,omitempty"`
	// MiddlewareRuntime is "node" when the middleware cannot run in the
	// embedded runtime; it becomes ssr.middlewareRuntime.
	MiddlewareRuntime string `json:"-"`
	// TrailingSlash is "always" when next.config sets trailingSlash: true and
	// "never" otherwise (Next's URL form).
	TrailingSlash string `json:"trailingSlash,omitempty"`
	// EnvDir is the relative directory loadEnvConfig(...) reads .env files
	// from; it becomes envDir. Empty when absent or the project root.
	EnvDir string `json:"-"`
	// EnvPrefix is set by the migrator when the app reads NEXT_PUBLIC_
	// variables; it becomes envPrefix.
	EnvPrefix []string `json:"-"`
	// MiddlewareRedirects are redirects the config cannot express; the
	// migrator injects them at the start of the middleware.
	MiddlewareRedirects []middlewareRedirect `json:"-"`
	// DeployTarget is "node" for output: 'standalone'; it becomes
	// deploy.target.
	DeployTarget string `json:"-"`
	// OutputFileTracingRoot is outputFileTracingRoot as a path relative to the
	// project (for example "../.."); TracingDepth is how many levels it is up
	// and TracingRel the project's path below it ("packages/web").
	OutputFileTracingRoot string `json:"-"`
	TracingDepth          int    `json:"-"`
	TracingRel            string `json:"-"`
	// Tailwind is true when the project's PostCSS config runs Tailwind.
	Tailwind bool `json:"-"`
	// NoCompress is set by `compress: false`. Next.js compresses responses by
	// default; the migrated config precompresses assets at build time instead.
	NoCompress bool `json:"-"`
	// Preamble holds next.config's top-level statements; the ones the code
	// pieces need are copied into krate.config.ts.
	Preamble *preambleSource `json:"-"`
	// ImageCode are images.remotePatterns entries carried as code.
	ImageCode []*codePiece `json:"-"`
}

// nextRedirect is written with an explicit statusCode (308 for Next's
// permanent: true, 307 for false) so the migrated app answers like Next.
type nextRedirect struct {
	Source      string                  `json:"source"`
	Destination string                  `json:"destination"`
	StatusCode  int                     `json:"statusCode,omitempty"`
	Has         []routesource.Condition `json:"has,omitempty"`
	Missing     []routesource.Condition `json:"missing,omitempty"`
}

type nextRewrite struct {
	Source      string                  `json:"source"`
	Destination string                  `json:"destination"`
	Has         []routesource.Condition `json:"has,omitempty"`
	Missing     []routesource.Condition `json:"missing,omitempty"`
}

type nextHeaderRule struct {
	Source  string                  `json:"source"`
	Headers map[string]string       `json:"headers"`
	Has     []routesource.Condition `json:"has,omitempty"`
	Missing []routesource.Condition `json:"missing,omitempty"`
	// Code is set instead of the fields above when the rule is computed: the
	// element is copied into krate.config.ts verbatim.
	Code *codePiece `json:"-"`
}

// middlewareRedirect is a redirect whose source or has/missing regex Go
// cannot run. It is written into the middleware instead of the config.
type middlewareRedirect struct {
	Index       int
	Source      string
	Destination string
	Status      int
	Has         []routesource.Condition
	Missing     []routesource.Condition
}

var nextConfigWrapper = regexp.MustCompile(`(?m)(?:export\s+default|module\.exports\s*=)\s*[A-Za-z_$][\w$]*(?:\.[\w$]+)*\s*\(`)

// nextConfigNotes is everything readNextConfig could not translate.
type nextConfigNotes struct {
	// Warnings are informational: the key is harmless or has no effect.
	Warnings []string
	// Skipped lists whole keys (redirects, rewrites, headers) that were not
	// read at all, each with the reason.
	Skipped []string
	// Manual is one item per rule or key that needs a human.
	Manual []ManualItem
	// Info are notes that need no action.
	Info []string
}

// readNextConfig extracts static redirects/rewrites/headers/basePath from a
// next.config file. Each rule and key is translated on its own: anything it
// cannot prove static is reported in notes and the rest still translates.
// has/missing conditions become config conditions; a redirect whose regex Go
// cannot run is recorded in MiddlewareRedirects. ok is
// false only when the config as a whole is not a static default export object
// (function configs, plugin wrappers, computed values).
func readNextConfig(path string) (data nextConfigData, notes nextConfigNotes, ok bool) {
	var out nextConfigData
	file := filepath.Base(path)
	src, err := os.ReadFile(path)
	if err != nil {
		notes.Warnings = []string{err.Error()}
		return out, notes, false
	}
	if nextConfigWrapper.Match(src) {
		notes.Warnings = append(notes.Warnings, "next.config is wrapped in a function call; plugin wrapper behavior is not migrated")
	}
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	obj := defaultExportObject(prog)
	if obj == nil {
		notes.Warnings = append(notes.Warnings, "config is not a static default export object")
		return out, notes, false
	}
	rc := &ruleCtx{file: file, dir: filepath.Dir(path), src: string(src), prog: prog}
	rc.pre = newPreambleSource(rc.src, prog, obj.Position.Offset)
	out.Preamble = rc.pre

	for _, p := range obj.Properties {
		if p != nil && (p.Spread || p.Computed) {
			notes.Manual = append(notes.Manual, ManualItem{
				Source: file,
				Reason: "next.config spreads or computes keys; the keys it adds were not read",
				Hint:   "copy the keys that matter (redirects, rewrites, headers, images) into krate.config.ts",
			})
			break
		}
	}

	if v := prop(obj, "basePath"); v != nil {
		if s, ok := stringValue(v); ok {
			out.BasePath = s
		} else {
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "basePath is computed and was not translated", Hint: "set basePath in krate.config.ts (see Base path in the config reference)"})
		}
	}
	if v := prop(obj, "trailingSlash"); v != nil {
		if on, ok := boolValue(v); ok {
			if on {
				out.TrailingSlash = "always"
			}
		} else {
			notes.Manual = append(notes.Manual, ManualItem{Source: file, Reason: "trailingSlash is computed and was not translated", Hint: "set trailingSlash in krate.config.ts: \"always\" matches trailingSlash: true, \"never\" matches Next.js' default of false"})
		}
	}
	if v := prop(obj, "compress"); v != nil {
		if on, ok := boolValue(v); ok {
			out.NoCompress = !on
		}
	}
	if v := prop(obj, "redirects"); v != nil {
		rules, items, reason := rc.redirects(v)
		if reason != "" {
			notes.Skipped = append(notes.Skipped, "redirects ("+reason+")")
		}
		out.Redirects = rules
		out.MiddlewareRedirects = rc.fallbacks
		notes.Manual = append(notes.Manual, items...)
	}
	if v := prop(obj, "rewrites"); v != nil {
		rws, items, reason := rc.rewrites(returnedValue(v))
		if reason != "" {
			notes.Skipped = append(notes.Skipped, "rewrites ("+reason+")")
		}
		out.Rewrites = rws
		notes.Manual = append(notes.Manual, items...)
	}
	if v := prop(obj, "headers"); v != nil {
		rules, items, reason := rc.headers(v)
		if reason != "" {
			notes.Skipped = append(notes.Skipped, "headers ("+reason+")")
			rc.headersLost = true
		}
		out.Headers = rules
		notes.Manual = append(notes.Manual, items...)
		notes.Manual = append(notes.Manual, rc.securityItem()...)
	}
	notes.Warnings = append(notes.Warnings, duplicateRuleWarnings(out)...)

	if v := prop(obj, "images"); v != nil {
		images, imageWarnings, imageCode := rc.images(v)
		if len(images) > 0 {
			out.Images = images
		}
		out.ImageCode = imageCode
		for _, w := range imageWarnings {
			if item, manual := imageManualItem(file, w); manual {
				notes.Manual = append(notes.Manual, item)
			} else {
				notes.Warnings = append(notes.Warnings, w)
			}
		}
	}
	if v := prop(obj, "env"); v != nil {
		if env, ok := staticStringMap(v); ok {
			out.Env = env
		} else {
			notes.Manual = append(notes.Manual, ManualItem{
				Source: file,
				Reason: "env is not a static string map and was not translated",
				Hint:   "Krate inlines only values whose names start with KRATE_PUBLIC_ (or a prefix in envPrefix) from .env at build time; put the values there (see Environment Variables)",
			})
		}
	}
	if loc := loadEnvConfigCall.FindStringIndex(rc.src); loc != nil {
		dir, ok := staticEnvDir(rc.src[loc[1]:])
		if ok {
			out.EnvDir = dir
		} else {
			notes.Manual = append(notes.Manual, ManualItem{
				Source: file,
				Reason: "loadEnvConfig(...) is called in next.config with a path that could not be resolved statically; the .env files it loads are not read by Krate",
				Hint:   "Krate loads .env, .env.<mode>, .env.local and .env.<mode>.local from the project root only (select the mode with --mode); set envDir in krate.config.ts to the directory that holds them (for example envDir: \"../..\" for a monorepo root), or export the variables in the shell",
			})
		}
	}
	translateOtherKeys(file, obj, &notes, serverFileKeys(rc, obj, &out, &notes))
	return out, notes, true
}

var loadEnvConfigCall = regexp.MustCompile(`\bloadEnvConfig\s*\(`)

// returnedValue unwraps `async () => value` and `async function () { return
// value }` (what Next's redirects/rewrites/headers usually are) to value. Any
// other expression is returned unchanged.
func returnedValue(v ast.Expr) ast.Expr {
	fn, ok := v.(*ast.ArrowFn)
	if !ok || fn.Generator {
		return v
	}
	body := fn.Body
	if len(body) != 1 {
		return v
	}
	ret, ok := body[0].(*ast.ReturnStmt)
	if !ok || ret.Value == nil {
		return v
	}
	if paren, ok := ret.Value.(*ast.ParenExpr); ok {
		return paren.Expr
	}
	return ret.Value
}

func defaultExportObject(prog *ast.Program) *ast.ObjectExpr {
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ExportStmt:
			if !s.Default {
				continue
			}
			if s.Local != "" {
				if obj := resolveConfigObject(&ast.Identifier{Name: s.Local}, prog, map[string]bool{}); obj != nil {
					return obj
				}
			}
			switch d := s.Declaration.(type) {
			case *ast.ExprStmt:
				if obj := resolveConfigObject(d.Expression, prog, map[string]bool{}); obj != nil {
					return obj
				}
			case *ast.FnDecl:
				if obj := functionReturnObject(d.Body); obj != nil {
					return obj
				}
			}
		case *ast.ExprStmt:
			bin, ok := s.Expression.(*ast.BinaryExpr)
			if !ok || bin.Op != "=" || !isModuleExports(bin.Left) {
				continue
			}
			if obj := resolveConfigObject(bin.Right, prog, map[string]bool{}); obj != nil {
				return obj
			}
		}
	}
	return nil
}

func resolveConfigObject(e ast.Expr, prog *ast.Program, seen map[string]bool) *ast.ObjectExpr {
	switch v := e.(type) {
	case *ast.ObjectExpr:
		return v
	case *ast.ParenExpr:
		return resolveConfigObject(v.Expr, prog, seen)
	case *ast.TypeAssertion:
		return resolveConfigObject(v.Expr, prog, seen)
	case *ast.ArrowFn:
		return functionReturnObject(v.Body)
	case *ast.Identifier:
		if seen[v.Name] {
			return nil
		}
		seen[v.Name] = true
		for _, stmt := range prog.Body {
			vs, ok := stmt.(*ast.VarStmt)
			if !ok {
				continue
			}
			for _, d := range vs.Decls {
				if d.Name == v.Name {
					return resolveConfigObject(d.Init, prog, seen)
				}
			}
		}
	case *ast.CallExpr:
		if len(v.Args) > 0 {
			return resolveConfigObject(v.Args[0], prog, seen)
		}
	}
	return nil
}

func functionReturnObject(body []ast.Stmt) *ast.ObjectExpr {
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			if obj, ok := ret.Value.(*ast.ObjectExpr); ok {
				return obj
			}
			return nil
		}
	}
	return nil
}

func isModuleExports(e ast.Expr) bool {
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		return false
	}
	obj, ok := m.Object.(*ast.Identifier)
	if !ok || obj.Name != "module" {
		return false
	}
	id, ok := m.Property.(*ast.Identifier)
	return ok && id.Name == "exports"
}

func prop(obj *ast.ObjectExpr, key string) ast.Expr {
	if obj == nil {
		return nil
	}
	for _, p := range obj.Properties {
		if p == nil || p.Spread || p.Computed {
			continue
		}
		if p.Key == key {
			return p.Value
		}
	}
	return nil
}

func stringValue(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		return "", false
	}
	return lit.Value, true
}

func intValue(e ast.Expr) (int, bool) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != ast.NumberLit {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscan(lit.Value, &n); err != nil {
		return 0, false
	}
	return n, true
}

func boolValue(e ast.Expr) (bool, bool) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != ast.BoolLit {
		return false, false
	}
	return lit.Value == "true", true
}

// configJSON renders the generated krate.config.ts. JSON object syntax is
// valid JavaScript, which keeps nested redirect/header data unambiguous.
func configJSON(entry string, next nextConfigData) []byte {
	opts := map[string]any{
		"outDir":    "dist",
		"pagesDir":  "src/pages",
		"publicDir": "public",
		"minify":    true,
		"sourcemap": false,
		"devServer": map[string]any{"port": 3000, "open": false},
	}
	if entry != "" {
		opts["entry"] = entry
	}
	if len(next.Head) > 0 {
		opts["head"] = next.Head
	}
	if v := trailingSlashValue(next); v != "" {
		opts["trailingSlash"] = v
	}
	if next.EnvDir != "" {
		opts["envDir"] = next.EnvDir
	}
	if len(next.EnvPrefix) > 0 {
		opts["envPrefix"] = next.EnvPrefix
	}
	if next.Tailwind {
		opts["tailwind"] = map[string]any{"enabled": true}
	}
	if v := compressValue(next); v != nil {
		opts["compress"] = v
	}
	if next.BasePath != "" {
		opts["basePath"] = next.BasePath
	}
	if len(next.Redirects) > 0 {
		opts["redirects"] = next.Redirects
	}
	if len(next.Rewrites) > 0 {
		opts["rewrites"] = next.Rewrites
	}
	if len(next.Headers) > 0 {
		opts["headers"] = next.headersValue()
	}
	if v := next.imagesValue(); v != nil {
		opts["images"] = v
	}
	if len(next.Site) > 0 {
		opts["site"] = next.Site
	}
	if len(next.ServerExternalPackages) > 0 {
		opts["serverExternalPackages"] = next.ServerExternalPackages
	}
	if len(next.OutputFileTracingIncludes) > 0 {
		opts["outputFileTracingIncludes"] = next.OutputFileTracingIncludes
	}
	if next.OutputFileTracingRoot != "" {
		opts["outputFileTracingRoot"] = next.OutputFileTracingRoot
	}
	if next.DeployTarget != "" {
		opts["deploy"] = map[string]any{"target": next.DeployTarget}
	}
	if next.MiddlewareRuntime != "" {
		opts["ssr"] = map[string]any{"middlewareRuntime": next.MiddlewareRuntime}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(opts)
	body := strings.TrimRight(buf.String(), "\n")
	pieces := next.codePieces()
	if len(pieces) == 0 {
		return []byte("import { defineConfig } from '@krate/core';\n\nexport default defineConfig(" + body + ");\n")
	}
	body = expandCodeMarkers(body, pieces)
	head := "import { defineConfig } from '@krate/core';\n\n"
	if pre := next.Preamble.build(activeTexts(pieces)); pre != "" {
		head = "import { defineConfig } from '@krate/core';\n" + pre + "\n\n"
	}
	return []byte(head + "export default defineConfig(" + body + ");\n")
}

func staticStringMap(e ast.Expr) (map[string]string, bool) {
	obj, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil, false
	}
	out := map[string]string{}
	for _, p := range obj.Properties {
		if p == nil || p.Spread || p.Computed {
			return nil, false
		}
		s, ok := stringValue(p.Value)
		if !ok {
			return nil, false
		}
		out[p.Key] = s
	}
	return out, true
}

// trailingSlashValue is the trailingSlash setting to write: Next serves
// /store (no slash) unless next.config sets trailingSlash: true.
func trailingSlashValue(next nextConfigData) string {
	if next.TrailingSlash == "always" {
		return "always"
	}
	return "never"
}

var postcssTailwind = regexp.MustCompile(`@tailwindcss/postcss|\btailwindcss\b`)

// usesTailwindPostCSS reports whether a root postcss.config file runs Tailwind.
func usesTailwindPostCSS(root string) bool {
	for _, name := range []string{"postcss.config.mjs", "postcss.config.js", "postcss.config.cjs", "postcss.config.ts", "postcss.config.mts", "postcss.config.cts"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err == nil && postcssTailwind.Match(data) {
			return true
		}
	}
	return false
}

// compressValue is the compress setting to write. Next.js compresses
// responses unless next.config sets compress: false; Krate's equivalent is
// build-time precompression (brotli and best-ratio gzip siblings that the
// server sends), so assets are not left to on-the-fly gzip.
func compressValue(next nextConfigData) map[string]any {
	if next.NoCompress {
		return nil
	}
	return map[string]any{"brotli": true, "gzip": true}
}
