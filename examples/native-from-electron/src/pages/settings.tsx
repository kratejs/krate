import { createSignal, createEffect } from '@krate/runtime';
import { getSettings, updateSettings, settings } from '../lib/store.host';

// A host signal reads as undefined until the host has pushed its first value.
const theme = () => settings()?.theme ?? 'light';
const fontSize = () => settings()?.fontSize ?? 16;

export default function SettingsPage() {
  const [error, setError] = createSignal('');
  createEffect(() => {
    getSettings();
  });

  // updateSettings rejects like ipcRenderer.invoke does when the handler throws.
  async function update(patch: { theme?: 'light' | 'dark'; fontSize?: number }) {
    try {
      setError('');
      await updateSettings(patch);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <main data-theme={theme()}>
      <style>{`
        main { min-height: 100vh; box-sizing: border-box; padding: 16px; font-family: system-ui, sans-serif; }
        main[data-theme='light'] { background: #ffffff; color: #1a1a1a; }
        main[data-theme='dark'] { background: #1e1e24; color: #ececf1; }
      `}</style>
      <h1>Settings</h1>
      <p>
        <label>
          Theme{' '}
          <select id="theme" value={theme()} onChange={(e) => update({ theme: e.currentTarget.value as 'light' | 'dark' })}>
            <option value="light">light</option>
            <option value="dark">dark</option>
          </select>
        </label>
      </p>
      <p>
        <label>
          Font size{' '}
          <input id="font-size" type="number" min="10" max="32" value={fontSize()} onChange={(e) => update({ fontSize: Number(e.currentTarget.value) })} />
        </label>
      </p>
      <p id="error">{error()}</p>
    </main>
  );
}
