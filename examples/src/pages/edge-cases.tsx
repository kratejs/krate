import { createSignal, createEffect } from '@krate/runtime';
import styles from './Card.module.css';

// ===== Edge Case 1: Number formats =====
const hexNum = 0xFF;
const octalNum = 0o77;
const binaryNum = 0b1010;
const floatNum = 3.14;
const expNum = 1e10;
const negExp = 1.5e-3;
const leadingDot = .5;
const trailingDot = 5.;

// ===== Edge Case 2: String escape sequences =====
const escapedStr = "hello\nworld\ttab\\slash\"quote'single";
const singleQuote = 'it\'s a test';
const templateLit = `Hello ${'world'}!`;
const templateMultiExpr = `${1 + 2} items`;
const templateNested = `Value: ${hexNum.toString(16)}`;

// ===== Edge Case 3: Ternary and conditional expressions =====
const ternarySimple = true ? "yes" : "no";
const ternaryNested = true ? (false ? "a" : "b") : "c";
const ternaryChained = 1 > 2 ? "big" : 2 > 3 ? "mid" : "small";

// ===== Edge Case 4: Logical operators =====
const logicalAnd = true && "truthy";
const logicalOr = false || "fallback";
const nullishCoalesce = null ?? "default";
const nullishWithVal = "exists" ?? "default";
const logicalAndChain = true && true && "both";

// ===== Edge Case 5: Comparison operators =====
const eqStrict = 1 === 1;
const neqStrict = 1 !== 2;
const eqLoose = 1 == "1";
const neqLoose = 1 != "2";
const gteCheck = 5 >= 5;
const lteCheck = 3 <= 3;

// ===== Edge Case 6: Unary operators =====
const negNum = -42;
const notBool = !false;
const typeCoerce = !!1;

// ===== Edge Case 7: Complex expressions =====
const complexMath = (10 + 5) * 2 - 3 / 1.5;
const moduloResult = 10 % 3;
const precedenceTest = 2 + 3 * 4 - 1;
const parenOverride = (2 + 3) * 4;

// ===== Edge Case 8: Array methods =====
const items = ['apple', 'banana', 'cherry', 'date'];
const filteredItems = items.filter(i => i.length > 5);
const mappedItems = items.map(i => i.toUpperCase());

// ===== Edge Case 9: Object literals =====
const person = { name: "Alice", age: 30, active: true };
const nested = { user: { name: "Bob", scores: [1, 2, 3] } };
const spread = { ...person, extra: "data" };
const computed = { ["key" + "1"]: "value" };

// ===== Edge Case 10: Function expressions =====
const arrowNoParam = () => 42;
const arrowOneParam = (x) => x * 2;
const arrowMultiParam = (x, y) => x + y;
const arrowBlockBody = (x) => { const doubled = x * 2; return doubled; };
const arrowObjectReturn = () => ({ key: "value", count: 1 });
const arrowNested = (a) => (b) => a + b;
const arrowDefaultParam = (x = 10) => x + 1;
const arrowRestParam = (...args) => args.length;
const arrowAsync = async () => "done";

// ===== Edge Case 11: Destructuring =====
const [first, second, ...rest] = items;
const { name, age } = person;

// ===== Edge Case 12: Optional chaining & nullish =====
const maybeObj = { a: { b: { c: 42 } } };
const optChain = maybeObj?.a?.b?.c;
const optChainCall = maybeObj?.a?.b?.toString;
const optChainComputed = maybeObj?.['a'];

// ===== Edge Case 13: Spread & rest =====
const arrSpread = [1, 2, ...[3, 4], 5];
const funcWithRest = (...nums: number[]) => nums.reduce((a, b) => a + b, 0);

// ===== Edge Case 14: Template with expression containers =====
const multiLineTemplate = `
  Line 1
  Line 2
  ${person.name} is here
`;

// ===== Edge Case 15: typeof / void / delete (these should not crash) =====
const typeOfCheck = typeof "hello";
const voidResult = void 0;

// ===== Edge Case 16: Comma operator =====
let commaResult;
commaResult = (1, 2, 3);

// ===== Edge Case 17: for-in / for-of loops =====
const keys: string[] = [];
const forInObj = { x: 1, y: 2, z: 3 };
for (const k in forInObj) {
  keys.push(k);
}

const forOfItems: string[] = [];
for (const item of items) {
  forOfItems.push(item);
}

// ===== Edge Case 18: Labeled statements =====
let labeledResult = "";
outer: for (let i = 0; i < 3; i++) {
  for (let j = 0; j < 3; j++) {
    if (j === 1) continue outer;
    labeledResult += `${i}${j}`;
  }
}

// ===== Edge Case 19: Complex switch with fall-through =====
function getStatusCode(code: number): string {
  switch (code) {
    case 200:
      return "OK";
    case 301:
      return "Moved";
    case 404:
      return "Not Found";
    case 500:
      return "Server Error";
    default:
      return "Unknown";
  }
}

