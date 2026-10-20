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

// React.* namespace

func TestRewriteReactNamespaceUseState(t *testing.T) {
	out := rewriteSrc(t, `
		import React from 'react';
		export default function Page() {
			const [count, setCount] = React.useState(0);
			return <div>{count}</div>;
		}
	`)
	if strings.Contains(out, "React") {
		t.Errorf("React namespace should be removed:\n%s", out)
	}
	if !strings.Contains(out, "createSignal") {
		t.Errorf("expected createSignal:\n%s", out)
	}
	// Bare read `{count}` must become `count()`.
	if !strings.Contains(out, "count()") {
		t.Errorf("expected bare read to be called:\n%s", out)
	}
}

func TestRewriteReactNamespaceForwardRef(t *testing.T) {
	out := rewriteSrc(t, `
		import * as React from 'react';
		const Comp = React.forwardRef((props, ref) => <div ref={ref} />);
		export default Comp;
	`)
	if strings.Contains(out, "forwardRef") {
		t.Errorf("forwardRef should be lowered to the inner function:\n%s", out)
	}
	if !strings.Contains(out, "props.ref") {
		t.Errorf("expected ref reads to become props.ref:\n%s", out)
	}
	if strings.Contains(out, "React.") {
		t.Errorf("React namespace should be gone:\n%s", out)
	}
}

func TestRewriteReactNamespaceCreateRef(t *testing.T) {
	out := rewriteSrc(t, `
		import { createRef } from 'react';
		const ref = createRef();
	`)
	if strings.Contains(out, "createRef") {
		t.Errorf("createRef should be lowered:\n%s", out)
	}
	if !strings.Contains(out, "current") {
		t.Errorf("expected a {current:null} object:\n%s", out)
	}
}

func TestRewriteReactEffectDepsAndMemo(t *testing.T) {
	out := rewriteSrc(t, `
		import { useEffect, useMemo, useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			useEffect(() => { console.log(count); }, [count]);
			const doubled = useMemo(() => count * 2, [count]);
			return <div>{doubled}</div>;
		}
	`)
	if !strings.Contains(out, "createEffectDeps") || !strings.Contains(out, "[count()]") {
		t.Fatalf("non-empty effect deps should lower to createEffectDeps with live deps:\n%s", out)
	}
	if !strings.Contains(out, "createMemoDeps") {
		t.Fatalf("useMemo should lower to createMemoDeps when deps are present:\n%s", out)
	}
}

func TestRewriteReactEffectEmptyDepsMountOnce(t *testing.T) {
	out := rewriteSrc(t, `
		import { useEffect } from 'react';
		export default function Page() {
			useEffect(() => {
				return () => {};
			}, []);
			return <div>x</div>;
		}
	`)
	if !strings.Contains(out, "onMount") || !strings.Contains(out, "onCleanup") {
		t.Fatalf("empty-deps effect should lower to onMount + onCleanup:\n%s", out)
	}
	if strings.Contains(out, "createEffect") {
		t.Fatalf("empty-deps effect must not lower to a reactive createEffect:\n%s", out)
	}
}

func TestRewriteReactCreateElementToH(t *testing.T) {
	out := rewriteSrc(t, `
		import { createElement } from 'react';
		export default function Page() {
			return createElement('div', { class: 'x' }, 'hi');
		}
	`)
	if !strings.Contains(out, "h(") || strings.Contains(out, "createElement(") {
		t.Errorf("createElement should map to h:\n%s", out)
	}
}

// bare-read auto-call

func TestRewriteReactBareReadInExpression(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			return <div>{count + 1}</div>;
		}
	`)
	if !strings.Contains(out, "count()") {
		t.Errorf("bare read in arithmetic should be called:\n%s", out)
	}
}

func TestRewriteReactBareReadInHandler(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			return <button onClick={() => setCount(count + 1)}>x</button>;
		}
	`)
	if !strings.Contains(out, "count() + 1") {
		t.Errorf("bare read in handler should be called:\n%s", out)
	}
	if strings.Contains(out, "setCount()") {
		t.Errorf("setter must not be auto-called:\n%s", out)
	}
}

