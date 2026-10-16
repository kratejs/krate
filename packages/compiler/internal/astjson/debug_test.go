package astjson

import (
	"encoding/json"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func TestDebugFindNumericKind(t *testing.T) {
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
	toks := lexer.New(src).Tokenize()
	prog := parser.New(toks).ParseProgram()
	enc, err := EncodeProgram(prog)
	if err != nil {
		t.Fatal(err)
	}
	var doc interface{}
	if err := json.Unmarshal(enc, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(v interface{}, path string)
	walk = func(v interface{}, path string) {
		switch n := v.(type) {
		case map[string]interface{}:
			if k, ok := n["kind"]; ok {
				if _, isStr := k.(string); !isStr {
					b, _ := json.MarshalIndent(n, "", "  ")
					t.Logf("NUMERIC KIND at %s: %T (%v)\n%s", path, k, k, b)
					return
				}
			}
			for key, val := range n {
				walk(val, path+"."+key)
			}
		case []interface{}:
			for i, item := range n {
				walk(item, path+"["+itoa(i)+"]")
			}
		}
	}
	walk(doc, "doc")
	t.Log("no numeric kinds found")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return "1"
}
