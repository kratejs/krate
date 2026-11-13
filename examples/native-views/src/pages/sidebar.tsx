import { createEffect } from '@krate/runtime';
import { getView } from '@krate/app/window';

// Tells the log view which entry was picked: a view-to-view message. The
// shell fills in who sent it.
export default function Sidebar() {
  // Tell the main page this view loaded, and at what size; again whenever the
  // layout resizes it.
  createEffect(() => {
    const report = () => void getView('main').post({ loaded: innerWidth + 'x' + innerHeight });
    report();
    addEventListener('resize', report);
  });
  return (
    <main>
      <h2>Sidebar</h2>
      <ul>
        {['Inbox', 'Drafts', 'Sent'].map((name) => (
          <li key={name}>
            <button onClick={() => getView('log').post({ line: 'picked ' + name })}>{name}</button>
          </li>
        ))}
      </ul>
    </main>
  );
}
