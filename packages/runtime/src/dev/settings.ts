// Per-browser overlay preferences, kept in localStorage.

import { esc, isMac, store } from './util.js';
import { icon } from './icons.js';

export type Corner = 'bl' | 'br' | 'tl' | 'tr';
export type Theme = 'system' | 'dark' | 'light';

export interface Settings {
  corner: Corner;
  theme: Theme;
  /** Allowlisted editor id passed to /__krate/open; "" uses devServer.editor. */
  editor: string;
  openOnRuntime: boolean;
  consoleIssues: boolean;
  /** List build warnings (KR5002, ...) with the issues. */
  showWarnings: boolean;
  collapsed: boolean;
}

export const SETTINGS_KEY = 'krate:dev-overlay';

export const DEFAULT_SETTINGS: Settings = {
  corner: 'bl',
  theme: 'system',
  editor: '',
  openOnRuntime: true,
  consoleIssues: true,
  showWarnings: true,
  collapsed: false,
};

const CORNERS: Corner[] = ['bl', 'br', 'tl', 'tr'];
const THEMES: Theme[] = ['system', 'dark', 'light'];

export function loadSettings(): Settings {
  const s = { ...DEFAULT_SETTINGS };
  try {
    const raw = JSON.parse(store.get(SETTINGS_KEY) || '{}') as Partial<Settings>;
    if (CORNERS.includes(raw.corner as Corner)) s.corner = raw.corner as Corner;
    if (THEMES.includes(raw.theme as Theme)) s.theme = raw.theme as Theme;
    if (typeof raw.editor === 'string' && /^[\w.-]{0,40}$/.test(raw.editor)) s.editor = raw.editor;
    for (const k of ['openOnRuntime', 'consoleIssues', 'showWarnings', 'collapsed'] as const) {
      if (typeof raw[k] === 'boolean') s[k] = raw[k] as boolean;
    }
  } catch {
    /* a corrupt entry falls back to the defaults */
  }
  return s;
}

export function saveSettings(s: Settings): void {
  store.set(SETTINGS_KEY, JSON.stringify(s));
}

/** The nearest corner of the viewport to a point. */
export function nearestCorner(x: number, y: number, w: number, h: number): Corner {
  return ((y < h / 2 ? 't' : 'b') + (x < w / 2 ? 'l' : 'r')) as Corner;
}

export type Shortcut = 'menu' | 'inspect' | 'issues' | 'hide';

/**
 * The overlay shortcut of a key event: Alt with K, I, E or the period key,
 * matched by physical key (`code`) so it works on every layout and with
 * macOS Option producing other characters.
 */
export function shortcutOf(e: KeyboardEvent): Shortcut | null {
  if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return null;
  switch (e.code) {
    case 'KeyK':
      return 'menu';
    case 'KeyI':
      return 'inspect';
    case 'KeyE':
      return 'issues';
    case 'Period':
      return 'hide';
  }
  return null;
}

const MAC_KEYS: Record<string, string> = { Alt: 'option', Mod: 'command', Shift: 'arrow-big-up', Enter: 'corner-down-left' };
const PC_KEYS: Record<string, string> = { Alt: 'Alt', Mod: 'Ctrl', Shift: 'Shift', Enter: 'Enter' };

/** The label of one key: an icon for macOS modifiers, text elsewhere. */
export function keyLabel(k: string, mac = isMac()): string {
  if (mac && MAC_KEYS[k]) return icon(MAC_KEYS[k]);
  return esc(PC_KEYS[k] || k);
}

/** A key combination in one kbd chip ("Alt K" / option icon + K). */
export function kbd(keys: string[], mac = isMac()): string {
  return '<span class="kbd">' + keys.map((k) => keyLabel(k, mac)).join(mac ? '' : ' ') + '</span>';
}

/** A key combination as separate kbd chips, for the shortcuts list. */
export function kbds(keys: string[], mac = isMac()): string {
  return keys.map((k) => '<span class="kbd">' + keyLabel(k, mac) + '</span>').join('');
}
