package bundler

import (
	"testing"
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
