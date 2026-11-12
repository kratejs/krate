import { createSignal, createEffect } from '@krate/runtime';
import { isKrateError } from '@krate/app';
import * as dialog from '@krate/app/dialog';
import * as fs from '@krate/app/fs';
import * as menu from '@krate/app/menu';
import * as notify from '@krate/app/notify';
import * as tray from '@krate/app/tray';
import { quit } from '@krate/app/app';
import { open as openWindow, main } from '@krate/app/window';
import { getSettings, loadDraft, saveDraft, settings } from '../lib/store.host';

// A host signal reads as undefined until the host has pushed its first value.
const theme = () => settings()?.theme ?? 'light';
const fontSize = () => settings()?.fontSize ?? 16;

function why(e: unknown): string {
  return isKrateError(e) ? e.code : String(e);
}

export default function Home() {
  const [text, setText] = createSignal('');
  const [file, setFile] = createSignal('');
  const [status, setStatus] = createSignal('');
  const [trayStatus, setTrayStatus] = createSignal('');
  const [notifyStatus, setNotifyStatus] = createSignal('');
  let draftTimer: ReturnType<typeof setTimeout> | undefined;

  // The preload script's openFile() and saveFileAs() are SDK calls here. A
  // path the user picked in a dialog is granted to the fs API; any other path
  // outside the app directories is refused with permission_denied.
  async function loadFile(path: string) {
    try {
      setText(await fs.readText(path));
      setFile(path);
      setStatus('opened ' + path);
    } catch (e) {
      setStatus('error ' + why(e));
    }
  }
  async function saveFile(path: string, content: string) {
    try {
      await fs.writeText(path, content);
      setFile(path);
      setStatus('saved ' + path);
    } catch (e) {
      setStatus('error ' + why(e));
    }
  }
  async function openDialog() {
    const [path] = await dialog.openFile({ filters: [{ name: 'Text', patterns: ['*.txt', '*.md'] }] });
    if (path) await loadFile(path); // [] when the user cancels
  }
  async function saveAs() {
    const path = await dialog.saveFile({ name: 'note.txt', overwrite: true }); // null when cancelled
    if (path) await saveFile(path, text());
  }
  function newNote() {
    setText('');
    setFile('');
    setStatus('new note');
  }
  async function sendNotification() {
    try {
      await notify.send({ id: 'reminder', title: 'Scratchpad', body: 'Note has ' + text().length + ' characters' });
      setNotifyStatus('notification sent');
    } catch (e) {
      setNotifyStatus('notification ' + why(e)); // "unsupported" without a notification service
    }
  }
  function edit(value: string) {
    setText(value);
    clearTimeout(draftTimer);
    draftTimer = setTimeout(() => saveDraft(value), 300);
  }

  // Everything Electron's main process did at startup happens once, here.
  createEffect(() => {
    loadDraft().then(setText);
    getSettings(); // loads the stored settings into the host signal

    menu.setApp([
      {
        label: '&File',
        submenu: [
          { id: 'file.new', label: 'New', accelerator: 'CmdOrCtrl+N' },
          { id: 'file.open', label: 'Open...', accelerator: 'CmdOrCtrl+O' },
          { id: 'file.saveAs', label: 'Save As...', accelerator: 'CmdOrCtrl+Shift+S' },
          { kind: 'separator' },
          { id: 'file.settings', label: 'Settings...', accelerator: 'CmdOrCtrl+,' },
          { kind: 'separator' },
          { id: 'file.quit', label: 'Quit', accelerator: 'CmdOrCtrl+Q' },
        ],
      },
      {
        label: '&Help',
        submenu: [{ id: 'help.about', label: 'About' }],
      },
    ]).catch((e) => setStatus('menu ' + why(e)));
    menu.onMenu('file.new', newNote);
    menu.onMenu('file.open', openDialog);
    menu.onMenu('file.saveAs', saveAs);
    menu.onMenu('file.settings', () => {
      openWindow({ path: '/settings', title: 'Settings', width: 420, height: 280, fixed: true });
    });
    menu.onMenu('file.quit', () => quit());
    menu.onMenu('help.about', () => {
      dialog.message({ kind: 'info', title: 'About', message: 'Scratchpad 1.0.0' });
    });

    fetch('/tray.png')
      .then((r) => r.arrayBuffer())
      .then((png) =>
        tray.set({
          tooltip: 'Scratchpad',
          icon: new Uint8Array(png),
          menu: [
            { id: 'tray.show', label: 'Show' },
            { id: 'tray.quit', label: 'Quit' },
          ],
        }),
      )
      .catch((e) => setTrayStatus('tray ' + why(e))); // "unsupported" with no tray host on Linux
    tray.onEvent((e) => {
      setTrayStatus('tray ' + e.kind + (e.id ? ' ' + e.id : ''));
      if (e.kind === 'click' || e.id === 'tray.show') {
        main().show();
        main().focus();
      }
      if (e.id === 'tray.quit') quit();
    });
    notify.onAction('reminder', '', () => main().focus());

    // Test hook: a native dialog cannot be clicked headlessly.
    (globalThis as any).__scratch = { loadFile, saveFile };
  });

  return (
    <main data-theme={theme()} style={'font-size:' + fontSize() + 'px'}>
      <style>{`
        main { min-height: 100vh; box-sizing: border-box; padding: 16px; font-family: system-ui, sans-serif; }
        main[data-theme='light'] { background: #ffffff; color: #1a1a1a; }
        main[data-theme='dark'] { background: #1e1e24; color: #ececf1; }
        textarea { width: 100%; box-sizing: border-box; font: inherit; }
      `}</style>
      <h1>Scratchpad</h1>
      <textarea id="note" rows={14} value={text()} onInput={(e) => edit(e.currentTarget.value)} />
      <p>
        <button id="open" onClick={openDialog}>Open...</button>{' '}
        <button id="save-as" onClick={saveAs}>Save As...</button>{' '}
        <button id="notify" onClick={sendNotification}>Notify</button>
      </p>
      <p id="file">{file()}</p>
      <p id="status">{status()}</p>
      <p id="tray-status">{trayStatus()}</p>
      <p id="notify-status">{notifyStatus()}</p>
    </main>
  );
}
