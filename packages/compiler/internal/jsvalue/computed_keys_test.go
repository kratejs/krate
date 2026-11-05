package jsvalue

import "testing"

// Object literals with `[expr]: value` keys use the key's value. The expected
// strings were checked against Node.
func TestComputedObjectKeys(t *testing.T) {
	cases := []struct{ src, want string }{
		{`(() => { const K = "color"; return JSON.stringify({ [K]: "red" }); })()`, `{"color":"red"}`},
		{"(() => { const a = 1; return JSON.stringify({ [`b${a}`]: 2 }); })()", `{"b1":2}`},
		{`(() => { const Kind = { A: "a", B: "b" }; return JSON.stringify({ [Kind.A]: 1, [Kind.B]: 2 }); })()`, `{"a":1,"b":2}`},
		{`JSON.stringify({ [1 + 2]: "x", [true]: "t", [null]: "n" })`, `{"3":"x","true":"t","null":"n"}`},
		{`(() => { const K = "a"; return JSON.stringify({ a: 1, [K]: 2, b: 3 }); })()`, `{"a":2,"b":3}`},
		{`(() => { const K = "m"; return ({ [K]() { return 5; } }).m(); })()`, `5`},
		{`(() => { const K = "v"; return ({ get [K]() { return 6; } }).v; })()`, `6`},
		{`Object.keys({ ["a" + "b"]: 1, c: 2 }).join()`, `ab,c`},
		{`(() => { const K = "a"; const { [K]: v, ...rest } = { a: 7, b: 8 }; return v + JSON.stringify(rest); })()`, `7{"b":8}`},
		{`[["x", 1], ["y", 2]].reduce((acc, [k, v]) => ({ ...acc, [k]: v }), {}).y`, `2`},
	}
	for _, tc := range cases {
		v := evalSrc(t, tc.src)
		if v.IsUnknown() {
			t.Errorf("%s: unresolved %#v", tc.src, v)
			continue
		}
		if got := v.ToString(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.src, got, tc.want)
		}
	}
}

// A key that is not known leaves the whole object unknown: folding it would
// drop or rename a key.
func TestComputedObjectKeyUnknownStaysUnknown(t *testing.T) {
	for _, src := range []string{
		`({ [unknownKey]: 1 })`,
		`JSON.stringify({ a: 1, [Math.random()]: 2 })`,
		`(() => { const { [unknownKey]: v } = { a: 1 }; return v; })()`,
		`(() => { const { [unknownKey]: v, ...rest } = { a: 1 }; return rest; })()`,
	} {
		if v := evalSrc(t, src); !v.IsUnknown() {
			t.Errorf("%s: folded to %#v", src, v)
		}
	}
}
