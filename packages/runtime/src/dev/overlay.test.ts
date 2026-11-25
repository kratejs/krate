import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KrateDev } from './app.js';
import { SETTINGS_KEY } from './settings.js';

class FakeEventSource {
  static last: FakeEventSource | undefined;
  readyState = 1;
  listeners: Record<string, ((e: any) => void)[]> = {};
  constructor(public url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(type: string, fn: (e: any) => void) {
    (this.listeners[type] ||= []).push(fn);
  }
  emit(type: string, data: unknown = {}) {
    for (const fn of this.listeners[type] || []) fn({ data: JSON.stringify(data) });
  }
  close() {
    this.readyState = 2;
  }
}

type Routes = Record<string, unknown>;

/** A fetch stub that answers the given API paths and 404s everything else. */
function stubFetch(routes: Routes) {
  const calls: { url: string; init?: RequestInit }[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    calls.push({ url, init });
    const path = new URL(url, 'http://localhost:3000').pathname;
    const key = (init?.method || 'GET') + ' ' + path;
    const body = key in routes ? routes[key] : routes[path];
    if (body === undefined) return new Response('not found', { status: 404 });
    if (typeof body === 'function') return (body as (u: string, i?: RequestInit) => Response)(url, init);
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fn);
  return calls;
}

const tick = () => new Promise((r) => setTimeout(r, 0));
async function flush(n = 6) {
  for (let i = 0; i < n; i++) await tick();
}

const BUILD = { id: 3, start: Date.now() - 5000, durationMs: 184.3, kind: 'partial', files: ['src/a.tsx'], routes: ['/blog/[slug]'], pages: 1, ok: true, diagnostics: 0 };

const API: Routes = {
  '/__krate/dev/info': { version: '0.14.2', logFile: '.krate/dev.log', docsURL: 'https://docs.test/#', editor: { configured: '', available: [{ id: 'zed', label: 'Zed' }] } },
  '/__krate/dev/activity': { builds: [BUILD], lastGoodAt: BUILD.start + 184 },
  '/__krate/dev/route': {
    pattern: '/blog/[slug]',
    source: 'src/pages/blog/[slug].tsx',
    mode: 'ssg',
    params: { slug: 'first' },
    dynamic: true,
    staticPaths: 24,
    layouts: ['src/pages/_layout.tsx'],
    sizes: { html: 14540, js: 61200, css: 6963, gzipJs: 21914, gzipRuntime: 13414, gzipIslands: 8499 },
    islands: [],
    budget: { js: 30720, route: 102400 },
    budgetUsage: { js: 61200, route: 82703 },
  },
  '/__krate/dev/routes': [
    { pattern: '/', source: 'src/pages/index.tsx', mode: 'ssg', kind: 'page', paths: ['/'] },
    { pattern: '/blog/[slug]', source: 'src/pages/blog/[slug].tsx', mode: 'ssg', kind: 'page', paths: ['/blog/first', '/blog/second'] },
  ],
  '/__krate/dev/logs': { seq: 2, lines: [{ seq: 1, t: Date.now(), src: 'api', lvl: 'error', text: 'TypeError: boom\n    at a\n    at b\n    at c' }] },
};

let app: KrateDev | undefined;
const shadow = () => app!.shadow;
const $ = (sel: string) => shadow().querySelector(sel) as HTMLElement | null;
const click = (sel: string) => {
  const el = $(sel);
  if (!el) throw new Error('no element ' + sel);
  el.click();
};
const key = (code: string, init: KeyboardEventInit = {}, target: EventTarget = document) =>
  target.dispatchEvent(new KeyboardEvent('keydown', { code, key: init.key || code, bubbles: true, composed: true, cancelable: true, ...init }));

async function start(bootstrap: Record<string, unknown> = {}, routes: Routes = API) {
  (window as any).__KRATE_DEV__ = {
    sse: '/__krate/hotreload',
    api: '/__krate/dev',
    open: '/__krate/open',
    errors: '/__krate/client-error',
    version: 'dev',
    page: { route: '/blog/[slug]', source: 'src/pages/blog/[slug].tsx', mode: 'ssg' },
    ...bootstrap,
  };
  const calls = stubFetch(routes);
  app = new KrateDev();
  app.init();
  await flush();
  return calls;
}

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.spyOn(navigator, 'sendBeacon').mockImplementation(() => true);
  localStorage.clear();
  sessionStorage.clear();
  history.replaceState(null, '', '/blog/first');
});

