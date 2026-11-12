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
  /** The window or view id the page was told it is in (Krate Native). */
  window?: string;
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
  /** Path of a frame stream carrying `{hmr: message}` frames (Krate Native). Replaces SSE. */
  frames?: string;
  /** Path of the host inspector feed (Krate Native). Enables the Host panel. */
  events?: string;
  /** Path of the per-process memory sample (Krate Native). Enables the Memory panel. */
  memory?: string;
  errors?: string;
  open?: string;
  overlay?: boolean;
  toolbar?: boolean;
  basePath?: string;
}

/** True inside a Krate Native window (`krate://app`, or `https://app.localhost` on Windows). */
export function isNativeOrigin(): boolean {
  try {
    return location.protocol === 'krate:' || location.origin === 'https://app.localhost';
  } catch {
    return false;
  }
}

function devConfig(): Required<DevConfig> {
  const raw = (window as any).__KRATE_DEV__ as DevConfig | undefined;
  const runtime = (window as any).__KRATE_CFG__ as DevConfig | undefined;
  const native = isNativeOrigin();
  return {
    sse: raw?.sse ?? '/__krate/hotreload',
    frames: raw?.frames ?? (native ? '/__krate/hmr' : ''),
    events: raw?.events ?? (native ? '/__krate/dev/events' : ''),
    memory: raw?.memory ?? (native ? '/__krate/dev/memory' : ''),
    errors: raw?.errors ?? (native ? '/__krate/dev/client-error' : '/__krate/client-error'),
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

// source-map symbolication
// When the build enables source maps (`--sourcemap` / `sourcemap: true`), the
// overlay decodes them so runtime stack traces point at real source locations
// instead of minified bundle offsets. Fetches fail silently when maps are
// absent, so this is always safe to run.

interface MapSegment {
  genCol: number;
  src: number;
  srcLine: number;
  srcCol: number;
}

interface DecodedMap {
  sources: string[];
  lines: MapSegment[][];
}

const B64_VLQ = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

/** Decodes a Base64 VLQ mapping segment into a delta array. */
export function decodeVLQ(str: string): number[] {
  const out: number[] = [];
  let shift = 0;
  let value = 0;
  for (let i = 0; i < str.length; i++) {
    const digit = B64_VLQ.indexOf(str[i]);
    if (digit === -1) continue;
    const cont = (digit & 32) !== 0;
    value += (digit & 31) << shift;
    if (cont) {
      shift += 5;
      continue;
    }
    const neg = (value & 1) === 1;
    value >>= 1;
    out.push(neg ? -value : value);
    value = 0;
    shift = 0;
  }
  return out;
}

/** Decodes a v3 `mappings` string into per-generated-line segments. */
export function decodeMappings(mappings: string): MapSegment[][] {
  const lines: MapSegment[][] = [];
  let src = 0;
  let srcLine = 0;
  let srcCol = 0;
  for (const line of mappings.split(';')) {
    const segs: MapSegment[] = [];
    let genCol = 0;
    if (line) {
      for (const part of line.split(',')) {
        if (!part) continue;
        const nums = decodeVLQ(part);
        genCol += nums[0] || 0;
        if (nums.length === 1) {
          // An end marker: no original position from here on.
          segs.push({ genCol, src: -1, srcLine: 0, srcCol: 0 });
        } else if (nums.length >= 4) {
          src += nums[1];
          srcLine += nums[2];
          srcCol += nums[3];
          segs.push({ genCol, src, srcLine, srcCol });
        }
      }
    }
    lines.push(segs);
  }
  return lines;
}

const mapCache = new Map<string, Promise<DecodedMap | null>>();

async function loadSourceMap(url: string): Promise<DecodedMap | null> {
  const cached = mapCache.get(url);
  if (cached) return cached;
  const p = (async () => {
    try {
      const u = new URL(url, location.href);
      if (u.protocol !== location.protocol || u.host !== location.host) return null;
      const res = await fetch(url + '.map');
      if (!res.ok) return null;
      const json = await res.json();
      if (!json || typeof json.mappings !== 'string') return null;
      return { sources: (json.sources as string[]) || [], lines: decodeMappings(json.mappings) } as DecodedMap;
    } catch {
      return null;
    }
  })();
  mapCache.set(url, p);
  return p;
}

interface StackFrame {
  prefix: string;
  url: string;
  line: number;
  col: number;
  suffix: string;
}

/** Parses a V8/Firefox stack line into its script location, if present. */
export function parseStackFrame(line: string): StackFrame | null {
  const patterns = [
    /^(.*?)((?:[a-z][a-z0-9+.-]*:)?\/\/[^\s)]+|\/[^\s):]+):(\d+):(\d+)(.*)$/,
  ];
  for (const re of patterns) {
    const m = re.exec(line);
    if (!m) continue;
    return { prefix: m[1], url: m[2], line: Number(m[3]), col: Number(m[4]), suffix: m[5] };
  }
  return null;
}

