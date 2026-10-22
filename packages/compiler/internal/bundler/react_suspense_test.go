package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// rewriteSrcDiags parses src, runs the React lowering, and returns the printed
// program and its diagnostics.
func rewriteSrcDiags(t *testing.T, src string) (string, []diag.Diagnostic) {
	t.Helper()
	prog := parseProg(t, src)
	diags := RewriteReactDiagnostics(prog)
	return astprint.Print(prog), diags
}

func codes(diags []diag.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		out = append(out, string(d.Code))
	}
	return out
}

func TestLazyLowersToImportAndMarker(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { lazy, Suspense } from 'react';
		const Chart = lazy(() => import('./Chart'));
		export default function Page() {
			return <Suspense fallback={<p>loading</p>}><Chart /></Suspense>;
		}
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !strings.Contains(out, `import Chart from "./Chart"`) {
		t.Errorf("expected a static default import:\n%s", out)
	}
	if !strings.Contains(out, `__krate_lazy__Chart = "./Chart"`) {
		t.Errorf("expected the lazy marker:\n%s", out)
	}
	if strings.Contains(out, "lazy(") || strings.Contains(out, "import(") {
		t.Errorf("lazy() call should be gone:\n%s", out)
	}
}

func TestLazyNamedExportMapping(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import React from 'react';
		export const Panel = React.lazy(() => import('./panels').then((m) => ({ default: m.SidePanel })));
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !strings.Contains(out, `import { SidePanel as Panel } from "./panels"`) {
		t.Errorf("expected a named import:\n%s", out)
	}
	if !strings.Contains(out, "export { Panel }") {
		t.Errorf("expected the binding to stay exported:\n%s", out)
	}
}

func TestLazyDiagnostics(t *testing.T) {
	_, diags := rewriteSrcDiags(t, `
		import { lazy } from 'react';
		const name = './X';
		const A = lazy(() => import(name));
		export default function Page() {
			const B = lazy(() => import('./B'));
			return <div />;
		}
	`)
	got := strings.Join(codes(diags), ",")
	if got != "KR4001,KR4002" {
		t.Fatalf("codes = %s, want KR4001,KR4002 (%v)", got, diags)
	}
	for _, d := range diags {
		if d.Hint == "" || d.Line == 0 {
			t.Errorf("diagnostic needs a hint and a position: %+v", d)
		}
	}
}

func TestLazyPackageWarns(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { lazy } from 'react';
		const Picker = lazy(() => import('some-picker'));
	`)
	if strings.Join(codes(diags), ",") != "KR4006" || !diags[0].IsWarning() {
		t.Fatalf("expected a KR4006 warning, got %v", diags)
	}
	if strings.Contains(out, "__krate_lazy__") {
		t.Errorf("package imports are not split:\n%s", out)
	}
}

func TestUsePromiseLowersToGetter(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { use } from 'react';
		function User(props) {
			const user = use(props.userPromise);
			return <p>{user.name}</p>;
		}
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !strings.Contains(out, "__krateUse(() => __krateRead(props.userPromise))") {
		t.Errorf("expected the __krateUse lowering:\n%s", out)
	}
	if !strings.Contains(out, "user().name") {
		t.Errorf("expected reads of the use() value to become getter calls:\n%s", out)
	}
}

func TestUseConditionalInsideInitializer(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { use, useState } from 'react';
		const a = Promise.resolve(1);
		function C() {
			const [on] = useState(true);
			const v = on ? use(a) : 0;
			return <p>{v}</p>;
		}
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !strings.Contains(out, "__krateUse(() => on() ? __krateRead(a) : 0)") && !strings.Contains(out, "__krateUse(() => (on() ? __krateRead(a) : 0))") {
		t.Errorf("expected the conditional use to stay inside the computation:\n%s", out)
	}
}

func TestUseContextStaysContextRead(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import { use, createContext } from 'react';
		const Theme = createContext('light');
		function C() {
			const theme = use(Theme);
			return <p>{use(Theme)}{theme}</p>;
		}
	`)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if strings.Contains(out, "__krateUse") {
		t.Errorf("context reads must not suspend:\n%s", out)
	}
	if strings.Count(out, "Theme.useContext()") != 2 {
		t.Errorf("expected both use(Theme) calls to become Theme.useContext():\n%s", out)
	}
}