afterEach(() => {
  app?.destroy();
  app = undefined;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.body.innerHTML = '';
  delete (window as any).__KRATE_DEV__;
});

describe('dock', () => {
  it('shows the route, mode and last build when idle', async () => {
    await start();
    const dock = $('.dock')!;
    expect(dock.className).toBe('dock');
    expect($('.dock .mode')!.textContent).toBe('SSG');
    expect($('.dock .route')!.textContent).toBe('/blog/[slug]');
    expect($('.dock .seg.ok')!.textContent).toBe('184ms');
    expect($('.dock .kbdseg')).not.toBeNull();
  });

  it('shows a rebuild in progress and its result', async () => {
    await start();
    FakeEventSource.last!.emit('build-start', { files: ['src/components/LikeButton.tsx'] });
    expect($('.dock')!.classList.contains('building')).toBe(true);
    expect($('.dock .seg.busy')!.textContent).toBe('Rebuilding LikeButton.tsx');
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 4, durationMs: 61 } });
    expect($('.dock')!.classList.contains('building')).toBe(false);
    expect($('.dock .seg.ok')!.textContent).toBe('61ms');
  });

  it('shows the issue count, a red ring, and a dot when collapsed', async () => {
    await start();
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ message: 'a' }, { message: 'b', file: 'src/b.tsx' }] });
    expect($('.dock')!.classList.contains('error')).toBe(true);
    expect($('.dock .badge.err')!.textContent).toBe('2 issues');
    click('[data-act=collapse]');
    expect($('.dock')!.classList.contains('mini')).toBe(true);
    expect($('.dock .pip')).not.toBeNull();
    expect(JSON.parse(localStorage.getItem(SETTINGS_KEY)!).collapsed).toBe(true);
  });

  it('says when the dev server is gone', async () => {
    await start();
    const es = FakeEventSource.last!;
    es.readyState = 0;
    es.emit('error');
    expect($('.dock')!.classList.contains('off')).toBe(true);
    expect($('.dock')!.textContent).toContain('Reconnecting to dev server');
    es.readyState = 1;
    es.emit('connected');
    expect($('.dock')!.classList.contains('off')).toBe(false);
  });

  it('keeps the host out of the way of page clicks', async () => {
    await start();
    const host = document.getElementById('krate-dev-root')!;
    expect(host.style.getPropertyValue('pointer-events')).toBe('none');
    expect(host.style.getPropertyValue('width')).toBe('0px');
    expect(host.style.getPropertyValue('height')).toBe('0px');
  });
});

