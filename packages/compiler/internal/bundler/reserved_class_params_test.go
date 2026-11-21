package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// A class method parameter named like a runtime-reserved name must be renamed
// together with its references; the walk used to rename only the references,
// leaving the parameter undeclared.
func TestReservedNameClassMethodParams(t *testing.T) {
	prog := parseProg(t, `
		export class Store {
			gpu(h) { return h.gpu; }
			static make(h = 1) { return h; }
		}
		export const K = class { set(h) { this.h = h; } };
	`)
	renameReservedBindings(prog)
	out := astprint.Print(prog)
	for _, want := range []string{"gpu(h$u)", "return h$u.gpu", "make(h$u = 1)", "set(h$u)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "gpu(h)") || strings.Contains(out, "set(h)") {
		t.Errorf("a parameter kept its reserved name:\n%s", out)
	}
}