func TestUseDiagnostics(t *testing.T) {
	_, diags := rewriteSrcDiags(t, `
		import { use } from 'react';
		function C(props) {
			const { name } = use(props.p);
			return <p>{use(props.q)}</p>;
		}
	`)
	got := strings.Join(codes(diags), ",")
	if got != "KR4004,KR4003" {
		t.Fatalf("codes = %s, want KR4004,KR4003 (%v)", got, diags)
	}
}

func TestTransitionStarterLowering(t *testing.T) {
	out := rewriteSrc(t, `
		import { useTransition } from 'react';
		function C() {
			const [isPending, startTransition] = useTransition();
			return <button onClick={() => startTransition(async () => {})}>{isPending ? 'busy' : 'idle'}</button>;
		}
	`)
	if !strings.Contains(out, "__krateTransitionStarter((v) => __krate_transition_isPending(v))") {
		t.Errorf("expected the async-aware starter:\n%s", out)
	}
	if !strings.Contains(out, "isPending()") {
		t.Errorf("expected isPending to be a getter:\n%s", out)
	}
}

func TestDeferredValueAndExternalStoreLowering(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState, useDeferredValue, useSyncExternalStore } from 'react';
		function C() {
			const [q] = useState('');
			const deferred = useDeferredValue(q, 'init');
			const online = useSyncExternalStore(subscribe, () => navigator.onLine, () => true);
			return <p>{deferred}{online ? 'on' : 'off'}</p>;
		}
	`)
	if !strings.Contains(out, "createDeferredValue(() => q(), \"init\")") && !strings.Contains(out, "createDeferredValue(() => q(), 'init')") {
		t.Errorf("expected createDeferredValue with a thunk and the initial value:\n%s", out)
	}
	if !strings.Contains(out, "createExternalStore(subscribe") {
		t.Errorf("expected createExternalStore:\n%s", out)
	}
	if !strings.Contains(out, "deferred()") || !strings.Contains(out, "online()") {
		t.Errorf("expected getter reads:\n%s", out)
	}
}

func TestSuspenseStrictModeProfilerJSX(t *testing.T) {
	out, diags := rewriteSrcDiags(t, `
		import * as React from 'react';
		import { Suspense as Boundary } from 'react';
		export default function Page() {
			return (
				<React.StrictMode>
					<React.Profiler id="p" onRender={() => {}}>
						<Boundary fallback={<i>wait</i>}><b>x</b></Boundary>
						<React.Suspense fallback={<i>w</i>}><b>y</b></React.Suspense>
					</React.Profiler>
				</React.StrictMode>
			);
		}
	`)
	if strings.Join(codes(diags), ",") != "KR4005" {
		t.Fatalf("expected a KR4005 warning, got %v", diags)
	}
	if strings.Contains(out, "StrictMode") || strings.Contains(out, "Profiler") || strings.Contains(out, "Boundary") || strings.Contains(out, "React.Suspense") {
		t.Errorf("expected StrictMode/Profiler fragments and <Suspense> tags:\n%s", out)
	}
	if strings.Count(out, "<Suspense") != 2 {
		t.Errorf("expected two built-in Suspense boundaries:\n%s", out)
	}
}

func TestDiagnosticCodesRegistered(t *testing.T) {
	for _, code := range []diag.Code{
		diag.CodeLazyLoader, diag.CodeLazyPlacement, diag.CodeUsePlacement, diag.CodeUseDestructure,
		diag.CodeProfilerOnRender, diag.CodeLazyPackage, diag.CodeLazyDefaultExport, diag.CodeSuspendNoBoundary,
	} {
		if _, ok := diag.Lookup(code); !ok {
			t.Errorf("%s is not registered", code)
		}
	}
}
