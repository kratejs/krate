package parser

import (
	"testing"

	"krate-compiler/internal/lexer"
)

func testParse(t *testing.T, name, src string) {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := New(tokens)
	p.Filename = "test.tsx"
	_ = p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("FAIL %s: %v", name, e)
		}
	} else {
		t.Logf("OK   %s", name)
	}
}

func TestEdgeCases(t *testing.T) {
	tests := []struct{ name, src string }{
		{"hex", "const x = 0xFF;"},
		{"octal", "const x = 0o77;"},
		{"binary", "const x = 0b1010;"},
		{"sci", "const x = 1e10;"},
		{"arr-spread", "const x = [...a, b];"},
		{"for-in", "for (const k in obj) { }"},
		{"for-of", "for (const x of items) { }"},
		{"typeof", "const x = typeof y;"},
		{"void", "const x = void 0;"},
		{"instanceof", "const x = a instanceof B;"},
		{"in-op", "const x = 'key' in obj;"},
		{"label", "outer: for (let i = 0; i < 10; i++) { }"},
		{"comma", "x = (1, 2, 3);"},
		{"exp-op", "const x = 2 ** 10;"},
		{"obj-destr", "const { a, b } = obj;"},
		{"handler", "export default function F() { const [v, setV] = createSignal(0); return <button onClick={() => { setV(0); }}>x</button>; }"},
		{"complex-jsx", "export default function F() { return <div><button onClick={() => { const x = 1; setVal(x); }}>ok</button></div>; }"},
		{"nested-tern", "const x = a ? b ? c : d : e;"},
		{"opt-chain-call", "const x = a?.b?.c;"},
		{"computed-opt", "const x = a?.[0];"},
		{"chained-call", "const x = foo(bar(baz()));"},
		{"rest-param", "function f(...args) { }"},
		{"default-param", "function f(x = 10) { }"},
		{"async-arrow", "const f = async () => 42;"},
		{"multi-var", "const a = 1, b = 2, c = 3;"},
		{"nullish-assoc", "x ??= 1;"},
		{"logical-assoc", "x &&= 1;"},
		{"or-assoc", "x ||= 1;"},
		{"bitwise-not", "const x = ~a;"},
		{"xor", "const x = a ^ b;"},
		{"shift-left", "const x = a << 2;"},
		{"shift-right", "const x = a >> 2;"},
		{"ushift-right", "const x = a >>> 2;"},
		{"bitwise-and", "const x = a & b;"},
		{"bitwise-or", "const x = a | b;"},
		{"for-of-complex", "for (const [key, val] of entries) { }"},
		{"obj-destr-param", "function f({ a, b }) { return a + b; }"},
		{"nested-obj-destr", "const { a: { b } } = obj;"},
		{"empty-array", "const x = [];"},
		{"empty-obj", "const x = {};"},
		{"nested-arr", "const x = [[1, 2], [3, 4]];"},
		{"iife", "const x = (() => 42)();"},
		{"chained-method", "const x = [1,2,3].filter(n => n > 1).map(n => n * 2);"},
		{"switch-nested", "switch(x) { case 1: switch(y) { case 2: break; } break; }"},
		{"try-finally", "try { x(); } finally { y(); }"},
		{"try-catch-finally", "try { x(); } catch(e) { y(); } finally { z(); }"},
		{"do-while", "do { x++; } while (x < 10);"},
		{"complex-condition", "if (a > 0 && b < 10 || c === null) { }"},
		{"ternary-complex", "const x = a > 0 ? (b < 10 ? 'low' : 'high') : 'zero';"},
		{"array-of-objs", "const x = [{ a: 1 }, { b: 2 }];"},
		{"nested-arrow", "const f = (a) => (b) => (c) => a + b + c;"},
		{"multi-param", "const f = (a, b, c) => a + b + c;"},
		{"member-expr", "const x = obj.foo.bar.baz;"},
		{"computed-member", "const x = obj['foo']['bar'];"},
		{"mixed-member", "const x = obj.foo['bar'].baz;"},
		{"new-no-args", "const x = new Foo;"},
		{"new-with-args", "const x = new Foo(1, 2, 3);"},
		{"new-nested", "const x = new Foo(new Bar());"},
		{"this-member", "const x = this.value;"},
		{"await-complex", "const x = await fetch(url);"},
		{"regex-literal", "const x = /test/gi;"},
		{"bool-literal", "const a = true; const b = false;"},
		{"null-literal", "const x = null;"},
		{"undefined-literal", "const x = undefined;"},
		{"import-default", "import Foo from './foo';"},
		{"import-named", "import { a, b } from './mod';"},
		{"import-namespace", "import * as ns from './mod';"},
		{"import-side-effect", "import './styles.css';"},
		{"export-default-fn", "export default function Foo() {}"},
		{"export-named", "export { a, b };"},
		{"export-const", "export const x = 1;"},
		{"export-function", "export function foo() {}"},
		{"export-async", "export async function foo() {}"},
		{"jsx-nested", "export default function F() { return <div><span>a</span><span>b</span></div>; }"},
		{"jsx-expr-child", "export default function F() { return <div>{1 + 2}</div>; }"},
		{"jsx-fragment", "export default function F() { return <><div>a</div><div>b</div></>; }"},
		{"jsx-bool-attr", "export default function F() { return <input disabled />; }"},
		{"jsx-attr-expr", "export default function F() { return <div class={dynamic}>x</div>; }"},
		{"jsx-spread-attr", "export default function F() { return <div {...props}>x</div>; }"},
		{"jsx-self-closing", "export default function F() { return <br />; }"},
		{"jsx-component", "function Child(p) { return <span>{p.name}</span>; } export default function F() { return <Child name='test' />; }"},
		{"jsx-component-children", "function Wrapper(p) { return <div>{p.children}</div>; } export default function F() { return <Wrapper><span>hi</span></Wrapper>; }"},
		{"nested-tern-jsx", "export default function F() { return <div>{x ? <a/> : <b/>}</div>; }"},
		{"complex-handler-body", "export default function F() { const [v, sv] = createSignal(0); return <button onClick={() => { if (v() > 10) { sv(0); } else { sv(v() + 1); } }}>x</button>; }"},
		{"async-handler", "export default function F() { return <button onClick={async () => { await fetch('/api'); }}>x</button>; }"},
		{"for-with-var", "for (var i = 0; i < 10; i++) { }"},
		{"for-empty", "for (;;) { break; }"},
		{"while-complex", "while (arr.length > 0) { arr.pop(); }"},
		{"switch-no-default", "switch(x) { case 1: break; case 2: break; }"},
		{"switch-fallthrough", "switch(x) { case 1: doSomething(); case 2: doOther(); break; }"},
		{"obj-method", "const obj = { method() { return 1; } };"},
		{"obj-computed-key", "const obj = { ['key']: 1 };"},
		{"arr-destr-default", "const [a = 1, b = 2] = arr;"},
		{"arr-destr-rest", "const [a, ...rest] = arr;"},
		{"deeply-nested-parens", "const x = (((1 + 2)));"},
		{"chained-ternary", "const x = a ? b : c ? d : e ? f : g;"},
		{"regex-after-return", "function f() { return /test/g; }"},
		{"regex-after-comma", "const a = 1, re = /test/;"},
		{"member-call-chain", "const x = obj.method().prop.func();"},
		{"conditional-optional", "const x = a?.b ?? c;"},
		{"nullish-chain", "const x = a ?? b ?? c ?? d;"},
		{"and-chain", "const x = a && b && c;"},
		{"or-chain", "const x = a || b || c;"},
		{"mixed-logical", "const x = a || b && c ?? d;"},
		{"increment-decrement", "i++; i--; ++i; --i;"},
		{"unary-plus-minus", "const x = +a; const y = -b;"},
		{"not-chain", "const x = !!a;"},
		{"typed-array-init", "const results: number[] = [];"},
		{"typed-array-no-init", "const results: number[];"},
		{"typed-object-init", "const data: { items: number[] } = { items: [] };"},
		{"typed-union", "const x: string | number = 'hello';"},
		{"typed-generic", "const x: Array<string> = [];"},
		{"typed-fn-param", "function f(x: string, y: number[]) { }"},
		{"typed-arrow-param", "const f = (x: string) => x.length;"},
		{"template-simple", "const x = `hello world`;"},
		{"template-expr", "const x = `hello ${name}`;"},
		{"template-multi-expr", "const x = `${a} and ${b}`;"},
		{"template-nested-expr", "const x = `${a ? b : c}`;"},
		{"template-jsx-attr", "export default function F() { return <div title={`hello ${name}`}>x</div>; }"},
		{"template-jsx-no-interp", "export default function F() { return <div title={`hello world`}>x</div>; }"},
		{"template-jsx-multi", "export default function F() { return <Layout sidebar={`<a>link</a>`} toc={`<h2>toc</h2>`}>x</Layout>; }"},
		{"template-jsx-child", "export default function F() { return <div>{`static content`}</div>; }"},
		{"template-tagged", "const x = html`<div>${value}</div>`;"},
		{"template-multiline", "const x = `\n  line1\n  line2\n`;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testParse(t, tt.name, tt.src)
		})
	}
}
