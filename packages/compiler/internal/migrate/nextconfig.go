package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
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
}

type nextRedirect struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Permanent   bool   `json:"permanent"`
}

type nextRewrite struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type nextHeaderRule struct {
	Source  string            `json:"source"`
	Headers map[string]string `json:"headers"`
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
}

// readNextConfig extracts static redirects/rewrites/headers/basePath from a
// next.config file. Each rule and key is translated on its own: anything it
// cannot prove static is reported in notes and the rest still translates. A
// rule guarded by has/missing is never emitted, because Krate matches on the
// path only and an unconditional copy would be looser than the original. ok is
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
	if v := prop(obj, "redirects"); v != nil {
		rules, items, reason := rc.redirects(v)
		if reason != "" {
			notes.Skipped = append(notes.Skipped, "redirects ("+reason+")")
		}
		out.Redirects = rules
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
		images, imageWarnings := imagesConfig(v)
		if len(images) > 0 {
			out.Images = images
		}
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
				Hint:   "Krate inlines only KRATE_PUBLIC_* values from .env at build time; put the values there (see Environment Variables)",
			})
		}
	}
	if loadEnvConfigCall.MatchString(rc.src) {
		notes.Manual = append(notes.Manual, ManualItem{
			Source: file,
			Reason: "loadEnvConfig(...) is called in next.config; the .env files it loads are not read by Krate",
			Hint:   "Krate loads .env, .env.<mode>, .env.local and .env.<mode>.local from the project root only (select the mode with --mode); copy or symlink the monorepo root .env into the project, or export the variables in the shell",
		})
	}
	translateOtherKeys(file, obj, &notes)
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
		opts["headers"] = next.Headers
	}
	if len(next.Images) > 0 {
		opts["images"] = next.Images
	}
	if len(next.Site) > 0 {
		opts["site"] = next.Site
	}
	body, _ := json.MarshalIndent(opts, "", "  ")
	return []byte("import { defineConfig } from '@krate/core';\n\nexport default defineConfig(" + string(body) + ");\n")
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
