import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  initRouter,
  destroyRouter,
  krateRouter,
} from './router';

// A fetch mock serving documents by path. Each route is either a page body or
// a full response description.
interface Route {
  status?: number;
  body?: string;
  type?: string;
  /** Final URL after a redirect. */
  redirect?: string;
  /** Rejects with this error (network failure). */
  fail?: Error;
  /** Resolves only when the gate is opened. */
  gate?: Promise<void>;
}

function page(title: string, body: string, head = ''): string {
  return `<!doctype html><html><head><title>${title}</title>${head}</head><body><div id="root">${body}</div></body></html>`;
}

let routes: Record<string, Route>;
let fetchMock: ReturnType<typeof vi.fn>;
let hardNavs: Array<[string, boolean]>;

function installFetch(): void {
  fetchMock = vi.fn(async (input: string, init?: RequestInit) => {
    const u = new URL(input, location.href);
    const r = routes[u.pathname + u.search] ?? routes[u.pathname];
    if (!r) {
      return mockResponse({ status: 404, body: 'nope', type: 'text/plain' }, u.href);
    }
    if (r.gate) await r.gate;
    if (init?.signal?.aborted) {
      const e = new Error('aborted');
      e.name = 'AbortError';
      throw e;
    }
    if (r.fail) throw r.fail;
    return mockResponse(r, r.redirect ? new URL(r.redirect, location.href).href : u.href);
  });
  vi.stubGlobal('fetch', fetchMock);
}

function mockResponse(r: Route, url: string): Response {
  const status = r.status ?? 200;
  return {
    ok: status >= 200 && status < 300,
    status,
    url,
    redirected: !!r.redirect,
    headers: new Headers({ 'content-type': r.type ?? 'text/html; charset=utf-8' }),
    text: async () => r.body ?? '',
  } as unknown as Response;
}

function link(href: string, text: string, attrs = ''): string {
  return `<a data-krate-link href="${href}" ${attrs}>${text}</a>`;
}

function click(el: Element): void {
  el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }));
}

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

function events(name: string): Array<Record<string, unknown>> {
  const got: Array<Record<string, unknown>> = [];
  window.addEventListener(name, (e) => got.push((e as CustomEvent).detail));
  return got;
}

beforeEach(() => {
  destroyRouter();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  history.replaceState(null, '', '/');
  document.title = 'Home';
  document.head.innerHTML = '<title>Home</title>';
  document.body.innerHTML = `<div id="root"><h1>Home</h1>${link('/about', 'About')}${link('/docs', 'Docs')}${link('/docs-old', 'Old')}</div>`;
  routes = {
    '/about': { body: page('About', `<h1>About</h1>${link('/', 'Home')}`) },
    '/docs': { body: page('Docs', `<h1>Docs</h1>${link('/docs', 'Docs')}${link('/docs/intro', 'Intro')}`) },
    '/docs/intro': { body: page('Intro', `<h1>Intro</h1>${link('/docs', 'Docs')}${link('/docs-old', 'Old')}`) },
  };
  installFetch();
  hardNavs = [];
  (globalThis as any).__krateHardNavigate = (url: string, replace: boolean) => hardNavs.push([url, replace]);
  initRouter();
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  delete (globalThis as any).__krateHardNavigate;
  delete (globalThis as any).__KRATE_CFG__;
  delete (document as any).startViewTransition;
});

describe('SPA navigation', () => {
  it('swaps the app root, title, history and announces the route', async () => {
    const nav = events('krate:navigate');
    click(document.querySelector('a[href="/about"]')!);
    await settle();
    expect(location.pathname).toBe('/about');
    expect(document.querySelector('#root h1')!.textContent).toBe('About');
    expect(document.title).toBe('About');
    expect(nav.length).toBe(1);
    expect(document.getElementById('__krate-route-announcer')!.textContent).toBe('About');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('hard-navigates when a redirect leaves the origin', async () => {
    routes['/out'] = { body: page('x', ''), redirect: 'https://example.com/elsewhere' };
    await krateRouter.push('/out');
    expect(hardNavs[0][0]).toBe('https://example.com/elsewhere');
  });
});

describe('races', () => {
  it('a slower superseded navigation never overwrites a newer one', async () => {
    let open!: () => void;
    routes['/slow'] = { body: page('Slow', '<h1>Slow</h1>'), gate: new Promise<void>((r) => (open = r)) };
    const first = krateRouter.push('/slow');
    const second = krateRouter.push('/about');
    await second;
    open();
    await first;
    await settle();
    expect(document.title).toBe('About');
    expect(location.pathname).toBe('/about');
  });
});
