package jsvalue

import (
	"math"
	"testing"
)

var point1, point2 = 0.1, 0.2

func TestNumberToString(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{1, "1"},
		{-1, "-1"},
		{100, "100"},
		{2.5, "2.5"},
		{-2.5, "-2.5"},
		{0.1, "0.1"},
		{point1 + point2, "0.30000000000000004"},
		{123.456, "123.456"},
		{1e20, "100000000000000000000"},
		{1e21, "1e+21"},
		{1.5e21, "1.5e+21"},
		{123456789012345680000, "123456789012345680000"},
		{1e-6, "0.000001"},
		{0.000001234, "0.000001234"},
		{1e-7, "1e-7"},
		{1.5e-7, "1.5e-7"},
		{-1.5e300, "-1.5e+300"},
		{1.7976931348623157e308, "1.7976931348623157e+308"},
		{5e-324, "5e-324"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{9007199254740993, "9007199254740992"},
		{-123456, "-123456"},
		{0.5, "0.5"},
	}
	for _, c := range cases {
		if got := NumberToString(c.in); got != c.want {
			t.Errorf("NumberToString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStringToNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"", 0},
		{"  12  ", 12},
		{"\n\t3.5", 3.5},
		{"0x1F", 31},
		{"0b101", 5},
		{"0o17", 15},
		{"1e3", 1000},
		{"+5", 5},
		{"-5", -5},
		{".5", 0.5},
		{"5.", 5},
		{"Infinity", math.Inf(1)},
		{"-Infinity", math.Inf(-1)},
		{"1_000", math.NaN()},
		{"abc", math.NaN()},
		{"inf", math.NaN()},
		{"NaN", math.NaN()},
		{"-0x1", math.NaN()},
		{"12px", math.NaN()},
		{"1e", math.NaN()},
	}
	for _, c := range cases {
		got := StringToNumber(c.in)
		if math.IsNaN(c.want) {
			if !math.IsNaN(got) {
				t.Errorf("StringToNumber(%q) = %v, want NaN", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("StringToNumber(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTruthiness(t *testing.T) {
	cases := []struct {
		v    Value
		want bool
	}{
		{Undefined(), false},
		{Null(), false},
		{Bool(false), false},
		{Bool(true), true},
		{Number(0), false},
		{Number(math.Copysign(0, -1)), false},
		{Number(math.NaN()), false},
		{Number(-1), true},
		{String(""), false},
		{String("0"), true},
		{String("false"), true},
		{String("null"), true},
		{NewArray(), true},
		{NewObject(), true},
		{JSX("<b></b>"), true},
	}
	for _, c := range cases {
		if got := c.v.ToBoolean(); got != c.want {
			t.Errorf("ToBoolean(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
	if _, known := Dynamic("x").Truthy(); known {
		t.Error("Unknown values must not report a known truthiness")
	}
}

func TestEquality(t *testing.T) {
	nan := Number(math.NaN())
	arr := NewArray(Number(1))
	cases := []struct {
		a, b          Value
		strict, loose bool
	}{
		{Number(1), Number(1), true, true},
		{Number(1), String("1"), false, true},
		{Number(0), String(""), false, true},
		{Number(0), Bool(false), false, true},
		{String("0"), Bool(false), false, true},
		{Null(), Undefined(), false, true},
		{Null(), Number(0), false, false},
		{Undefined(), Bool(false), false, false},
		{nan, nan, false, false},
		{Number(0), Number(math.Copysign(0, -1)), true, true},
		{arr, arr, true, true},
		{NewArray(Number(1)), NewArray(Number(1)), false, false},
		{NewArray(Number(1)), String("1"), false, true},
		{NewArray(Number(1), Number(2)), String("1,2"), false, true},
		{String("a"), String("a"), true, true},
		{String("a"), String("b"), false, false},
	}
	for _, c := range cases {
		if got := StrictEquals(c.a, c.b); got != c.strict {
			t.Errorf("%#v === %#v = %v, want %v", c.a, c.b, got, c.strict)
		}
		if got := LooseEquals(c.a, c.b); got != c.loose {
			t.Errorf("%#v == %#v = %v, want %v", c.a, c.b, got, c.loose)
		}
	}
}

func TestRenderChild(t *testing.T) {
	cases := []struct {
		v    Value
		want string
	}{
		{Undefined(), ""},
		{Null(), ""},
		{Bool(true), ""},
		{Bool(false), ""},
		{Number(0), "0"},
		{Number(math.NaN()), "NaN"},
		{String("<img src=x onerror=alert(1)>"), "&lt;img src=x onerror=alert(1)&gt;"},
		{JSX("<b>x</b>"), "<b>x</b>"},
		{NewArray(Number(1), Null(), String("a"), Bool(false), NewArray(Number(2), JSX("<i></i>"))), "1a2<i></i>"},
	}
	for _, c := range cases {
		got, err := RenderChild(c.v)
		if err != nil {
			t.Errorf("RenderChild(%#v) error: %v", c.v, err)
		}
		if got != c.want {
			t.Errorf("RenderChild(%#v) = %q, want %q", c.v, got, c.want)
		}
	}
	if _, err := RenderChild(NewObject()); err == nil {
		t.Error("rendering a plain object as a child must fail")
	}
	if _, err := RenderChild(Dynamic("x")); err == nil {
		t.Error("rendering an Unknown value must fail")
	}
}

// evalSrc evaluates a JS expression with a few bindings in scope.
func evalSrc(t *testing.T, src string) Value {
	t.Helper()
	expr := ParseExprSource(src)
	if expr == nil {
		t.Fatalf("parse %q failed", src)
	}
	in := New(Hooks{})
	in.Scope.Define("count", Number(0))
	in.Scope.Define("q", String("0"))
	in.Scope.Define("n", Null())
	in.Scope.Define("name", String("bob"))
	items, _ := ParseLiteralSource(`[{n:"a",on:true,v:3},{n:"b",on:false,v:1},{n:"c",on:true,v:2}]`)
	in.Scope.Define("items", items)
	obj, _ := ParseLiteralSource(`{a:1,b:"x",c:null,nested:{deep:true}}`)
	in.Scope.Define("obj", obj)
	return in.Eval(expr)
}

func TestInterpExpressions(t *testing.T) {
	cases := []struct {
		src  string
		want string // JSSource of the result
	}{
		{`"5" + "3"`, `'53'`},
		{`"5" + 3`, `'53'`},
		{`5 + 3`, `8`},
		{`"5" - 3`, `2`},
		{`"a" - 1`, `NaN`},
		{`7 % 2.5`, `2`},
		{`-7 % 2`, `-1`},
		{`5 % 0`, `NaN`},
		{`1 / 0`, `Infinity`},
		{`-1 / 0`, `-Infinity`},
		{`0 / 0`, `NaN`},
		{`-count`, `-0`},
		{`-5 + 2`, `-3`},
		{`2 ** 10`, `1024`},
		{`typeof count`, `'number'`},
		{`typeof q`, `'string'`},
		{`typeof n`, `'object'`},
		{`typeof undefined`, `'undefined'`},
		{`typeof items`, `'object'`},
		{`typeof (() => 1)`, `'function'`},
		{`count && "x"`, `0`},
		{`q && "x"`, `'x'`},
		{`"false" && "x"`, `'x'`},
		{`n ?? "d"`, `'d'`},
		{`count ?? "d"`, `0`},
		{`count || "d"`, `'d'`},
		{`"10" < "9"`, `true`},
		{`10 < 9`, `false`},
		{`"10" < 9`, `false`},
		{`NaN < 1`, `false`},
		{`NaN >= 1`, `false`},
		{`null >= 0`, `true`},
		{`undefined == null`, `true`},
		{`undefined === null`, `false`},
		{`"1" == 1`, `true`},
		{`"1" === 1`, `false`},
		{`NaN == NaN`, `false`},
		{`obj?.nested?.deep`, `true`},
		{`n?.x`, `undefined`},
		{`n?.x.y.z`, `undefined`},
		{`obj.missing?.x`, `undefined`},
		{`items.filter(i => i.on).map(i => i.n)`, `['a','c']`},
		{`items.find(i => i.v === 1).n`, `'b'`},
		{`items.findIndex(i => i.v > 5)`, `-1`},
		{`items.some(i => !i.on)`, `true`},
		{`items.every(i => i.on)`, `false`},
		{`items.map(i => i.n).includes("b")`, `true`},
		{`items.map(i => i.n).indexOf("c")`, `2`},
		{`items.map(i => i.n).join("-")`, `'a-b-c'`},
		{`items.map(i => i.n).join()`, `'a,b,c'`},
		{`items.slice(1).length`, `2`},
		{`items.slice(-1)[0].n`, `'c'`},
		{`[1, 2].concat([3], 4)`, `[1,2,3,4]`},
		{`items.map(i => i.n).reverse()`, `['c','b','a']`},
		{`items.map(i => i.v).sort()`, `[1,2,3]`},
		{`items.map(i => i.v).sort((a, b) => b - a)`, `[3,2,1]`},
		{`items.reduce((acc, i) => acc + i.v, 0)`, `6`},
		{`[[1, [2]], 3].flat()`, `[1,[2],3]`},
		{`Object.keys(obj)`, `['a','b','c','nested']`},
		{`Object.values({x: 1, y: "z"})`, `[1,'z']`},
		{`Object.entries({x: 1})`, `[['x',1]]`},
		{`Object.keys({b: 1, 2: 1, a: 1, 1: 1})`, `['1','2','b','a']`},
		{`name.toUpperCase()`, `'BOB'`},
		{`"  hi ".trim()`, `'hi'`},
		{`"a,b,c".split(",")`, `['a','b','c']`},
		{`"abc".split("")`, `['a','b','c']`},
		{`"hello".slice(1, -1)`, `'ell'`},
		{`"hello".startsWith("he")`, `true`},
		{`"hello".endsWith("lo")`, `true`},
		{`"hello".includes("ll")`, `true`},
		{`"a-b-c".replace("-", "+")`, `'a+b-c'`},
		{`"a-b-c".replaceAll("-", "+")`, `'a+b+c'`},
		{`"5".padStart(3, "0")`, `'005'`},
		{`"ab".repeat(2)`, `'abab'`},
		{"`x${count}y${n}`", `'x0ynull'`},
		{"`${[1, 2]}`", `'1,2'`},
		{`count ? "y" : "n"`, `'n'`},
		{`q ? "y" : "n"`, `'y'`},
		{`String(12.50)`, `'12.5'`},
		{`Number("12px")`, `NaN`},
		{`parseInt("12px")`, `12`},
		{`parseFloat("3.5e2x")`, `350`},
		{`Math.max(1, 3, 2)`, `3`},
		{`Math.round(2.5)`, `3`},
		{`Math.round(-2.5)`, `-2`},
		{`(1.005).toFixed(2)`, `'1.00'`},
		{`(2.5).toFixed(0)`, `'3'`},
		{`(1234.5678).toFixed(1)`, `'1234.6'`},
		{`JSON.stringify({a: [1, "x", null, undefined], b: undefined})`, `'{"a":[1,"x",null,null]}'`},
		{`[1, null, undefined, 2] + ""`, `'1,,,2'`},
		{`{} + ""`, `'[object Object]'`},
		{`Array.isArray(items)`, `true`},
		{`Array.from({length: 3}, (_, i) => i * 2)`, `[0,2,4]`},
		{`[...items.map(i => i.v), 4]`, `[3,1,2,4]`},
		{`({...obj, a: 2}).a`, `2`},
		{`((a, b = 2) => a + b)(1)`, `3`},
		{`(({a, b: x = 5}) => a + x)({a: 1})`, `6`},
		{`(([a, , c]) => a + c)([1, 2, 3])`, `4`},
		{`((...r) => r.length)(1, 2, 3)`, `3`},
		{`(() => { const out = []; for (let i = 0; i < 3; i++) { out.push(i * i); } return out; })()`, `[0,1,4]`},
		{`(() => { let s = 0; for (const i of [1, 2, 3]) { if (i === 2) continue; s += i; } return s; })()`, `4`},
		{`items.length`, `3`},
		{`name.length`, `3`},
		{`name[0]`, `'b'`},
		{`"0" == false`, `true`},
		{`0x1F + 0b11 + 0o7 + 1_000`, `1041`},
		{`1e21`, `1e+21`},
	}
	for _, c := range cases {
		v := evalSrc(t, c.src)
		got, ok := JSSource(v)
		if !ok {
			t.Errorf("%s: got non-literal %#v, want %s", c.src, v, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("%s = %s, want %s", c.src, got, c.want)
		}
	}
}

func TestInterpUnknownAndUnsupported(t *testing.T) {
	dynamic := []string{
		`someGlobal`,
		`someGlobal + 1`,
		`someGlobal && "x"`,
		`someGlobal ? 1 : 2`,
		`items.map(i => someGlobal)[0]`,
		`items.filter(i => someGlobal)`,
	}
	for _, src := range dynamic {
		if v := evalSrc(t, src); !v.IsUnknown() || v.IsUnsupported() {
			t.Errorf("%s: want a Dynamic unknown, got %#v", src, v)
		}
	}
	unsupported := []string{
		`n.x`,
		`undefined.y`,
		`"a".replace(/a/, "b")`,
		`count.toString(2)`,
		`(1)()`,
	}
	for _, src := range unsupported {
		if v := evalSrc(t, src); !v.IsUnsupported() {
			t.Errorf("%s: want Unsupported, got %#v", src, v)
		}
	}
	// Short-circuit operators decide without evaluating an unknown side.
	if v := evalSrc(t, `count && someGlobal`); v.IsUnknown() {
		t.Errorf("0 && x must short-circuit, got %#v", v)
	}
}

func TestReverseDoesNotMutate(t *testing.T) {
	in := New(Hooks{})
	arr := NewArray(Number(1), Number(2))
	in.Scope.Define("a", arr)
	in.Eval(ParseExprSource(`a.reverse()`))
	if src, _ := JSSource(arr); src != "[1,2]" {
		t.Errorf("reverse mutated the source array: %s", src)
	}
}

func TestLegacyRoundTrip(t *testing.T) {
	cases := map[string]string{
		"true":      "true",
		"null":      "null",
		"12":        "12",
		"1e+21":     "1e+21",
		"012":       "'012'",
		"1.50":      "'1.50'",
		"[1,'a']":   "[1,'a']",
		"{a:1}":     "{a:1}",
		"hello":     "'hello'",
		"[not json": "'[not json'",
	}
	for in, want := range cases {
		got, _ := JSSource(FromLegacy(in))
		if got != want {
			t.Errorf("FromLegacy(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFromJSONPreservesOrder(t *testing.T) {
	v, ok := FromJSON(`{"z":1,"a":[true,null,"s"],"m":{"k":2.5}}`)
	if !ok {
		t.Fatal("FromJSON failed")
	}
	if got, _ := JSSource(v); got != `{z:1,a:[true,null,'s'],m:{k:2.5}}` {
		t.Errorf("FromJSON = %s", got)
	}
}