describe('error overlay', () => {
  const diags = [
    { file: 'src/components/LikeButton.tsx', line: 14, col: 7, code: 'KR5002', message: 'invalid event handler `onclick`', hint: 'rename it to onClick' },
  ];
  const warnings = [{ file: 'src/components/Newsletter.tsx', line: 22, col: 3, code: 'KR5005', severity: 'warning', message: 'ignored default' }];
  const withFix: Routes = {
    ...API,
    '/__krate/dev/source': { file: 'src/components/LikeButton.tsx', lang: 'tsx', start: 13, lines: ['      class="like"', '      onclick={toggle}', '    >'] },
    'GET /__krate/dev/fix': { changes: [{ file: 'src/components/LikeButton.tsx', line: 14, col: 7, code: 'KR5002', from: 'onclick', to: 'onClick' }] },
    'POST /__krate/dev/fix': { applied: true, changes: [{}] },
  };

  it('pages through build issues with the rail and arrow keys', async () => {
    await start({}, withFix);
    FakeEventSource.last!.emit('build-error', { diagnostics: diags, warnings });
    await flush();
    expect(app!.modalOpen).toBe(true);
    expect($('.m-title')!.textContent).toBe('Build failed');
    expect($('.pager span')!.textContent).toBe('1 of 2');
    expect(shadow().querySelectorAll('.rail .issue')).toHaveLength(2);
    expect($('.rail .issue.e')!.getAttribute('aria-current')).toBe('true');
    expect($('.rail .issue.w')).not.toBeNull();
    expect($('.d-meta a')!.getAttribute('href')).toBe('https://docs.test/#kr5002');
    expect($('.cf-l.hl .n')!.textContent).toBe('14');
    expect($('.cf-l.hl .sq')!.textContent).toBe('onclick');
    expect($('.cf-l.ann')!.textContent).toContain('expected onClick');
    expect($('.hint')!.textContent).toContain('rename it to onClick');
    expect($('.diff .add')!.textContent).toContain('onClick={toggle}');
    key('ArrowRight', { key: 'ArrowRight' });
    expect($('.pager span')!.textContent).toBe('2 of 2');
    expect($('.m-title')!.textContent).toBe('Build failed');
    key('ArrowLeft', { key: 'ArrowLeft' });
    expect($('.pager span')!.textContent).toBe('1 of 2');
  });

  it('applies a fix through the dev server', async () => {
    const calls = await start({}, withFix);
    FakeEventSource.last!.emit('build-error', { diagnostics: diags, warnings });
    await flush();
    key('KeyF', { key: 'f' });
    await flush();
    const post = calls.find((c) => c.init?.method === 'POST' && c.url.endsWith('/fix'));
    expect(JSON.parse(String(post!.init!.body))).toEqual({ file: 'src/components/LikeButton.tsx', code: 'KR5002' });
    expect((post!.init!.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect($('.m-foot')!.textContent).toContain('Fix applied, rebuilding.');
  });

  it('closes when a rebuild passes', async () => {
    await start({}, withFix);
    FakeEventSource.last!.emit('build-error', { diagnostics: diags, warnings });
    await flush();
    // The passing build reports its warnings (none), then a reload for
    // another page clears build errors without reloading this tab.
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 9 }, warnings: [] });
    FakeEventSource.last!.emit('reload', { pages: ['/other'] });
    expect(app!.modalOpen).toBe(false);
    expect(app!.visibleIssues()).toHaveLength(0);
  });

  it('keeps the warnings a passing build reports, without opening', async () => {
    await start({}, withFix);
    FakeEventSource.last!.emit('build-error', { diagnostics: diags, warnings });
    await flush();
    key('Escape', { key: 'Escape' });
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 9 }, warnings });
    FakeEventSource.last!.emit('reload', { pages: ['/other'] });
    expect(app!.modalOpen).toBe(false);
    expect(app!.visibleIssues().map((i) => i.code)).toEqual(['KR5005']);
    expect($('.dock')!.classList.contains('warn')).toBe(true);
    expect($('.dock')!.classList.contains('error')).toBe(false);
    expect($('.dock .badge.warn')!.textContent).toBe('1 warning');
  });

  it('splits a runtime stack into app frames and folded framework frames', async () => {
    await start();
    const err = new TypeError("Cannot read properties of undefined (reading 'count')");
    err.stack =
      "TypeError: Cannot read properties of undefined (reading 'count')\n" +
      '    at load (src/components/LikeButton.tsx:22:31)\n' +
      '    at o (http://localhost:3000/chunks/runtime.abc.js:1:10)\n' +
      '    at p (http://localhost:3000/chunks/runtime.abc.js:1:20)\n' +
      '    at LikeButton (src/components/LikeButton.tsx:9:3)';
    window.dispatchEvent(new ErrorEvent('error', { error: err, message: 'Uncaught ' + err.message }));
    await flush();
    expect(app!.modalOpen).toBe(true);
    expect($('.m-title')!.textContent).toBe('Unhandled runtime error');
    expect($('.d-msg')!.textContent).toBe("Cannot read properties of undefined (reading 'count')");
    expect(shadow().querySelectorAll('.fr.app')).toHaveLength(2);
    expect($('.fr.fold')!.textContent).toContain('2 framework frames');
    click('.fr.fold');
    expect(shadow().querySelectorAll('.fr.fw')).toHaveLength(2);
    expect($('.subtabs button')!.textContent).toContain('2 of 4');
    click('[data-act=ignore]');
    expect(app!.modalOpen).toBe(false);
    expect(JSON.parse(sessionStorage.getItem('krate:dev-ignored')!)).toHaveLength(1);
  });

  it('shows a server crash with its log and restarts the sidecar', async () => {
    const calls = await start({}, { ...API, 'POST /__krate/dev/action': () => new Response('{"ok":true}', { status: 202 }) });
    FakeEventSource.last!.emit('client-error', {
      clientError: {
        kind: 'server',
        message: 'SSR renderer failed: exited unexpectedly',
        source: 'SSR renderer',
        detail: 'exited unexpectedly',
        exitCode: 1,
        restart: 'ssr',
        log: ['GET /dashboard', 'ReferenceError: window is not defined'],
      },
    });
    await flush();
    expect($('.m-title')!.textContent).toBe('SSR sidecar crashed');
    expect($('.m-sub')!.textContent).toContain('exited with code 1');
    expect($('.dock .badge')!.textContent).toBe('Server down');
    click('[data-act=restart]');
    await flush();
    const post = calls.find((c) => c.init?.method === 'POST' && c.url.endsWith('/action'));
    expect(JSON.parse(String(post!.init!.body))).toEqual({ action: 'restart-sidecar', target: 'ssr' });
    expect(app!.visibleIssues()).toHaveLength(0);
  });

  it('does not open for console.error but counts it, and respects the setting', async () => {
    await start();
    console.error('Failed to fetch /api/likes: 500');
    expect(app!.modalOpen).toBe(false);
    expect($('.dock .badge')!.textContent).toBe('1 issue');
    app!.setSettings({ consoleIssues: false });
    console.error('another');
    expect(app!.visibleIssues()).toHaveLength(1);
  });
});

