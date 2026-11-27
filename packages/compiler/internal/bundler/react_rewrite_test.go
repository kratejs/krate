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

// A Krate-native provider carries signal getters as getters, like a native
// hook's return value.
func TestRewriteNativeProviderKeepsGetters(t *testing.T) {
	out := rewriteSrc(t, `
		import { createContext, createSignal } from '@krate/runtime';
		const Ctx = createContext({ count: () => 0, setCount: (n) => {} });
		export default function Page() {
			const [count, setCount] = createSignal(0);
			return <Ctx.Provider value={{ count, setCount, nested: { read: count } }}><b>{count()}</b></Ctx.Provider>;
		}
	`)
	if strings.Contains(out, "count: count()") || strings.Contains(out, "read: count()") {
		t.Errorf("a native provider value must keep the getters bare:\n%s", out)
	}
	if !strings.Contains(out, "<b>{count()}</b>") {
		t.Errorf("reads outside the provider value stay calls:\n%s", out)
	}
}

// React source reads a state value, so its provider value is auto-called.
func TestRewriteReactProviderValueStillAutoCalled(t *testing.T) {
	out := rewriteSrc(t, `
		import { createContext, useState } from 'react';
		const Ctx = createContext({ count: 0 });
		export default function Page() {
			const [count, setCount] = useState(0);
			return <Ctx.Provider value={{ count, setCount }}><b>{count}</b></Ctx.Provider>;
		}
	`)
	if !strings.Contains(out, "count: count()") {
		t.Errorf("a React provider value reads the state:\n%s", out)
	}
}

// A getter is the reactive source of a resource as it stands.
func TestRewriteCreateResourceKeepsGetterSource(t *testing.T) {
	out := rewriteSrc(t, `
		import { createSignal, createResource } from '@krate/runtime';
		export default function Page() {
			const [id, setId] = createSignal('a');
			const [user] = createResource(id, (key) => Promise.resolve(key));
			const [other] = createResource(() => id(), (key) => Promise.resolve(key));
			return <p>{user()}{other()}</p>;
		}
	`)
	if !strings.Contains(out, "createResource(id,") {
		t.Errorf("a bare getter source must stay a getter:\n%s", out)
	}
	if strings.Contains(out, "createResource(id(),") {
		t.Errorf("the source was called once and frozen:\n%s", out)
	}
}
