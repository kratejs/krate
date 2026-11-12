import { hostSignal } from '@krate/app/host';
import * as kv from '@krate/app/kv';
import * as fs from '@krate/app/fs';

// Host code runs in the native process, like Electron's main process. Each
// exported function replaces one ipcMain.handle(...) channel; the host signal
// replaces webContents.send('settings', ...) to every window.

export interface Settings {
  theme: 'light' | 'dark';
  fontSize: number;
}

const defaults: Settings = { theme: 'light', fontSize: 16 };

export const settings = hostSignal<Settings>(defaults);

let loaded: Promise<void> | undefined;

function load(): Promise<void> {
  loaded ??= kv.get<Settings>('settings').then((saved) => {
    if (saved) settings.set({ ...defaults, ...saved });
  });
  return loaded;
}

export async function getSettings(): Promise<Settings> {
  await load();
  return settings.get();
}

export async function updateSettings(patch: Partial<Settings>): Promise<Settings> {
  await load();
  const next = { ...settings.get(), ...patch };
  if (next.theme !== 'light' && next.theme !== 'dark') throw new Error('theme must be light or dark');
  if (!(next.fontSize >= 10 && next.fontSize <= 32)) throw new Error('fontSize must be 10 to 32');
  await kv.set('settings', next);
  settings.set(next);
  return next;
}

// A relative path is relative to the app data directory (userData in Electron).
export async function loadDraft(): Promise<string> {
  return (await fs.exists('draft.txt')) ? fs.readText('draft.txt') : '';
}

export async function saveDraft(text: string): Promise<void> {
  await fs.writeText('draft.txt', text);
}
