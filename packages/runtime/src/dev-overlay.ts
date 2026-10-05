/**
 * Dev-only error overlay + toolbar.
 *
 * Bundled separately as `krate-dev.js` and injected only by `krate dev`; it is
 * never part of the production runtime. It renders structured compiler
 * diagnostics (file:line:col + source + caret + hint) and client runtime errors,
 * forwards runtime errors back to the dev server, and offers open-in-editor.
 */

interface DevDiagnostic {
  file?: string;
  line?: number;
  col?: number;
  message: string;
  hint?: string;
  source?: string;
}

interface DevClientError {
  message: string;
  stack?: string;
  url?: string;
  line?: number;
  col?: number;
  kind?: string;
}

interface DevEvent {
  type?: string;
  routes?: string[];
  diagnostics?: DevDiagnostic[];
  buildOk?: boolean;
  clientError?: DevClientError;
}

interface DevConfig {
  sse?: string;
  errors?: string;
  open?: string;
  overlay?: boolean;
  toolbar?: boolean;
  basePath?: string;
}

function devConfig(): Required<DevConfig> {
  const raw = (window as any).__KRATE_DEV__ as DevConfig | undefined;
  const runtime = (window as any).__KRATE_CFG__ as DevConfig | undefined;
  return {
    sse: raw?.sse ?? '/__krate/hotreload',
    errors: raw?.errors ?? '/__krate/client-error',
    open: raw?.open ?? '/__krate/open',
    overlay: raw?.overlay ?? true,
    toolbar: raw?.toolbar ?? true,
    basePath: raw?.basePath ?? runtime?.basePath ?? '',
  };
}

function esc(s: unknown): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function norm(p: string): string {
  return p.replace(/\/+$/, '') || '/';
}

/** Strips the configured base path so match patterns (unprefixed) line up. */
function stripBase(pathname: string, basePath: string): string {
  if (basePath && (pathname === basePath || pathname.startsWith(basePath + '/'))) {
    const rest = pathname.slice(basePath.length);
    return rest === '' ? '/' : rest;
  }
  return pathname;
}

/**
 * Route matcher: a segment starting with `[` matches anything; `[...x]` matches
 * the rest. Exported (pure) for testing.
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

const ICON_RELOAD =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>';

const ICON_ALERT =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>' +
  '<path d="M12 9v4"/><path d="M12 17h.01"/></svg>';

const STYLE = `
:host { all: initial; }
* { box-sizing: border-box; }

/* ── palette ─────────────────────────────────────────────────────────── */
:host {
  --red: #f87171;
  --green: #34d399;
  --bg: #111114;
  --bg-2: #17171c;
  --line: #2a2a31;
  --fg: #e5e7eb;
  --muted: #9ca3af;
}

/* ── toolbar ─────────────────────────────────────────────────────────── */
.toolbar {
  position: fixed; left: 50%; bottom: 0; z-index: 2147483001;
  transform: translate(-50%, 54%);
  transition: transform .28s cubic-bezier(.22,.61,.36,1),
              border-top-color .35s ease;
  display: flex; align-items: stretch; gap: 2px;
  background: var(--bg);
  border: 1px solid var(--line);
  border-top: 3px solid var(--green);
  border-radius: 16px 16px 0 0;
  padding: 7px 8px 9px;
  box-shadow: 0 -10px 30px rgba(0,0,0,.45);
  font: 12px/1.3 ui-sans-serif, system-ui, -apple-system, sans-serif;
  color: var(--fg);
}
.toolbar:hover, .toolbar:focus-within { transform: translate(-50%, 0); }
.toolbar.bad { border-top-color: var(--red); }

.tb-btn {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 3px; min-width: 56px; padding: 5px 10px;
  background: transparent; border: 1px solid transparent; border-radius: 11px;
  color: var(--muted); cursor: pointer; font: inherit;
  transition: color .15s ease, background .15s ease;
}
.tb-btn:hover { color: var(--fg); background: rgba(255,255,255,.06); }
.tb-btn .ic { display: block; width: 18px; height: 18px; }
.tb-btn .tb-label { font-size: 10px; letter-spacing: .02em; }
.toolbar.bad .tb-btn.errors { color: var(--red); }

