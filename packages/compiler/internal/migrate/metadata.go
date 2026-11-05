package migrate

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// staticMetadata returns the object literal of `export const metadata = {...}`.
func staticMetadata(src []byte) (*ast.ObjectExpr, bool) {
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	if prog == nil {
		return nil, false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "metadata" {
				continue
			}
			obj, ok := d.Init.(*ast.ObjectExpr)
			return obj, ok
		}
	}
	return nil, false
}

// nextMetadataTypeImport matches `import type { Metadata, ... } from "next"`
// and the value-less form `import { type Metadata } from "next"`.
var nextMetadataTypeImport = regexp.MustCompile(`(?m)^([ \t]*import[ \t]+(?:type[ \t]+)?\{[^}]*\}[ \t]*from[ \t]+)(["'])next["']`)

// nextMetadataTypeNames are the type names the runtime exports for the Metadata API.
var nextMetadataTypeNames = map[string]bool{
	"Metadata": true, "ResolvingMetadata": true, "MetadataRoute": true, "ResolvingViewport": true, "Viewport": true,
}

// rewriteMetadataTypes points the `Metadata` and `ResolvingMetadata` type
// imports of `next` at @krate/runtime/server, where the Metadata API now
// lives. metadata and generateMetadata exports themselves need no rewrite:
// pages and layouts run them natively. Imports of anything else from "next"
// are left alone and reported by the generic next.* checks.
func rewriteMetadataTypes(rep *Report) {
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind != KindPage && f.Kind != KindLayout && f.Kind != KindAsset && f.Kind != KindMetadataRoute && f.Kind != KindNotFound && f.Kind != KindForbidden && f.Kind != KindUnauthorized && f.Kind != KindError {
			continue
		}
		src := string(f.Content)
		if !strings.Contains(src, `"next"`) && !strings.Contains(src, `'next'`) {
			continue
		}
		changed := false
		out := nextMetadataTypeImport.ReplaceAllStringFunc(src, func(stmt string) string {
			m := nextMetadataTypeImport.FindStringSubmatch(stmt)
			open := strings.Index(stmt, "{")
			end := strings.Index(stmt, "}")
			if open < 0 || end < open {
				return stmt
			}
			for _, spec := range strings.Split(stmt[open+1:end], ",") {
				name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(spec), "type "))
				if i := strings.Index(name, " as "); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
				if name != "" && !nextMetadataTypeNames[name] {
					return stmt
				}
			}
			changed = true
			return m[1] + m[2] + "@krate/runtime/server" + m[2]
		})
		if changed {
			f.Content = []byte(out)
			f.Rewrites = append(f.Rewrites, `rewrote the Metadata type imports from "next" to @krate/runtime/server`)
		}
	}
}

func firstImageURL(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Literal:
		s, _ := stringValue(v)
		return s
	case *ast.ArrayExpr:
		if len(v.Elements) > 0 {
			return firstImageURL(v.Elements[0])
		}
	case *ast.ObjectExpr:
		s, _ := stringValue(prop(v, "url"))
		return s
	}
	return ""
}
