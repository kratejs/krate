package lexer

import "testing"

const benchSource = `
import { createSignal, createEffect } from '@krate/runtime';

export default function Page({ items }: { items: string[] }) {
  const [count, setCount] = createSignal(0);
  createEffect(() => { console.log(count()); });
  return (
    <div class="flex gap-2 p-4" data-id="root">
      <button onClick={() => setCount(count() + 1)}>Count: {count}</button>
      <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul>
    </div>
  );
}
`

func BenchmarkLex(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = New(benchSource).Tokenize()
	}
}
