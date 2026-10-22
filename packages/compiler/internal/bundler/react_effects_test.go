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

func findDiag(t *testing.T, diags []diag.Diagnostic, code diag.Code) diag.Diagnostic {
	t.Helper()
	for _, d := range diags {
		if d.Code == code {
			return d
		}
	}
	t.Fatalf("no %s diagnostic in %v", code, diags)
	return diag.Diagnostic{}
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

func TestEffectDepsConstArrayIsInlined(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useEffect, useState } from 'react';
export default function Page() {
  const [a, setA] = useState(0);
  const [b, setB] = useState(0);
  const deps = [a, b];
  useEffect(() => { log(a); }, deps);
  return <p>{a}</p>;
}`)
	mustContain(t, out, "}, () => {\n    return [a(), b()];\n  });")
	if len(diags) != 0 {
		t.Fatalf("a local const array needs no diagnostic: %v", diags)
	}
}

func TestEffectDepsUnresolvedIdentifierWarns(t *testing.T) {
	src := `import { useEffect } from 'react';
function useLogger(value, deps) {
  useEffect(() => { log(value); }, deps);
}
export default function Page() {
  useLogger(1, [1]);
  return <p>x</p>;
}`
	out, diags := rewriteDiag(t, src)
	mustContain(t, out, "}, () => {\n    return deps;\n  });")
	d := findDiag(t, diags, diag.CodeDepsUnresolved)
	if d.Line != 3 || d.Col != 36 || d.File != "page.tsx" || !d.IsWarning() {
		t.Fatalf("diagnostic position/severity = %s:%d:%d warning=%v", d.File, d.Line, d.Col, d.IsWarning())
	}
	if !strings.Contains(d.Hint, "useEffect(fn, [a, b])") || d.Source != "  useEffect(() => { log(value); }, deps);" {
		t.Fatalf("diagnostic hint/source = %q / %q", d.Hint, d.Source)
	}
}

func TestEffectDepsShadowedConstIsNotInlined(t *testing.T) {
	_, diags := rewriteDiag(t, `import { useEffect } from 'react';
const deps = [1];
export default function Page(props) {
  const run = (deps) => useEffect(() => {}, deps);
  return <p>x</p>;
}`)
	findDiag(t, diags, diag.CodeDepsUnresolved)
}

func TestEffectDepsExpressionComparedAtRunTime(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useEffect, useState } from 'react';
export default function Page(props) {
  const [a, setA] = useState(0);
  useEffect(() => {}, props.deps);
  useEffect(() => {}, a > 1 ? [a] : []);
  useEffect(() => {}, [...props.items, a]);
  return <p>{a}</p>;
}`)
	mustContain(t, out, "return props.deps;", "return a() > 1 ? [a()] : [];", "return [...props.items, a()];")
	if len(diags) != 0 {
		t.Fatalf("live expressions need no diagnostic: %v", diags)
	}
}

func TestEffectWithoutDepsSynthesizesStateAndProps(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useEffect, useState, useReducer } from 'react';
function Child({ label, size = 1 }) {
  useEffect(() => { log(label); });
  return <p>{label}</p>;
}
export default function Page(props) {
  const [a, setA] = useState(0);
  const local = 3;
  const [s, dispatch] = useReducer((x, y) => x + y, 0);
  useEffect(() => { log(a); });
  return <Child label="x" />;
}`)
	mustContain(t, out,
		"}, () => {\n    return [label, size];\n  });",
		"}, () => {\n    return [a(), s(), ...krateDepsOf(props)];\n  });")
	d := findDiag(t, diags, diag.CodeEffectNoDeps)
	if d.Line != 3 || d.Col != 3 {
		t.Fatalf("KR2002 at %d:%d, want 3:3", d.Line, d.Col)
	}
}

func TestEffectNullDepsIsNoDeps(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useEffect, useState } from 'react';
export default function Page() {
  const [a, setA] = useState(0);
  useEffect(() => {}, null);
  useEffect(() => {}, undefined);
  return <p>{a}</p>;
}`)
	if strings.Count(out, "return [a()];") != 2 {
		t.Fatalf("null/undefined deps should behave like omitted deps:\n%s", out)
	}
	for _, d := range diags {
		if d.Code == diag.CodeDepsNotArray {
			t.Fatalf("null deps must not warn KR2006: %v", d)
		}
	}
}

func TestEffectNonArrayDepsWarns(t *testing.T) {
	_, diags := rewriteDiag(t, `import { useEffect } from 'react';
export default function Page() {
  useEffect(() => {}, 5);
  return <p>x</p>;
}`)
	d := findDiag(t, diags, diag.CodeDepsNotArray)
	if d.Line != 3 || d.Col != 23 {
		t.Fatalf("KR2006 at %d:%d, want 3:23", d.Line, d.Col)
	}
}

func TestAsyncEffectWarns(t *testing.T) {
	_, diags := rewriteDiag(t, `import { useEffect } from 'react';
export default function Page() {
  useEffect(async () => { await load(); }, []);
  return <p>x</p>;
}`)
	d := findDiag(t, diags, diag.CodeAsyncEffect)
	if d.Line != 3 || d.Col != 13 {
		t.Fatalf("KR2003 at %d:%d, want 3:13", d.Line, d.Col)
	}
}

