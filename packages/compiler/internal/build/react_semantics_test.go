package build

import (
	"strings"
	"testing"
)

// TestReactWrapperKeepsSignalBindings guards the SSR-wrapper bug: a signal-less
// passthrough wrapper must not flatten a stateful child into static HTML.
func TestReactWrapperKeepsSignalBindings(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function Panel({ children }: any) { return <div>{children}</div>; }
		function Counter() {
			const [n, setN] = createSignal(0);
			return <div><button onClick={() => setN(n() + 1)}>inc</button><span>{n()}</span></div>;
		}
		export default function Page() { return <Panel><Counter /></Panel>; }
	`
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "<!--k:") {
		t.Fatalf("expected hydration marker in wrapped child:\n%s", html)
	}
	if !strings.Contains(js, "kbindText") || !strings.Contains(js, "kbindHandler") {
		t.Fatalf("expected child bindings in hydration:\n%s", js)
	}
}

// TestReactFunctionPropChildHandler guards function props reaching a child with
// its own signals: the child must invoke the registry function, not an
// inlined parent local.
func TestReactFunctionPropChildHandler(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		function Field({ value, onChange }: any) {
			const [focused, setFocused] = createSignal(false);
			return <input value={value} onChange={onChange} onFocus={() => setFocused(true)} />;
		}
		export default function Page() {
			const [name, setName] = createSignal('Ada');
			return <Field value={name()} onChange={(event: any) => setName(event.target.value)} />;
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "props.onChange") {
		t.Fatalf("expected the child handler to use props.onChange:\n%s", js)
	}
	if !strings.Contains(js, "onChange:(event)=>setName(") {
		t.Fatalf("expected the handler in the parent props registry:\n%s", js)
	}
}

// TestReactDynamicContextStaysLive verifies a provider value getter is passed
// to a consumer component and read through the props registry instead of
// folding to the context default.
func TestReactDynamicContextStaysLive(t *testing.T) {
	page := `
		import { createSignal, createContext, useContext } from '@krate/runtime';
		const Ctx = createContext('indigo');
		function Consumer() {
			const value = useContext(Ctx);
			return <span>{value}</span>;
		}
		export default function Page() {
			const [v, setV] = createSignal('indigo');
			return (
				<div>
					<Ctx.Provider value={v()}><Consumer /></Ctx.Provider>
					<button onClick={() => setV('emerald')}>toggle</button>
				</div>
			);
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "props.__krate_ctx_Ctx") {
		t.Fatalf("consumer should read the live context prop:\n%s", js)
	}
	if !strings.Contains(js, "__krate_ctx_Ctx:()=>") {
		t.Fatalf("provider should register a value getter:\n%s", js)
	}
	if strings.Contains(js, "var value='indigo'") || strings.Contains(js, "var value=\"indigo\"") {
		t.Fatalf("context value was folded to its default:\n%s", js)
	}
}
