import { createSignal } from '@krate/runtime';

export default function Home() {
  const [count, setCount] = createSignal(0);
  const items = Array.from({ length: 100 }, (_, i) => i);

  return (
    <main>
      <h1>Benchmark</h1>
      <p>Count: {count()}</p>
      <button onClick={() => setCount((c) => c + 1)}>+</button>
      <ul>
        {items.map((i) => (
          <li>Item {i}</li>
        ))}
      </ul>
    </main>
  );
}