function remapLocation(map: DecodedMap, line: number, col: number): { source: string; line: number; col: number } | null {
  let best: MapSegment | null = null;
  const segs = map.lines[line - 1];
  if (segs && segs.length > 0) {
    for (const s of segs) {
      if (s.genCol <= col - 1) best = s;
      else break;
    }
    if (!best) best = segs[0];
  } else {
    // An unminified hydration map has a mapping only where a marker starts;
    // it holds until the next one, so a line without any takes the last
    // mapping before it.
    for (let i = Math.min(line - 1, map.lines.length) - 1; i >= 0 && !best; i--) {
      const prev = map.lines[i];
      if (prev && prev.length > 0) best = prev[prev.length - 1];
    }
    if (!best) return null;
  }
  if (best.src < 0) return null;
  const source = map.sources[best.src] || '';
  return { source, line: best.srcLine + 1, col: best.srcCol + 1 };
}

/** Rewrites each mappable frame of a stack trace to its original location. */
export async function symbolicateStack(stack: string): Promise<string> {
  if (!stack || stack.indexOf('.js') === -1) return stack;
  const lines = stack.split('\n');
  const out = await Promise.all(
    lines.map(async (line) => {
      const frame = parseStackFrame(line);
      if (!frame) return line;
      const map = await loadSourceMap(frame.url);
      if (!map) return line;
      const loc = remapLocation(map, frame.line, frame.col);
      // The generic label of a compiler-generated bundle says nothing more
      // than the frame's own file and line.
      if (!loc || !loc.source || loc.source === 'krate-hydration.js') return line;
      return frame.prefix + loc.source + ':' + loc.line + ':' + loc.col + frame.suffix;
    }),
  );
  return out.join('\n');
}

/**
 * Route matcher: a segment starting with `[` matches anything; `[...x]` matches
 * the rest. Exported (pure) for testing.
 */
function formatArg(a: unknown): string {
  if (a instanceof Error) return a.message || String(a);
  if (typeof a === 'string') return a;
  try {
    return JSON.stringify(a) ?? String(a);
  } catch {
    return String(a);
  }
}

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

/**
 * Splits a buffer of Krate frames (u32 length, JSON, u16 blob count, blobs)
 * into its complete JSON values. `rest` is the incomplete tail to prepend to
 * the next chunk. Exported (pure) for testing.
 */
export function parseFrames(buf: Uint8Array): { values: unknown[]; rest: Uint8Array } {
  const values: unknown[] = [];
  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const td = new TextDecoder();
  let pos = 0;
  while (buf.length - pos >= 4) {
    const n = view.getUint32(pos);
    let end = pos + 4 + n;
    if (buf.length < end + 2) break;
    const json = td.decode(buf.subarray(pos + 4, end));
    const blobs = view.getUint16(end);
    end += 2;
    let complete = true;
    for (let i = 0; i < blobs; i++) {
      if (buf.length < end + 4) {
        complete = false;
        break;
      }
      end += 4 + view.getUint32(end);
      if (buf.length < end) {
        complete = false;
        break;
      }
    }
    if (!complete) break;
    try {
      values.push(JSON.parse(json));
    } catch {
      /* skip a malformed frame */
    }
    pos = end;
  }
  return { values, rest: buf.subarray(pos) };
}

/** The host-side stack a KrateError carries in dev (`data.stack`), or "". */
export function hostStackOf(reason: unknown): string {
  const data = (reason as { data?: unknown } | null)?.data;
  const stack = data && typeof data === 'object' ? (data as { stack?: unknown }).stack : undefined;
  return typeof stack === 'string' ? stack : '';
}