func TestRewriteReactBareReadInTemplateAndTernary(t *testing.T) {
	src := "import { useState } from 'react';\n" +
		"export default function Page() {\n" +
		"  const [n, setN] = useState(1);\n" +
		"  const [on, setOn] = useState(false);\n" +
		"  return <div>{`n=${n}`}{on ? \"y\" : \"n\"}</div>;\n" +
		"}"
	out := rewriteSrc(t, src)
	if !strings.Contains(out, "n()") || !strings.Contains(out, "on()") {
		t.Errorf("template/ternary reads should be called:\n%s", out)
	}
}

func TestRewriteReactMemoBareRead(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState, useMemo } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			const doubled = useMemo(() => count * 2, [count]);
			return <div>{doubled}</div>;
		}
	`)
	if !strings.Contains(out, "doubled()") {
		t.Errorf("memo getter should be auto-called:\n%s", out)
	}
	if !strings.Contains(out, "count()") {
		t.Errorf("memo body should read count as call:\n%s", out)
	}
}

func TestRewriteReactShadowingNotCalled(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			return <div>{[1].map(count => count + 1)}</div>;
		}
	`)
	if strings.Contains(out, "count()") {
		t.Errorf("shadowing parameter must not be auto-called:\n%s", out)
	}
}

func TestRewriteReactAlreadyCalledNotDoubled(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			return <div>{count()}</div>;
		}
	`)
	if strings.Contains(out, "count()()") {
		t.Errorf("already-called getter must not be re-wrapped:\n%s", out)
	}
}

// structural lowerings

func TestRewriteReactUseRefObject(t *testing.T) {
	out := rewriteSrc(t, `
		import { useRef } from 'react';
		export default function Page() {
			const ref = useRef(null);
			return <div ref={ref} />;
		}
	`)
	if strings.Contains(out, "useRef(") {
		t.Errorf("useRef call should be lowered:\n%s", out)
	}
	if !strings.Contains(out, "current") {
		t.Errorf("expected a {current:...} object:\n%s", out)
	}
}

func TestRewriteReactUseCallbackUnwrapped(t *testing.T) {
	out := rewriteSrc(t, `
		import { useCallback } from 'react';
		export default function Page() {
			const cb = useCallback(() => 42, []);
			return <button onClick={cb}>x</button>;
		}
	`)
	if strings.Contains(out, "useCallback(") {
		t.Errorf("useCallback should unwrap to the function:\n%s", out)
	}
}

func TestRewriteReactMemoIdentity(t *testing.T) {
	out := rewriteSrc(t, `
		import { memo } from 'react';
		const Inner = memo(() => <div>hi</div>);
		export default Inner;
	`)
	if strings.Contains(out, "memo(") {
		t.Errorf("memo should be an identity lower:\n%s", out)
	}
}

func TestRewriteReactUseContextLowered(t *testing.T) {
	out := rewriteSrc(t, `
		import { createContext, useContext } from 'react';
		const Ctx = createContext(0);
		export default function Page() {
			const v = useContext(Ctx);
			return <div>{v}</div>;
		}
	`)
	if strings.Contains(out, "useContext(Ctx)") {
		t.Errorf("useContext should lower to member call:\n%s", out)
	}
	if !strings.Contains(out, "Ctx.useContext()") {
		t.Errorf("expected Ctx.useContext():\n%s", out)
	}
}

// style objects

func TestRewriteReactStyleObject(t *testing.T) {
	out := rewriteSrc(t, `
		export default function Page() {
			return <div style={{ fontSize: 12, color: 'red', zIndex: 3 }}>x</div>;
		}
	`)
	if !strings.Contains(out, "font-size:12px;color:red;z-index:3") {
		t.Errorf("style object should fold to a CSS string:\n%s", out)
	}
	if strings.Contains(out, "{fontSize") || strings.Contains(out, "zIndex") {
		t.Errorf("style object keys should be kebab-cased:\n%s", out)
	}
}

func TestRewriteReactStyleObjectNonLiteralUntouched(t *testing.T) {
	out := rewriteSrc(t, `
		export default function Page(props) {
			return <div style={{ width: props.w }}>x</div>;
		}
	`)
	if strings.Contains(out, `style="`) {
		t.Errorf("non-literal style object must be left alone:\n%s", out)
	}
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

