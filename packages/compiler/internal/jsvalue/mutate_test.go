package jsvalue

import (
	"strings"
	"testing"
)

// runBody evaluates `(() => { body })()` with in-place methods enabled.
func runBody(t *testing.T, body string, mut func(in *Interp)) Value {
	t.Helper()
	expr := ParseExprSource("(() => {" + body + "})()")
	if expr == nil {
		t.Fatalf("parse failed: %s", body)
	}
	in := New(Hooks{})
	in.InPlace = true
	if mut != nil {
		mut(in)
	}
	return in.Eval(expr)
}

func TestInterpLabelsAndFlow(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"break outer", `const o = []; outer: for (let i = 0; i < 3; i++) { for (let j = 0; j < 3; j++) { if (j === 2) continue outer; if (i === 2) break outer; o.push(i * 10 + j); } } return o;`, `[0,1,10,11]`},
		{"labelled block", `let x = 0; blk: { x = 1; if (x) break blk; x = 2; } return x;`, `1`},
		{"continue in while", `let i = 0, s = 0; while (i < 5) { i++; if (i % 2) continue; s += i; } return s;`, `6`},
		{"break in switch inside loop", `const o = []; for (const k of [1, 2, 3]) { switch (k) { case 2: break; default: o.push(k); } } return o;`, `[1,3]`},
		{"continue in switch inside loop", `const o = []; for (const k of [1, 2, 3]) { switch (k) { case 2: continue; } o.push(k); } return o;`, `[1,3]`},
		{"return in loop", `for (const k of [1, 2, 3]) { if (k === 2) return k * 10; } return -1;`, `20`},
		{"do while", `let n = 0; do { n += 2; } while (n < 5); return n;`, `6`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runBody(t, tc.body, nil)
			src, ok := JSSource(got)
			if !ok || strings.ReplaceAll(src, " ", "") != tc.want {
				t.Fatalf("got %v (%q), want %s", got.Reason(), src, tc.want)
			}
		})
	}
}

func TestInterpInPlaceMethods(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"sort in place", `const a = [3, 1, 2]; a.sort(); return a;`, `[1,2,3]`},
		{"sort comparator", `const a = [1, 3, 2]; a.sort((x, y) => y - x); return a;`, `[3,2,1]`},
		{"reverse in place", `const a = [1, 2, 3]; a.reverse(); return a;`, `[3,2,1]`},
		{"pop shift", `const a = [1, 2, 3]; const l = a.pop(); const f = a.shift(); return [l, f, a];`, `[3,1,[2]]`},
		{"unshift", `const a = [3]; const n = a.unshift(1, 2); return [n, a];`, `[3,[1,2,3]]`},
		{"splice remove", `const a = [1, 2, 3, 4]; const r = a.splice(1, 2); return [r, a];`, `[[2,3],[1,4]]`},
		{"splice insert", `const a = [1, 4]; a.splice(1, 0, 2, 3); return a;`, `[1,2,3,4]`},
		{"splice negative start", `const a = [1, 2, 3]; a.splice(-1); return a;`, `[1,2]`},
		{"index assignment grows", `const a = [1]; a[2] = 3; return a.length;`, `3`},
		{"delete", `const o = { a: 1, b: 2 }; delete o.a; return Object.keys(o);`, `['b']`},
		{"alias sees a change", `const a = [1]; const b = a; b.push(2); return a;`, `[1,2]`},
		{"map and set", `const m = new Map(); m.set("a", 1).set("b", 2); const s = new Set([1]); s.add(2); s.delete(1); return [m.size, [...m.keys()], [...s]];`, `[2,['a','b'],[2]]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runBody(t, tc.body, nil)
			src, ok := JSSource(got)
			if !ok || strings.ReplaceAll(src, " ", "") != tc.want {
				t.Fatalf("got %v (%q), want %s", got.Reason(), src, tc.want)
			}
		})
	}
}

// Without InPlace the values may be shared, so sort and reverse return a
// changed copy and the others are left to the browser.
func TestInterpWithoutInPlaceKeepsReceivers(t *testing.T) {
	got := runBody(t, `const a = [3, 1, 2]; const s = a.sort(); return [a, s];`, func(in *Interp) { in.InPlace = false })
	if src, _ := JSSource(got); strings.ReplaceAll(src, " ", "") != `[[3,1,2],[1,2,3]]` {
		t.Fatalf("got %q", src)
	}
	got = runBody(t, `const a = [1]; a.pop(); return a;`, func(in *Interp) { in.InPlace = false })
	if !got.IsUnsupported() {
		t.Fatalf("pop without InPlace should be unsupported, got %v", got)
	}
}

func TestInterpLoopLimits(t *testing.T) {
	got := runBody(t, `let n = 0; for (let i = 0; i < 50; i++) n++; return n;`, func(in *Interp) { in.LoopLimit = 10 })
	if !got.IsUnsupported() || !strings.Contains(got.Reason(), "loop iteration limit exceeded (10 iterations in total)") {
		t.Fatalf("total limit: got %v", got.Reason())
	}
	got = runBody(t, `let n = 0; for (let i = 0; i < 20000; i++) n++; return n;`, nil)
	if !got.IsUnsupported() || !strings.Contains(got.Reason(), "(10000 iterations)") {
		t.Fatalf("default cap: got %v", got.Reason())
	}
	got = runBody(t, `let n = 0; for (let i = 0; i < 20000; i++) n++; return n;`, func(in *Interp) { in.LoopCap = 30000 })
	if n, _ := JSSource(got); n != "20000" {
		t.Fatalf("raised cap: got %v %q", got.Reason(), n)
	}
}

func TestClonerKeepsAliasingAndIsolates(t *testing.T) {
	orig, ok := ParseLiteralSource(`{ list: [{ n: 1 }], other: null }`)
	if !ok {
		t.Fatal("parse")
	}
	first := orig.obj.props["list"].arr.Elems[0]
	orig.obj.props["other"] = first // two names for one object
	c := NewCloner()
	dup := c.Clone(orig)
	dup.obj.props["list"].arr.Elems[0].Set("n", Number(9))
	if v, _ := first.Get("n"); v.ToNumber() != 1 {
		t.Fatal("the original changed")
	}
	if v, _ := dup.obj.props["other"].Get("n"); v.ToNumber() != 9 {
		t.Fatal("the two names no longer share one object")
	}
	if !DeepEqual(orig, NewCloner().Clone(orig)) {
		t.Fatal("a fresh copy is not DeepEqual to its source")
	}
	if DeepEqual(orig, dup) {
		t.Fatal("a changed copy is DeepEqual to the source")
	}
	m := NewMap()
	m.coll.put(first, Number(1))
	mc := NewCloner().Clone(m)
	if _, ok := mc.coll.lookup(first); ok {
		t.Fatal("the copied Map is keyed by the original object")
	}
	if mc.coll.size != 1 {
		t.Fatalf("copied Map size = %d", mc.coll.size)
	}
}