describe('panel', () => {
  it('renders the route tab from the dev server and the page', async () => {
    document.title = 'Post';
    document.body.insertAdjacentHTML(
      'beforeend',
      '<script type="application/json" id="__krate_dev_components">' +
        JSON.stringify([
          { n: 0, name: 'RootLayout', tier: 'static', parent: -1 },
          { n: 1, key: 'k1', name: 'LikeButton', tier: 'client', parent: 0, file: 'src/components/LikeButton.tsx', line: 8 },
        ]) +
        '</script>',
    );
    (globalThis as any).__krate_dev_signals = [{ c: 'k1', name: 'count', get: () => 1 }];
    (globalThis as any).__krate_dev_hydration = { k1: 0.9 };
    await start();
    click('[data-act=panel]');
    await flush();
    const body = $('.p-body')!.textContent!;
    expect(body).toContain('/blog/[slug]');
    expect(body).toContain('1 of 24 paths from generateStaticParams');
    expect(body).toContain('21.4 KB gzipped');
    // The budget compares raw bytes with raw bytes, as checks.budget does.
    const budgets = Array.from(shadow().querySelectorAll('.bgt')).map((b) => b.textContent + (b.classList.contains('over') ? ' over' : ''));
    expect(budgets).toEqual(['JS59.8 of 30 KB over', 'Route total80.8 of 100 KB']);
    expect($('.bgt-note')!.textContent).toContain('not minified');
    expect($('.row .name')!.textContent).toBe('LikeButton');
    expect($('.row .sub')!.textContent).toBe('1 signal');
    expect($('.row .right')!.textContent).toBe('0.9ms');
    expect(shadow().querySelectorAll('.chip.warn')).toHaveLength(4);
    const tabs = Array.from(shadow().querySelectorAll('.tab')).map((t) => t.textContent);
    expect(tabs).toEqual(['Route', 'Inspect', 'Activity', 'Logs', 'Settings']);
    delete (globalThis as any).__krate_dev_signals;
    delete (globalThis as any).__krate_dev_hydration;
  });

  it('polls logs only while the logs tab is open and filters them', async () => {
    const calls = await start();
    const logCalls = () => calls.filter((c) => c.url.includes('/logs')).length;
    expect(logCalls()).toBe(0);
    click('[data-act=panel]');
    click('[data-tab=logs]');
    await flush();
    expect(logCalls()).toBe(1);
    expect($('.lg.e .m')!.textContent).toContain('+ 2 more lines');
    click('[data-act=logmore]');
    expect($('.lg.e .m')!.textContent).toContain('at c');
    click('[data-act=logf][data-v=browser]');
    expect($('.log')!.textContent).toContain('No log lines match the filters');
    click('[data-act=close-panel]');
    const n = logCalls();
    await new Promise((r) => setTimeout(r, 1100));
    expect(logCalls()).toBe(n);
  });

  it('stores settings and applies the theme', async () => {
    await start();
    click('[data-act=panel]');
    click('[data-tab=settings]');
    click('[data-act=theme][data-v=light]');
    expect(document.getElementById('krate-dev-root')!.getAttribute('data-theme')).toBe('light');
    click('[data-act=corner][data-v=tr]');
    expect($('.kd')!.className).toBe('kd c-tr');
    const sel = $('select[data-input=editor]') as HTMLSelectElement;
    sel.value = 'zed';
    sel.dispatchEvent(new Event('change', { bubbles: true }));
    expect(JSON.parse(localStorage.getItem(SETTINGS_KEY)!)).toMatchObject({ theme: 'light', corner: 'tr', editor: 'zed' });
    app!.destroy();
    app = new KrateDev();
    app.init();
    expect(document.getElementById('krate-dev-root')!.getAttribute('data-theme')).toBe('light');
    expect($('.kd')!.className).toBe('kd c-tr');
  });

  it('passes the chosen editor to /__krate/open', async () => {
    const calls = await start({}, { ...API, '/__krate/open': () => new Response(null, { status: 204 }) });
    app!.setSettings({ editor: 'zed' });
    await app!.openInEditor('src/a.tsx', 3);
    expect(calls.some((c) => c.url === '/__krate/open?file=src%2Fa.tsx&line=3&editor=zed')).toBe(true);
  });

  it('hides what the server does not provide when endpoints 404', async () => {
    const calls = await start({}, {});
    expect(app!.activity).toBe('missing');
    expect($('.dock .seg.ok')!.textContent).toBe('Ready');
    click('[data-act=panel]');
    await flush();
    const tabs = Array.from(shadow().querySelectorAll('.tab')).map((t) => t.textContent);
    expect(tabs).toEqual(['Route', 'Logs', 'Settings']);
    expect($('.p-body')!.textContent).toContain('/blog/[slug]');
    expect($('[data-act=view-html]')).toBeNull();
    expect($('[data-input=editor]')).toBeNull();
    click('[data-tab=logs]');
    await flush();
    expect($('.p-body')!.textContent).toContain('does not share its log');
    expect(calls.every((c) => !c.init || c.init.method !== 'POST')).toBe(true);
  });

  it('works on a host without the dev API (native)', async () => {
    const calls = await start({ api: undefined, page: undefined }, {});
    expect(calls).toHaveLength(0);
    expect($('.dock .route')!.textContent).toBe('/blog/first');
  });
});

