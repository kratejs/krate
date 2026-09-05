package parser

import (
	"testing"
)

// TestSyntaxRobustness verifies that a broad set of syntactically valid
// TypeScript/TSX constructs parse without producing any errors. This is the
// safety net that guarantees users can write idiomatic TypeScript and the
// compiler will not reject or mangle it. Every entry here must parse cleanly.
func TestSyntaxRobustness(t *testing.T) {
	tests := []struct{ name, src string }{
		// ── `in` / `instanceof` as binary operators ──
		{"in-operator-if", "if (k in obj) { out.push(obj[k]); }"},
		{"in-operator-const", "const x = 'key' in obj;"},
		{"in-operator-loop-body", "for (const k of keys) { if (k in obj) { out.push(obj[k]); } }"},
		{"instanceof-operator", "const x = [] instanceof Array;"},
		{"instanceof-negative", "const x = !(a instanceof B);"},
		{"instanceof-complex", "const x = obj instanceof Foo && obj.bar instanceof Baz;"},

		// ── Optional parameters ──
		{"optional-param", "function f(x?: string) { return x; }"},
		{"optional-param-default", "function f(x?: string, y: number = 1) { return x; }"},
		{"optional-param-rest", "function f(x?: string, ...rest: number[]) { return x; }"},

		// ── Arrow function return type annotations ──
		{"arrow-return-type", "const f = (x: string): number => x.length;"},
		{"arrow-return-type-obj", "const f = (x: string): { n: number } => ({ n: x.length });"},
		{"arrow-return-type-array", "const f = (x: string[]): string[] => x;"},
		{"arrow-return-type-void", "const f = (x: string): void => { console.log(x); };"},
		{"arrow-return-type-promise", "const f = (): Promise<void> => Promise.resolve();"},

		// ── Numbers (all literal forms) ──
		{"hex", "const x = 0xFF;"},
		{"octal", "const x = 0o77;"},
		{"binary", "const x = 0b1010;"},
		{"scientific", "const x = 1e10;"},
		{"negative-exponent", "const x = 1.5e-3;"},
		{"leading-dot", "const x = .5;"},
		{"trailing-dot", "const x = 5.;"},
		{"bigint", "const x = 123n;"},

		// ── Strings ──
		{"double-quote-escaped", `const x = "tab\tnewline\nslash\\done";`},
		{"single-quote-escaped", `const x = 'it\'s a test';`},
		{"unicode-escape", `const x = "caf\u00e9 \u{1F600}";`},

		// ── Template literals ──
		{"template-simple", "const x = `hello`;"},
		{"template-expr", "const x = `${name} here`;"},
		{"template-nested-expr", "const x = `${a ? b : c}`;"},
		{"template-multiline", "const x = `\n  line1\n  ${v}\n`;"},

		// ── Type annotations ──
		{"typed-array", "const x: number[] = [];"},
		{"typed-union", "const x: string | number = 'a';"},
		{"typed-generic", "const x: Array<string> = [];"},
		{"typed-object", "const x: { items: number[] } = { items: [] };"},
		{"typed-fn-params", "function f(x: string, y: number[]) { }"},
		{"typed-record", "function f(x: Record<string, string>) { }"},
		{"typed-arrow-param", "const f = (x: string): number => x.length;"},

		// ── Logical / nullish / comparison ──
		{"logical-and", "const x = a && b && c;"},
		{"logical-or", "const x = a || b || c;"},
		{"nullish-chain", "const x = a ?? b ?? c ?? 'd';"},
		{"mixed-logical", "const x = a || b && c ?? d;"},

		// ── Bitwise / shifts / unary ──
		{"bitwise-not", "const x = ~a;"},
		{"xor", "const x = a ^ b;"},
		{"shift-left", "const x = a << 2;"},
		{"shift-right", "const x = a >> 2;"},
		{"ushift-right", "const x = a >>> 2;"},
		{"bitwise-and", "const x = a & b;"},
		{"bitwise-or", "const x = a | b;"},
		{"not-not", "const x = !!a;"},

		// ── Assignment operators ──
		{"add-assign", "x += 1;"},
		{"mult-assign", "x *= 2;"},
		{"mod-assign", "x %= 2;"},

		// ── Destructuring ──
		{"array-destructure", "const [a, b, ...rest] = arr;"},
		{"array-destructure-default", "const [a = 1, b = 2] = arr;"},
		{"object-destructure", "const { a, b } = obj;"},
		{"nested-object-destructure", "const { a: { b } } = obj;"},
		{"object-destructure-rename", "const { a: renamed } = obj;"},
		{"destructure-param", "function f({ a, b }) { return a + b; }"},
		{"for-of-destructure", "for (const [key, val] of entries) { }"},

		// ── Spread / rest ──
		{"array-spread", "const x = [1, 2, ...[3, 4], 5];"},
		{"object-spread", "const x = { ...a, ...b };"},
		{"rest-param", "function f(...args: number[]) { }"},

		// ── Optional chaining ──
		{"optional-chain", "const x = a?.b?.c;"},
		{"optional-chain-call", "const x = a?.b?.();"},
		{"optional-chain-index", "const x = a?.[0];"},
		{"optional-nullish", "const x = a?.b ?? c;"},

		// ── Control flow ──
		{"for-in", "for (const k in obj) { }"},
		{"for-of", "for (const x of items) { }"},
		{"do-while", "do { x++; } while (x < 10);"},
		{"while", "while (x < 10) { x++; }"},
		{"label", "outer: for (let i = 0; i < 3; i++) { continue outer; }"},
		{"switch-fallthrough", "switch(x) { case 1: f(); case 2: g(); break; default: h(); }"},
		{"try-catch-finally", "try { a(); } catch (e) { b(); } finally { c(); }"},
		{"nested-try", "try { try { a(); } finally { } } catch (e) { }"},

		// ── Functions / arrows ──
		{"function-decl", "function f(a: number, b: number): number { return a + b; }"},
		{"function-default-param", "function f(x = 10) { return x; }"},
		{"arrow-no-params", "const f = () => 42;"},
		{"arrow-single-param", "const f = x => x * 2;"},
		{"arrow-block", "const f = (x) => { const y = x * 2; return y; };"},
		{"arrow-object-return", "const f = () => ({ a: 1 });"},
		{"arrow-nested", "const f = (a) => (b) => a + b;"},
		{"arrow-async", "const f = async () => { await fetch('/x'); };"},
		{"async-arrow-params", "const f = async (x: string): Promise<string> => { return await Promise.resolve(x); };"},
		{"async-effect", "createEffect(async () => { const res = await Promise.resolve('v'); setMsg(res); });"},

		// ── Objects / arrays ──
		{"object-method", "const o = { method() { return 1; } };"},
		{"object-computed-key", "const o = { ['key']: 1 };"},
		{"object-shorthand", "const o = { a, b };"},
		{"nested-array", "const x = [[1, 2], [3, 4]];"},
		{"array-of-objects", "const x = [{ a: 1 }, { b: 2 }];"},

		// ── new / this / chains ──
		{"new-no-args", "const x = new Foo;"},
		{"new-with-args", "const x = new Foo(1, 2);"},
		{"new-nested", "const x = new Foo(new Bar());"},
		{"this-member", "const x = this.value;"},
		{"member-call-chain", "const x = obj.method().prop.func();"},
		{"chained-methods", "const x = [1,2,3].filter(n => n > 1).map(n => n * 2);"},
		{"iife", "const x = (() => 42)();"},

		// ── typeof / void / delete ──
		{"typeof", "const x = typeof y;"},
		{"void", "const x = void 0;"},
		{"delete", "const x = delete obj.key;"},

		// ── Comma / precedence ──
		{"comma", "x = (1, 2, 3);"},
		{"precedence", "const x = 2 + 3 * 4 - 1;"},
		{"paren-override", "const x = (2 + 3) * 4;"},
		{"nested-parens", "const x = (((1 + 2)));"},
		{"ternary-complex", "const x = a ? b ? c : d : e;"},
		{"chained-ternary", "const x = a ? b : c ? d : e ? f : g;"},

		// ── Multiple declarations ──
		{"multi-var", "const a = 1, b = 2, c = a + b;"},
		{"multi-let", "let a = 1, b = 2;"},

		// ── Identifiers (edge naming) ──
		{"camel-case", "const camelCase = 1;"},
		{"pascal-case", "const PascalCase = 1;"},
		{"snake-case", "const snake_case = 1;"},
		{"upper-case", "const UPPER_CASE = 1;"},
		{"dollar-ident", "const $dollar = 1;"},
		{"underscore-ident", "const _under = 1;"},

		// ── Imports / exports ──
		{"import-default", "import Foo from './foo';"},
		{"import-named", "import { a, b } from './mod';"},
		{"import-renamed", "import { a as b } from './mod';"},
		{"import-namespace", "import * as ns from './mod';"},
		{"import-type", "import type { Foo } from './mod';"},
		{"import-side-effect", "import './styles.css';"},
		{"export-default-fn", "export default function Foo() {}"},
		{"export-named", "export { a, b };"},
		{"export-const", "export const x = 1;"},
		{"export-function", "export function foo() {}"},
		{"export-async", "export async function foo() {}"},
		{"bare-async-fn", "async function load() { return await fetch('/api'); }"},
		{"bare-async-fn-jsx", "async function Slow() { return <div class=\"slow\">Slow loaded</div>; } export default function F() { return <Slow />; }"},

		// ── TSX / JSX ──
		{"jsx-nested", "export default function F() { return <div><span>a</span><span>b</span></div>; }"},
		{"jsx-expr-child", "export default function F() { return <div>{1 + 2}</div>; }"},
		{"jsx-fragment", "export default function F() { return <><div>a</div><div>b</div></>; }"},
		{"jsx-bool-attr", "export default function F() { return <input disabled />; }"},
		{"jsx-attr-expr", "export default function F() { return <div class={dynamic}>x</div>; }"},
		{"jsx-spread-attr", "export default function F() { return <div {...props}>x</div>; }"},
		{"jsx-self-closing", "export default function F() { return <br />; }"},
		{"jsx-component", "function Child(p) { return <span>{p.name}</span>; } export default function F() { return <Child name='test' />; }"},
		{"jsx-component-children", "function Wrapper(p) { return <div>{p.children}</div>; } export default function F() { return <Wrapper><span>hi</span></Wrapper>; }"},
		{"jsx-nested-ternary", "export default function F() { return <div>{x ? <a/> : <b/>}</div>; }"},
		{"jsx-template-attr", "export default function F() { return <div title={`hello ${name}`}>x</div>; }"},
		{"jsx-handler", "export default function F() { const [v, sv] = createSignal(0); return <button onClick={() => { sv(v() + 1); }}>x</button>; }"},
		{"jsx-spread-plus-attr", "export default function F() { return <div {...{ class: 'c', 'data-x': '1' }} />; }"},

		// ── Generic type parameters / assertions ──
		{"generic-fn-decl-simple", "function id<T>(x: T): T { return x; }"},
		{"generic-fn-decl-extends", "function id<T extends object>(x: T): T { return x; }"},
		{"generic-fn-decl-multi", "function pick<K extends string, V>(k: K, v: V): V { return v; }"},
		{"generic-fn-decl-default", "function noop<T = string>() { return null; }"},
		{"angle-bracket-cast-primitive", "const cast = <string>generic({ x: 1 }).x;"},
		{"angle-bracket-cast-union", "const v = <number | string>raw;"},
		{"as-const-in-array-destructure", "const [a, , c = 'd' as const] = tup;"},
		{"satisfies-operator", "const lvl = Level.High satisfies Level;"},

		// ── type-only imports / exports ──
		{"import-type-named", "import type { FC } from 'krate';"},
		{"import-type-default", "import type Foo from 'foo';"},
		{"import-type-namespace", "import type * as Types from 'types';"},

		// ── class members ──
		{"class-fields", "class Counter { label = 'c'; static kind = 'k'; }"},
		{"class-getter", "class Counter { get value() { return this.label; } }"},
		{"class-method-this-param", "class Counter { method(this: Counter): string { return 'm'; } }"},
		{"class-method-generic", "class Box { wrap<T>(v: T): T { return v; } }"},
		{"class-new-fields", "export default function F() { const c = new Counter(); c.label = 'x'; return <div>{c.label}</div>; }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testParse(t, tt.name, tt.src)
		})
	}
}
