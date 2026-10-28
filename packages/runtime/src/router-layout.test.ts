import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter, krateRouter } from './router';
import {
  createEffect,
  createSignal,
  onCleanup,
  disposeAll,
  krateDisposeExcept,
  krateEnterScope,
  krateLeaveScope,
  krateKeepScopes,
} from './signal';

interface Entry {
  id: string;
  k: 'layout' | 'template';
  d: number;
  p: boolean;
  why?: string;
}

function routeInfo(chain: Entry[]): string {
  return `<script type="application/json" id="__krate_route">${JSON.stringify({ v: 1, chain })}</script>`;
}

const LAYOUT: Entry = { id: '_lroot_', k: 'layout', d: 0, p: true };

function shell(inner: string, title = 'Nav'): string {
  return `<header id="nav"><input id="q">${title}</header><!--kl:_lroot_-->${inner}<!--/kl:_lroot_-->`;
}

function doc(title: string, rootHTML: string, chain: Entry[]): string {
  return `<!doctype html><html><head><title>${title}</title></head><body><div id="root">${rootHTML}</div>${routeInfo(chain)}</body></html>`;
}

let routes: Record<string, string>;

function setup(rootHTML: string, chain: Entry[], path = '/a'): void {
  history.replaceState(null, '', path);
  document.head.innerHTML = '<title>A</title>';
  document.body.innerHTML = `<div id="root">${rootHTML}</div>${routeInfo(chain)}`;
  initRouter();
}

beforeEach(() => {
  destroyRouter();
  disposeAll();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  const g = globalThis as any;
  g.krateDisposeExcept = krateDisposeExcept;
  g.krateKeepScopes = krateKeepScopes;
  g.disposeAll = disposeAll;
  g.__krateHardNavigate = () => {};
  routes = {};
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string) => {
      const u = new URL(input, location.href);
      const body = routes[u.pathname];
      return {
        ok: !!body,
        status: body ? 200 : 404,
        url: u.href,
        headers: new Headers({ 'content-type': 'text/html' }),
        text: async () => body || '',
      } as unknown as Response;
    })
  );
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  const g = globalThis as any;
  delete g.krateDisposeExcept;
  delete g.krateKeepScopes;
  delete g.disposeAll;
  delete g.__krateHardNavigate;
  delete g.__KRATE_CFG__;
});

describe('layout persistence', () => {
  it('keeps a shared layout mounted and swaps only its children', async () => {
    setup(shell('<main>Page A</main>'), [LAYOUT]);
    const nav = document.getElementById('nav')!;
    (document.getElementById('q') as HTMLInputElement).value = 'typed';
    routes['/b'] = doc('B', shell('<main>Page B</main>'), [LAYOUT]);
    await krateRouter.push('/b');
    expect(document.querySelector('main')!.textContent).toBe('Page B');
    expect(document.getElementById('nav')).toBe(nav);
    expect((document.getElementById('q') as HTMLInputElement).value).toBe('typed');
  });

  it('keeps effects of the kept layout scope and disposes page effects', async () => {
    setup(shell('<main>Page A</main>'), [LAYOUT]);
    const log: string[] = [];
    const [count, setCount] = createSignal(0);
    // Layout hydration code runs inside its route scope.
    expect(krateEnterScope('_lroot_')).toBe(true);
    createEffect(() => {
      log.push('layout:' + count());
      onCleanup(() => log.push('layout-cleanup'));
    });
    krateLeaveScope();
    // Page hydration code (no scope).
    createEffect(() => {
      onCleanup(() => log.push('page-cleanup'));
    });
    onCleanup(() => log.push('page-root-cleanup'));
    log.length = 0;
    routes['/b'] = doc('B', shell('<main>Page B</main>'), [LAYOUT]);
    await krateRouter.push('/b');
    expect(log).toContain('page-cleanup');
    expect(log).toContain('page-root-cleanup');
    expect(log).not.toContain('layout-cleanup');
    log.length = 0;
    setCount(1);
    await new Promise((r) => setTimeout(r, 0));
    // The layout effect is still live.
    expect(log).toEqual(['layout-cleanup', 'layout:1']);
  });

  it('remounts layouts the compiler marked as not persistable', async () => {
    (globalThis as any).__KRATE_CFG__ = { viewTransitions: false, dev: true };
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const blocked = { ...LAYOUT, p: false, why: 'the page uses class components' };
    setup(shell('<main>Page A</main>'), [blocked]);
    const nav = document.getElementById('nav')!;
    routes['/b'] = doc('B', shell('<main>Page B</main>'), [blocked]);
    await krateRouter.push('/b');
    expect(document.getElementById('nav')).not.toBe(nav);
    expect(warn.mock.calls.some((c) => String(c[0]).includes('class components'))).toBe(true);
    warn.mockRestore();
  });

  it('swaps the whole root when the layouts differ', async () => {
    setup(shell('<main>Page A</main>'), [LAYOUT]);
    const nav = document.getElementById('nav')!;
    const other: Entry = { id: '_lother_', k: 'layout', d: 0, p: true };
    routes['/b'] = doc('B', '<aside>other</aside><!--kl:_lother_--><main>B</main><!--/kl:_lother_-->', [other]);
    await krateRouter.push('/b');
    expect(nav.isConnected).toBe(false);
    expect(document.querySelector('aside')).not.toBeNull();
  });

  it('remounts a layout under a dynamic segment when the segment value changes', async () => {
    const section: Entry = { id: '_lpost_', k: 'layout', d: 2, p: true };
    const html = (slug: string) =>
      shell(`<div id="post-shell">${slug}<!--kl:_lpost_--><p>${slug}</p><!--/kl:_lpost_--></div>`);
    setup(html('a'), [LAYOUT, section], '/blog/a');
    const nav = document.getElementById('nav')!;
    const postShell = document.getElementById('post-shell')!;
    routes['/blog/b'] = doc('B', html('b'), [LAYOUT, section]);
    await krateRouter.push('/blog/b');
    // The root layout stays, the [slug] layout remounts.
    expect(document.getElementById('nav')).toBe(nav);
    expect(document.getElementById('post-shell')).not.toBe(postShell);
    expect(document.querySelector('p')!.textContent).toBe('b');
  });
});

