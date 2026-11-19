import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter, krateRouter } from './router';

// The router re-runs a page's scripts after a navigation. Shared component
// chunks (chunks/c/) behave like the runtime chunk: loaded once per document,
// never re-run, and always before the page script that calls into them.

function page(title: string, scripts: string[], head = ''): string {
  const tags = scripts.map((s) => (s.startsWith('<') ? s : `<script src="${s}"></script>`)).join('');
  return `<!doctype html><html><head><title>${title}</title>${head}</head><body><div id="root"><h1>${title}</h1></div>${tags}</body></html>`;
}

let appended: HTMLScriptElement[];

interface Preload {
  rel: string;
  as: string;
  path: string;
  crossorigin: string | null;
  /** The page heading when the preload was added. */
  shown: string;
  link: HTMLLinkElement;
}
let preloads: Preload[];
/** When set, preloads settle only when the test calls release(). */
let holdPreloads: boolean;
let held: HTMLLinkElement[];
function release(): void {
  for (const l of held.splice(0)) l.dispatchEvent(new Event('load'));
}

beforeEach(() => {
  destroyRouter();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  history.replaceState(null, '', '/');
  document.head.innerHTML = '<title>Home</title>';
  // The first document's scripts (typed so happy-dom does not fetch them).
  document.body.innerHTML =
    '<div id="root"><h1>Home</h1></div>' +
    '<script type="text/x-test" src="/chunks/runtime.aaaaaa.js"></script>' +
    '<script type="text/x-test" src="/chunks/c/theme.bbbbbb.js"></script>' +
    '<script type="text/x-test" src="/index.cccccc.js"></script>';
  preloads = [];
  holdPreloads = false;
  held = [];
  const docs: Record<string, string> = {
    '/m': page('M', ['<script type="module" src="/m/index.gggggg.js"></script>'], '<link rel="modulepreload" href="/chunks/island_x.hhhhhh.js">'),
    '/a': page('A', ['/chunks/runtime.aaaaaa.js', '/chunks/c/theme.bbbbbb.js', '/chunks/c/page.dddddd.js', '/a/index.eeeeee.js']),
    '/b': page('B', ['/chunks/runtime.aaaaaa.js', '/chunks/c/theme.bbbbbb.js', '/b/index.ffffff.js']),
  };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string) => {
      const u = new URL(input, location.href);
      return {
        ok: true,
        status: 200,
        url: u.href,
        redirected: false,
        headers: new Headers({ 'content-type': 'text/html' }),
        text: async () => docs[u.pathname] ?? '',
      } as unknown as Response;
    }),
  );
  // jsdom does not fetch scripts: record each one and report it loaded.
  appended = [];
  const append = document.body.appendChild.bind(document.body);
  vi.spyOn(document.body, 'appendChild').mockImplementation(<T extends Node>(n: T): T => {
    if (n instanceof HTMLScriptElement) {
      appended.push(n);
      // A non-script type keeps happy-dom from fetching it.
      n.setAttribute('data-type', n.type);
      n.type = 'text/x-test';
      queueMicrotask(() => n.dispatchEvent(new Event('load')));
    }
    append(n);
    return n;
  });
  // Preload links: record them, and settle them (happy-dom never loads them).
  const headAppend = document.head.appendChild.bind(document.head);
  vi.spyOn(document.head, 'appendChild').mockImplementation(<T extends Node>(n: T): T => {
    if (n instanceof HTMLLinkElement && (n.rel === 'preload' || n.rel === 'modulepreload')) {
      preloads.push({
        rel: n.rel,
        as: n.getAttribute('as') || '',
        path: new URL(n.href).pathname,
        crossorigin: n.getAttribute('crossorigin'),
        shown: document.querySelector('#root h1')?.textContent || '',
        link: n,
      });
      if (holdPreloads) held.push(n);
      else queueMicrotask(() => n.dispatchEvent(new Event('load')));
    }
    headAppend(n);
    return n;
  });
  initRouter();
});

afterEach(() => {
  destroyRouter();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete (globalThis as any).__KRATE_CFG__;
});

function paths(scripts: HTMLScriptElement[]): string[] {
  return scripts.map((s) => new URL(s.src).pathname);
}

describe('page scripts across navigations', () => {
  it('loads shared chunks once, in order before the page script', async () => {
    await krateRouter.push('/a');
    expect(paths(appended)).toEqual(['/chunks/c/page.dddddd.js', '/a/index.eeeeee.js']);
    // Classic scripts run in insertion order, as on a full page load.
    expect(appended.every((s) => s.async === false)).toBe(true);
    expect(appended[0].hasAttribute('data-krate-spa')).toBe(false);
    expect(appended[1].hasAttribute('data-krate-spa')).toBe(true);

    appended = [];
    await krateRouter.push('/b');
    // The theme and page chunks stay; only the new page script runs.
    expect(paths(appended)).toEqual(['/b/index.ffffff.js']);
    const present = paths(Array.from(document.querySelectorAll('script[src]')));
    expect(present.filter((p) => p.startsWith('/chunks/c/'))).toEqual(['/chunks/c/theme.bbbbbb.js', '/chunks/c/page.dddddd.js']);
    expect(present).not.toContain('/a/index.eeeeee.js');
    expect(present).not.toContain('/index.cccccc.js');
  });
});

describe('script preloading', () => {
  it('preloads the page scripts before the swap and drops the links once they ran', async () => {
    holdPreloads = true;
    const nav = krateRouter.push('/a');
    await new Promise((r) => setTimeout(r, 20));
    // Classic scripts are preloaded as scripts, without crossorigin (the way
    // loadScript requests them), while the old page is still shown.
    expect(preloads.map((p) => [p.rel, p.as, p.path, p.crossorigin, p.shown])).toEqual([
      ['preload', 'script', '/chunks/c/page.dddddd.js', null, 'Home'],
      ['preload', 'script', '/a/index.eeeeee.js', null, 'Home'],
    ]);
    expect(document.querySelector('#root h1')!.textContent).toBe('Home');
    expect(appended).toEqual([]);
    release();
    await nav;
    expect(document.querySelector('#root h1')!.textContent).toBe('A');
    expect(paths(appended)).toEqual(['/chunks/c/page.dddddd.js', '/a/index.eeeeee.js']);
    expect(preloads.every((p) => !p.link.isConnected)).toBe(true);
  });

  it('modulepreloads module scripts and the document modulepreload links', async () => {
    await krateRouter.push('/m');
    expect(preloads.map((p) => [p.rel, p.path, p.crossorigin])).toEqual([
      ['modulepreload', '/m/index.gggggg.js', null],
      ['modulepreload', '/chunks/island_x.hhhhhh.js', null],
    ]);
    expect(appended.map((s) => s.getAttribute('data-type'))).toEqual(['module']);
  });

  it('a superseded navigation removes its preload links', async () => {
    holdPreloads = true;
    const first = krateRouter.push('/a');
    await new Promise((r) => setTimeout(r, 20));
    expect(preloads.length).toBe(2);
    holdPreloads = false;
    await krateRouter.push('/b');
    release();
    await first;
    expect(document.querySelector('#root h1')!.textContent).toBe('B');
    expect(document.head.querySelectorAll('link[rel="preload"], link[rel="modulepreload"]').length).toBe(0);
  });
});
