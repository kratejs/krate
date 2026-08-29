import { createSignal, createEffect } from '@krate/runtime';
import { greet, sum, joinList, clamp, pick, DEFAULT_LIMIT, FRAMEWORK_NAME } from '@/lib/helpers';
import Badge from '@/components/ui/badge';

// ===== Multiple variable declarations (must be declared before use) =====
const a = 1, b = 2, c = a + b;

// ===== Number formats =====
const hexNum = 0xFF;
const octalNum = 0o77;
const binaryNum = 0b1010;
const floatNum = 3.14;
const expNum = 1e10;
const leadingDot = .5;
const trailingDot = 5.;
const bigintLiteral = 123n;

// ===== String forms =====
const doubleQuoted = "double \"quote\"";
const singleQuoted = 'single \'quote\'';
const escapedStr = "tab\tnewline\nslash\\done";
const unicodeStr = "caf\u00e9 \u{1F600}";
const templateLit = `Hello ${FRAMEWORK_NAME}!`;
const templateExpr = `${1 + 2} + ${3 * 4} = ${sum(1, 2, 3)}`;

// ===== Type annotations (must be skipped, not executed) =====
const typedArr: number[] = [1, 2, 3];
const typedUnion: string | number = "hello";
const typedGeneric: Array<string> = ["a", "b"];
const typedObj: { items: number[]; label: string } = { items: [1], label: "x" };
function typedFn(x: string, y?: number, ...rest: string[]): string { return x; }
const typedArrow = (x: string): number => x.length;

// ===== Logical / nullish / comparison =====
const logicalAnd = true && "truthy";
const logicalOr = false || "fallback";
const nullish = null ?? "default";
const nullishChain = a ?? b ?? c ?? "end";const eqStrict = 1 === 1;
const neqStrict = 1 !== 2;
const gte = 5 >= 5;
const lte = 3 <= 3;
const expOperator = Math.pow(2, 10);

// ===== Bitwise / unary =====
const bitwiseNot = ~5;
const xor = 5 ^ 3;
const shiftLeft = 1 << 4;
const shiftRight = 32 >> 2;
const ushift = -1 >>> 1;
const bitAnd = 6 & 3;
const bitOr = 4 | 1;
const notNot = !!1;
const negNum = -42;
const typeOfCheck = typeof "hello";
const voidResult = void 0;
const inCheck = "key" in { key: 1 };
const instanceCheck = [] instanceof Array;

// ===== Arrays & object literals =====
const items = ['apple', 'banana', 'cherry', 'date'];
const nested = { user: { name: "Bob", scores: [1, 2, 3] } };
const spreadObj = { ...nested, extra: "data" };
const spreadArr = [1, 2, ...[3, 4], 5];
const computed = { ["key" + "1"]: "value" };
const emptyArr: any[] = [];
const emptyObj = {};

// ===== Array methods =====
const filtered = items.filter(i => i.length > 5);
const mapped = items.map(i => i.toUpperCase());
const joined = items.join("-");
const totalLen = items.reduce((acc, i) => acc + i.length, 0);
const sorted = [...items].sort();
const sliced = items.slice(0, 2);

// ===== Function expressions =====
const arrowNoParam = () => 42;
const arrowOneParam = (x: number) => x * 2;
const arrowMulti = (x: number, y: number) => x + y;
const arrowBlock = (x: number) => { const doubled = x * 2; return doubled; };
const arrowObj = () => ({ key: "value", count: 1 });
const arrowNested = (a: number) => (b: number) => a + b;
const arrowDefault = (x: number = 10) => x + 1;
const arrowRest = (...args: any[]) => args.length;

// ===== Destructuring =====
const [first, second, ...rest] = items;
const { name: personName, age } = nested.user;
const [head = "default", ...tail] = items;

// ===== Optional chaining & computed =====
const maybeObj = { a: { b: { c: 42 } } };
const optChain = maybeObj?.a?.b?.c;
const optChainCall = maybeObj?.a?.b?.toString;
const optChainComputed = maybeObj?.['a'];
const nullishOpt = maybeObj?.a?.missing ?? "fallback";

// ===== Template literals (multi-line, nested) =====
const multiLine = `
  Line 1
  Line 2
  ${personName} is here
`;
const templateNested = `Value: ${hexNum.toString(16)}`;

// ===== Comma operator =====
let commaResult;
commaResult = (1, 2, 3);

// ===== for-in / for-of loops =====
const keys: string[] = [];
const forInObj = { x: 1, y: 2, z: 3 };
for (const k in forInObj) {
  keys.push(k);
}

const forOfItems: string[] = [];
for (const item of items) {
  forOfItems.push(item);
}

for (const [idx, item] of items.entries()) {
  void idx;
  void item;
}

// ===== Labeled statements =====
let labeledResult = "";
outer: for (let i = 0; i < 3; i++) {
  for (let j = 0; j < 3; j++) {
    if (j === 1) continue outer;
    labeledResult += `${i}${j}`;
  }
}

