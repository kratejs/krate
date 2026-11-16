package tsstrip

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestRendererDecoratorForms(t *testing.T) {
	tests := []struct {
		name, src, decl string
		fileTier        string
	}{
		{"function", "@renderer\nexport function Canvas() { return <div/>; }\n", "Canvas", ""},
		{"const arrow", "@renderer\nexport const Canvas = () => <div/>;\n", "Canvas", ""},
		{"default export", "@renderer\nexport default function Canvas() { return <div/>; }\n", "Canvas", ""},
		{"anonymous default", "@renderer export default function () { return <div/>; }\n", "", ""},
		{"with client", "@renderer\n@client\nexport default function Canvas() { return <div/>; }\n", "Canvas", "client"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := parseOK(t, "a.tsx", tt.src)
			if prog.Tier != tt.fileTier {
				t.Errorf("file tier = %q, want %q", prog.Tier, tt.fileTier)
			}
			if !declRenderer(prog, tt.decl) {
				t.Errorf("%q is not marked as a renderer", tt.decl)
			}
			if got := BlankTierDecorators("a.tsx", tt.src); strings.Contains(got, "@renderer") {
				t.Errorf("decorator not blanked: %q", got)
			}
		})
	}
}

func TestRendererDecoratorErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"async", "@renderer\nexport async function Canvas() { return <div/>; }\n", "async"},
		{"class", "@renderer\nexport class Canvas {}\n", "not a class"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse("a.tsx", tt.src, Options{})
			if len(errs) == 0 || !strings.Contains(errs[0].Error(), tt.want) {
				t.Fatalf("want an error mentioning %q, got %v", tt.want, errs)
			}
			if d, ok := errs[0].(diag.Diagnostic); !ok || d.Code != diag.CodeRendererTier {
				t.Errorf("want KR7117, got %+v", errs[0])
			}
		})
	}
}

// A project's own `renderer` class decorator is left alone.
func TestRendererDecoratorUserDecoratorWins(t *testing.T) {
	src := "import { renderer } from './deco';\n@renderer\nexport class Canvas {}\n"
	if got := BlankTierDecorators("a.tsx", src); got != src {
		t.Errorf("user decorator was blanked: %q", got)
	}
}

func declRenderer(prog *ast.Program, name string) bool {
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name == name && s.Renderer {
				return true
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil && d.Name == name && d.Renderer {
					return true
				}
			}
		}
	}
	return false
}