.tb-center {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 1px; min-width: 130px; padding: 2px 16px;
}
.tb-center .brand { font-size: 12px; font-weight: 700; letter-spacing: .05em; color: #f3f4f6; }
.tb-center .route {
  font-size: 10px; color: var(--muted);
  max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

/* ── overlay ─────────────────────────────────────────────────────────── */
.overlay {
  position: fixed; inset: 0; z-index: 2147483000;
  background: rgba(9, 9, 11, 0.72); backdrop-filter: blur(6px);
  display: flex; align-items: flex-start; justify-content: center;
  padding: 4vh 16px; overflow: auto;
  font: 13px/1.5 "SF Mono", Menlo, Monaco, Consolas, "Courier New", monospace;
  color: var(--fg);
}
.overlay[hidden] { display: none; }
.card {
  width: min(920px, 100%); background: var(--bg);
  border: 1px solid var(--line);
  border-radius: 14px; box-shadow: 0 24px 64px rgba(0,0,0,0.5); overflow: hidden;
}
.card-head {
  display: flex; align-items: center; gap: 10px; padding: 14px 18px;
  border-bottom: 1px solid var(--line); background: var(--bg-2);
}
.title { font-weight: 600; font-size: 14px; color: var(--red); }
.count { color: var(--muted); font-size: 12px; }
.spacer { flex: 1; }
.btn {
  border: 1px solid var(--line); background: transparent; color: var(--muted);
  border-radius: 9px; padding: 6px 12px; cursor: pointer; font: inherit; font-size: 12px;
  transition: color .15s ease, background .15s ease, border-color .15s ease;
}
.btn:hover { color: var(--fg); background: rgba(255,255,255,.06); border-color: #44444f; }
.list { padding: 6px 0; }
.diag { padding: 14px 18px; border-bottom: 1px solid #1f1f26; }
.diag:last-child { border-bottom: none; }
.actions { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.loc {
  display: inline-flex; align-items: center; gap: 6px; color: #60a5fa;
  background: transparent; border: none; padding: 0; cursor: pointer;
  font: inherit; font-size: 12px;
}
.loc:hover { text-decoration: underline; }
.msg { color: #f3f4f6; white-space: pre-wrap; word-break: break-word; }
.frame {
  margin-top: 10px; background: #0b0b0e; border: 1px solid #26262e;
  border-radius: 7px; padding: 8px 10px; overflow-x: auto; white-space: pre;
}
.frame .src { color: #cbd5e1; }
.frame .caret { color: var(--red); }
.hint { margin-top: 8px; color: #a3e635; }
.stack {
  margin-top: 10px; color: var(--muted); white-space: pre-wrap; word-break: break-word;
  max-height: 320px; overflow: auto;
}
`;

class KrateDev {
  private cfg = devConfig();
  private shadow!: ShadowRoot;
  private overlay!: HTMLElement;
  private toolbar!: HTMLElement;
  private list!: HTMLElement;
  private title!: HTMLElement;
  private routeEl!: HTMLElement;
  private buildDiags: DevDiagnostic[] = [];
  private runtimeErrors: { title: string; message: string; stack?: string }[] = [];
  private seen = new Set<string>();

  init(): void {
    const host = document.createElement('div');
    host.id = 'krate-dev-root';
    (document.body || document.documentElement).appendChild(host);
    this.shadow = host.attachShadow({ mode: 'open' });

    const style = document.createElement('style');
    style.textContent = STYLE;
    this.shadow.appendChild(style);

    this.overlay = document.createElement('div');
    this.overlay.className = 'overlay';
    this.overlay.hidden = true;
    this.overlay.innerHTML =
      '<div class="card"><div class="card-head">' +
      '<span class="title"></span><span class="count"></span>' +
      '<span class="spacer"></span>' +
      '<button class="btn close" type="button">Dismiss (Esc)</button></div>' +
      '<div class="list"></div></div>';
    this.shadow.appendChild(this.overlay);

    this.title = this.overlay.querySelector('.title')!;
    this.list = this.overlay.querySelector('.list')!;

    if (this.cfg.toolbar) {
      this.toolbar = document.createElement('div');
      this.toolbar.className = 'toolbar';
      this.toolbar.innerHTML =
        '<button class="tb-btn errors" type="button" title="Show errors">' +
        ICON_ALERT +
        '<span class="tb-label">Errors</span></button>' +
        '<div class="tb-center"><span class="brand">krate</span>' +
        '<span class="route"></span></div>' +
        '<button class="tb-btn reload" type="button" title="Reload">' +
        ICON_RELOAD +
        '<span class="tb-label">Reload</span></button>';
      this.shadow.appendChild(this.toolbar);
      this.routeEl = this.toolbar.querySelector('.route')!;
      this.routeEl.textContent = location.pathname;
      this.watchLocation();
    }

    this.shadow.addEventListener('click', (e) => this.onClick(e));
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') this.hide();
    });

    this.connectSSE();
    this.captureRuntimeErrors();
  }

  private onClick(e: Event): void {
    const t = (e.target as HTMLElement).closest('.btn, .loc, .tb-btn') as HTMLElement | null;
    if (!t) return;
    // Toolbar buttons are inside a `:focus-within` container that keeps the
    // half-hidden toolbar raised; blur after acting so a click (with no errors
    // to show) lets it slide back down. Keyboard focus still raises it.
    if (t.classList.contains('tb-btn')) t.blur();
    if (t.classList.contains('close')) {
      this.hide();
      return;
    }
    if (t.classList.contains('errors') || t.classList.contains('toggle')) {
      if (this.overlay.hidden) this.show();
      else this.hide();
      return;
    }
    if (t.classList.contains('reload')) {
      location.reload();
      return;
    }
    if (t.classList.contains('loc') || t.classList.contains('open')) {
      this.openInEditor(t.getAttribute('data-file') || '', t.getAttribute('data-line') || '');
    }
  }

  private openInEditor(file: string, line: string): void {
    if (!file) return;
    const url = this.cfg.open + '?file=' + encodeURIComponent(file) + '&line=' + encodeURIComponent(line);
    fetch(url).catch(() => {});
  }

  /** Keep the toolbar route label in sync with SPA (history) navigation. */
  private watchLocation(): void {
    const update = () => {
      if (this.routeEl) this.routeEl.textContent = location.pathname;
    };
    window.addEventListener('popstate', update);
    const h = history as any;
    const push = h.pushState.bind(history);
    h.pushState = function (...args: unknown[]) {
      const r = push(...args);
      update();
      return r;
    };
    const replace = h.replaceState.bind(history);
    h.replaceState = function (...args: unknown[]) {
      const r = replace(...args);
      update();
      return r;
    };
    // Safety net for routers that swap content without touching history.
    setInterval(update, 500);
  }

  private connectSSE(): void {
    let sse: EventSource;
    try {
      sse = new EventSource(this.cfg.sse);
    } catch {
      return;
    }
    sse.addEventListener('reload', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data);
        if (
          d.pages &&
          Array.isArray(d.pages) &&
          !d.pages.some((p: string) => routeMatches(p, location.pathname, this.cfg.basePath))
        ) {
          return;
        }
      } catch {
        /* full reload */
      }
      location.reload();
    });
    sse.addEventListener('build-error', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data);
        this.buildDiags = Array.isArray(d.diagnostics) ? d.diagnostics : [];
      } catch {
        this.buildDiags = [];
      }
      this.setStatus(this.buildDiags.length === 0);
      this.render();
    });
    sse.addEventListener('client-error', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data) as DevEvent;
        if (d.clientError) this.addRuntime('Runtime Error', d.clientError.message, d.clientError.stack);
      } catch {
        /* ignore */
      }
    });
  }

  private captureRuntimeErrors(): void {
    window.addEventListener('error', (e: ErrorEvent) => {
      const stack = e.error?.stack || '';
      this.report({
        message: e.message || 'Unknown error',
        stack,
        url: e.filename,
        line: e.lineno,
        col: e.colno,
        kind: 'error',
      });
      this.addRuntime('Runtime Error', e.message || 'Unknown error', stack);
    });
    window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
      const reason = e.reason;
      const message = reason instanceof Error ? reason.message : String(reason);
      const stack = reason instanceof Error ? reason.stack : '';
      this.report({ message, stack, url: location.href, kind: 'unhandledrejection' });
      this.addRuntime('Unhandled Promise Rejection', message, stack);
    });
  }

  private report(payload: DevClientError): void {
    const key = payload.message + '|' + (payload.stack || '');
    if (this.seen.has(key)) return;
    this.seen.add(key);
    try {
      const body = JSON.stringify(payload);
      if (navigator.sendBeacon) {
        navigator.sendBeacon(this.cfg.errors, new Blob([body], { type: 'application/json' }));
      } else {
        fetch(this.cfg.errors, { method: 'POST', body, headers: { 'Content-Type': 'application/json' }, keepalive: true });
      }
    } catch {
      /* reporting is best-effort */
    }
  }

  private addRuntime(title: string, message: string, stack?: string): void {
    this.runtimeErrors.push({ title, message, stack });
    this.setStatus(false);
    this.render();
  }

  private setStatus(ok: boolean): void {
    if (!this.toolbar) return;
    this.toolbar.classList.toggle('bad', !ok);
  }

  private frameHTML(d: DevDiagnostic): string {
    if (!d.source) return '';
    const col = Math.max(1, d.col || 1);
    const caret = ' '.repeat(col - 1) + '^';
    return '<div class="frame"><span class="src">' + esc(d.source) + '</span>\n<span class="caret">' + esc(caret) + '</span></div>';
  }

  private diagHTML(d: DevDiagnostic): string {
    let actions = '';
    if (d.file) {
      const loc = d.file + (d.line ? ':' + d.line + (d.col ? ':' + d.col : '') : '');
      actions =
        '<div class="actions">' +
        '<button class="loc" data-file="' + esc(d.file) + '" data-line="' + esc(d.line || '') + '">' + esc(loc) + '</button>' +
        '<button class="btn open" data-file="' + esc(d.file) + '" data-line="' + esc(d.line || '') + '">Open in editor</button>' +
        '</div>';
    }
    const hint = d.hint ? '<div class="hint">hint: ' + esc(d.hint) + '</div>' : '';
    return '<div class="diag">' + actions + '<div class="msg">' + esc(d.message) + '</div>' + this.frameHTML(d) + hint + '</div>';
  }

  private runtimeHTML(r: { title: string; message: string; stack?: string }): string {
    const stack = r.stack ? '<div class="stack">' + esc(r.stack) + '</div>' : '';
    return '<div class="diag"><div class="msg">' + esc(r.message) + '</div>' + stack + '</div>';
  }

  private render(): void {
    const total = this.buildDiags.length + this.runtimeErrors.length;
    if (total === 0) {
      this.hide();
      return;
    }
    this.title.textContent = this.buildDiags.length > 0 ? 'Compilation failed' : 'Runtime error';
    let html = '';
    for (const d of this.buildDiags) html += this.diagHTML(d);
    for (const r of this.runtimeErrors) html += this.runtimeHTML(r);
    this.list.innerHTML = html;
    (this.overlay.querySelector('.count') as HTMLElement).textContent = total > 1 ? String(total) : '';
    if (this.cfg.overlay) this.overlay.hidden = false;
  }

  private show(): void {
    if (this.buildDiags.length + this.runtimeErrors.length > 0) this.overlay.hidden = false;
  }

  private hide(): void {
    this.overlay.hidden = true;
  }
}

/** Initialise the dev overlay. Safe to call more than once. */
export function initDevOverlay(): void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  if ((window as any).__krate_dev_overlay) return;
  (window as any).__krate_dev_overlay = true;
  try {
    new KrateDev().init();
  } catch (err) {
    // Never let the dev tooling break the app.
    console.error('[krate] dev overlay failed to initialise', err);
  }
}

if (typeof window !== 'undefined' && typeof document !== 'undefined') {
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initDevOverlay, { once: true });
  } else {
    initDevOverlay();
  }
}
