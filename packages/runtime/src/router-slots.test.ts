import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter, krateRouter, matchRoutePattern, compareRouteSpecificity } from './router';
import { disposeAll, krateDisposeExcept, krateDisposeScopes, krateKeepScopes } from './signal';

const KEY = '_sabc_modal';

function chain(src: string, scope: string): string {
  const info = {
    v: 1,
    chain: [{ id: '_lroot_', k: 'layout', d: 0, p: true, s: [{ n: 'modal', key: KEY, scope, src, i: ['photo/[id]'] }] }],
  };
  return `<script type="application/json" id="__krate_route">${JSON.stringify(info)}</script>`;
}

function shell(children: string, slot: string): string {
  return `<nav id="nav">nav</nav><main><!--kl:_lroot_-->${children}<!--/kl:_lroot_--></main><aside id="slot"><!--ks:${KEY}-->${slot}<!--/ks:${KEY}--></aside>`;
}

function doc(title: string, root: string, info: string): string {
  return `<!doctype html><html><head><title>${title}</title></head><body><div id="root">${root}</div>${info}</body></html>`;
}

function fragment(id: string): string {
  const info = { v: 1, frag: { key: KEY, scope: '_lfrag_', target: 'photo/[id]' } };
  return `<!doctype html><html><head><title>x</title></head><body><div id="root"><!--ks:${KEY}--><div id="modal">Modal ${id}</div><!--/ks:${KEY}--></div><script type="application/json" id="__krate_route">${JSON.stringify(info)}</script></body></html>`;
}

let routes: Record<string, { status?: number; body: string }>;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  destroyRouter();
  disposeAll();
  const g = globalThis as any;
  g.__KRATE_CFG__ = { viewTransitions: false };
  g.krateDisposeExcept = krateDisposeExcept;
  g.krateDisposeScopes = krateDisposeScopes;
  g.krateKeepScopes = krateKeepScopes;
  g.disposeAll = disposeAll;
  g.__krateHardNavigate = () => {};
  routes = {
    '/_krate/i/_sabc_modal/photo/1': { body: fragment('1') },
    '/_krate/i/_sabc_modal/photo/2': { body: fragment('2') },
    '/about': { body: doc('About', shell('<p id="about">about</p>', '<i id="def"></i>'), chain('default', '_ldef_')) },
    '/photo/1': { body: doc('Photo', shell('<h1 id="photo">photo</h1>', '<i id="def"></i>'), chain('default', '_ldef_')) },
  };
  fetchMock = vi.fn(async (input: string) => {
    const u = new URL(input, location.href);
    const r = routes[u.pathname];
    const status = r ? (r.status ?? 200) : 404;
    return {
      ok: status < 300,
      status,
      url: u.href,
      headers: new Headers({ 'content-type': r ? 'text/html' : 'text/plain' }),
      text: async () => (r ? r.body : 'missing'),
    } as unknown as Response;
  });
  vi.stubGlobal('fetch', fetchMock);
  history.replaceState(null, '', '/');
  document.head.innerHTML = '<title>Feed</title>';
  document.body.innerHTML = `<div id="root">${shell('<section id="feed">feed</section>', '<i id="def"></i>')}</div>${chain('default', '_ldef_')}`;
  initRouter();
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  const g = globalThis as any;
  for (const k of ['__KRATE_CFG__', 'krateDisposeExcept', 'krateDisposeScopes', 'krateKeepScopes', 'disposeAll', '__krateHardNavigate']) delete g[k];
});

async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
}

function pop(state: unknown, url: string): void {
  history.replaceState(state, '', url);
  window.dispatchEvent(new PopStateEvent('popstate', { state }));
}

describe('intercepting routes', () => {
  it('soft navigation renders the intercepting fragment into the slot', async () => {
    const feed = document.getElementById('feed');
    await krateRouter.push('/photo/1');
    expect(fetchMock.mock.calls.map((c) => new URL(c[0]).pathname)).toEqual(['/_krate/i/_sabc_modal/photo/1']);
    expect(location.pathname).toBe('/photo/1');
    expect(document.getElementById('modal')!.textContent).toBe('Modal 1');
    expect(document.getElementById('feed')).toBe(feed);
    expect(document.getElementById('def')).toBeNull();
  });

  it('back restores the slot content without refetching; forward reopens it', async () => {
    const homeState = history.state;
    const def = document.getElementById('def');
    await krateRouter.push('/photo/1');
    const photoState = history.state;
    fetchMock.mockClear();
    pop(homeState, '/');
    await settle();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(document.getElementById('modal')).toBeNull();
    expect(document.getElementById('def')).toBe(def);
    pop(photoState, '/photo/1');
    await settle();
    expect(document.getElementById('modal')!.textContent).toBe('Modal 1');
  });

  it('chained intercepts replace the fragment', async () => {
    await krateRouter.push('/photo/1');
    await krateRouter.push('/photo/2');
    expect(document.getElementById('modal')!.textContent).toBe('Modal 2');
    expect(document.querySelectorAll('#modal').length).toBe(1);
  });

  it('a soft navigation without a slot match keeps the slot content', async () => {
    await krateRouter.push('/photo/1');
    await krateRouter.push('/about');
    expect(document.getElementById('about')).not.toBeNull();
    expect(document.getElementById('modal')!.textContent).toBe('Modal 1');
  });

  it('a slot page match on the new route replaces the slot', async () => {
    routes['/settings'] = {
      body: doc('Settings', shell('<p id="settings">s</p>', '<b id="slot-settings"></b>'), chain('page', '_lset_')),
    };
    await krateRouter.push('/photo/1');
    await krateRouter.push('/settings');
    expect(document.getElementById('modal')).toBeNull();
    expect(document.getElementById('slot-settings')).not.toBeNull();
  });

  it('falls back to the real page (with an error event) when the fragment is missing', async () => {
    delete routes['/_krate/i/_sabc_modal/photo/1'];
    const errs: unknown[] = [];
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    window.addEventListener('krate:navigate-error', (e) => errs.push((e as CustomEvent).detail));
    await krateRouter.push('/photo/1');
    await settle();
    warn.mockRestore();
    expect(errs.length).toBeGreaterThan(0);
    expect(document.getElementById('photo')).not.toBeNull();
  });
});

describe('route patterns', () => {
  it('matches catch-alls like the compiler', () => {
    expect(matchRoutePattern('/docs/a/b', 'docs/[...slug]')).toEqual({ slug: 'a/b' });
    expect(matchRoutePattern('/docs', 'docs/[...slug]')).toBeNull();
    expect(matchRoutePattern('/shop', 'shop/[[...f]]')).toEqual({ f: '' });
    expect(matchRoutePattern('/photo/7', 'photo/[id]')).toEqual({ id: '7' });
  });

  it('orders static > dynamic > catch-all > optional catch-all', () => {
    const list = ['shop/[[...f]]', 'shop/[...f]', 'shop/[id]', 'shop/cart'];
    list.sort(compareRouteSpecificity);
    expect(list).toEqual(['shop/cart', 'shop/[id]', 'shop/[...f]', 'shop/[[...f]]']);
  });
});
