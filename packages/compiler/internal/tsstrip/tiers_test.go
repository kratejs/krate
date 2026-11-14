package tsstrip

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func parseOK(t *testing.T, path, src string) *ast.Program {
	t.Helper()
	prog, errs := Parse(path, src, Options{})
	if len(errs) > 0 {
		t.Fatalf("Parse: %v", errs)
	}
	return prog
}

func TestTierDecoratorForms(t *testing.T) {
	tests := []struct {
		name, src string
		fileTier  string
	}{
		{"same line", "@static export default function P() { return <p/>; }\n", "static"},
		{"anonymous default", "@static\nexport default function () { return <p/>; }\n", "static"},
		{"default by name", "@server\nfunction P() { return <p/>; }\nexport default P;\n", "server"},
		{"sole component export", "@runtime\nexport const Live = () => <p/>;\nexport const config = {};\n", "runtime"},
		{"not exported", "@server\nfunction Helper() { return <p/>; }\nexport default function P() { return <Helper/>; }\n", ""},
		{"named next to a default", "@server\nexport function A() { return <p/>; }\nexport default function P() { return <A/>; }\n", ""},
		{"comment between", "@client // interactive\n/** Docs. */\nexport default function P() { return <p/>; }\n", "client"},
		{"other decorator between", "@server\n@log({ a: 1 })\nexport default class Box {}\nfunction log(o: any) { return (c: any) => c; }\n", "server"},
		{"crlf", "@server\r\nexport default function P() { return <p/>; }\r\n", "server"},
		{"async runtime", "@runtime\nexport default async function P() { return <p/>; }\n", "runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if prog := parseOK(t, "a.tsx", tt.src); prog.Tier != tt.fileTier {
				t.Errorf("file tier = %q, want %q", prog.Tier, tt.fileTier)
			}
			if got := ModuleTier("a.tsx", tt.src); got != tt.fileTier {
				t.Errorf("ModuleTier = %q, want %q", got, tt.fileTier)
			}
		})
	}
}
