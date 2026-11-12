import { createSignal, createEffect } from '@krate/runtime';
import { onMenu } from '@krate/app/menu';

// Reacts to a menu item. Nothing opens this route by itself: tests/ navigates
// a view here and fires the menu event with the test driver.
export default function Events() {
  const [events, setEvents] = createSignal<string[]>([]);
  createEffect(() => {
    onMenu('view.refresh', (e) => setEvents([...events(), 'menu ' + e.id]));
  });
  return (
    <main>
      <h1>Events</h1>
      <p id="events">{events().join(', ')}</p>
    </main>
  );
}