describe('templates', () => {
  it('remounts the template on every navigation, keeping the parent layout', async () => {
    const tpl: Entry = { id: '_ltpl_', k: 'template', d: 0, p: false };
    const html = (page: string) =>
      shell(`<section id="tpl"><input id="t"><!--kl:_ltpl_--><main>${page}</main><!--/kl:_ltpl_--></section>`);
    setup(html('A'), [LAYOUT, tpl]);
    const nav = document.getElementById('nav')!;
    const section = document.getElementById('tpl')!;
    (document.getElementById('t') as HTMLInputElement).value = 'stale';
    routes['/b'] = doc('B', html('B'), [LAYOUT, tpl]);
    await krateRouter.push('/b');
    expect(document.getElementById('nav')).toBe(nav);
    expect(document.getElementById('tpl')).not.toBe(section);
    expect((document.getElementById('t') as HTMLInputElement).value).toBe('');
    expect(document.querySelector('main')!.textContent).toBe('B');
  });
});

describe('route scopes', () => {
  it('krateEnterScope skips kept scopes only while the router keeps them', () => {
    krateKeepScopes(['_lroot_']);
    expect(krateEnterScope('_lroot_')).toBe(false);
    expect(krateEnterScope('_lother_')).toBe(true);
    krateLeaveScope();
    krateKeepScopes([]);
    expect(krateEnterScope('_lroot_')).toBe(true);
    krateLeaveScope();
  });

  it('krateDisposeExcept([]) is a full teardown', () => {
    const log: string[] = [];
    krateEnterScope('_lroot_');
    onCleanup(() => log.push('layout'));
    krateLeaveScope();
    onCleanup(() => log.push('page'));
    krateDisposeExcept([]);
    expect(log.sort()).toEqual(['layout', 'page']);
  });

  it('effects created later by a kept effect inherit its scope', async () => {
    const log: string[] = [];
    const [show, setShow] = createSignal(false);
    krateEnterScope('_lroot_');
    createEffect(() => {
      if (show()) {
        createEffect(() => onCleanup(() => log.push('nested')));
      }
    });
    krateLeaveScope();
    setShow(true);
    await new Promise((r) => setTimeout(r, 0));
    krateDisposeExcept(['_lroot_']);
    expect(log).toEqual([]);
    disposeAll();
    expect(log).toEqual(['nested']);
  });
});