// ===== Edge Case 20: Nested try/catch/finally =====
function nestedTry(): string {
  try {
    try {
      return "inner";
    } catch (e) {
      return "inner-catch";
    } finally {
      // finally block
    }
  } catch (e) {
    return "outer-catch";
  } finally {
    return "outer-finally";
  }
}

// ===== Edge Case 21: Complex JSX expressions =====
function ComplexComponent() {
  const [count, setCount] = createSignal(0);
  const [items2, setItems2] = createSignal<string[]>(['a', 'b']);

  const doubled = () => count() * 2;

  return (
    <div class="complex">
      <h1>Edge Cases</h1>

      {/* Conditional rendering */}
      <div>
        {count() > 0 && <span>Positive: {count()}</span>}
        {count() === 0 ? <span>Zero</span> : <span>Non-zero: {count()}</span>}
      </div>

      {/* Array mapping with key */}
      <ul>
        {items.map((item, i) => (
          <li key={item}>{item} ({i})</li>
        ))}
      </ul>

      {/* Nested ternary in JSX */}
      <p>Status: {count() > 10 ? "High" : count() > 5 ? "Medium" : "Low"}</p>

      {/* Expression with template literal */}
      <p>{`Count: ${count()}`}</p>

      {/* Computed className */}
      <div class={`badge ${count() > 0 ? 'active' : 'inactive'}`}>
        Badge
      </div>

      {/* Spread in JSX */}
      <input {...{ type: "text", placeholder: "Enter..." }} />

      {/* Complex event handlers */}
      <button onClick={() => setCount(c => c + 1)}>Increment</button>
      <button onClick={() => {
        setCount(0);
        setItems2(['reset']);
      }}>Reset</button>

      {/* Nested component with complex props */}
      <ChildComponent
        title={`Items (${items2().length})`}
        items={items2()}
        onAction={(val: string) => setItems2([...items2(), val])}
      />
    </div>
  );
}

// ===== Edge Case 22: Component with children =====
function ChildComponent(props: { title: string; items: string[]; onAction: (val: string) => void }) {
  return (
    <div class="child">
      <h2>{props.title}</h2>
      <ul>
        {props.items.map(item => <li>{item}</li>)}
      </ul>
      <button onClick={() => props.onAction("new")}>Add</button>
    </div>
  );
}

// ===== Edge Case 23: Multiple variable declarations =====
const a = 1, b = 2, c = a + b;

// ===== Edge Case 24: Complex expressions as JSX children =====
function ExpressionChildren() {
  const [show, setShow] = createSignal(true);

  return (
    <div>
      {/* Short-circuit with JSX */}
      {show() && <div class="visible">Visible content</div>}

      {/* Ternary with JSX on both branches */}
      {show()
        ? <div class="on">Shown</div>
        : <div class="off">Hidden</div>
      }

      {/* Nested expressions */}
      <div>
        {show() ? (1 + 2 > 2 ? "big" : "small") : "none"}
      </div>
    </div>
  );
}

// ===== Edge Case 25: Complex arrow functions in handlers =====
function HandlerEdgeCases() {
  const [val, setVal] = createSignal("hello");
  const [num, setNum] = createSignal(42);

  return (
    <div>
      <input
        value={val()}
        onInput={(e) => {
          const target = e.target as HTMLInputElement;
          setVal(target.value);
        }}
      />
      <button onClick={() => setNum(n => n + 1)}>Inc</button>
      <button onClick={() => {
        const current = num();
        if (current > 100) {
          setNum(0);
        } else {
          setNum(current * 2);
        }
      }}>Double or Reset</button>
      <p>{val()} - {num()}</p>
    </div>
  );
}

// ===== Edge Case 26: For loop rendering =====
function ForLoopDemo() {
  const [count, setCount] = createSignal(0);
  const results: number[] = [];
  for (let i = 0; i < 5; i++) {
    results.push(i * 2);
  }

  return (
    <div>
      <h2>For Loop Results</h2>
      <ul>
        {results.map(r => <li>{r}</li>)}
      </ul>
    </div>
  );
}

// ===== Edge Case 27: While loop rendering =====
function WhileLoopDemo() {
  let x = 0;
  const results: number[] = [];
  while (x < 5) {
    results.push(x);
    x++;
  }

  return (
    <div>
      <h2>While Loop Results</h2>
      <ul>
        {results.map(r => <li>{r}</li>)}
      </ul>
    </div>
  );
}

// ===== Edge Case 28: Switch in JSX =====
function StatusBadge(props: { code: number }) {
  const color = props.code < 300 ? "green" : props.code < 400 ? "yellow" : "red";
  return <span class={color}>{getStatusCode(props.code)}</span>;
}

