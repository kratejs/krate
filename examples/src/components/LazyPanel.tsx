import { useState } from 'react';

// Loaded through React.lazy() on /react-suspense: its HTML is rendered at
// build time and its hydration code ships as a separate chunk.
export default function LazyPanel(props: { title: string }) {
  const [clicks, setClicks] = useState(0);
  return (
    <button
      type="button"
      data-testid="lazy-panel"
      class="rounded-lg border border-slate-700 px-4 py-2 text-slate-200"
      onClick={() => setClicks(clicks + 1)}
    >
      {props.title}: {clicks}
    </button>
  );
}
