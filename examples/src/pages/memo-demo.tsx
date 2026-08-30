import { createSignal, createMemo } from '@krate/runtime';

export default function MemoPage() {
  const [count, setCount] = createSignal(2);
  const doubled = createMemo(() => count() * 2);

  return (
    <div class="page">
      <h1>Memo Demo</h1>
      <p>Count: <span>{count()}</span></p>
      <p>Doubled: <span>{doubled()}</span></p>
      <button onClick={() => setCount(count() + 1)}>+</button>
      <button onClick={() => setCount(count() - 1)}>-</button>
    </div>
  )
}
