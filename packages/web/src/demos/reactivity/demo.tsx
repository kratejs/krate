import { createSignal, createMemo, onMount, onCleanup } from "@krate/runtime";
import "./demo.css";

// Three signals and two memos. Each value below is bound to its own text node;
// a MutationObserver counts the writes the browser really sees.
export default function Demo() {
  const [a, setA] = createSignal(2);
  const [b, setB] = createSignal(3);
  const [c, setC] = createSignal(0);
  const sum = createMemo(() => a() + b());
  const size = createMemo(() => (sum() >= 10 ? "big" : "small"));

  const [writes, setWrites] = createSignal([0, 0, 0, 0, 0]);
  const [clicks, setClicks] = createSignal(0);
  let root: HTMLElement | undefined;

  onMount(() => {
    const seen = new MutationObserver((records) => {
      const next = writes().slice();
      let changed = false;
      for (const r of records) {
        const el = (r.target.nodeType === 3 ? r.target.parentElement : r.target) as HTMLElement;
        const node = el.closest("[data-node]") as HTMLElement | null;
        if (!node) continue;
        next[Number(node.dataset.node)] += 1;
        changed = true;
        node.classList.remove("rx-flash");
        void node.offsetWidth;
        node.classList.add("rx-flash");
      }
      if (changed) setWrites(next);
    });
    seen.observe(root!, { subtree: true, childList: true, characterData: true });
    onCleanup(() => seen.disconnect());
  });

  const bump = (set: (f: (n: number) => number) => void) => () => {
    set((n) => n + 1);
    setClicks((n) => n + 1);
  };
  const total = createMemo(() => writes().reduce((x, y) => x + y, 0));

  return (
    <div class="rx" ref={root}>
      <div class="dk-row">
        <button type="button" class="dk-btn" onClick={bump(setA)}>a + 1</button>
        <button type="button" class="dk-btn" onClick={bump(setB)}>b + 1</button>
        <button type="button" class="dk-btn" onClick={bump(setC)}>c + 1</button>
      </div>

      <ul class="rx-nodes" aria-label="Bound values and their DOM writes">
        <li><code>a()</code><span class="rx-val" data-node="0">{a()}</span><output class="rx-writes" data-writes="0">{writes()[0]}</output></li>
        <li><code>b()</code><span class="rx-val" data-node="1">{b()}</span><output class="rx-writes" data-writes="1">{writes()[1]}</output></li>
        <li><code>sum() = a() + b()</code><span class="rx-val" data-node="2">{sum()}</span><output class="rx-writes" data-writes="2">{writes()[2]}</output></li>
        <li><code>size() = sum() &gt;= 10</code><span class="rx-val" data-node="3">{size()}</span><output class="rx-writes" data-writes="3">{writes()[3]}</output></li>
        <li><code>c()</code><span class="rx-val" data-node="4">{c()}</span><output class="rx-writes" data-writes="4">{writes()[4]}</output></li>
      </ul>

      <p class="rx-total" aria-live="polite">
        <strong data-total>{total()}</strong> text writes in <strong>{clicks()}</strong> clicks.
        A component that re-renders on every change would touch <strong>{clicks() * 5}</strong> nodes.
      </p>
    </div>
  );
}
