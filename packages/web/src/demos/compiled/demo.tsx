import { createSignal } from "@krate/runtime";
import "./demo.css";

// Only the text node that reads count() and the click handler are bound;
// everything else is static HTML.
export default function Demo() {
  const [count, setCount] = createSignal(0);
  return (
    <div class="cm">
      <p class="cm-label">Clicked <strong>{count()}</strong> times</p>
      <button type="button" class="dk-btn dk-btn-primary" onClick={() => setCount(count() + 1)}>
        Click me
      </button>
    </div>
  );
}