describe('command menu and shortcuts', () => {
  it('opens with Alt+K, filters, moves with arrows and runs with Enter', async () => {
    await start();
    key('KeyK', { altKey: true, key: 'k' });
    await flush();
    expect(app!.pal!.hidden).toBe(false);
    const input = app!.pal!.querySelector('input')!;
    input.value = 'settings';
    input.dispatchEvent(new Event('input'));
    const names = Array.from(app!.pal!.querySelectorAll('.pi .name')).map((n) => n.textContent);
    expect(names).toEqual(['Open settings']);
    input.value = '';
    input.dispatchEvent(new Event('input'));
    expect($('.pi[aria-selected=true] .name')!.textContent).toBe('/');
    key('ArrowDown', { key: 'ArrowDown' });
    expect($('.pi[aria-selected=true] .name')!.textContent).toBe('/blog/[slug]');
    key('ArrowUp', { key: 'ArrowUp' });
    expect($('.pi[aria-selected=true] .name')!.textContent).toBe('/');
    input.value = 'open set';
    input.dispatchEvent(new Event('input'));
    key('Enter', { key: 'Enter' });
    expect(app!.pal!.hidden).toBe(true);
    expect(app!.panelOpen).toBe(true);
    expect(app!.tab).toBe('settings');
  });

  it('opens the source file with Mod+Enter', async () => {
    const calls = await start({}, { ...API, '/__krate/open': () => new Response(null, { status: 204 }) });
    key('KeyK', { altKey: true, key: 'k' });
    await flush();
    const input = app!.pal!.querySelector('input')!;
    input.value = 'slug';
    input.dispatchEvent(new Event('input'));
    key('Enter', { key: 'Enter', ctrlKey: true });
    await flush();
    expect(calls.some((c) => c.url.startsWith('/__krate/open?file=src%2Fpages%2Fblog%2F%5Bslug%5D.tsx'))).toBe(true);
  });

  it('ignores shortcuts while the user types in the page', async () => {
    await start();
    const input = document.createElement('input');
    document.body.appendChild(input);
    key('KeyK', { altKey: true, key: 'k' }, input);
    expect(app!.pal).toBeUndefined();
    key('KeyK', { altKey: true, key: 'k' }, document.body);
    expect(app!.pal!.hidden).toBe(false);
    key('Escape', { key: 'Escape' });
    expect(app!.pal!.hidden).toBe(true);
  });

  it('toggles the inspector and hides everything', async () => {
    document.body.insertAdjacentHTML(
      'beforeend',
      '<p data-kd="0">x</p><script type="application/json" id="__krate_dev_components">[{"n":0,"name":"Card","tier":"static","parent":-1}]</script>',
    );
    await start();
    key('KeyI', { altKey: true, key: 'i' });
    expect(app!.inspector.on).toBe(true);
    expect($('.dock')!.classList.contains('inspect')).toBe(true);
    expect($('.dock .seg.on')!.textContent).toContain('Inspecting');
    expect($('.legend')!.textContent).toContain('Static 1');
    key('Escape', { key: 'Escape' });
    expect(app!.inspector.on).toBe(false);
    key('Period', { altKey: true, key: '.' });
    expect($('.dock')!.hidden).toBe(true);
    key('Period', { altKey: true, key: '.' });
    expect($('.dock')!.hidden).toBe(false);
  });

  it('opens the issues with Alt+E', async () => {
    await start();
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ message: 'x' }] });
    key('Escape', { key: 'Escape' });
    expect(app!.modalOpen).toBe(false);
    key('KeyE', { altKey: true, key: 'e' });
    expect(app!.modalOpen).toBe(true);
  });
});