// ===== Switch with fall-through =====
function getStatusCode(code: number): string {
  switch (code) {
    case 200:
      return "OK";
    case 301:
      return "Moved";
    case 404:
      return "Not Found";
    default:
      return "Unknown";
  }
}

// ===== do-while / while =====
let w = 0;
const whileResults: number[] = [];
while (w < 3) {
  whileResults.push(w);
  w++;
}

let d = 0;
const doWhileResults: number[] = [];
do {
  doWhileResults.push(d);
  d++;
} while (d < 3);

// ===== try / catch / finally =====
function runTry(): string {
  try {
    return "try";
  } catch (e) {
    return "catch";
  } finally {
    // finally block
  }
}

// ===== new / this / member chains =====
const dateLen = "2020-01-01".split("-").length;
const now = new Date("2020-01-01").getFullYear();
const chain = items.filter(n => n.length > 3).map(n => n.toUpperCase()).join("|");

// ===== Object method shorthand =====
const calc = {
  add(x: number, y: number) { return x + y; },
  mul: (x: number, y: number) => x * y,
};

// ===== Async / await in components (client-side only) =====
function AsyncDemo() {
  const [msg, setMsg] = createSignal("");
  const [done, setDone] = createSignal(false);

  createEffect(async () => {
    const res = await Promise.resolve("resolved");
    setMsg(res);
    setDone(true);
  });

  return (
    <div>
      <h2>Async / Await</h2>
      <p>{done() ? msg() : "pending"}</p>
    </div>
  );
}

// ===== Arrow functions in handlers =====
function HandlerDemo() {
  const [count, setCount] = createSignal(0);

  return (
    <div>
      <h2>Handlers</h2>
      <button onClick={() => setCount(c => c + 1)}>Inc</button>
      <button onClick={() => {
        const cur = count();
        setCount(cur > 5 ? 0 : cur * 2);
      }}>Double or Reset</button>
      <p>Count: {count()}</p>
    </div>
  );
}

// ===== Conditional / list rendering =====
function RenderDemo() {
  const [show, setShow] = createSignal(true);

  return (
    <div>
      <h2>Rendering</h2>
      {show() && <span class="visible">Visible</span>}
      {!show() || <span class="fallback">Fallback</span>}
      {show() ? <b>On</b> : <i>Off</i>}
      <ul>
        {items.map((item, i) => <li key={item}>{i}: {item}</li>)}
      </ul>
    </div>
  );
}

// ===== Nested / fragments / spread attrs =====
function MiscDemo() {
  return (
    <>
      <h2>Misc</h2>
      <div {...{ class: "spread", "data-x": "1" }}>
        <input disabled={true} required readOnly />
        <Badge label="fragment + spread attrs" />
        <Badge label="fragments render children" />
      </div>
    </>
  );
}

// ===== Complex nested JSX =====
function DeepDemo() {
  return (
    <section>
      <article>
        <header>
          <nav>
            <ul>
              <li><a href="/about">About</a></li>
            </ul>
          </nav>
        </header>
        <main>
          <p>Content</p>
        </main>
      </article>
    </section>
  );
}

// ===== IIFE in JSX =====
function IIFEDemo() {
  return <p>{(() => 42)()}</p>;
}

// ===== String edge values =====
const camelCase = "camelCase";
const PascalCase = "PascalCase";
const snake_case = "snake_case";
const UPPER_CASE = "UPPER_CASE";
const $dollar = "dollar";
const _underscore = "underscore";
const dataAttr = "data-id";

export default function SyntaxRobustness() {
  return (
    <div class="syntax-robustness">
      <h1>Syntax Robustness</h1>

      <p>The module-level declarations below exercise a broad range of TypeScript/TSX syntax — numbers, strings, templates, type annotations, operators, destructuring, loops, control flow, functions, and async — and compile without errors.</p>

      <div>
        <h2>Number Formats</h2>
        <p>hex={hexNum}, octal={octalNum}, binary={binaryNum}, float={floatNum}, exp={expNum}, trailingDot={trailingDot}, bigint={bigintLiteral}</p>
      </div>

      <div>
        <h2>Strings</h2>
        <p>{doubleQuoted}</p>
        <p>{singleQuoted}</p>
      </div>

      <div>
        <h2>Type Annotations</h2>
        <p>typedUnion={typedUnion}</p>
      </div>

      <div>
        <h2>Logic &amp; Comparison</h2>
        <p>and={logicalAnd}, or={logicalOr}, strictEq={eqStrict}, gte={gte}</p>
      </div>

      <div>
        <h2>Multiple Decls</h2>
        <p>a+b+c={a + b + c}</p>
      </div>

      <div>
        <h2>Switch</h2>
        <p>200 = "OK", 404 = "Not Found"</p>
      </div>

      <AsyncDemo />
      <HandlerDemo />
      <RenderDemo />
      <MiscDemo />
      <DeepDemo />
      <IIFEDemo />
    </div>
  );
}
