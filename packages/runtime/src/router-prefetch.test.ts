import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter } from './router';

// Records which elements the router observes and lets a test report them as visible.
let observed: Element[];
let callback: ((entries: any[]) => void) | null;

class FakeIO {
  constructor(cb: (entries: any[]) => void) {
    callback = cb;
  }
  observe(el: Element): void {
    if (!observed.includes(el)) observed.push(el);
  }
  unobserve(): void {}
  disconnect(): void {}
}

let fetched: string[];

function link(i: number, attrs: string): string {
  return `<a data-krate-link href="/p${i}" ${attrs}>p${i}</a>`;
}

function mount(html: string): void {
  document.body.innerHTML = `<div id="root">${html}</div>`;
}

function start(): void {
  initRouter();
}

async function flush(): Promise<void> {
  await new Promise((r) => setTimeout(r, 80));
}

function showAll(): void {
  callback!(observed.map((target) => ({ isIntersecting: true, target })));
}

beforeEach(() => {
  destroyRouter();
  observed = [];
  callback = null;
  fetched = [];
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  history.replaceState(null, '', '/');
  vi.stubGlobal('IntersectionObserver', FakeIO);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string) => {
      fetched.push(new URL(input, location.href).pathname);
      return {
        ok: true,
        status: 200,
        url: new URL(input, location.href).href,
        redirected: false,
        headers: new Headers({ 'content-type': 'text/html' }),
        text: async () => '<!doctype html><html><head><title>x</title></head><body><div id="root"></div></body></html>',
      } as unknown as Response;
    })
  );
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  delete (globalThis as any).__KRATE_CFG__;
});

describe('viewport prefetch', () => {
  it('does not observe default links', () => {
    mount(link(1, 'data-prefetch') + link(2, 'data-prefetch=""') + link(3, ''));
    start();
    expect(observed).toHaveLength(0);
  });

  it('observes and prefetches links that opt in', async () => {
    mount(link(1, 'data-prefetch="viewport"') + link(2, 'data-prefetch'));
    start();
    expect(observed.map((e) => e.getAttribute('href'))).toEqual(['/p1']);
    showAll();
    await flush();
    expect(fetched).toEqual(['/p1']);
  });

  it('never observes a link with prefetch disabled', () => {
    mount(link(1, 'data-prefetch="false"'));
    start();
    expect(observed).toHaveLength(0);
  });

  it('caps viewport prefetches per page', async () => {
    mount(Array.from({ length: 12 }, (_, i) => link(i + 1, 'data-prefetch="viewport"')).join(''));
    start();
    expect(observed).toHaveLength(12);
    showAll();
    await flush();
    expect(fetched).toHaveLength(5);
  });

  it('honours the site-wide viewport default', () => {
    (globalThis as any).__KRATE_CFG__ = { viewTransitions: false, prefetch: 'viewport' };
    mount(link(1, 'data-prefetch') + link(2, 'data-prefetch="false"'));
    start();
    expect(observed.map((e) => e.getAttribute('href'))).toEqual(['/p1']);
  });

  it('skips viewport and intent prefetch under Save-Data', async () => {
    vi.stubGlobal('navigator', { ...navigator, connection: { saveData: true } });
    mount(link(1, 'data-prefetch="viewport"') + link(2, 'data-prefetch'));
    start();
    expect(observed).toHaveLength(0);
    document.querySelector('a[href="/p2"]')!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 400));
    expect(fetched).toHaveLength(0);
  });

  it('skips prefetch on a 2g connection', () => {
    vi.stubGlobal('navigator', { ...navigator, connection: { effectiveType: '2g' } });
    mount(link(1, 'data-prefetch="viewport"'));
    start();
    expect(observed).toHaveLength(0);
  });

  it('still prefetches on hover intent', async () => {
    mount(link(2, 'data-prefetch'));
    start();
    document.querySelector('a[href="/p2"]')!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 400));
    expect(fetched).toEqual(['/p2']);
  });
});
