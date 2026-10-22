package bundler

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestDerivedRenderValueFromState(t *testing.T) {
	out, diags := rewriteDiag(t, `
		import { useState, useEffect } from 'react';
		export default function Page() {
			const [raw, setRaw] = useState("12");
			const value = Number(raw);
			const label = value > 1 ? "items" : "item";
			useEffect(() => { console.log(value); }, [value]);
			return <p onClick={() => setRaw("7")}>{value} {label}</p>;
		}
	`)
	mustContain(t, out,
		"const value = krateDerived(() => Number(raw()))",
		"const label = krateDerived(() => value() > 1 ? \"items\" : \"item\")",
		"console.log(value())",
		"[value()]",
		"{value()}",
		"{label()}",
	)
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

func TestDerivedLeavesStaticValuesAlone(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [n, setN] = useState(0);
			const title = "Hello";
			const sum = 1 + 2;
			const inc = () => setN(n + 1);
			return <p onClick={inc}>{title} {sum} {n}</p>;
		}
	`)
	if strings.Contains(out, "krateDerived") {
		t.Errorf("static values and closures must stay plain:\n%s", out)
	}
}

func TestDerivedPropsReadInComponent(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		function Child(props) {
			const doubled = props.count * 2;
			return <p>{doubled}</p>;
		}
		export default function Page() {
			const [count] = useState(1);
			return <Child count={count} />;
		}
	`)
	mustContain(t, out, "const doubled = krateDerived(() => props.count * 2)", "{doubled()}")
}

func TestDerivedLetBuiltByStatements(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [active] = useState(false);
			let cls = "tab";
			if (active) cls += " on";
			return <div className={cls} />;
		}
	`)
	mustContain(t, out, "let cls = krateDerived(() => {", "let cls = \"tab\";", "return cls;", "className={cls()}")
	if strings.Count(out, "if (active())") != 1 {
		t.Errorf("the building statement must move into the derived function:\n%s", out)
	}
}

func TestDerivedValueReassignedInHandlerWarns(t *testing.T) {
	_, diags := rewriteDiag(t, `
		import { useState } from 'react';
		export default function Page() {
			const [n] = useState(0);
			let last = n;
			return <button onClick={() => { last = 5; }}>{last}</button>;
		}
	`)
	d := findDiag(t, diags, diag.CodeStaleRenderValue)
	if !strings.Contains(d.Message, "`last`") {
		t.Errorf("diagnostic should name the binding: %s", d.Message)
	}
}

func TestDerivedGroupWritingTwoValuesWarns(t *testing.T) {
	out, diags := rewriteDiag(t, `
		import { useState } from 'react';
		export default function Page() {
			const [n] = useState(0);
			let a = n;
			let b = 0;
			if (n > 1) { a = 1; b = 2; }
			return <p>{a}{b}</p>;
		}
	`)
	findDiag(t, diags, diag.CodeStaleRenderValue)
	if strings.Contains(out, "let a = krateDerived") {
		t.Errorf("a group that also writes b must not be derived:\n%s", out)
	}
}

func TestCustomHookTupleAndObjectResults(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState, useCallback } from 'react';
		function useToggle(initial) {
			const [on, setOn] = useState(initial);
			const toggle = useCallback(() => setOn((v) => !v), []);
			return [on, toggle];
		}
		function useCounter(step) {
			const [n, setN] = useState(0);
			return { n, inc: () => setN(n + step) };
		}
		export default function Page() {
			const [open, toggleOpen] = useToggle(false);
			const a = useCounter(1);
			return <div>
				<button onClick={toggleOpen}>{String(open)}</button>
				<button onClick={a.inc}>{a.n}</button>
				<button onClick={() => toggleOpen()}>again</button>
			</div>;
		}
	`)
	mustContain(t, out,
		"return krateDerived(() => [on(), toggle()])",
		"return krateDerived(() => ({ n: n(), inc: () => setN(n() + step) }))",
		"= krateHook(useToggle(false))",
		"const a = krateHook(useCounter(1))",
		"onClick={toggleOpen()}",
		"{String(open())}",
		"{a().n}",
		"onClick={a().inc}",
		"toggleOpen()()",
	)
}

func TestContextReadIsDerivedValue(t *testing.T) {
	out := rewriteSrc(t, `
		import { createContext, useContext, useEffect } from 'react';
		const Theme = createContext({ color: "black" });
		function Label() {
			const theme = useContext(Theme);
			const { color } = useContext(Theme);
			useEffect(() => { console.log(theme.color, color); }, [theme]);
			return <span data-color={theme.color}>{color}</span>;
		}
		export default function Page() { return <Label />; }
	`)
	mustContain(t, out,
		"const theme = krateDerived(() => Theme.useContext())",
		"console.log(theme().color, color())",
		"[theme()]",
		"data-color={theme().color}",
	)
}

func TestShorthandPropertyOfGetterIsCalled(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [page] = useState(1);
			const options = { page };
			return <p>{options.page}</p>;
		}
	`)
	mustContain(t, out, "page: page()", "{options().page}")
}

func TestReservedRuntimeNamesAreRenamed(t *testing.T) {
	out := rewriteSrc(t, `
		import { useState } from 'react';
		export default function Page() {
			const [hits] = useState(["a"]);
			const findSlot = 1;
			function kbindText(x) { return x; }
			return <ul>{hits.map((h) => <li key={h}>{h}{findSlot}{kbindText(h)}</li>)}</ul>;
		}
	`)
	for _, name := range []string{"h", "findSlot", "kbindText"} {
		if !strings.Contains(out, name+"$u") {
			t.Errorf("%s should be renamed:\n%s", name, out)
		}
	}
	if strings.Contains(out, "(h)") || strings.Contains(out, "{h}") {
		t.Errorf("no user binding may keep the name h:\n%s", out)
	}
}

func TestReservedNameFromImportIsKept(t *testing.T) {
	out := rewriteSrc(t, `
		import { h } from '@krate/runtime';
		export default function Page() { return h('div', null); }
	`)
	if strings.Contains(out, "h$u") {
		t.Errorf("an imported runtime binding must not be renamed:\n%s", out)
	}
}

func TestNativeModuleIsNotDerived(t *testing.T) {
	out := rewriteSrc(t, `
		export default function Page() {
			const [n, setN] = createSignal(1);
			const doubled = n() * 2;
			return <p>{doubled}</p>;
		}
	`)
	if strings.Contains(out, "krateDerived") {
		t.Errorf("Krate-native modules keep run-once semantics:\n%s", out)
	}
}
