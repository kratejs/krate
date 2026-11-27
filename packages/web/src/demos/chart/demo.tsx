import { createSignal, createMemo, createEffect, onCleanup } from "@krate/runtime";
import "./demo.css";

const W = 600;
const H = 180;

// A small deterministic generator, so the first render matches on the server
// and in the browser.
function seeded(seed: number) {
  return () => {
    seed = (seed * 1664525 + 1013904223) % 4294967296;
    return seed / 4294967296;
  };
}
function initial(n: number) {
  const next = seeded(7);
  const out: number[] = [];
  let v = 50;
  for (let i = 0; i < n; i++) {
    v = Math.min(95, Math.max(5, v + (next() - 0.5) * 14));
    out.push(Math.round(v * 10) / 10);
  }
  return out;
}

// A stream of readings feeds one signal; every number and shape on screen is a
// memo over it. Only the effect that owns the timer knows about time.
export default function Demo() {
  const calm = typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const [data, setData] = createSignal<number[]>(initial(48));
  const [live, setLive] = createSignal(!calm);
  const [size, setSize] = createSignal(48);

  createEffect(() => {
    if (!live()) return;
    const timer = setInterval(() => {
      const last = data()[data().length - 1];
      const v = Math.min(95, Math.max(5, last + (Math.random() - 0.5) * 14));
      setData([...data(), Math.round(v * 10) / 10].slice(-size()));
    }, 450);
    onCleanup(() => clearInterval(timer));
  });

  const shown = createMemo(() => data().slice(-size()));
  const line = createMemo(() => {
    const d = shown();
    const dx = W / Math.max(1, d.length - 1);
    return d.map((v, i) => (i * dx).toFixed(1) + "," + (H - (v / 100) * H).toFixed(1)).join(" ");
  });
  const area = createMemo(() => "M0," + H + " L" + line().replace(/ /g, " L") + " L" + W + "," + H + " Z");
  const latest = createMemo(() => shown()[shown().length - 1]);
  const high = createMemo(() => Math.max(...shown()));
  const low = createMemo(() => Math.min(...shown()));
  const mean = createMemo(() => (shown().reduce((a, b) => a + b, 0) / shown().length).toFixed(1));

  return (
    <div class="ch">
      <div class="ch-stats">
        <div><span class="ch-k">Latest</span><strong>{latest()}</strong></div>
        <div><span class="ch-k">High</span><strong>{high()}</strong></div>
        <div><span class="ch-k">Low</span><strong>{low()}</strong></div>
        <div><span class="ch-k">Mean</span><strong>{mean()}</strong></div>
      </div>
      <svg class="ch-svg" viewBox={"0 0 " + W + " " + H} role="img" aria-label="Line chart of the most recent readings" preserveAspectRatio="none">
        <path class="ch-area" d={area()} />
        <polyline class="ch-line" points={line()} fill="none" vector-effect="non-scaling-stroke" />
      </svg>
      <div class="ch-controls">
        <button type="button" class="dk-btn ch-live" aria-pressed={live()} onClick={() => setLive(!live())}>Live</button>
        <label class="ch-field">Window <output>{size()}</output>
          <input type="range" min="12" max="120" value={size()} onInput={(e) => setSize(Number((e.target as HTMLInputElement).value))} />
        </label>
      </div>
    </div>
  );
}