func TestMemoAndCallbackLowering(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useMemo, useCallback, useState, useEffect } from 'react';
export default function Page() {
  const [a, setA] = useState(1);
  const double = useMemo(() => a * 2, [a]);
  const fmt = useMemo(() => (x) => x + a, [a]);
  const onClick = useCallback(() => setA(a + 1), [a]);
  useEffect(() => { onClick(); log(fmt(1)); double(); }, [onClick]);
  return <button onClick={onClick}>{double}</button>;
}`)
	mustContain(t, out,
		"const double = createMemoDeps(() => a() * 2, () => {\n    return [a()];\n  });",
		"const onClick = createCallbackDeps(() => setA(a() + 1), () => {\n    return [a()];\n  });",
		// Getters that hold functions are called before being invoked.
		"onClick()();",
		"log(fmt()(1));",
		// A zero-argument call of a memo getter stays a read (Krate style).
		"double();",
		"return [onClick()];",
		"onClick={onClick()}",
		"{double()}")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestMemoAndCallbackWithoutDepsWarn(t *testing.T) {
	out, diags := rewriteDiag(t, `import { useMemo, useCallback, useState } from 'react';
export default function Page() {
  const [a, setA] = useState(1);
  const double = useMemo(() => a * 2);
  const cb = useCallback(() => a);
  return <p onClick={cb}>{double}</p>;
}`)
	mustContain(t, out, "const double = createMemo(() => a() * 2);", "const cb = () => a();")
	if d := findDiag(t, diags, diag.CodeMemoNoDeps); d.Line != 4 {
		t.Fatalf("KR2005 line %d", d.Line)
	}
	if d := findDiag(t, diags, diag.CodeCallbackNoDeps); d.Line != 5 {
		t.Fatalf("KR2004 line %d", d.Line)
	}
}

func TestUseImperativeHandleDepsBecomeThunk(t *testing.T) {
	out, _ := rewriteDiag(t, `import { useImperativeHandle, useState, forwardRef } from 'react';
const Input = forwardRef((props, ref) => {
  const [v, setV] = useState('');
  useImperativeHandle(ref, () => ({ value: v }), [v]);
  return <input />;
});
export default Input;`)
	mustContain(t, out, "useImperativeHandle(props.ref, () => ({\n    value: v()\n  }), () => {\n    return [v()];\n  });")
}

func TestUseReducerLazyInitIsPreserved(t *testing.T) {
	out, _ := rewriteDiag(t, `import { useReducer } from 'react';
export default function Page() {
  const [s, dispatch] = useReducer((x, a) => x + a, 2, (n) => n * 3);
  return <button onClick={() => dispatch(1)}>{s}</button>;
}`)
	mustContain(t, out, "createReducer((x, a) => x + a, ((n) => n * 3)(2), krateHookState)")
}

func TestHookInLoopOrConditionWarns(t *testing.T) {
	_, diags := rewriteDiag(t, `import { useState, useEffect, use } from 'react';
export default function Page(props) {
  const values = [];
  for (const f of props.fields) {
    const [v] = useState(f);
    values.push(v);
  }
  if (props.tracking) {
    useEffect(() => {}, []);
  }
  if (props.ctx) {
    use(props.ctx);
  }
  const ok = () => { if (props.x) { return 1; } };
  return <p>{values.join(" ")}</p>;
}`)
	var hits []diag.Diagnostic
	for _, d := range diags {
		if d.Code == diag.CodeHookPlacement {
			hits = append(hits, d)
		}
	}
	if len(hits) != 2 || hits[0].Line != 5 || hits[0].Col != 17 || !strings.Contains(hits[0].Message, "inside a loop") ||
		hits[1].Line != 9 || !strings.Contains(hits[1].Message, "inside a condition") {
		t.Fatalf("want KR2008 at 5:17 (loop) and line 9 (condition), got %v", hits)
	}
}

func TestStateLoweringUsesReactSemantics(t *testing.T) {
	out, _ := rewriteDiag(t, `import { useState, useReducer } from 'react';
function loadDraft() { return 'saved'; }
const makeInit = (n) => ({ count: n, history: [] });
export default function Page() {
  const [draft, setDraft] = useState(loadDraft);
  const [n, setN] = useState(() => 1 + 1);
  const [plain, setPlain] = useState(0);
  const [s, dispatch] = useReducer((st, a) => st, 5, makeInit);
  const [r, send] = useReducer((st, a) => st, 0);
  return <p>{draft}{n}{plain}{s.count}{r}</p>;
}`)
	mustContain(t, out,
		"createSignal(loadDraft(), krateHookState)",
		"createSignal((() => 1 + 1)(), krateHookState)",
		"createSignal(0, krateHookState)",
		"createReducer((st, a) => st, makeInit(5), krateHookState)",
		"createReducer((st, a) => st, 0, krateHookState)")
}

func TestFreshDepsReRunEveryRender(t *testing.T) {
	out, _ := rewriteDiag(t, `import { useEffect, useState, useCallback } from 'react';
export default function Page(props) {
  const [a, setA] = useState(0);
  const handler = () => a;
  const stable = useCallback(() => a, [a]);
  function named() { return a; }
  useEffect(() => {}, [handler]);
  useEffect(() => {}, [() => a, stable]);
  useEffect(() => {}, [named, props.x]);
  return <p>{a}</p>;
}`)
	mustContain(t, out,
		"return [...[a(), ...krateDepsOf(props)]];",
		"return [...[a(), ...krateDepsOf(props)], stable()];",
		"return [...[a(), ...krateDepsOf(props)], props.x];")
}

func TestEffectCodesAreRegistered(t *testing.T) {
	for _, code := range []diag.Code{diag.CodeDepsUnresolved, diag.CodeEffectNoDeps, diag.CodeAsyncEffect,
		diag.CodeCallbackNoDeps, diag.CodeMemoNoDeps, diag.CodeDepsNotArray, diag.CodeHookPlacement} {
		info, ok := diag.Lookup(code)
		if !ok || info.Title == "" || !strings.HasPrefix(string(code), "KR2") {
			t.Fatalf("code %s not registered in the KR2xxx range", code)
		}
	}
}
