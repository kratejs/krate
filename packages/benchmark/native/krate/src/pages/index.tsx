import { createEffect, createSignal } from '@krate/runtime';
import { Ping, Report } from '../lib/bench.go';

// The benchmark page: a list, a button that calls host code, and a one-time
// first-paint report. The Electron and Tauri apps render the same page.
function measureFirstPaint(send: (record: string) => void) {
  let sent = false;
  const finish = (fcp: number) => {
    if (sent) return;
    sent = true;
    send(
      JSON.stringify({
        fcp,
        timeOrigin: performance.timeOrigin,
        paintAtMs: performance.timeOrigin + fcp,
        reportedAtMs: Date.now(),
        userAgent: navigator.userAgent,
      }),
    );
  };
  try {
    const po = new PerformanceObserver((list) => {
      for (const e of list.getEntries()) {
        if (e.name === 'first-contentful-paint') {
          po.disconnect();
          finish(e.startTime);
        }
      }
    });
    po.observe({ type: 'paint', buffered: true });
  } catch {
    // no paint timing: the timer below reports the second frame
  }
  setTimeout(() => requestAnimationFrame(() => requestAnimationFrame(() => finish(performance.now()))), 10000);
}

const items = Array.from({ length: 100 }, (_, i) => 'Item ' + (i + 1));

export default function Home() {
  const [clicks, setClicks] = createSignal(0);
  const [reply, setReply] = createSignal('');

  createEffect(() => {
    measureFirstPaint((record) => {
      Report(record);
    });
  });

  const ping = async () => {
    const n = clicks();
    setReply(await Ping(n));
    setClicks(n + 1);
  };

  return (
    <main>
      <h1>Benchmark</h1>
      <button id="ping" onClick={ping}>Call host</button>
      <p id="reply">{reply()}</p>
      <ul id="list">
        {items.map((t) => (
          <li>{t}</li>
        ))}
      </ul>
    </main>
  );
}
