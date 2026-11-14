package jsvalue

import (
	"testing"
)

// evalSrc evaluates a JS expression with a few bindings in scope.
func evalSrc(t *testing.T, src string) Value { return evalSrcVolatile(t, src, false) }

// evalSrcVolatile is evalSrc with the build environment (clock, time zone,
// locale) readable or not, as for text baked into HTML or for a constant.
func evalSrcVolatile(t *testing.T, src string, volatile bool) Value {
	t.Helper()
	expr := ParseExprSource(src)
	if expr == nil {
		t.Fatalf("parse %q failed", src)
	}
	in := New(Hooks{})
	in.Volatile = volatile
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
		{`123n`, `123n`},
		{`0xFFn`, `255n`},
		{`typeof 1n`, `'bigint'`},
		{"`${10n}`", `'10'`},
		{`1n === 1n`, `true`},
		{`(obj?.a)`, `1`},
		{`2 ** 3 ** 2`, `512`},
		{`(() => { let x = 2; x **= 3; return x; })()`, `8`},
		{`(() => { let a = 0, b = null, c = 1; a ||= 5; b ??= 6; c &&= 7; return [a, b, c]; })()`, `[5,6,7]`},
		{`(() => { const o = {}; o.k ??= "v"; return o.k; })()`, `'v'`},
		{`(({a = 1, b: {c = 2} = {}}) => a + c)({})`, `3`},
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
