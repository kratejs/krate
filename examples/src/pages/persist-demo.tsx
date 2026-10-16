import { createSignal, createEffect } from '@krate/runtime';

// Demonstrates persistent signal state. The counter value is stored in
// localStorage under "krate-demo-count" and restored on reload. Cross-tab sync
// is on by default for localStorage, so opening this page in two tabs keeps
// them in step. The theme preference is kept in sessionStorage.
//
// Durable SSR state: if the page embeds window.__KRATE_STATE__ (for example via
// a krate.state.json file), that server-chosen value wins on first hydration.
export default function PersistDemo() {
  const [count, setCount] = createSignal(0, { persist: 'krate-demo-count' });
  const [theme, setTheme] = createSignal('light', {
    persist: { key: 'krate-demo-theme', store: 'session' },
  });

  createEffect(() => {
    document.documentElement.setAttribute('data-theme', theme());
  });

  return (
    <div class="persist-demo">
      <h1>Persistent State</h1>
      <p>
        This counter survives a full page reload (localStorage) and syncs across
        tabs. The theme preference is scoped to the browser session.
      </p>

      <button class="count" onClick={() => setCount(count() + 1)}>
        Count: {count()}
      </button>

      <button
        class="reset"
        onClick={() => {
          setCount(0);
        }}
      >
        Reset
      </button>

      <div class="theme-controls">
        <button
          class={theme() === 'light' ? 'active' : ''}
          onClick={() => setTheme('light')}
        >
          Light
        </button>
        <button
          class={theme() === 'dark' ? 'active' : ''}
          onClick={() => setTheme('dark')}
        >
          Dark
        </button>
      </div>
    </div>
  );
}
