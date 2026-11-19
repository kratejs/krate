import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  initRouter,
  destroyRouter,
  krateRouter,
} from './router';
import { callServerAction } from './actions';
import { swapPreservingIslands, unmountIslandsIn } from './island-swap';

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
    json: async () => JSON.parse(r.body ?? 'null'),
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

  it('follows html and body attribute changes and keeps runtime-set ones', async () => {
    routes['/themed'] = {
      body: '<!doctype html><html class="dark" lang="fr"><head><title>T</title></head><body data-x="1"><div id="root"><h1>T</h1></div></body></html>',
    };
    document.documentElement.setAttribute('data-runtime', '1');
    await krateRouter.push('/themed');
    await settle();
    expect(document.documentElement.getAttribute('class')).toBe('dark');
    expect(document.documentElement.getAttribute('lang')).toBe('fr');
    expect(document.body.getAttribute('data-x')).toBe('1');
    expect(document.documentElement.getAttribute('data-runtime')).toBe('1');
    await krateRouter.push('/about');
    await settle();
    expect(document.documentElement.hasAttribute('class')).toBe(false);
    expect(document.body.hasAttribute('data-x')).toBe(false);
    expect(document.documentElement.getAttribute('data-runtime')).toBe('1');
  });

  it('hard-navigates when a redirect leaves the origin', async () => {
    routes['/out'] = { body: page('x', ''), redirect: 'https://example.com/elsewhere' };
    await krateRouter.push('/out');
    expect(hardNavs[0][0]).toBe('https://example.com/elsewhere');
  });
});

describe('focus', () => {
  const search = (q: string) =>
    page('Store', `<div><input aria-label="Search templates" value="${q}"><input aria-label="Other"></div>${q ? '<button aria-label="Clear">x</button>' : ''}`);

  // A query-only navigation (router.replace('?q=...')) or a refresh replaces
  // the page DOM; the focused control keeps focus, as in React.
  it('keeps focus on the same control across a same-page navigation', async () => {
    routes['/store?q=cv'] = { body: search('cv') };
    routes['/store'] = { body: search('') };
    await krateRouter.push('/store?q=cv');
    await settle();
    const input = document.querySelector('input[aria-label="Search templates"]') as HTMLInputElement;
    input.focus();
    input.setSelectionRange(0, 0);
    await krateRouter.replace('/store', { scroll: false });
    await settle();
    const now = document.querySelector('input[aria-label="Search templates"]') as HTMLInputElement;
    expect(now).not.toBe(input);
    expect(document.activeElement).toBe(now);

    (document.querySelector('input[aria-label="Other"]') as HTMLInputElement).focus();
    await krateRouter.refresh();
    await settle();
    expect(document.activeElement).toBe(document.querySelector('input[aria-label="Other"]'));
  });

  it('moves focus to the content on a navigation to another page', async () => {
    routes['/store'] = { body: search('') };
    await krateRouter.push('/store');
    await settle();
    (document.querySelector('input[aria-label="Search templates"]') as HTMLInputElement).focus();
    await krateRouter.push('/about');
    await settle();
    expect(document.activeElement).toBe(document.getElementById('root'));
  });
});

// Islands as the island loader lists them: a record with the handle its
// chunk's mount returned, on the island's open comment.
interface FakeIsland {
  name: string;
  status: string;
  start: Comment;
  handle: { update: ReturnType<typeof vi.fn>; unmount: ReturnType<typeof vi.fn>; slots: never[] };
  connectedAtUnmount?: boolean;
  gone?: boolean;
}

function islandHTML(e: string, props: unknown, html: string): string {
  return `<!--krate-island:${JSON.stringify({ m: 'cm', e, s: '/chunks/' + e + '.js', p: props })}-->${html}<!--/krate-island-->`;
}

/** Mounts every island of the document not mounted yet (what the loader does after a swap). */
function mountIslands(onUnmount?: (r: FakeIsland) => void): FakeIsland[] {
  const out: FakeIsland[] = [];
  const w = document.createTreeWalker(document.body, 128);
  for (let n = w.nextNode() as Comment | null; n; n = w.nextNode() as Comment | null) {
    if (!n.data.startsWith('krate-island:') || (n as any).__krateIsland) continue;
    const rec: FakeIsland = {
      name: JSON.parse(n.data.slice(13)).e,
      status: 'hydrated',
      start: n,
      handle: {
        update: vi.fn(),
        unmount: vi.fn(() => {
          rec.connectedAtUnmount = rec.start.isConnected;
          if (onUnmount) onUnmount(rec);
        }),
        slots: [],
      },
    };
    (n as any).__krateIsland = rec;
    out.push(rec);
  }
  return out;
}

