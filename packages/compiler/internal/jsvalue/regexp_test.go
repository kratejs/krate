package jsvalue

import (
	"strings"
	"testing"
)

func TestRegExpBasicsAndProperties(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// Literals, canonical flags, properties and serialization
		{`/abc/gi.source`, `'abc'`},
		{`/abc/ig.flags`, `'gi'`},
		{`/abc/ig.toString()`, `'/abc/gi'`},
		{`String(/abc/gi)`, `'/abc/gi'`},
		{`/abc/g.global`, `true`},
		{`/abc/g.ignoreCase`, `false`},
		{`/abc/imsuy.ignoreCase && /abc/imsuy.multiline && /abc/imsuy.dotAll && /abc/imsuy.unicode && /abc/imsuy.sticky`, `true`},
		{`/abc/d.hasIndices`, `true`},
		{`typeof /abc/`, `'object'`},
		{`/abc/ instanceof RegExp`, `true`},
		{`"abc" instanceof RegExp`, `false`},
		{`/a/ === /a/`, `false`},
		{`(() => { const r = /a/; return r === r; })()`, `true`},
		{`JSON.stringify(/abc/g)`, `'{}'`},
		{`Boolean(/a/)`, `true`},
		{`+{...(/a/g)}`, `NaN`},
		{`Object.keys(/a/g)`, `[]`},

		// Constructor forms (new RegExp vs RegExp)
		{`new RegExp().toString()`, `'/(?:)/'`},
		{`new RegExp("a/b", "g").toString()`, `'/a\\/b/g'`},
		{`new RegExp(undefined, "i").toString()`, `'/(?:)/i'`},
		{`new RegExp(null).source`, `'null'`},
		{`(() => { const r = /foo/g; return RegExp(r) === r; })()`, `true`},
		{`(() => { const r = /foo/g; const c = new RegExp(r); return c !== r && c.source === "foo" && c.flags === "g"; })()`, `true`},
		{`(() => { const r = /foo/g; const c = new RegExp(r, "i"); return c.flags === "i" && c.source === "foo"; })()`, `true`},
		{`(() => { const r = /foo/g; const c = RegExp(r, ""); return c !== r && c.flags === ""; })()`, `true`},

		// JSSource / object storage
		{`({ a: /\b\w+\b/g, b: /\d+/ })`, `{a:/\b\w+\b/g,b:/\d+/}`},

		// test and exec
		{`/\d+/.test("item 42")`, `true`},
		{`/\d+/.test("none")`, `false`},
		{`"item 42".match(/(\d+)/)[1]`, `'42'`},
		{`"item 42".match(/(\d+)/).index`, `5`},
		{`"item 42".match(/(\d+)/).input`, `'item 42'`},
		{`"item 42".match(/(\d+)/).groups`, `undefined`},
		{`"a1 b22 c333".match(/\d+/g)`, `['1','22','333']`},
		{`"none".match(/\d+/g)`, `null`},
		{`"abc".match(/a(x)?/)[1]`, `undefined`},
		{`"abc".match(/a(x)?/).length`, `2`},

		// Named capture groups
		{`"2026-11-26".match(/(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})/).groups`, `{year:'2026',month:'11',day:'26'}`},
		{`(() => { const m = "a".match(/a(?<opt>b)?/); return [m.groups.opt === undefined, "opt" in m.groups]; })()`, `[true,true]`},

		// Indices flag (d)
		{`(() => { const m = "xx12yy".match(/(?<num>\d+)/d); return [m.indices[0], m.indices[1], m.indices.groups.num]; })()`, `[[2,4],[2,4],[2,4]]`},
		{`(() => { const m = "a".match(/a(b)?/d); return m.indices[1] === undefined; })()`, `true`},

		// JS-specific features that Go RE2 lacks: lookbehind, backreferences, sticky
		{`"price: $42".match(/(?<=\$)\d+/)[0]`, `'42'`},
		{`"a1 b2".match(/(?<!b)\d+/g)`, `['1']`},
		{`"go to the the store".match(/\b(\w+)\s+\1\b/)[0]`, `'the the'`},
		{`(() => { const r = /\w+/y; r.lastIndex = 4; const a = r.exec("foo bar"); const b = r.exec("foo bar"); return [a[0], a.index, b, r.lastIndex]; })()`, `['bar',4,null,0]`},

		// lastIndex state across exec / test loops
		{`(() => { const r = /\d+/g; const out = []; let m; while ((m = r.exec("x10 y20 z30"))) { out.push([m[0], m.index, r.lastIndex]); } return [out, r.lastIndex]; })()`, `[[['10',1,3],['20',5,7],['30',9,11]],0]`},
		{`(() => { const r = /a/; r.lastIndex = 5; const m = r.exec("a"); return [m[0], r.lastIndex]; })()`, `['a',5]`},
		{`(() => { const r = /\w+/g; r.lastIndex = 4; return [r.test("one two"), r.lastIndex, r.test("one two"), r.lastIndex]; })()`, `[true,7,false,0]`},

		// matchAll (iterator, preserves original lastIndex, respects initial lastIndex)
		{`[..."a1 b22".matchAll(/([a-z])(\d+)/g)].map(m => m[1] + ":" + m[2] + "@" + m.index)`, `['a:1@0','b:22@3']`},
		{`(() => { const r = /\d+/g; r.lastIndex = 3; const found = Array.from("10 20 30".matchAll(r), m => m[0]); return [found, r.lastIndex]; })()`, `[['20','30'],3]`},
		{`[..."a1 b2".matchAll("\\d")].map(m => m[0])`, `['1','2']`},

		// replace and replaceAll with string replacers
		{`"a-b-c".replace(/-/g, "_")`, `'a_b_c'`},
		{`"a-b-c".replace(/-/, "_")`, `'a_b-c'`},
		{`"2026-11".replace(/(?<y>\d{4})-(?<m>\d{2})/, "$<m>/$<y>")`, `'11/2026'`},
		{`"foo bar".replace(/(\w+) (\w+)/, "$2 $1 ($&)")`, `'bar foo (foo bar)'`},
		{`"a1b2".replaceAll(/\d/g, "#")`, `'a#b#'`},

		// replace and replaceAll with callback replacers
		{`"a1 b22".replace(/([a-z])(\d+)/g, (full, ch, num, off) => ch.toUpperCase() + (Number(num) * 2) + "@" + off)`, `'A2@0 B44@3'`},
		{`"2026-11".replace(/(?<y>\d{4})-(?<m>\d{2})/, (_m, _p1, _p2, _off, _s, g) => g.m + "-" + g.y)`, `'11-2026'`},
		{`"a".replace(/a/, () => "$&")`, `'$&'`},
		{`"hi 😀 12".replace(/\d+/g, (m, off) => m + "@" + off)`, `'hi 😀 12@6'`},
		{`"x1 y2".replaceAll(/\d/g, (d) => String(Number(d) + 10))`, `'x11 y12'`},

		// search
		{`"hello 42 world".search(/\d+/)`, `6`},
		{`"hello world".search(/\d+/)`, `-1`},
		{`(() => { const r = /\d+/g; r.lastIndex = 9; const idx = "a1b2".search(r); return [idx, r.lastIndex]; })()`, `[1,9]`},
		{`"hello 42".search("\\d+")`, `6`},

		// split
		{`"a, b,  c".split(/,\s*/)`, `['a','b','c']`},
		{`"a, b, c".split(/,\s*/, 2)`, `['a','b']`},
		{`"a, b, c".split(/,\s*/, 0)`, `[]`},
		{`"a1b".split(/(1)|(2)/).map(x => x === undefined ? "_" : x)`, `['a','1','_','b']`},

		// Map / Set key identity
		{`(() => { const r = /x/g; const m = new Map([[r, 1]]); return m.get(r); })()`, `1`},
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

func TestRegExpErrorsAndDiagnostics(t *testing.T) {
	cases := []struct {
		src     string
		wantSub string
	}{
		{`"abc".matchAll(/a/)`, "non-global RegExp"},
		{`"abc".replaceAll(/a/, "b")`, "non-global RegExp"},
		{`new RegExp("[")`, "SyntaxError"},
		{`new RegExp("a", "z")`, "SyntaxError"},
		{`"abc".startsWith(/a/)`, "must not be a regular expression"},
		{`"abc".includes(/a/)`, "must not be a regular expression"},
		{`"abc".endsWith(/a/)`, "must not be a regular expression"},
	}
	for _, c := range cases {
		v := evalSrc(t, c.src)
		if !v.IsUnsupported() {
			t.Errorf("%s: expected Unsupported, got %#v", c.src, v)
			continue
		}
		if !strings.Contains(v.Reason(), c.wantSub) {
			t.Errorf("%s: reason %q does not contain %q", c.src, v.Reason(), c.wantSub)
		}
	}
}

func TestRegExpToASTRoundTrip(t *testing.T) {
	v := NewRegExp(`\b(foo|bar)\b`, "gi")
	if !v.IsRegExp() {
		t.Fatalf("expected RegExp, got %#v", v)
	}
	if !v.ContainsRuntimeObject() {
		t.Fatalf("expected ContainsRuntimeObject to be true for RegExp")
	}
	expr := ToAST(v)
	if expr == nil {
		t.Fatalf("ToAST returned nil")
	}
	in := New(Hooks{})
	v2 := in.Eval(expr)
	if !v2.IsRegExp() || v2.ToString() != v.ToString() {
		t.Fatalf("round-trip mismatch: got %#v, want %#v", v2, v)
	}
}
