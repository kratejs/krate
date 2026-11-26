package bundler

import (
	"strings"
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

// A custom hook of a Krate-native module hands the getters it created to its
// caller, not their current values; a component or a plain function still
// reads them.
func TestNativeHookReturnsItsGetters(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		not  []string
	}{
		{
			name: "object",
			src: `import { createSignal } from "@krate/runtime";
export function useToggle(initial = false) {
  const [on, setOn] = createSignal(initial);
  return { on, toggle: () => setOn(!on()) };
}`,
			want: []string{"return {", "on,", "toggle:"},
			not:  []string{"on: on()"},
		},
		{
			name: "array",
			src: `import { createSignal } from "@krate/runtime";
export function useFlag() {
  const [on, setOn] = createSignal(false);
  return [on, () => setOn(!on())] as const;
}`,
			want: []string{"[on,"},
			not:  []string{"[on(),"},
		},
		{
			name: "a plain function still reads the value",
			src: `import { createSignal } from "@krate/runtime";
export function snapshot() {
  const [on] = createSignal(false);
  return { on };
}`,
			want: []string{"on: on()"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := rewriteSrc(t, tc.src)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(out, n) {
					t.Errorf("output has %q:\n%s", n, out)
				}
			}
		})
	}
}
