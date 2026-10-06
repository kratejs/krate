import { createSignal } from "@krate/runtime";

/**
 * A live, hydrated demo for the landing page. This is a real Krate client
 * component: it ships a tiny hydration bundle and reacts with signals — the
 * same primitive the marketing copy describes.
 */
export function CounterDemo() {
  const [count, setCount] = createSignal(0);

  return (
    <div class="demo">
      <button type="button" aria-label="Decrease" onClick={() => setCount((c) => c - 1)}>
        −
      </button>
      <span class="demo-value">{count()}</span>
      <button type="button" aria-label="Increase" onClick={() => setCount((c) => c + 1)}>
        +
      </button>
    </div>
  );
}