interface HostEvent {
  seq: number;
  kind: string;
  fn: string;
  id?: number;
  time?: string;
  durNs?: number;
  code?: string;
  message?: string;
}

/** One line of the Host panel. Exported (pure) for testing. */
export function hostEventLine(ev: HostEvent): string {
  const t = ev.time ? new Date(ev.time).toLocaleTimeString() : '';
  const dur = ev.durNs ? (ev.durNs / 1e6 >= 10 ? Math.round(ev.durNs / 1e6) : Math.round(ev.durNs / 1e5) / 10) + 'ms' : '';
  const err = ev.code ? ' ' + ev.code + (ev.message ? ': ' + ev.message : '') : '';
  return [t, ev.kind, ev.fn, dur].filter(Boolean).join(' ') + err;
}

/** The memstat report the shell serves at /__krate/dev/memory (bytes). */
export interface MemoryReport {
  os?: string;
  source?: string;
  procs?: { pid: number; name?: string; role: string; rss?: number; pss: number; privateDirty: number }[];
  total?: { pss: number; privateDirty: number; rss?: number };
  notes?: string[];
}

/** Format a byte count for the Memory panel. Exported (pure) for testing. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return 'n/a';
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(2) + ' GiB';
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MiB';
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + ' KiB';
  return Math.round(n) + ' B';
}

/**
 * The JS heap of this page, or null where the engine does not expose it.
 * Chromium has performance.memory; WebKit (the engine of the Linux and macOS
 * webviews) has no heap size API, so the panel shows the web process row
 * (PSS and private dirty, which include the JS heap) instead.
 */
export function jsHeapBytes(): number | null {
  const m = (typeof performance !== 'undefined' ? (performance as any).memory : undefined) as { usedJSHeapSize?: number } | undefined;
  return m && typeof m.usedJSHeapSize === 'number' ? m.usedJSHeapSize : null;
}

/** The rows of the Memory panel as [label, value] pairs. Exported (pure) for testing. */
export function memoryRows(rep: MemoryReport | null, domNodes: number, heap: number | null): [string, string][] {
  const rows: [string, string][] = [];
  if (rep && rep.procs) {
    for (const p of rep.procs) {
      rows.push([p.role + ' (' + p.pid + ')', 'PSS ' + formatBytes(p.pss) + ' / private dirty ' + formatBytes(p.privateDirty)]);
    }
    if (rep.total) rows.push(['total', 'PSS ' + formatBytes(rep.total.pss) + ' / private dirty ' + formatBytes(rep.total.privateDirty)]);
  } else {
    rows.push(['processes', 'unavailable']);
  }
  rows.push(['DOM nodes', String(domNodes)]);
  rows.push(['JS heap', heap === null ? 'unavailable in this engine (see the web process row)' : formatBytes(heap)]);
  return rows;
}

const ICON_MEM =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<rect x="4" y="6" width="16" height="12" rx="1"/><path d="M8 10v4M12 10v4M16 10v4"/><path d="M7 18v2M12 18v2M17 18v2"/></svg>';

const ICON_RELOAD =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>';

const ICON_ALERT =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>' +
  '<path d="M12 9v4"/><path d="M12 17h.01"/></svg>';

const ICON_HOST =
  '<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
  '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8"/><path d="M12 16v4"/></svg>';

