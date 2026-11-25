import { siteConfig } from '../site-config.js';

export interface DevPage {
  route: string;
  source?: string;
  mode?: string;
}

export interface DevConfig {
  sse?: string;
  /** Path of a frame stream carrying `{hmr: message}` frames (Krate Native). Replaces SSE. */
  frames?: string;
  /** Path of the host inspector feed (Krate Native). Enables the Host tab. */
  events?: string;
  /** Path of the per-process memory sample (Krate Native). Enables the Memory tab. */
  memory?: string;
  errors?: string;
  open?: string;
  /** Base of the /__krate/dev JSON endpoints; absent on hosts that do not serve them. */
  api?: string;
  version?: string;
  overlay?: boolean;
  toolbar?: boolean;
  basePath?: string;
  page?: DevPage;
}

export type ResolvedConfig = Required<Omit<DevConfig, 'page'>> & { page?: DevPage };

/** True inside a Krate Native window (`krate://app`, or `https://app.localhost` on Windows). */
export function isNativeOrigin(): boolean {
  try {
    return location.protocol === 'krate:' || location.origin === 'https://app.localhost';
  } catch {
    return false;
  }
}

export function devConfig(): ResolvedConfig {
  const raw = (window as any).__KRATE_DEV__ as DevConfig | undefined;
  const runtime = siteConfig() as DevConfig;
  const native = isNativeOrigin();
  return {
    sse: raw?.sse ?? '/__krate/hotreload',
    frames: raw?.frames ?? (native ? '/__krate/hmr' : ''),
    events: raw?.events ?? (native ? '/__krate/dev/events' : ''),
    memory: raw?.memory ?? (native ? '/__krate/dev/memory' : ''),
    errors: raw?.errors ?? (native ? '/__krate/dev/client-error' : '/__krate/client-error'),
    open: raw?.open ?? '/__krate/open',
    api: raw?.api ?? '',
    version: raw?.version ?? '',
    overlay: raw?.overlay ?? true,
    toolbar: raw?.toolbar ?? true,
    basePath: raw?.basePath ?? runtime?.basePath ?? '',
    page: raw?.page,
  };
}

export function esc(s: unknown): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** Escapes text and renders `backtick` spans as <code>. */
export function richText(s: string): string {
  return esc(s).replace(/`([^`]+)`/g, '<code>$1</code>');
}

function norm(p: string): string {
  return p.replace(/\/+$/, '') || '/';
}

/** Strips the configured base path so match patterns (unprefixed) line up. */
export function stripBase(pathname: string, basePath: string): string {
  if (basePath && (pathname === basePath || pathname.startsWith(basePath + '/'))) {
    const rest = pathname.slice(basePath.length);
    return rest === '' ? '/' : rest;
  }
  return pathname;
}

/**
 * Route matcher: a segment starting with `[` matches anything; `[...x]` matches
 * the rest.
 */
export function routeMatches(pattern: string, pathname: string, basePath = ''): boolean {
  const a = norm(stripBase(pathname, basePath)).split('/');
  const b = norm(pattern).split('/');
  for (let i = 0; i < b.length; i++) {
    const s = b[i];
    if (s.charAt(0) === '[') {
      if (s.indexOf('[...') === 0) return true;
      continue;
    }
    if (s !== a[i]) return false;
  }
  return a.length === b.length;
}

export function formatArg(a: unknown): string {
  if (a instanceof Error) return a.message || String(a);
  if (typeof a === 'string') return a;
  try {
    return JSON.stringify(a) ?? String(a);
  } catch {
    return String(a);
  }
}

/** Kilobytes with one decimal, without the unit. */
export function kb(n: number | undefined): string {
  if (!n || n < 0) return '0';
  const v = n / 1024;
  return v >= 100 ? String(Math.round(v)) : v.toFixed(1).replace(/\.0$/, '');
}

/** A duration: "184ms" below a second, "2.9s" above. */
export function fmtMs(ms: number | undefined): string {
  if (ms === undefined || !Number.isFinite(ms)) return '';
  if (ms < 10) return ms.toFixed(1) + 'ms';
  if (ms < 1000) return Math.round(ms) + 'ms';
  return (ms / 1000).toFixed(ms < 10000 ? 1 : 0) + 's';
}

/** A wall clock time as HH:MM:SS. */
export function clock(t: number): string {
  const d = new Date(t);
  const p = (n: number) => String(n).padStart(2, '0');
  return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}

export function basename(p: string): string {
  const i = p.replace(/\\/g, '/').lastIndexOf('/');
  return i === -1 ? p : p.slice(i + 1);
}

export function dirname(p: string): string {
  const s = p.replace(/\\/g, '/');
  const i = s.lastIndexOf('/');
  return i === -1 ? '' : s.slice(0, i + 1);
}

export function plural(n: number, one: string, many = one + 's'): string {
  return n + ' ' + (n === 1 ? one : many);
}

export function isMac(): boolean {
  try {
    const nav = navigator as Navigator & { userAgentData?: { platform?: string } };
    const p = nav.userAgentData?.platform || nav.platform || nav.userAgent || '';
    return /mac|iphone|ipad/i.test(p);
  } catch {
    return false;
  }
}

/**
 * True when a key event comes from something the user types into on the page:
 * an input, textarea, select or editable element. Elements inside the overlay's
 * own shadow root are not counted.
 */
export function isTypingTarget(e: Event, ownHost?: Element | null): boolean {
  const path = typeof e.composedPath === 'function' ? e.composedPath() : [];
  const t = (path[0] || e.target) as HTMLElement | null;
  if (!t || !(t as Element).tagName) return false;
  if (ownHost && path.includes(ownHost)) return false;
  const tag = t.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') {
    const type = ((t as HTMLInputElement).type || 'text').toLowerCase();
    return !['button', 'checkbox', 'radio', 'submit', 'reset', 'range', 'color', 'file', 'image'].includes(type);
  }
  return t.isContentEditable === true || t.getAttribute?.('contenteditable') === 'true';
}

/** Copies text to the clipboard; resolves false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    try {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.cssText = 'position:fixed;opacity:0;left:-9999px';
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      return ok;
    } catch {
      return false;
    }
  }
}

/** localStorage without the exceptions of private windows and blocked storage. */
export const store = {
  get(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      localStorage.setItem(key, value);
    } catch {
      /* storage is a convenience */
    }
  },
};
