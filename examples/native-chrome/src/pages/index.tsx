import { createSignal, createEffect } from '@krate/runtime';
import * as chrome from '@krate/app/chrome';
import * as win from '@krate/app/window';

// A window with no system title bar. The bar below is the page's own: the
// data-krate-drag attribute (or -webkit-app-region: drag in CSS) makes it
// drag the window and double-click maximize it, and the buttons in it stay
// clickable. The page draws its own window buttons because titleBar is "none".
export default function Home() {
  const [state, setState] = createSignal<chrome.ChromeState | null>(null);
  const [note, setNote] = createSignal('');

  createEffect(() => {
    chrome
      .mainChrome()
      .state()
      .then(setState)
      .catch((e) => setNote(String(e)));
  });

  // Operations one platform lacks reject with code "unsupported".
  async function tryIt(name: string, run: () => Promise<unknown>) {
    try {
      await run();
      setNote(name + ': ok');
    } catch (e) {
      setNote(name + ': ' + (e as { code?: string }).code);
    }
  }

  return (
    <div class="frame">
      <style>{`
        html, body { margin: 0; height: 100%; background: transparent; font: 14px system-ui, sans-serif; }
        .frame { height: 100%; display: flex; flex-direction: column; background: #1f2430; color: #e8eaf0; }
        .bar { height: 40px; display: flex; align-items: center; justify-content: space-between;
               padding: 0 12px; background: #2a3040; -webkit-app-region: drag; user-select: none; }
        .bar button { -webkit-app-region: no-drag; margin-left: 6px; }
        main { padding: 16px; }
      `}</style>
      <div class="bar" id="bar" data-krate-drag>
        <strong>Native Chrome</strong>
        <span>
          <button id="min" onClick={() => win.main().minimize()}>_</button>
          <button id="max" onClick={() => chrome.mainChrome().toggleMaximize()}>[]</button>
          <button id="close" onClick={() => win.main().close()}>x</button>
        </span>
      </div>
      <main>
        <p id="titlebar">{state() ? 'title bar: ' + state()!.titleBar : 'reading state...'}</p>
        <p id="raw">{JSON.stringify(state())}</p>
        <p id="features">{state() ? 'features: ' + state()!.features.join(', ') : ''}</p>
        <button id="backdrop" onClick={() => tryIt('backdrop', () => chrome.mainChrome().setBackdrop('mica'))}>Mica</button>
        <button id="vibrancy" onClick={() => tryIt('vibrancy', () => chrome.mainChrome().setVibrancy('sidebar'))}>Vibrancy</button>
        <p id="note">{note()}</p>
      </main>
    </div>
  );
}
