import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  initRouter,
  destroyRouter,
  krateRouter,
  navigate,
  getHistoryState,
  subscribeNavigation,
  invalidatePrefetchCache,
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

  it('ignores modified clicks and target=_blank links', async () => {
    // Keep happy-dom from performing the default (real) navigation.
    const stop = (e: Event) => e.preventDefault();
    window.addEventListener('click', stop);
    document.getElementById('root')!.innerHTML = link('/about', 'x', 'target="_blank"');
    click(document.querySelector('a')!);
    const a = document.createElement('a');
    a.setAttribute('data-krate-link', '');
    a.href = '/about';
    document.body.appendChild(a);
    a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, metaKey: true }));
    await settle();
    window.removeEventListener('click', stop);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('stores NavigateOptions.state on the history entry', async () => {
    await krateRouter.push('/about', { state: { from: 'test' } });
    expect(getHistoryState()).toEqual({ from: 'test' });
    expect(location.pathname).toBe('/about');
  });

  it('replace() does not add a history entry', async () => {
    const before = history.length;
    await krateRouter.replace('/about');
    expect(history.length).toBe(before);
    expect(location.pathname).toBe('/about');
  });

  it('navigate() drives the SPA router and returns false', async () => {
    expect(navigate('/about')).toBe(false);
    await settle();
    expect(document.title).toBe('About');
  });

  it('follows the final URL after a redirect', async () => {
    routes['/old'] = { body: page('About', '<h1>About</h1>'), redirect: '/about?moved=1' };
    await krateRouter.push('/old');
    expect(location.pathname + location.search).toBe('/about?moved=1');
  });

  it('hard-navigates for non-HTML responses', async () => {
    routes['/file.json'] = { body: '{}', type: 'application/json' };
    await krateRouter.push('/file.json');
    expect(hardNavs).toEqual([['http://localhost:3000/file.json', false]]);
    expect(location.pathname).toBe('/');
  });

  it('hard-navigates when a redirect leaves the origin', async () => {
    routes['/out'] = { body: page('x', ''), redirect: 'https://example.com/elsewhere' };
    await krateRouter.push('/out');
    expect(hardNavs[0][0]).toBe('https://example.com/elsewhere');
  });

  it('cancels a navigation when krate:navigate-start is prevented', async () => {
    const stop = (e: Event) => e.preventDefault();
    window.addEventListener('krate:navigate-start', stop);
    await krateRouter.push('/about');
    window.removeEventListener('krate:navigate-start', stop);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(location.pathname).toBe('/');
  });

  it('notifies subscribers with pathname, search and status', async () => {
    const seen: string[] = [];
    const off = subscribeNavigation((s) => seen.push(`${s.status}:${s.pathname}${s.search}`));
    await krateRouter.push('/docs?x=1');
    off();
    expect(seen).toContain('loading:/');
    expect(seen[seen.length - 1]).toBe('idle:/docs?x=1');
  });
});

describe('view transitions', () => {
  it('updates history and focus only after the transition callback ran', async () => {
    (globalThis as any).__KRATE_CFG__ = { viewTransitions: 'auto' };
    const order: string[] = [];
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    (document as any).startViewTransition = (cb: () => void) => {
      // The browser runs the update callback asynchronously.
      const updateCallbackDone = gate.then(() => {
        order.push('mutate');
        cb();
      });
      return { updateCallbackDone, finished: updateCallbackDone };
    };
    const origPush = history.pushState.bind(history);
    const spy = vi.spyOn(history, 'pushState').mockImplementation((...args: Parameters<History['pushState']>) => {
      order.push('push:' + document.title);
      origPush(...args);
    });
    const p = krateRouter.push('/about');
    await settle();
    expect(order).toEqual([]);
    release();
    await p;
    spy.mockRestore();
    expect(order).toEqual(['mutate', 'push:About']);
  });
});

