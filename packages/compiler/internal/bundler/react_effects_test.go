package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// rewriteDiag runs RewriteReactDiag over src and returns the printed program
// and its diagnostics.
func rewriteDiag(t *testing.T, src string) (string, []diag.Diagnostic) {
	t.Helper()
	prog := parseProg(t, src)
	diags := RewriteReactDiag(prog, "page.tsx", src)
	return astprint.Print(prog), diags
}

// mustContain checks fragments with whitespace runs collapsed, so the
// assertions do not depend on the printer's indentation.
func mustContain(t *testing.T, out string, frags ...string) {
	t.Helper()
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	flat := norm(out)
	for _, f := range frags {
		if !strings.Contains(flat, norm(f)) {
			t.Fatalf("missing %q in:\n%s", norm(f), out)
		}
	}
}

func TestEffectPhasesLowerToCreateEffectDeps(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useEffect, useLayoutEffect, useInsertionEffect, useState } from 'react';
export default function Page() {
  const [n, setN] = useState(0);
  useEffect(() => { log(n); }, [n]);
  useLayoutEffect(() => { log(1); }, []);
  useInsertionEffect(() => { log(2); }, [n]);
  return <p>{n}</p>;
}`)
	mustContain(t, out,
		"createEffectDeps(() => {\n    log(n());\n  }, () => {\n    return [n()];\n  });",
		"createEffectDeps(() => {\n    log(1);\n  }, () => {\n    return [];\n  }, \"layout\");",
		"createEffectDeps(() => {\n    log(2);\n  }, () => {\n    return [n()];\n  }, \"insertion\");")
	if strings.Contains(out, "onMount") || strings.Contains(out, "createEffect(") {
		t.Fatalf("effects must not lower to onMount or auto-tracked createEffect:\n%s", out)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
