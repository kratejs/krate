import { createSignal } from '@krate/runtime';
import { addView, onRemoteEvent, onMessage, post, setUserAgent, setRules } from '@krate/app/remote';

// A remote page next to the app's own page. The remote view has a storage
// partition of its own and no access to the app: it cannot call the SDK or
// fetch the app. It may navigate only to the origin it started on; links to
// other sites open in the system browser. The page below talks to it through
// the message channel, which the view opts in to with `messages: true`.
//
// The functions on globalThis let the end-to-end test choose the address of
// its local web server; an app would pass its own URL.
interface Seen {
  messages: unknown[];
  events: unknown[];
  errors: string[];
}
const seen: Seen = { messages: [], events: [], errors: [] };
(globalThis as unknown as { __remote: Seen }).__remote = seen;

export default function Home() {
  const [status, setStatus] = createSignal('no remote view yet');

  const onError = (e: unknown) => seen.errors.push(String(e));
  onRemoteEvent((e) => seen.events.push(e), { onError });
  onMessage((m) => {
    seen.messages.push(m);
    setStatus(`${seen.messages.length} message(s), last from ${m.origin}`);
  }, { onError });

  const g = globalThis as unknown as Record<string, unknown>;
  g.openRemote = async (url: string, options: Record<string, unknown> = {}) => {
    const view = await addView({ url, partition: 'persist:example', messages: true, side: 'right', size: 0.5, ...options });
    setStatus('remote view ' + view.id);
    return view.id;
  };
  g.postRemote = (view: string, data: unknown, origin: string) => post(view, data, origin);
  g.configureRemote = async (partition: string, ua: string, blockPattern: string) => {
    await setUserAgent(partition, ua);
    await setRules(partition, [{ urls: [blockPattern], action: 'block' }]);
  };

  return (
    <main>
      <h1>Native Remote</h1>
      <p id="status">{status()}</p>
    </main>
  );
}
