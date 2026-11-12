import { createEffect, createSignal } from '@krate/runtime';
import { getView, onViewMessage } from '@krate/app/window';

export default function Log() {
  const [lines, setLines] = createSignal<string[]>([]);
  onViewMessage(({ from, data }) => {
    const line = (data as { line?: string }).line ?? JSON.stringify(data);
    setLines([...lines(), (from || 'host') + ': ' + line]);
  });
  // Tell the main page this view loaded, and at what size; again whenever the
  // layout resizes it.
  createEffect(() => {
    const report = () => void getView('main').post({ loaded: innerWidth + 'x' + innerHeight });
    report();
    addEventListener('resize', report);
  });
  return (
    <main>
      <h2>Log</h2>
      <pre id="lines">{lines().join('\n')}</pre>
    </main>
  );
}
