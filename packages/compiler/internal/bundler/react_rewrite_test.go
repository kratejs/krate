package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

// rewriteSrc parses src, runs RewriteReact, and returns the printed program.
func rewriteSrc(t *testing.T, src string) string {
	t.Helper()
	prog := parseProg(t, src)
	RewriteReact(prog)
	return astprint.Print(prog)
}

// useReducer

func TestRewriteReactUseReducer(t *testing.T) {
	out := rewriteSrc(t, `
		import { useReducer } from 'react';
		export default function Page() {
			const [count, dispatch] = useReducer((s, a) => s + a, 0);
			return <button onClick={() => dispatch(1)}>{count}</button>;
		}
	`)
	if !strings.Contains(out, "createReducer") {
		t.Errorf("useReducer should map to createReducer:\n%s", out)
	}
	if !strings.Contains(out, "count()") {
		t.Errorf("reducer getter should be auto-called:\n%s", out)
	}
	if strings.Contains(out, "dispatch()") {
		t.Errorf("dispatch must not be auto-called:\n%s", out)
	}
}

func TestRewriteReactUseTransitionLowered(t *testing.T) {
	out := rewriteSrc(t, `
		import { useTransition } from 'react';
		export default function Page() {
			const [pending, startTransition] = useTransition();
			return <button onClick={() => startTransition(() => {})}>{pending() ? 'x' : 'y'}</button>;
		}
	`)
	if strings.Contains(out, "useTransition(") {
		t.Errorf("useTransition call should be lowered:\n%s", out)
	}
	if !strings.Contains(out, "createSignal(false)") {
		t.Errorf("expected a pending signal:\n%s", out)
	}
	if !strings.Contains(out, "startTransition") {
		t.Errorf("expected the starter binding:\n%s", out)
	}
}
