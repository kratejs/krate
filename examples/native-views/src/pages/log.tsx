import { createSignal } from '@krate/runtime';
import { onViewMessage } from '@krate/app/window';

export default function Log() {
  const [lines, setLines] = createSignal<string[]>([]);
  onViewMessage(({ from, data }) => {
    const line = (data as { line?: string }).line ?? JSON.stringify(data);
    setLines([...lines(), (from || 'host') + ': ' + line]);
  });
  return (
    <main>
      <h2>Log</h2>
      <pre id="lines">{lines().join('\n')}</pre>
    </main>
  );
}
