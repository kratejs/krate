package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

// benchSrc is a representative TSX component exercising imports, hooks, JSX,
// expressions, and control flow.
const benchSrc = `import { createSignal, onMount } from '@krate/runtime';
import { Card } from './card';

interface Item { id: number; label: string; done: boolean }

export default function Todo() {
  const [items, setItems] = createSignal<Item[]>([
    { id: 1, label: 'first', done: false },
    { id: 2, label: 'second', done: true },
  ]);
  const [filter, setFilter] = createSignal<'all' | 'open'>('all');

  const visible = () => items().filter((it) => filter() === 'all' || !it.done);
  const toggle = (id: number) =>
    setItems((prev) => prev.map((it) => (it.id === id ? { ...it, done: !it.done } : it)));

  onMount(() => console.log('mounted'));

  return (
    <Card title="Todos">
      <div class="flex items-center gap-2">
        {visible().map((it) => (
          <label key={it.id} class={it.done ? 'done' : ''}>
            <input type="checkbox" checked={it.done} onChange={() => toggle(it.id)} />
            {it.label}
          </label>
        ))}
      </div>
      <button onClick={() => setFilter(filter() === 'all' ? 'open' : 'all')}>
        {filter() === 'all' ? 'Show open' : 'Show all'}
      </button>
    </Card>
  );
}
`

// BenchmarkParse measures lexing + parsing of a representative TSX module.
func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = New(lexer.New(benchSrc).Tokenize()).ParseProgram()
	}
}