describe('hash navigation', () => {
  it('handles hash-only links in place without fetching', async () => {
    document.getElementById('root')!.innerHTML = `${link('#sec', 'Sec')}<div id="sec">x</div>`;
    const scrolled = vi.fn();
    (document.getElementById('sec') as any).scrollIntoView = scrolled;
    click(document.querySelector('a')!);
    await settle();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(location.hash).toBe('#sec');
    expect(scrolled).toHaveBeenCalled();
  });

  it('does not refetch on a hash-only popstate', async () => {
    history.pushState(history.state, '', '/#a');
    window.dispatchEvent(new PopStateEvent('popstate', { state: history.state }));
    await settle();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('a same-page link with a hash scrolls instead of fetching', async () => {
    await krateRouter.push('/docs');
    fetchMock.mockClear();
    await krateRouter.push('/docs#part');
    expect(fetchMock).not.toHaveBeenCalled();
    expect(location.href).toBe('http://localhost:3000/docs#part');
  });
});

describe('scroll restoration', () => {
  it('restores the scroll position of the entry being returned to', async () => {
    routes['/'] = { body: page('Home', '<h1>Home</h1>') };
    const homeState = history.state;
    window.scrollTo(0, 420);
    await krateRouter.push('/about');
    expect(window.scrollY).toBe(0);
    const aboutState = history.state;
    window.scrollTo(0, 77);
    // Simulate the browser's history step back to "/": the URL and state
    // change first, then popstate fires.
    history.replaceState(homeState, '', '/');
    window.dispatchEvent(new PopStateEvent('popstate', { state: homeState }));
    await settle();
    expect(document.title).toBe('Home');
    expect(window.scrollY).toBe(420);
    history.replaceState(aboutState, '', '/about');
    window.dispatchEvent(new PopStateEvent('popstate', { state: aboutState }));
    await settle();
    expect(document.title).toBe('About');
    expect(window.scrollY).toBe(77);
  });
});

describe('error handling', () => {
  it('shows a server-rendered 404 document in place and reports the status', async () => {
    const errs = events('krate:navigate-error');
    routes['/missing'] = { status: 404, body: page('Not found', '<h1>Not found</h1>') };
    await krateRouter.push('/missing');
    expect(document.title).toBe('Not found');
    expect(location.pathname).toBe('/missing');
    expect(errs).toEqual([{ url: 'http://localhost:3000/missing', status: 404 }]);
    expect(hardNavs).toEqual([]);
  });

  it('falls back to <basePath>/404.html when the 404 response is not HTML', async () => {
    (globalThis as any).__KRATE_CFG__ = { viewTransitions: false, basePath: '/app' };
    routes['/app/gone'] = { status: 404, body: 'nope', type: 'text/plain' };
    routes['/app/404.html'] = { body: page('404', '<h1>Lost</h1>') };
    await krateRouter.push('/app/gone');
    expect(document.querySelector('#root h1')!.textContent).toBe('Lost');
    expect(fetchMock.mock.calls.map((c) => new URL(c[0], location.href).pathname)).toContain('/app/404.html');
  });

  it('uses /500.html for server errors without an HTML body', async () => {
    routes['/boom'] = { status: 503, body: '', type: 'text/plain' };
    routes['/500.html'] = { body: page('Error', '<h1>Broken</h1>') };
    await krateRouter.push('/boom');
    expect(document.querySelector('#root h1')!.textContent).toBe('Broken');
  });

  it('does not show the 404 page for 403 responses', async () => {
    routes['/secret'] = { status: 403, body: 'forbidden', type: 'text/plain' };
    routes['/404.html'] = { body: page('404', '<h1>Lost</h1>') };
    await krateRouter.push('/secret');
    expect(hardNavs).toEqual([['http://localhost:3000/secret', false]]);
    expect(document.querySelector('#root h1')!.textContent).toBe('Home');
  });

  it('emits krate:navigate-error on network failure, then hard-navigates', async () => {
    const errs = events('krate:navigate-error');
    routes['/net'] = { fail: new TypeError('Failed to fetch') };
    await krateRouter.push('/net');
    expect(errs.length).toBe(1);
    expect(errs[0].status).toBe(0);
    expect(hardNavs).toEqual([['http://localhost:3000/net', false]]);
  });

  it('lets the app cancel the hard-reload fallback', async () => {
    const stop = (e: Event) => e.preventDefault();
    window.addEventListener('krate:navigate-error', stop);
    routes['/net'] = { fail: new TypeError('Failed to fetch') };
    await krateRouter.push('/net');
    window.removeEventListener('krate:navigate-error', stop);
    expect(hardNavs).toEqual([]);
    expect(location.pathname).toBe('/');
  });

  it('never uses document.write', async () => {
    const spy = vi.spyOn(document, 'write');
    routes['/missing'] = { status: 404, body: page('Not found', '<h1>Not found</h1>') };
    await krateRouter.push('/missing');
    expect(spy).not.toHaveBeenCalled();
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

describe('prefetch cache', () => {
  it('serves a prefetched document once, then refetches', async () => {
    await krateRouter.prefetch('/about');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await krateRouter.push('/about');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    routes['/'] = { body: page('Home', '<h1>Home</h1>') };
    await krateRouter.push('/');
    await krateRouter.push('/about');
    // The cache hit was consumed: revisiting fetches fresh HTML.
    expect(fetchMock.mock.calls.filter((c) => String(c[0]).endsWith('/about')).length).toBe(2);
  });

  it('expires entries after the TTL', async () => {
    (globalThis as any).__KRATE_CFG__ = { viewTransitions: false, prefetchTTL: 0 };
    await krateRouter.prefetch('/about');
    await new Promise((r) => setTimeout(r, 5));
    await krateRouter.push('/about');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('invalidatePrefetchCache drops entries', async () => {
    await krateRouter.prefetch('/about');
    invalidatePrefetchCache('/about');
    await krateRouter.push('/about');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('does not cache error responses', async () => {
    routes['/missing'] = { status: 404, body: page('Not found', '') };
    await krateRouter.prefetch('/missing');
    await krateRouter.push('/missing');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});

describe('refresh and revalidation', () => {
  it('refresh() refetches the current route bypassing the cache and keeps history', async () => {
    await krateRouter.push('/about');
    routes['/about'] = { body: page('About v2', '<h1>About v2</h1>') };
    await krateRouter.prefetch('/about');
    const len = history.length;
    window.scrollTo(0, 33);
    await krateRouter.refresh();
    expect(document.title).toBe('About v2');
    expect(history.length).toBe(len);
    expect(window.scrollY).toBe(33);
    const last = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
    expect(last[1].cache).toBe('no-cache');
  });

  it('krate:revalidate refreshes the current route', async () => {
    await krateRouter.push('/about');
    routes['/about'] = { body: page('Fresh', '<h1>Fresh</h1>') };
    window.dispatchEvent(new CustomEvent('krate:revalidate', { detail: { paths: ['/about'] } }));
    await settle();
    expect(document.title).toBe('Fresh');
  });
});

describe('active links', () => {
  it('marks exact matches with aria-current and segments separately', async () => {
    await krateRouter.push('/docs/intro');
    const docs = document.querySelector('a[href="/docs"]')!;
    const old = document.querySelector('a[href="/docs-old"]')!;
    expect(docs.getAttribute('aria-current')).toBeNull();
    expect(docs.getAttribute('data-krate-active')).toBe('segment');
    expect(old.hasAttribute('data-krate-active')).toBe(false);
    await krateRouter.push('/docs');
    expect(document.querySelector('a[href="/docs"]')!.getAttribute('aria-current')).toBe('page');
  });
});

describe('forms', () => {
  it('intercepts GET form submissions', async () => {
    routes['/search'] = { body: page('Search', '<h1>Results</h1>') };
    document.getElementById('root')!.innerHTML =
      '<form action="/search"><input name="q" value="krate"><button name="go" value="1">Go</button></form>';
    const form = document.querySelector('form')!;
    const ev = new Event('submit', { bubbles: true, cancelable: true });
    (ev as any).submitter = form.querySelector('button');
    form.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
    await settle();
    expect(location.pathname + location.search).toBe('/search?q=krate&go=1');
    expect(document.title).toBe('Search');
  });

  it('leaves POST forms alone', async () => {
    document.getElementById('root')!.innerHTML = '<form method="post" action="/search"><input name="q"></form>';
    const ev = new Event('submit', { bubbles: true, cancelable: true });
    document.querySelector('form')!.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
  });
});

describe('focus', () => {
  it('focuses the root temporarily and removes tabindex on blur', async () => {
    await krateRouter.push('/about');
    const root = document.getElementById('root')!;
    expect(document.activeElement).toBe(root);
    expect(root.getAttribute('tabindex')).toBe('-1');
    root.blur();
    expect(root.hasAttribute('tabindex')).toBe(false);
  });
});
