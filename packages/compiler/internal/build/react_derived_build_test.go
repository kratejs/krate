package build

import (
	"strings"
	"testing"
)

// Render-scope values of React source components: derived values, custom
// hook results, context values and module-level state (react_derived.go,
// irtree/derived.go, irtree/moduleconsts_ir.go).

func TestDerivedRenderValuesHydrateAsGetters(t *testing.T) {
	html, js := buildReactPage(t, `
		import { useState, useEffect } from 'react';
		export default function Page() {
			const [items, setItems] = useState(["a", "b"]);
			const [count, setCount] = useState(3);
			const n = items.length;
			const label = n > 1 ? "items" : "item";
			let cls = "base";
			if (count > 2) cls += " on";
			const value = Number(count);
			useEffect(() => { console.log(value); }, [value]);
			return (
				<div className={cls}>
					<p id="out">{n} {label}</p>
					<button onClick={() => setItems([...items, "z"])}>add</button>
					<button onClick={() => setCount(count + 1)}>inc</button>
				</div>
			);
		}
	`)
	for _, want := range []string{
		"var n=krateDerived(()=>items().length);",
		"var label=krateDerived(()=>((n() > 1)?'items':'item'));",
		"var cls=krateDerived(()=>{\n  let cls='base';\n  if ((count() > 2)) {\n    (cls += ' on');\n  }\n  return cls;\n});",
		"var value=krateDerived(()=>Number(count()));",
		"return [value()];",
		`kbindText("3",()=>n());`,
		`kbindText("4",()=>label());`,
		`kbindAttr("1","class",()=>cls());`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS missing %q:\n%s", want, js)
		}
	}
	// The SSR output is each getter's build-time value.
	for _, want := range []string{`class="base on"`, "<!--k:3-->2<!--/k:3-->", "<!--k:4-->items<!--/k:4-->"} {
		if !strings.Contains(html, want) {
			t.Errorf("SSR missing %q:\n%s", want, html)
		}
	}
}