describe('islands across a swap', () => {
  // The island loader chunk installs the island-preserving swap.
  beforeEach(() => {
    (globalThis as any).__krateIslandSwap = { swap: swapPreservingIslands, unmount: unmountIslandsIn };
  });
  afterEach(() => {
    delete (globalThis as any).__krateIslandSwap;
  });

  const order = (shares: number) =>
    page('Order', `<main><h1>Order 1</h1><p id="shares">${shares} shares</p><div class="actions">${islandHTML('ShareButton', { orderId: 1 }, '<button id="share">Share</button>')}</div></main>`);

  it('a revalidating server action refreshes the page in place, keeping its islands mounted', async () => {
    routes['/orders/1'] = { body: order(0) };
    routes['/__krate/action/share'] = {
      type: 'application/json',
      body: JSON.stringify({ ok: true, result: { url: 'x' }, revalidated: true, revalidation: { paths: [{ path: '/dashboard/shares', type: 'page' }] } }),
    };
    await krateRouter.push('/orders/1');
    await settle();
    const [share] = mountIslands();
    const button = document.getElementById('share');
    // The island opened a dialog in a portal.
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);
    routes['/orders/1'] = { body: order(1) };
    fetchMock.mockClear();
    await callServerAction('/__krate/action/share', [1]);
    await settle();
    // Like Next, a revalidation of another path still refreshes the current
    // route (the server rendered it with fresh data)...
    expect(fetchMock.mock.calls.map((c) => new URL(c[0], location.href).pathname)).toEqual(['/__krate/action/share', '/orders/1']);
    expect(document.getElementById('shares')!.textContent).toBe('1 shares');
    // ...in place: the island keeps its DOM and its root, its dialog stays
    // driven by it.
    expect(document.getElementById('share')).toBe(button);
    expect(share.handle.unmount).not.toHaveBeenCalled();
    expect(share.handle.update).not.toHaveBeenCalled();
    expect(mountIslands()).toEqual([]);
    expect(document.querySelectorAll('[role="dialog"]').length).toBe(1);
  });

  it('router.replace to the same page keeps a focused input of an island, re-rendering it with new props', async () => {
    const store = (q: string) =>
      page('Store', `<main><div class="filters">${islandHTML('StoreFilters', { q }, `<input aria-label="Search templates" value="${q}"><button>Clear</button>`)}</div><ul><li>${q || 'all'}</li></ul></main>`);
    routes['/templates?q=cv'] = { body: store('cv') };
    routes['/templates'] = { body: store('') };
    await krateRouter.push('/templates?q=cv');
    await settle();
    const [filters] = mountIslands();
    const input = document.querySelector('input[aria-label="Search templates"]') as HTMLInputElement;
    input.focus();
    const blurs = vi.fn();
    input.addEventListener('blur', blurs);
    await krateRouter.replace('/templates', { scroll: false });
    await settle();
    expect(document.querySelector('input[aria-label="Search templates"]')).toBe(input);
    expect(document.activeElement).toBe(input);
    expect(blurs).not.toHaveBeenCalled();
    expect(filters.handle.update).toHaveBeenCalledWith({ q: '' });
    expect(filters.handle.unmount).not.toHaveBeenCalled();
    expect(document.querySelector('li')!.textContent).toBe('all');
  });

  it('keeps an island around the whole page and the island in its server children', async () => {
    const wrapped = (q: string) =>
      page(
        'Store',
        islandHTML(
          'Providers',
          { __krateChildren: { __krateSlotRef: 'p' + q } },
          `<!--krate-slot:p${q}--><div class="shell">${islandHTML('Filters', { q }, `<input id="q" value="${q}">`)}<ul><li>${q || 'all'}</li></ul></div><!--/krate-slot:p${q}--><section id="toaster"></section>`,
        ),
      );
    routes['/wrapped?q=cv'] = { body: wrapped('cv') };
    routes['/wrapped'] = { body: wrapped('') };
    await krateRouter.push('/wrapped?q=cv');
    await settle();
    const [providers, filters] = mountIslands();
    // Providers adopted its server children (the slot markers go, the nodes stay).
    const open = Array.from(document.getElementById('root')!.childNodes).find((n) => n.nodeType === 8 && (n as Comment).data === 'krate-slot:pcv')!;
    const shell = document.querySelector('.shell')!;
    open.parentNode!.removeChild(open.nextSibling!.nextSibling!);
    open.parentNode!.removeChild(open);
    (providers.handle as any).slots = [{ id: 'pcv', html: shell.outerHTML, set: [shell] }];
    const input = document.getElementById('q') as HTMLInputElement;
    input.focus();
    await krateRouter.replace('/wrapped', { scroll: false });
    await settle();
    expect(document.querySelector('.shell')).toBe(shell);
    expect(document.getElementById('q')).toBe(input);
    expect(document.activeElement).toBe(input);
    expect(document.querySelector('li')!.textContent).toBe('all');
    expect(providers.handle.unmount).not.toHaveBeenCalled();
    expect(providers.handle.update).not.toHaveBeenCalled();
    expect(filters.handle.unmount).not.toHaveBeenCalled();
    expect(filters.handle.update).toHaveBeenCalledWith({ q: '' });
  });

  it('a navigation that drops an island unmounts it before its markup goes', async () => {
    routes['/orders/1'] = { body: order(0) };
    await krateRouter.push('/orders/1');
    await settle();
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);
    const [share] = mountIslands(() => dialog.remove());
    await krateRouter.push('/about');
    await settle();
    expect(share.handle.unmount).toHaveBeenCalledTimes(1);
    expect(share.connectedAtUnmount).toBe(true);
    expect(document.querySelectorAll('[role="dialog"]').length).toBe(0);
    expect(document.querySelector('#root h1')!.textContent).toBe('About');
  });

  it('a navigation to another page remounts an island whose props differ', async () => {
    const post = (id: number) => page('Post', `<main>${islandHTML('Like', { id }, `<button id="like">${id}</button>`)}</main>`);
    routes['/posts/1'] = { body: post(1) };
    routes['/posts/2'] = { body: post(2) };
    await krateRouter.push('/posts/1');
    await settle();
    const [like] = mountIslands();
    const button = document.getElementById('like');
    await krateRouter.push('/posts/2');
    await settle();
    expect(like.handle.unmount).toHaveBeenCalled();
    expect(document.getElementById('like')).not.toBe(button);
    expect(document.getElementById('like')!.textContent).toBe('2');
  });
});