describe('build warnings', () => {
  const warning = { file: 'src/components/Like.tsx', line: 4, col: 23, code: 'KR5002', severity: 'warning', message: 'invalid event handler `onclick` on <button>: React event props are camelCase', hint: 'rename it to onClick' };
  const routes: Routes = {
    ...API,
    '/__krate/dev/code': { code: 'KR5002', title: 'Lowercase event handler prop', explanation: 'React spells event props in camelCase: `onClick`.' },
    '/__krate/dev/source': { file: 'src/components/Like.tsx', lang: 'tsx', start: 3, lines: ['  const [n, setN] = createSignal(0);', '  return <button class="like" onclick={() => setN(n() + 1)}>Like</button>;', '}'] },
    'GET /__krate/dev/fix': { changes: [{ file: 'src/components/Like.tsx', line: 4, col: 31, code: 'KR5002', from: 'onclick', to: 'onClick' }] },
    'POST /__krate/dev/fix': { applied: true, changes: [{}] },
  };

  it('shows an amber dock badge that opens them, never the overlay by itself', async () => {
    await start({}, routes);
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 5 }, warnings: [warning, warning] });
    await flush();
    expect(app!.modalOpen).toBe(false);
    expect($('.dock')!.className).toContain('warn');
    expect($('.dock .badge.warn')!.textContent).toBe('1 warning');
    click('[data-act=issues]');
    await flush();
    expect(app!.modalOpen).toBe(true);
    expect($('.m-title')!.textContent).toBe('Build warnings');
    expect($('.m-title')!.className).toContain('amber');
    expect($('.m-sub')!.textContent).toContain('1 warning in 1 file');
    expect($('.d-meta .code.w')!.textContent).toBe('KR5002');
    expect($('.d-meta .chip')!.textContent).toBe('Lowercase event handler prop');
    expect($('.cf')!.className).toBe('cf w');
    expect($('.diff .add')!.textContent).toContain('onClick={() => setN(n() + 1)}');
    expect($('.m-foot')!.textContent).toContain('The build passed with warnings');
  });

  it('lists warnings in the rail with the errors, amber-tinted', async () => {
    await start({}, routes);
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ message: 'Unexpected token', file: 'src/pages/a.tsx', line: 2 }], warnings: [warning] });
    await flush();
    expect(app!.modalOpen).toBe(true);
    expect(shadow().querySelectorAll('.rail .issue.e')).toHaveLength(1);
    expect(shadow().querySelectorAll('.rail .issue.w')).toHaveLength(1);
    expect($('.rail .issue.w .top1')!.textContent).toContain('warning');
    expect($('.dock .badge.err')!.textContent).toBe('1 issue');
  });

  it('applies the safe fix and fix all, and clears once the rebuild reports none', async () => {
    const calls = await start({}, routes);
    FakeEventSource.last!.emit('build-warnings', { warnings: [warning, { ...warning, file: 'src/components/Other.tsx', line: 9 }] });
    click('[data-act=issues]');
    await flush();
    click('[data-act=fix]');
    await flush();
    const posts = () => calls.filter((c) => c.init?.method === 'POST' && c.url.endsWith('/fix')).map((c) => JSON.parse(String(c.init!.body)));
    expect(posts()).toEqual([{ file: 'src/components/Like.tsx', code: 'KR5002' }]);
    click('[data-act=fix-all]');
    await flush();
    expect(posts()[1]).toEqual({ all: true });
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 6 }, warnings: [] });
    expect(app!.modalOpen).toBe(false);
    expect($('.dock .badge')).toBeNull();
  });

  it('hides them with the Show build warnings setting', async () => {
    await start({}, routes);
    FakeEventSource.last!.emit('build-warnings', { warnings: [warning] });
    expect($('.dock .badge.warn')).not.toBeNull();
    click('[data-act=panel]');
    click('[data-tab=settings]');
    click('[data-act=setting][data-k=showWarnings]');
    expect(app!.settings.showWarnings).toBe(false);
    expect($('.dock .badge')).toBeNull();
    expect(app!.visibleIssues()).toHaveLength(0);
  });

  it('explains the code in a popover that keeps the docs link', async () => {
    await start({}, routes);
    FakeEventSource.last!.emit('build-warnings', { warnings: [warning] });
    click('[data-act=issues]');
    await flush();
    click('.whatbtn');
    expect($('.codepop')!.textContent).toContain('React spells event props in camelCase');
    expect($('.codepop a')!.getAttribute('href')).toBe('https://docs.test/#kr5002');
    key('Escape', { key: 'Escape' });
    expect($('.codepop')).toBeNull();
    expect(app!.modalOpen).toBe(true);
    key('Escape', { key: 'Escape' });
    expect(app!.modalOpen).toBe(false);
  });
});

