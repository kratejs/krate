package migrate

import (
	"encoding/json"
	"fmt"
	"os"
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

// readNextConfig extracts static redirects/rewrites/headers/basePath from a
// next.config file. It returns ok=false for function configs, plugin wrappers,
// and anything it cannot prove static.
func readNextConfig(path string) (nextConfigData, []string, bool) {
	var out nextConfigData
	src, err := os.ReadFile(path)
	if err != nil {
		return out, []string{err.Error()}, false
	}
	var wrapperWarnings []string
	if nextConfigWrapper.Match(src) {
		wrapperWarnings = append(wrapperWarnings, "next.config is wrapped in a function call; plugin wrapper behavior is not migrated")
	}
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	obj := defaultExportObject(prog)
	if obj == nil {
		return out, []string{"config is not a static default export object"}, false
	}

	if v := prop(obj, "basePath"); v != nil {
		if s, ok := stringValue(v); ok {
			out.BasePath = s
		}
	}
	if v := prop(obj, "redirects"); v != nil {
		arr, ok := v.(*ast.ArrayExpr)
		if !ok {
			return out, []string{"redirects is not a static array"}, false
		}
		for i, el := range arr.Elements {
			item, ok := el.(*ast.ObjectExpr)
			if !ok {
				return out, []string{fmt.Sprintf("redirects[%d] is not an object", i)}, false
			}
			source, ok1 := stringValue(prop(item, "source"))
			dest, ok2 := stringValue(prop(item, "destination"))
			if !ok1 || !ok2 {
				return out, []string{fmt.Sprintf("redirects[%d] has a non-static source or destination", i)}, false
			}
			perm := false
			if p := prop(item, "permanent"); p != nil {
				if b, ok := boolValue(p); ok {
					perm = b
				}
			}
			if sc := prop(item, "statusCode"); sc != nil {
				if n, ok := intValue(sc); ok {
					perm = n == 301 || n == 308
				}
			}
			out.Redirects = append(out.Redirects, nextRedirect{Source: source, Destination: dest, Permanent: perm})
		}
	}
	if v := prop(obj, "rewrites"); v != nil {
		rws, ok := rewritesValue(v)
		if !ok {
			return out, []string{"rewrites is not a static array or beforeFiles/afterFiles/fallback object"}, false
		}
		out.Rewrites = rws
	}
	if v := prop(obj, "headers"); v != nil {
		arr, ok := v.(*ast.ArrayExpr)
		if !ok {
			return out, []string{"headers is not a static array"}, false
		}
		for i, el := range arr.Elements {
			item, ok := el.(*ast.ObjectExpr)
			if !ok {
				return out, []string{fmt.Sprintf("headers[%d] is not an object", i)}, false
			}
			source, ok := stringValue(prop(item, "source"))
			if !ok {
				return out, []string{fmt.Sprintf("headers[%d] has a non-static source", i)}, false
			}
			harr, ok := prop(item, "headers").(*ast.ArrayExpr)
			if !ok {
				return out, []string{fmt.Sprintf("headers[%d].headers is not a static array", i)}, false
			}
			rule := nextHeaderRule{Source: source, Headers: map[string]string{}}
			for j, hel := range harr.Elements {
				hm, ok := hel.(*ast.ObjectExpr)
				if !ok {
					return out, []string{fmt.Sprintf("headers[%d].headers[%d] is not an object", i, j)}, false
				}
				k, ok1 := stringValue(prop(hm, "key"))
				val, ok2 := stringValue(prop(hm, "value"))
				if !ok1 || !ok2 {
					return out, []string{fmt.Sprintf("headers[%d].headers[%d] has a non-static key or value", i, j)}, false
				}
				rule.Headers[k] = val
			}
			out.Headers = append(out.Headers, rule)
		}
	}
	var warnings []string
	if v := prop(obj, "images"); v != nil {
		images, imageWarnings := imagesConfig(v)
		if len(images) > 0 {
			out.Images = images
		}
		warnings = append(warnings, imageWarnings...)
	}
	if v := prop(obj, "env"); v != nil {
		if env, ok := staticStringMap(v); ok {
			out.Env = env
		} else {
			warnings = append(warnings, "next.config env is not a static string map")
		}
	}
	if prop(obj, "experimental") != nil {
		warnings = append(warnings, "next.config experimental keys are not translated")
	}
	return out, append(wrapperWarnings, warnings...), true
}

func rewritesValue(v ast.Expr) ([]nextRewrite, bool) {
	if arr, ok := v.(*ast.ArrayExpr); ok {
		return rewriteArray(arr)
	}
	obj, ok := v.(*ast.ObjectExpr)
	if !ok {
		return nil, false
	}
	var out []nextRewrite
	for _, key := range []string{"beforeFiles", "afterFiles", "fallback"} {
		v := prop(obj, key)
		if v == nil {
			continue
		}
		arr, ok := v.(*ast.ArrayExpr)
		if !ok {
			return nil, false
		}
		rws, ok := rewriteArray(arr)
		if !ok {
			return nil, false
		}
		out = append(out, rws...)
	}
	return out, true
}

func rewriteArray(arr *ast.ArrayExpr) ([]nextRewrite, bool) {
	var out []nextRewrite
	for _, el := range arr.Elements {
		item, ok := el.(*ast.ObjectExpr)
		if !ok {
			return nil, false
		}
		source, ok1 := stringValue(prop(item, "source"))
		dest, ok2 := stringValue(prop(item, "destination"))
		if !ok1 || !ok2 {
			return nil, false
		}
		out = append(out, nextRewrite{Source: source, Destination: dest})
	}
	return out, true
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
		"entry":     entry,
		"outDir":    "dist",
		"pagesDir":  "src/pages",
		"publicDir": "public",
		"minify":    true,
		"sourcemap": false,
		"devServer": map[string]any{"port": 3000, "open": false},
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