describe('a second copy of the router module', () => {
  // Island bundles carry their own router.ts; its useRouter().replace() used to
  // do a full document load because that copy is never initialized.
  it('hands navigations to the initialized router', async () => {
    const spec = './router?island-copy';
    const copy = (await import(/* @vite-ignore */ spec)) as typeof import('./router');
    expect(copy.krateRouter).not.toBe(krateRouter);
    const seen: string[] = [];
    const off = copy.subscribeNavigation((s) => seen.push(s.pathname + s.search));
    routes['/store?q=cv'] = { body: page('Store', '<input aria-label="q">') };
    await copy.krateRouter.replace('/store?q=cv', { scroll: false });
    await settle();
    expect(hardNavs).toEqual([]);
    expect(location.pathname + location.search).toBe('/store?q=cv');
    expect(copy.getNavigationState().search).toBe('?q=cv');
    expect(seen).toContain('/store?q=cv');
    off();
  });

  // A page with islands only has no page script to start the router.
  it('starts the page router when it has not started yet', async () => {
    destroyRouter();
    const g = globalThis as any;
    g.initRouter = initRouter;
    g.krateRouter = krateRouter;
    try {
      const spec = './router?island-copy-start';
      const copy = (await import(/* @vite-ignore */ spec)) as typeof import('./router');
      routes['/templates'] = { body: page('Templates', '<input aria-label="q">') };
      await copy.krateRouter.replace('/templates', { scroll: false });
      await settle();
      expect(hardNavs).toEqual([]);
      expect(location.pathname).toBe('/templates');
      expect(g[Symbol.for('krate.router')]).toBe(krateRouter);
    } finally {
      delete g.initRouter;
      delete g.krateRouter;
    }
  });

  it('falls back to the page navigate when the page registered no router', async () => {
    destroyRouter();
    const g = globalThis as any;
    const nav = vi.fn(() => false);
    g.navigate = nav;
    g.krateRouter = { push() {} };
    try {
      const spec = './router?island-copy-navigate';
      const copy = (await import(/* @vite-ignore */ spec)) as typeof import('./router');
      await copy.krateRouter.replace('/templates', { scroll: false });
      await copy.krateRouter.push('/store');
      expect(nav.mock.calls).toEqual([
        ['/templates', { scroll: false, replace: true }],
        ['/store', { replace: false }],
      ]);
    } finally {
      delete g.navigate;
      delete g.krateRouter;
    }
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