func TestRewriteReactNamespaceUseReducer(t *testing.T) {
	out := rewriteSrc(t, `
		import React from 'react';
		export default function Page() {
			const [s, dispatch] = React.useReducer((s, a) => a, 0);
			return <div>{s}</div>;
		}
	`)
	if !strings.Contains(out, "createReducer") || strings.Contains(out, "React.") {
		t.Errorf("React.useReducer should map to createReducer:\n%s", out)
	}
}

// Fragment

func TestRewriteReactFragmentNamedImport(t *testing.T) {
	out := rewriteSrc(t, `
		import { Fragment } from 'react';
		export default function Page() {
			return <Fragment><div>a</div><div>b</div></Fragment>;
		}
	`)
	if strings.Contains(out, "Fragment") {
		t.Errorf("Fragment tag should lower to a JSX fragment:\n%s", out)
	}
	if !strings.Contains(out, "<>") {
		t.Errorf("expected a JSX fragment:\n%s", out)
	}
}

func TestRewriteReactFragmentNamespace(t *testing.T) {
	out := rewriteSrc(t, `
		import React from 'react';
		export default function Page() {
			return <React.Fragment><div>a</div></React.Fragment>;
		}
	`)
	if strings.Contains(out, "Fragment") || strings.Contains(out, "React.") {
		t.Errorf("React.Fragment should lower to a JSX fragment:\n%s", out)
	}
}

// useId

func TestRewriteReactUseIdMarker(t *testing.T) {
	out := rewriteSrc(t, `
		import { useId } from 'react';
		export default function Page() {
			const id = useId();
			return <div id={id} />;
		}
	`)
	if strings.Contains(out, "= useId()") {
		t.Errorf("useId should be lowered to a marker:\n%s", out)
	}
	if !strings.Contains(out, "__krate_useId()") {
		t.Errorf("expected the useId marker:\n%s", out)
	}
}

// no React import

func TestRewriteReactNoReactLeavesKrateCodeAlone(t *testing.T) {
	out := rewriteSrc(t, `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [x, setX] = createSignal(0);
			return <div>{x()}</div>;
		}
	`)
	if !strings.Contains(out, "createSignal") || !strings.Contains(out, "x()") {
		t.Errorf("krate-native code should be untouched:\n%s", out)
	}
}

// react/jsx-runtime (compiled React output)

func TestRewriteReactJSXRuntimeIntrinsic(t *testing.T) {
	out := rewriteSrc(t, `
		import { jsx as _jsx } from "react/jsx-runtime";
		export default function Page() {
			return _jsx("div", { className: "box", children: "hi" });
		}
	`)
	if strings.Contains(out, "jsx-runtime") || strings.Contains(out, "_jsx(") {
		t.Errorf("jsx-runtime call should be lowered:\n%s", out)
	}
	if !strings.Contains(out, "<div") || !strings.Contains(out, "class") || !strings.Contains(out, "hi") {
		t.Errorf("expected a real div element with children:\n%s", out)
	}
}

func TestRewriteReactJSXRuntimeComponentAndArray(t *testing.T) {
	out := rewriteSrc(t, `
		import { jsx, jsxs } from "react/jsx-runtime";
		export default function Page({ items }) {
			return jsxs("ul", { children: items.map((i) => jsx("li", { children: i })) });
		}
	`)
	if strings.Contains(out, `jsxs("ul"`) {
		t.Errorf("jsxs should be lowered:\n%s", out)
	}
	if !strings.Contains(out, "<ul>") || !strings.Contains(out, "<li>") {
		t.Errorf("expected ul/li elements:\n%s", out)
	}
}

func TestRewriteReactJSXRuntimeFragment(t *testing.T) {
	out := rewriteSrc(t, `
		import { jsx, Fragment } from "react/jsx-runtime";
		export default function Page() {
			return jsx(Fragment, { children: [jsx("a", {}), jsx("b", {})] });
		}
	`)
	if strings.Contains(out, "Fragment") {
		t.Errorf("Fragment should lower to a JSX fragment:\n%s", out)
	}
	if !strings.Contains(out, "<a") || !strings.Contains(out, "<b") {
		t.Errorf("expected both children:\n%s", out)
	}
}