describe('last good page', () => {
  it('dismisses the overlay and the dock says the page is the last good build until it passes', async () => {
    await start();
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ message: 'Unexpected token', file: 'src/pages/a.tsx', line: 2 }] });
    await flush();
    expect($('.m-foot')!.textContent).toContain('Last good build');
    click('[data-act=last-good]');
    expect(app!.modalOpen).toBe(false);
    expect($('.dock .seg.stale')!.textContent).toContain('Last good build');
    expect($('.dock .badge.err')!.textContent).toBe('1 issue');
    FakeEventSource.last!.emit('build-end', { build: { ...BUILD, id: 7 }, warnings: [] });
    FakeEventSource.last!.emit('reload', { pages: ['/other'] });
    expect($('.dock .seg.stale')).toBeNull();
    expect($('.dock .badge')).toBeNull();
  });

  it('is not offered on the error shell, which has no last good page', async () => {
    await start({ page: undefined });
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ message: 'Unexpected token' }] });
    await flush();
    expect($('[data-act=last-good]')).toBeNull();
  });
});

describe('server crash subtitle', () => {
  it('names the runtime and the restarts of the last minute', async () => {
    await start();
    FakeEventSource.last!.emit('client-error', {
      clientError: { kind: 'server', message: 'SSR renderer failed: exited unexpectedly', source: 'SSR renderer', detail: 'exited unexpectedly', exitCode: 1, restart: 'ssr', runtime: 'node 22.11', restarts: 2 },
    });
    await flush();
    const sub = $('.m-sub')!.textContent!;
    expect(sub.startsWith('node 22.11 \u00B7 exited with code 1 \u00B7 ')).toBe(true);
    expect(sub.endsWith(' \u00B7 restarted twice in the last minute')).toBe(true);
  });
});