const STYLE = `
:host { all: initial; }
* { box-sizing: border-box; }

/* palette */
:host {
  --red: #f87171;
  --green: #34d399;
  --bg: #111111;
  --bg-2: #171717;
  --line: #2a2a2a;
  --fg: #e5e5e5;
  --muted: #a3a3a3;
}

/* toolbar */
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

/* overlay */
.overlay {
  position: fixed; inset: 0; z-index: 2147483000;
  background: rgba(10, 10, 10, 0.72); backdrop-filter: blur(6px);
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
.btn:hover { color: var(--fg); background: rgba(255,255,255,.06); border-color: #444444; }
.list { padding: 6px 0; }
.diag { padding: 14px 18px; border-bottom: 1px solid #1f1f1f; }
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
  margin-top: 10px; background: #0b0b0b; border: 1px solid #262626;
  border-radius: 7px; padding: 8px 10px; overflow-x: auto; white-space: pre;
}
.frame .src { color: #d4d4d4; }
.frame .caret { color: var(--red); }
.hint { margin-top: 8px; color: #a3e635; }
.hostpanel {
  position: fixed; right: 12px; bottom: 12px; z-index: 2147483002;
  width: min(560px, calc(100vw - 24px)); max-height: 50vh; display: flex; flex-direction: column;
  background: var(--bg); border: 1px solid var(--line); border-radius: 12px;
  box-shadow: 0 16px 48px rgba(0,0,0,.5); overflow: hidden;
  font: 12px/1.45 "SF Mono", Menlo, Monaco, Consolas, "Courier New", monospace; color: var(--fg);
}
.hostpanel[hidden] { display: none; }
.hostpanel .card-head { padding: 8px 12px; }
.hostrows { overflow: auto; padding: 4px 0; }
.hostrow { padding: 2px 12px; white-space: pre-wrap; word-break: break-word; }
.hostrow.err { color: var(--red); }
.hostrow.sig { color: var(--muted); }
.mempanel {
  position: fixed; right: 12px; bottom: 12px; z-index: 2147483002;
  width: min(460px, calc(100vw - 24px));
  background: var(--bg); border: 1px solid var(--line); border-radius: 12px;
  box-shadow: 0 16px 48px rgba(0,0,0,.5); overflow: hidden;
  font: 12px/1.45 "SF Mono", Menlo, Monaco, Consolas, "Courier New", monospace; color: var(--fg);
}
.mempanel[hidden] { display: none; }
.mempanel .card-head { padding: 8px 12px; }
.memrow { display: flex; gap: 12px; justify-content: space-between; padding: 2px 12px; }
.memrow .k { color: var(--muted); }
.memrow .v { text-align: right; }
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
  private runtimeErrors: { title: string; message: string; stack?: string; hostStack?: string }[] = [];
  private hostPanel?: HTMLElement;
  private hostRows?: HTMLElement;
  private hostSeq = 0;
  private hostTimer?: number;
  private memPanel?: HTMLElement;
  private memRows?: HTMLElement;
  private memTimer?: number;
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
        (this.cfg.events
          ? '<button class="tb-btn host" type="button" title="Host calls">' + ICON_HOST + '<span class="tb-label">Host</span></button>'
          : '') +
        (this.cfg.memory
          ? '<button class="tb-btn mem" type="button" title="Memory">' + ICON_MEM + '<span class="tb-label">Memory</span></button>'
          : '') +
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

    if (this.cfg.frames) this.connectFrames();
    else this.connectSSE();
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
    if (t.classList.contains('host')) {
      this.toggleHost();
      return;
    }
    if (t.classList.contains('mem')) {
      this.toggleMemory();
      return;
    }
    if (t.classList.contains('memclose')) {
      this.toggleMemory(false);
      return;
    }
    if (t.classList.contains('hostclose')) {
      this.toggleHost(false);
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
    let connectedOnce = false;
    sse.addEventListener('connected', () => {
      // A second "connected" means the stream dropped and came back: the dev
      // server restarted (config or env change), so the page is stale.
      if (connectedOnce) location.reload();
      connectedOnce = true;
    });
    const parse = (e: Event): any => {
      try {
        return JSON.parse((e as MessageEvent).data);
      } catch {
        return null;
      }
    };
    sse.addEventListener('reload', (e) => this.onReload(parse(e)));
    sse.addEventListener('build-error', (e) => this.onBuildError(parse(e)));
    sse.addEventListener('client-error', (e) => this.onClientError(parse(e)));
  }

  /**
   * Krate Native: the same events as SSE, as `{hmr: {type, ...}}` frames on a
   * streaming fetch. Reconnects after the stream ends; a second "connected"
   * means the shell restarted, so the page is stale.
   */
  private connectFrames(): void {
    let connectedOnce = false;
    const run = async (): Promise<void> => {
      const res = await fetch(this.cfg.frames, { cache: 'no-store' });
      if (!res.ok || !res.body) throw new Error('hmr stream unavailable');
      const reader = res.body.getReader();
      let buf: Uint8Array = new Uint8Array(0);
      for (;;) {
        const { done, value } = await reader.read();
        if (done) return;
        const joined = new Uint8Array(buf.length + value.length);
        joined.set(buf);
        joined.set(value, buf.length);
        const parsed = parseFrames(joined);
        buf = parsed.rest;
        for (const v of parsed.values) {
          const m = (v as { hmr?: any } | null)?.hmr;
          if (!m || typeof m !== 'object') continue;
          if (m.type === 'connected') {
            if (connectedOnce) location.reload();
            connectedOnce = true;
          } else if (m.type === 'reload') this.onReload(m);
          else if (m.type === 'build-error') this.onBuildError(m);
          else if (m.type === 'client-error') this.onClientError(m);
        }
      }
    };
    const loop = (): void => {
      run().then(
        () => setTimeout(loop, 1000),
        () => setTimeout(loop, 1000),
      );
    };
    loop();
  }

  private onReload(d: any): void {
    // A rebuild without diagnostics supersedes any compile error on screen,
    // whichever route it touched.
    if (this.buildDiags.length > 0) {
      this.buildDiags = [];
      this.setStatus(true);
      this.render();
    }
    let cssOnly = false;
    if (d) {
      cssOnly = d.cssOnly === true;
      const pages = d.pages;
      if (
        pages &&
        Array.isArray(pages) &&
        !pages.some((p: string) => p === '/404' || p === '/500' || routeMatches(p, location.pathname, this.cfg.basePath))
      ) {
        return;
      }
    }
    if (cssOnly) {
      this.swapStylesheets().catch(() => location.reload());
      return;
    }
    location.reload();
  }

  private onBuildError(d: any): void {
    this.buildDiags = d && Array.isArray(d.diagnostics) ? d.diagnostics : [];
    this.setStatus(this.buildDiags.length === 0);
    this.render();
  }

  private onClientError(d: DevEvent | null): void {
    if (!d || !d.clientError) return;
    const err = d.clientError;
    // This tab reported the error itself and already shows it.
    if (this.seen.has(err.message + '|' + (err.stack || ''))) return;
    this.addRuntime('Runtime Error', err.message, err.stack);
  }

  private toggleHost(force?: boolean): void {
    if (!this.cfg.events) return;
    if (!this.hostPanel) {
      const el = document.createElement('div');
      el.className = 'hostpanel';
      el.hidden = true;
      el.innerHTML =
        '<div class="card-head"><span class="title" style="color:inherit">Host calls</span>' +
        '<span class="spacer"></span><button class="btn hostclose" type="button">Close</button></div>' +
        '<div class="hostrows"></div>';
      this.shadow.appendChild(el);
      this.hostPanel = el;
      this.hostRows = el.querySelector('.hostrows') as HTMLElement;
    }
    const open = force ?? this.hostPanel.hidden;
    this.hostPanel.hidden = !open;
    if (this.hostTimer) clearInterval(this.hostTimer);
    this.hostTimer = undefined;
    if (open) {
      void this.pollHost();
      this.hostTimer = setInterval(() => void this.pollHost(), 1000) as unknown as number;
    }
  }

  /** The Memory panel samples only while it is open (every 2 seconds). */
  private toggleMemory(force?: boolean): void {
    if (!this.cfg.memory) return;
    if (!this.memPanel) {
      const el = document.createElement('div');
      el.className = 'mempanel';
      el.hidden = true;
      el.innerHTML =
        '<div class="card-head"><span class="title" style="color:inherit">Memory</span>' +
        '<span class="spacer"></span><button class="btn memclose" type="button">Close</button></div>' +
        '<div class="memrows"></div>';
      this.shadow.appendChild(el);
      this.memPanel = el;
      this.memRows = el.querySelector('.memrows') as HTMLElement;
    }
    const open = force ?? this.memPanel.hidden;
    this.memPanel.hidden = !open;
    if (this.memTimer) clearInterval(this.memTimer);
    this.memTimer = undefined;
    if (open) {
      void this.pollMemory();
      this.memTimer = setInterval(() => void this.pollMemory(), 2000) as unknown as number;
    }
  }

  private async pollMemory(): Promise<void> {
    let rep: MemoryReport | null = null;
    try {
      const res = await fetch(this.cfg.memory, { cache: 'no-store' });
      if (res.ok) rep = (await res.json()) as MemoryReport;
    } catch {
      /* the panel is best-effort */
    }
    const rows = memoryRows(rep, document.getElementsByTagName('*').length, jsHeapBytes());
    this.memRows!.innerHTML = rows
      .map(([k, v]) => '<div class="memrow"><span class="k">' + esc(k) + '</span><span class="v">' + esc(v) + '</span></div>')
      .join('');
  }

  private async pollHost(): Promise<void> {
    try {
      const res = await fetch(this.cfg.events + '?since=' + this.hostSeq, { cache: 'no-store' });
      if (!res.ok) return;
      const parsed = parseFrames(new Uint8Array(await res.arrayBuffer()));
      const rows = this.hostRows!;
      const pinned = rows.scrollTop + rows.clientHeight >= rows.scrollHeight - 8;
      for (const v of parsed.values) {
        const ev = (v as { ev?: HostEvent } | null)?.ev;
        if (!ev) continue;
        this.hostSeq = Math.max(this.hostSeq, ev.seq);
        const row = document.createElement('div');
        row.className = 'hostrow' + (ev.code ? ' err' : ev.kind === 'signal' ? ' sig' : '');
        row.textContent = hostEventLine(ev);
        rows.appendChild(row);
      }
      while (rows.childElementCount > 200) rows.firstElementChild!.remove();
      if (pinned) rows.scrollTop = rows.scrollHeight;
    } catch {
      /* the panel is best-effort */
    }
  }

  /**
   * Apply a CSS-only rebuild without a full reload: fetch the freshly built
   * document, add its stylesheet links, and drop the ones it no longer has.
   * Inline <style> changes cannot be diffed safely, so they fall back to a
   * reload (the caller reloads when this rejects).
   */
  private async swapStylesheets(): Promise<void> {
    const res = await fetch(location.href, { cache: 'no-store', headers: { Accept: 'text/html' } });
    if (!res.ok) throw new Error('fetch failed');
    const next = new DOMParser().parseFromString(await res.text(), 'text/html');
    const inline = (d: Document) =>
      Array.from(d.querySelectorAll('style'))
        .map((el) => el.textContent || '')
        .join('\u0000');
    if (inline(next) !== inline(document)) throw new Error('inline styles changed');
    const abs = (href: string) => new URL(href, location.href).href;
    const want = Array.from(next.querySelectorAll('link[rel~="stylesheet"]')).map((l) => abs(l.getAttribute('href') || ''));
    const current = Array.from(document.querySelectorAll('link[rel~="stylesheet"]')) as HTMLLinkElement[];
    const have = new Set(current.map((l) => abs(l.getAttribute('href') || '')));
    const loads: Promise<void>[] = [];
    for (const href of want) {
      if (have.has(href)) continue;
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = href;
      loads.push(
        new Promise<void>((resolve, reject) => {
          link.onload = () => resolve();
          link.onerror = () => reject(new Error('stylesheet failed'));
        }),
      );
      document.head.appendChild(link);
    }
    await Promise.all(loads);
    const keep = new Set(want);
    for (const l of current) {
      if (!keep.has(abs(l.getAttribute('href') || ''))) l.remove();
    }
    // A same-named stylesheet is cache-busted in place.
    for (const l of current) {
      const href = abs(l.getAttribute('href') || '');
      if (!keep.has(href) || !l.isConnected) continue;
      const u = new URL(href);
      u.searchParams.set('_krate', String(Date.now()));
      l.href = u.href;
    }
  }

  private captureRuntimeErrors(): void {
    window.addEventListener('error', (e) => this.onError(e));
    window.addEventListener('unhandledrejection', (e) => this.onRejection(e));
    // A script or stylesheet that fails to load fires on its element and does
    // not bubble, so only the capture phase sees it.
    window.addEventListener('error', (e) => this.onResourceError(e), true);
    this.captureConsole();
    // This bundle loads after the page script, so the page's first render ran
    // before these listeners existed. The head buffers what happened until now.
    const early = (window as any).__KRATE_EARLY_ERRORS__ as any[] | undefined;
    (window as any).__KRATE_EARLY_ERRORS__ = undefined;
    for (const e of early ?? []) {
      if (e.type === 'console') this.onConsole(e.message as string, e.stack as string | undefined);
      else if (e.type === 'error' && e.target && e.target !== window) this.onResourceError(e as ErrorEvent);
      else if (e.type === 'error') this.onError(e as ErrorEvent);
      else this.onRejection(e as PromiseRejectionEvent);
    }
  }

  private captureConsole(): void {
    const c = console as any;
    const orig = c.error;
    c.error = (...args: unknown[]) => {
      try {
        const first = args.find((a) => a instanceof Error) as Error | undefined;
        this.onConsole(args.map(formatArg).join(' '), first?.stack);
      } catch {
        /* never break the page's own logging */
      }
      return orig.apply(c, args);
    };
  }

  private onConsole(message: string, stack?: string): void {
    this.report({ message, stack, url: location.href, kind: 'console' });
    this.addRuntime('console.error', message, stack);
  }

  private onResourceError(e: Event): void {
    const t = e.target as (HTMLElement & { src?: string; href?: string }) | null;
    if (!t || t === (window as any) || !t.tagName) return;
    const src = t.src || t.href || '';
    if (!src) return;
    const message = 'failed to load <' + t.tagName.toLowerCase() + '> ' + src;
    this.report({ message, url: src, kind: 'resource' });
    this.addRuntime('Resource Error', message);
  }

  private onError(e: ErrorEvent): void {
    const stack = e.error?.stack || '';
    const message = e.message || 'Unknown error';
    const hostStack = hostStackOf(e.error);
    this.report({
      message,
      stack: hostStack ? stack + '\nHost stack:\n' + hostStack : stack,
      url: e.filename,
      line: e.lineno,
      col: e.colno,
      kind: 'error',
    });
    this.addRuntime('Runtime Error', message, stack, hostStack);
  }

  private onRejection(e: PromiseRejectionEvent): void {
    const reason = e.reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    const stack = reason instanceof Error ? reason.stack : '';
    const hostStack = hostStackOf(reason);
    this.report({ message, stack: hostStack ? (stack || '') + '\nHost stack:\n' + hostStack : stack, url: location.href, kind: 'unhandledrejection' });
    this.addRuntime('Unhandled Promise Rejection', message, stack, hostStack);
  }

  private report(payload: DevClientError): void {
    const key = payload.message + '|' + (payload.stack || '');
    if (this.seen.has(key)) return;
    this.seen.add(key);
    const win = (globalThis as any).__KRATE_WINDOW__;
    if (typeof win === 'string' && win) payload.window = win;
    if (!payload.stack || payload.stack.indexOf('.js') === -1) {
      this.send(payload);
      return;
    }
    // Map the stack to the sources before it reaches the terminal, but never
    // hold the report back for long: the source maps are best-effort.
    const raw = payload.stack;
    Promise.race([symbolicateStack(raw), new Promise<string>((r) => setTimeout(() => r(raw), 1500))])
      .catch(() => raw)
      .then((stack) => this.send({ ...payload, stack }));
  }

  private send(payload: DevClientError): void {
    const body = JSON.stringify(payload);
    // sendBeacon throws on the custom scheme of a native webview (WebKitGTK),
    // so a failed or absent beacon falls back to fetch.
    try {
      if (!this.cfg.frames && navigator.sendBeacon) {
        if (navigator.sendBeacon(this.cfg.errors, new Blob([body], { type: 'application/json' }))) return;
      }
    } catch {
      /* fall through to fetch */
    }
    try {
      fetch(this.cfg.errors, { method: 'POST', body, headers: { 'Content-Type': 'application/json' }, keepalive: true }).catch(() => {});
    } catch {
      /* reporting is best-effort */
    }
  }

  private addRuntime(title: string, message: string, stack?: string, hostStack?: string): void {
    const entry = { title, message, stack, hostStack };
    this.runtimeErrors.push(entry);
    this.setStatus(false);
    this.render();
    // Render immediately with the raw stack, then upgrade in place if a source
    // map lets us point at the original location.
    if (stack) {
      void symbolicateStack(stack).then((mapped) => {
        if (mapped && mapped !== stack) {
          entry.stack = mapped;
          this.render();
        }
      });
    }
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

  private runtimeHTML(r: { title: string; message: string; stack?: string; hostStack?: string }): string {
    const host = r.hostStack ? '<div class="stack">Host stack:\n' + esc(r.hostStack) + '</div>' : '';
    const stack = host + (r.stack ? '<div class="stack">' + esc(r.stack) + '</div>' : '');
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
