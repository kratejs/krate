import { createSignal } from '@krate/runtime';
import styles from './Counter.module.css';

export default function Counter() {
  const [count, setCount] = createSignal(0);

  return (
    <div class={styles.counter}>
      <div class="demo-count">{count()}</div>
      <div class="demo-controls">
        <button
          class="btn btn-secondary"
          type="button"
          onClick={() => setCount((c) => c - 1)}
        >
          −
        </button>
        <button
          class="btn btn-primary"
          type="button"
          onClick={() => setCount(0)}
        >
          Reset
        </button>
        <button
          class="btn btn-secondary"
          type="button"
          onClick={() => setCount((c) => c + 1)}
        >
          +
        </button>
      </div>
    </div>
  );
}