describe('request tab', () => {
  it('lists the failed requests near a runtime error and hides when there are none', async () => {
    await start({}, { ...API, '/api/likes': () => new Response('nope', { status: 500 }), '/api/ok': { ok: true } });
    await fetch('/api/ok');
    await fetch('/api/likes', { method: 'POST' });
    await fetch('/api/missing');
    await flush();
    // The overlay's own requests are not the page's.
    expect(app!.requests.list.map((r) => r.status + ' ' + r.method + ' ' + r.url)).toEqual(['500 POST /api/likes', '404 GET /api/missing']);
    const err = new TypeError('boom');
    err.stack = 'TypeError: boom\n    at load (src/components/LikeButton.tsx:22:31)';
    window.dispatchEvent(new ErrorEvent('error', { error: err, message: 'Uncaught TypeError: boom' }));
    await flush();
    const tab = $('[data-act=rt-tab][data-v=requests]')!;
    expect(tab.textContent).toBe('Requests2');
    tab.click();
    const rows = Array.from(shadow().querySelectorAll('.req')).map((r) => r.querySelector('.st')!.textContent + ' ' + r.querySelector('.u')!.textContent);
    expect(rows).toEqual(['500 /api/likes', '404 /api/missing']);
  });

  it('does not list its own report again when the dev server echoes it with a mapped stack', async () => {
    const calls = await start({}, { ...API, '/broken/index.abc.js.map': { version: 3, sources: ['src/components/Thrower.tsx'], mappings: 'AAMI' } });
    const err = new TypeError('boom');
    err.stack = 'TypeError: boom\n    at explode (http://localhost:3000/broken/index.abc.js:1:5)';
    window.dispatchEvent(new ErrorEvent('error', { error: err, message: 'Uncaught TypeError: boom' }));
    await flush(12);
    expect(calls.some((c) => c.url.endsWith('index.abc.js.map'))).toBe(true);
    const sent = JSON.parse(await ((navigator.sendBeacon as any).mock.calls[0][1] as Blob).text());
    expect(sent.stack).toContain('src/components/Thrower.tsx:7:');
    FakeEventSource.last!.emit('client-error', { clientError: sent });
    await flush();
    expect(app!.visibleIssues()).toHaveLength(1);
    expect(app!.visibleIssues()[0].file).toBe('src/components/Thrower.tsx');
  });

  it('has no Requests tab without failed requests', async () => {
    await start();
    window.dispatchEvent(new ErrorEvent('error', { error: new Error('x'), message: 'x' }));
    await flush();
    expect($('[data-act=rt-tab][data-v=requests]')).toBeNull();
  });
});