func TestRewriteReactJSXRuntimeMemberComponent(t *testing.T) {
	out := rewriteSrc(t, `
		import { jsx } from "react/jsx-runtime";
		export default function Page() {
			return jsx(Popover.Root, { children: jsx(Popover.Trigger, {}) });
		}
	`)
	if !strings.Contains(out, "<Popover.Root") || !strings.Contains(out, "<Popover.Trigger") {
		t.Errorf("dotted component tags should be preserved:\n%s", out)
	}
}

func TestRewriteReactJSXRuntimeNamespace(t *testing.T) {
	out := rewriteSrc(t, `
		import * as jsxRuntime from "react/jsx-runtime";
		export default function Page() {
			return jsxRuntime.jsx("span", { id: "x" });
		}
	`)
	if strings.Contains(out, "jsxRuntime") {
		t.Errorf("jsx-runtime namespace should be removed:\n%s", out)
	}
	if !strings.Contains(out, "<span") {
		t.Errorf("expected a span element:\n%s", out)
	}
}

// compat runtime APIs

func TestRewriteReactCompatImport(t *testing.T) {
	out := rewriteSrc(t, `
		import { Children } from 'react';
		export default function Page({ items }) {
			return <ul>{Children.map(items, (i) => <li>{i}</li>)}</ul>;
		}
	`)
	if !strings.Contains(out, "@krate/runtime/react") || !strings.Contains(out, "Children") {
		t.Errorf("expected Children imported from @krate/runtime/react:\n%s", out)
	}
}

func TestRewriteReactCompatMember(t *testing.T) {
	out := rewriteSrc(t, `
		import React from 'react';
		export default function Page({ a }) {
			return <div>{React.cloneElement(a, { x: 1 })}</div>;
		}
	`)
	if strings.Contains(out, "React.") {
		t.Errorf("React namespace should be gone:\n%s", out)
	}
	if !strings.Contains(out, "cloneElement") || !strings.Contains(out, "@krate/runtime/react") {
		t.Errorf("expected cloneElement from @krate/runtime/react:\n%s", out)
	}
}

func TestReactCompatSpecifier(t *testing.T) {
	cases := map[string]string{
		"react":                  "@krate/runtime/react",
		"react-dom":              "@krate/runtime/react",
		"react-dom/client":       "@krate/runtime/react",
		"react/jsx-runtime":      "@krate/runtime/react/jsx-runtime",
		"react/jsx-dev-runtime":  "@krate/runtime/react/jsx-runtime",
		"@radix-ui/react-slot":   "@krate/runtime/slot",
		"react-router":           "",
		"@radix-ui/react-dialog": "",
	}
	for in, want := range cases {
		if got := reactCompatSpecifier(in); got != want {
			t.Errorf("reactCompatSpecifier(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteReactDOMCreatePortal(t *testing.T) {
	out := rewriteSrc(t, `
		import { createPortal } from 'react-dom';
		export default function Page() {
			return createPortal(<div />, document.body);
		}
	`)
	if strings.Contains(out, "react-dom") {
		t.Errorf("react-dom import should be removed:\n%s", out)
	}
	if !strings.Contains(out, "createPortal(") {
		t.Errorf("expected a createPortal call:\n%s", out)
	}
	if !strings.Contains(out, "@krate/runtime/react") {
		t.Errorf("expected the compat runtime import:\n%s", out)
	}
}

func TestRewriteReactDOMNamespaceFlushSync(t *testing.T) {
	out := rewriteSrc(t, `
		import * as ReactDOM from 'react-dom';
		export default function Page() {
			ReactDOM.flushSync(() => {});
			return <div />;
		}
	`)
	if strings.Contains(out, "ReactDOM") {
		t.Errorf("ReactDOM namespace should be removed:\n%s", out)
	}
	if !strings.Contains(out, "flushSync(") {
		t.Errorf("expected a flushSync call:\n%s", out)
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
