import { createSignal, createEffect } from '@krate/runtime';
import { main, onViewMessage, pane, split } from '@krate/app/window';
import { onMenu } from '@krate/app/menu';

// The main page splits two more views off its window: a sidebar on the left and
// a log under the main page. Drag the dividers; the panes keep their minimum
// sizes. Each view is a route of this same app and shares its web process.
export default function Home() {
  const [status, setStatus] = createSignal('opening views...');
  const [heard, setHeard] = createSignal('');
  // The views that loaded, with the size their page saw.
  const [loaded, setLoaded] = createSignal<string[]>([]);

  // Native events the app reacts to; the test driver emits them (see tests/).
  const [events, setEvents] = createSignal<string[]>([]);
  createEffect(() => {
    onMenu('view.refresh', (e) => setEvents([...events(), 'menu ' + e.id]));
  });

  createEffect(() => {
    openViews().catch((e) => setStatus('could not open the views: ' + (e instanceof Error ? e.message : String(e))));
  });

  async function openViews() {
    const win = main();
    // A narrower scope for the log: it may use nothing but the window API.
    const sidebar = await win.addView({ id: 'sidebar', path: '/sidebar', side: 'left', size: 0.25, minWidth: 180 });
    const log = await win.addView({
      id: 'log', path: '/log', side: 'bottom', size: 0.3, minHeight: 90, permissions: ['window'],
    });
    setStatus('views: ' + (await win.views()).map((v) => v.id).join(', '));
    await log.post({ line: 'sidebar and log are open' });
    // Name the whole layout: the sidebar left, this page above the log. After the
    // user drags a divider, win.layout() returns the ratios to save and set back.
    await win.setLayout(split('horizontal', pane(sidebar, { minWidth: 180 }), split('vertical', pane(win.id), pane(log), 0.7), 0.25));
  }

  onViewMessage(({ from, data }) => {
    const size = (data as { loaded?: string }).loaded;
    if (size) setLoaded([...loaded(), from + ' ' + size]);
    else setHeard(from + ': ' + JSON.stringify(data));
  });

  return (
    <main>
      <h1>Native Views</h1>
      <p id="status">{status()}</p>
      <p id="loaded">{loaded().join(', ')}</p>
      <p id="heard">{heard()}</p>
      <p id="events">{events().join(', ')}</p>
    </main>
  );
}