// ===== Edge Case 29: Null / undefined / boolean rendering =====
function NullEdgeCases() {
  const [val, setVal] = createSignal(null);

  return (
    <div>
      <p>{val() ?? "No value"}</p>
      <p>{val() ? "Has value" : "No value"}</p>
      <button onClick={() => setVal("found")}>Set Value</button>
    </div>
  );
}

// ===== Edge Case 30: Deeply nested JSX =====
function DeepNesting() {
  return (
    <div>
      <section>
        <article>
          <header>
            <nav>
              <ul>
                <li><a href="/home">Home</a></li>
                <li><a href="/about">About</a></li>
              </ul>
            </nav>
          </header>
          <main>
            <p>Content</p>
          </main>
        </article>
      </section>
    </div>
  );
}

// ===== Edge Case 31: Boolean attributes =====
function BoolAttrs() {
  return (
    <div>
      <input disabled={true} />
      <input disabled={false} />
      <input type="text" required readonly />
    </div>
  );
}

// ===== Edge Case 32: Fragment variants =====
function FragmentVariants() {
  return (
    <>
      <div>First</div>
      <div>Second</div>
      <>
        <span>Nested fragment A</span>
        <span>Nested fragment B</span>
      </>
    </>
  );
}

// ===== Edge Case 33: Object in JSX expression =====
function ObjectInJSX() {
  return (
    <div>
      <p>{JSON.stringify({ hello: "world" })}</p>
    </div>
  );
}

// ===== Edge Case 34: Chained method calls =====
function ChainedMethods() {
  const result = [1, 2, 3, 4, 5]
    .filter(n => n > 2)
    .map(n => n * 10)
    .join(", ");

  return <div>{result}</div>;
}

// ===== Edge Case 35: IIFE in JSX =====
function IIFEDemo() {
  return (
    <div>
      <p>{(() => 42)()}</p>
    </div>
  );
}

// ===== Edge Case 36: Nested component calls =====
function NestedComponents() {
  return (
    <div>
      <ChildComponent
        title="Test"
        items={['x', 'y']}
        onAction={() => {}}
      />
    </div>
  );
}

// ===== Edge Case 37: Expression container with member expression =====
function MemberExprDemo() {
  const data = { items: [1, 2, 3], count: 42 };
  return (
    <div>
      <p>{data.count}</p>
      <p>{data.items.length}</p>
    </div>
  );
}

// ===== Edge Case 38: Short-circuit with complex right side =====
function ShortCircuitDemo() {
  const [show, setShow] = createSignal(true);

  return (
    <div>
      {show() && <ChildComponent title="Dynamic" items={[]} onAction={() => {}} />}
      {!show() || <span>Fallback</span>}
    </div>
  );
}

// ===== Edge Case 39: Mixed control flow =====
function MixedControlFlow() {
  const [items2, setItems2] = createSignal([]);
  const [loading, setLoading] = createSignal(false);

  if (loading()) {
    return <div>Loading...</div>;
  }

  if (items2().length === 0) {
    return <div>No items</div>;
  }

  return (
    <ul>
      {items2().map(item => <li>{item}</li>)}
    </ul>
  );
}

// ===== Edge Case 40: CSS module binding =====
function CSSModuleDemo() {
  return (
    <div class={styles.card}>
      <p>Card with CSS module</p>
    </div>
  );
}

// ===== Edge Case 41: Client-side API fetch =====
function APIData() {
  const [apiMsg, setApiMsg] = createSignal("");
  const [apiMethod, setApiMethod] = createSignal("");
  const [apiErr, setApiErr] = createSignal("");
  const [apiDone, setApiDone] = createSignal(false);

  createEffect(() => {
    fetch("/api/hello")
      .then((res) => res.json())
      .then((json) => {
        setApiDone(true);
        setApiMsg(json.message);
        setApiMethod(json.method);
      })
      .catch((e) => {
        setApiErr(String(e));
        setApiDone(true);
      });
  });

  return (
    <div>
      <h2>API Data</h2>
      {!apiDone() ? (
        <p class="api-loading">Loading...</p>
      ) : apiErr() ? (
        <p class="api-error">Error: {apiErr()}</p>
      ) : (
        <div>
          <p class="api-message">{apiMsg()}</p>
          <p class="api-method">{apiMethod()}</p>
        </div>
      )}
    </div>
  );
}

export default function EdgeCases() {
  return (
    <div class="edge-cases">
      <h1>JavaScript/TypeScript Edge Cases</h1>
      <ComplexComponent />
      <ExpressionChildren />
      <HandlerEdgeCases />
      <ForLoopDemo />
      <WhileLoopDemo />
      <NullEdgeCases />
      <DeepNesting />
      <BoolAttrs />
      <FragmentVariants />
      <ObjectInJSX />
      <ChainedMethods />
      <IIFEDemo />
      <NestedComponents />
      <MemberExprDemo />
      <ShortCircuitDemo />
      <CSSModuleDemo />
      <APIData />
    </div>
  );
}
