package parser_test

// shapeCases pairs a source with its expected astprint rendering. Each case
// pins the AST shape of one bug fixed in the parser hardening pass: the
// printed form shows exactly how the source was grouped and what survived.
var shapeCases = []struct {
	name, src, want string
}{
	// Keyword property names after `.` / `?.`, as keys and as members.
	{"member-keyword-delete", "cache.delete(k);", "cache.delete(k);"},
	{"member-keyword-many", "a.new.with.void.typeof.await.yield.this.true.class.default;", "a.new.with.void.typeof.await.yield.this.true.class.default;"},
	{"optional-keyword", "a?.delete;", "a?.delete;"},
	{"object-keyword-keys", "x = { new: 1, delete: 2, class: 3, default: 4 };", "x = { new: 1, delete: 2, class: 3, default: 4 };"},
	{"export-as-default", "const x = 1; export { x as default };", "const x = 1;\nexport { x as default };"},
	{"import-default-as", `import { default as y } from "m";`, `import { default as y } from "m";`},
	{"export-string-name", `export { "a-b" as x } from "m";`, `export { "a-b" as x } from "m";`},

	// new binds to the whole member chain.
	{"new-member-chain", "new a.b.C(1);", "new a.b.C(1);"},
	{"new-no-args", "new X;", "new X();"},
	{"new-paren-call", "new (foo())();", "new (foo())();"},
	{"new-then-member", "new a.b().c;", "new a.b().c;"},
	{"new-target", "function f() { return new.target; }", "function f() {\n  return new.target;\n}"},
	{"new-generic", "new Map<K, V>();", "new Map();"},
	{"new-new", "new new X()();", "new new X()();"},

	// Tagged templates.
	{"tagged", "css`a ${b}`;", "css`a ${b}`;"},
	{"tagged-member", "x.y`t`;", "x.y`t`;"},
	{"tagged-call", "f()`t`;", "f()`t`;"},

	// Prefix operators.
	{"unary-plus", "y = +a;", "y = +a;"},
	{"unary-all", "z = [+a, -a, !a, ~a, typeof a, void a, delete a.b, ++a, --a];", "z = [+a, -a, !a, ~a, typeof a, void a, delete a.b, ++a, --a];"},
	{"unary-nested", "z = - -a;", "z = - -a;"},
	{"await", "async function f() { await a; }", "async function f() {\n  await a;\n}"},

	// Object literal methods and accessors.
	{"obj-get-set", "o = { get x() { return 1; }, set x(v) {} };", "o = { get x() {\n  return 1;\n}, set x(v) {} };"},
	{"obj-async-gen", "o = { async f() {}, *g() {}, async *h() {} };", "o = { async f() {}, *g() {}, async *h() {} };"},
	{"obj-computed", "o = { [k]: 1, [m]() {}, get [g]() { return 1; } };", "o = { [k]: 1, [m]() {}, get [g]() {\n  return 1;\n} };"},
	{"obj-modifier-names", "o = { get: 1, set: 2, async, static: 3 };", "o = { get: 1, set: 2, async, static: 3 };"},

	// Class expressions and bodies.
	{"class-expr", "const A = class extends B {};", "const A = class extends B {};"},
	{"class-members", "class A { static #n = 1; #m() {} get x() { return 1; } set x(v) {} static async *g() {} static { init(); } accessor y = 2; [k] = 3; 'q'() {} f; }",
		"class A {\n  static #n = 1;\n  #m() {}\n  get x() {\n    return 1;\n  }\n  set x(v) {}\n  static async *g() {}\n  static {\n    init();\n  }\n  accessor y = 2;\n  [k] = 3;\n  \"q\"() {}\n  f;\n}"},
	{"class-private-in", "class A { #x; has(o) { return #x in o; } }", "class A {\n  #x;\n  has(o) {\n    return #x in o;\n  }\n}"},
	{"class-keyword-members", "class A { delete() {} new = 1; static default() {} }", "class A {\n  delete() {}\n  new = 1;\n  static default() {}\n}"},
	{"class-ts-implements", "class A implements I { m() {} }", "class A {\n  m() {}\n}"},
	{"class-ts-modifiers", "class A<T> extends B<T> { private readonly x: number = 1; public m(): void {} declare y: string; abstract z(): void; }", "class A extends B {\n  x = 1;\n  m() {}\n}"},

	// Statements.
	{"for-await", "async function f() { for await (const x of y) {} }", "async function f() {\n  for await (const x of y) {\n  }\n}"},
	{"labeled", "outer: for (;;) { break outer; }", "outer: for (; ; ) {\n  break outer;\n}"},
	{"using", "{ using x = a(); }", "{\n  using x = a();\n}"},
	{"await-using", "async function f() { await using x = a(); }", "async function f() {\n  await using x = a();\n}"},
	{"using-identifier", "using(x);", "using(x);"},
	{"import-attributes", `import x from "./a.json" with { type: "json" };`, `import x from "./a.json" with { type: "json" };`},
	{"export-attributes", `export { a } from "./a.json" with { type: "json" };`, `export { a } from "./a.json" with { type: "json" };`},
	{"dynamic-import-options", `import(x, { with: { type: "json" } });`, `import(x, { with: { type: "json" } });`},
	{"yield-star", "function* g() { yield* h(); yield; yield 1; }", "function* g() {\n  yield* h();\n  yield;\n  yield 1;\n}"},
	{"export-default-async-arrow", "export default async () => {};", "export default async () => {};"},
	{"catch-annotation", "try {} catch (e: unknown) {}", "try {\n} catch (e) {\n}"},
	{"catch-object-pattern", "try {} catch ({ message }) {}", "try {\n} catch ({ message }) {\n}"},
	{"catch-array-pattern", "try {} catch ([a]) {}", "try {\n} catch ([a]) {\n}"},
	{"if-empty-body", "if (x) ;", "if (x) {\n}"},
	{"destructuring-defaults", "const { a, d = 1, e: { f = 2 } = {}, ...r } = o;", "const { a, d = 1, e: { f = 2 } = {}, ...r } = o;"},
	{"for-in-var", "for (var k in o) {}", "for (var k in o) {\n}"},
	{"with", "with (o) { x; }", "with (o) {\n  x;\n}"},
	{"optional-chain-paren", "(a?.b).c;", "(a?.b).c;"},
	{"array-holes", "x = [, a, , b, ,];", "x = [, a, , b, ,];"},

	// Operators.
	{"exponent", "x = a ** b ** c;", "x = a ** b ** c;"},
	{"logical-assign", "a ||= b; a &&= c; a ??= d; a **= 2; a >>>= 1;", "a ||= b;\na &&= c;\na ??= d;\na **= 2;\na >>>= 1;"},

	// Lexer: regex, numbers, hashbang.
	{"regex-class-slash", "/[/]/.test(s);", "/[/]/.test(s);"},
	{"regex-v-flag", "x = /[\\p{L}--\\p{N}]/v;", "x = /[\\p{L}--\\p{N}]/v;"},
	{"inc-then-div", "x = a++ / 2;", "x = a++ / 2;"},
	{"regex-after-if", "if (x) /re/.test(y);", "if (x) {\n  /re/.test(y);\n}"},
	{"conditional-dot-number", "x = a?.5:1;", "x = a ? .5 : 1;"},
	{"hex-plus", "x = 0xE+1;", "x = 0xE + 1;"},
	{"hashbang", "#!/usr/bin/env node\nx;", "x;"},

	// TypeScript generics must not open JSX or swallow code.
	{"generic-arrow-comma", "const f = <T,>(x: T) => x;\nconst n = 1;", "const f = (x) => x;\nconst n = 1;"},
	{"generic-arrow-extends", "const f = <T extends X>(x: T) => x;", "const f = (x) => x;"},
	{"generic-async-arrow", "const f = async <T,>() => 1;", "const f = async () => 1;"},
	{"type-generic-fn", "type F = <T>(x: T) => T;\nconst n = 1;", "const n = 1;"},
	{"interface-call-signature", "interface I { <T>(a: T): T }\nconst n = 1;", "const n = 1;"},
	{"nested-generic-annotation", "const m: Map<string, Set<string>> = new Map(); const n = 1;", "const m = new Map();\nconst n = 1;"},
	{"generic-ge-split", "let a: Array<number>= [];", "let a = [];"},
	{"non-null-asi", "a\n!b();", "a;\n!b();"},
	{"non-null", "a!.b;", "a.b;"},
}
