import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter, navigate } from './router';

function page(title: string, head: string, body: string): string {
  return `<!doctype html><html><head><title>${title}</title>${head}</head><body><div id="root">${body}</div></body></html>`;
}

const sheet = (name: string) => `<link rel="stylesheet" href="/${name}.css">`;

let routes: Record<string, string>;

function hrefs(): string[] {
  return Array.from(document.querySelectorAll('link[rel="stylesheet"]')).map((l) => new URL((l as HTMLLinkElement).href).pathname);
}

function linkFor(name: string): HTMLLinkElement | null {
  return (Array.from(document.querySelectorAll('link[rel="stylesheet"]')) as HTMLLinkElement[]).find((l) => new URL(l.href).pathname === `/${name}.css`) ?? null;
}

async function tick(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

beforeEach(() => {
  destroyRouter();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  history.replaceState(null, '', '/');
  routes = {};
  // happy-dom tries to fetch the stylesheets; the tests settle them by hand.
  vi.spyOn(console, 'error').mockImplementation(() => {});
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string) => {
      const u = new URL(input, location.href);
      const body = routes[u.pathname] ?? '';
      return {
        ok: true,
        status: 200,
        url: u.href,
        redirected: false,
        headers: new Headers({ 'content-type': 'text/html; charset=utf-8' }),
        text: async () => body,
      } as unknown as Response;
    })
  );
  document.head.innerHTML = '<title>Home</title>' + sheet('page-a') + sheet('tw');
  document.body.innerHTML = '<div id="root"><p>home</p></div>';
  initRouter();
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('router stylesheets', () => {
  it('keeps the incoming document order and removes stale sheets after the swap', async () => {
    routes['/b'] = page('B', sheet('page-b') + sheet('tw'), '<p>b</p>');
    const nav = navigate('/b');
    await tick();
    // Still on the old page: the old sheet stays until the new one loaded.
    expect(document.querySelector('#root')!.textContent).toBe('home');
    expect(linkFor('page-a')).not.toBeNull();
    const added = linkFor('page-b')!;
    expect(added.getAttribute('media')).toBe('not all');
    added.dispatchEvent(new Event('load'));
    await nav;
    await tick();
    expect(document.querySelector('#root')!.textContent).toBe('b');
    expect(hrefs()).toEqual(['/page-b.css', '/tw.css']);
    expect(linkFor('page-b')!.hasAttribute('media')).toBe(false);
  });

  it('waits for the new sheet to load before swapping', async () => {
    routes['/b'] = page('B', sheet('tw') + sheet('page-b'), '<p>b</p>');
    const nav = navigate('/b');
    await tick();
    expect(document.querySelector('#root')!.textContent).toBe('home');
    expect(hrefs()).toEqual(['/page-a.css', '/tw.css', '/page-b.css']);
    linkFor('page-b')!.dispatchEvent(new Event('load'));
    await nav;
    await tick();
    expect(document.querySelector('#root')!.textContent).toBe('b');
    expect(hrefs()).toEqual(['/tw.css', '/page-b.css']);
  });

  it('proceeds when a stylesheet fails to load', async () => {
    routes['/b'] = page('B', sheet('page-b') + sheet('tw'), '<p>b</p>');
    const nav = navigate('/b');
    await tick();
    linkFor('page-b')!.dispatchEvent(new Event('error'));
    await nav;
    await tick();
    expect(document.querySelector('#root')!.textContent).toBe('b');
  });

  it('proceeds after the timeout when a stylesheet never settles', async () => {
    routes['/b'] = page('B', sheet('page-b') + sheet('tw'), '<p>b</p>');
    vi.useFakeTimers();
    const nav = navigate('/b');
    await vi.advanceTimersByTimeAsync(3100);
    await nav;
    await vi.advanceTimersByTimeAsync(10);
    expect(document.querySelector('#root')!.textContent).toBe('b');
    expect(hrefs()).toEqual(['/page-b.css', '/tw.css']);
  });
});
