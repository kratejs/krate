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
	if !strings.Contains(out, "forwardRef") {
		t.Errorf("expected forwardRef:\n%s", out)
	}
	if strings.Contains(out, "React.") {
		t.Errorf("React namespace should be gone:\n%s", out)
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
