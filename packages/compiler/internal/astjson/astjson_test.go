package astjson

import (
	"reflect"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseSource(src string) *ast.Program {
	toks := lexer.New(src).Tokenize()
	return parser.New(toks).ParseProgram()
}

func TestRoundTripProgram(t *testing.T) {
	src := `
import { createSignal, onMount } from 'krate';
import type { Props } from './types';

export default function App(props: Props) {
  const [count, setCount] = createSignal(0);
  const fn = (x: number): number => x * 2;
  const obj = { a: 1, b: 'two', ...(count() > 0 ? { c: 3 } : {}) };
  const arr = [1, 'a', null, true];
` + "  const msg = `value: ${count() + fn(1)}`;\n" + `
  let res: string | null = null;
  try {
    res = maybeFetch();
  } catch (e) {
    res = String(e);
  } finally {
    onMount(() => console.log('mounted'));
  }
  switch (count()) {
    case 1: res = 'one'; break;
    default: res = 'many';
  }
  for (const k of keys) { if (k in obj) { setCount(count() + 1); } }
  while (count() < 10) { setCount(count() + 1); }
  const worker = new Worker(new URL('./w.ts', import.meta.url), { type: 'module' });
  const data = import('./data.json');
  const maybe = obj?.fn?.() ?? items?.[0] ?? fallback;

  return (
    <div class={count() > 5 ? 'big' : 'small'} data-n={count()} {...props}>
      <h1 {conditional}>Hi {props.title}</h1>
      {count() > 0 && <span>{count()}</span>}
      {false || <b>bold</b>}
      <button onClick={() => setCount(count() + 1)} disabled={count() > 9}>+</button>
      <>
        <p>first</p>
        <p>second</p>
      </>
      <input ref={el => el && el.focus()} />
    </div>
  );
}
`

	prog := parseSource(src)
	if prog == nil {
		t.Fatal("failed to parse source")
	}
	if len(prog.Body) == 0 {
		t.Fatal("expected program body")
	}

	enc, err := EncodeProgram(prog)
	if err != nil {
		t.Fatalf("EncodeProgram: %v", err)
	}

	// Round-trip through the document.
	round, err := DecodeProgram(enc)
	if err != nil {
		t.Fatalf("DecodeProgram: %v", err)
	}
	if !reflect.DeepEqual(prog, round) {
		t.Fatal("round-trip mismatch")
	}

	// Re-encoding the decoded program must be byte-identical (stable form).
	enc2, err := EncodeProgram(round)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if string(enc) != string(enc2) {
		t.Fatalf("re-encode mismatch:\n--- first ---\n%s\n--- second ---\n%s", enc, enc2)
	}
}

func TestRoundTripDeepJSX(t *testing.T) {
	src := `export default function F() { return <div><ul>{items.map(i => <li><a href={'/i/' + i.id}>{i.name}</a></li>)}</ul></div>; }`
	prog := parseSource(src)
	if prog == nil || len(prog.Body) == 0 {
		t.Fatal("failed to parse")
	}
	enc, err := EncodeProgram(prog)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	round, err := DecodeProgram(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(prog, round) {
		t.Fatal("round-trip mismatch for nested JSX")
	}
}
