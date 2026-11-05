import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { createEffect, createRoot, krateFlushSync, krateHook } from './index';
import {
  krateResetNavigation,
  useParams,
  usePathname,
  useRouter,
  useSearchParams,
  useSelectedLayoutSegment,
  useSelectedLayoutSegments,
} from './navigation';
import * as serverNav from './navigation-server';

const REQUEST_STORE = Symbol.for('krate.request.store');

function setURL(url: string, route = ''): void {
  history.replaceState(null, '', url);
  (globalThis as any).__KRATE_ROUTE__ = route;
}

function navigated(): void {
  window.dispatchEvent(new CustomEvent('krate:navigate-end'));
}

beforeEach(() => {
  krateResetNavigation();
  setURL('/');
});

afterEach(() => {
  delete (globalThis as any).__KRATE_ROUTE__;
  delete (globalThis as any)[REQUEST_STORE];
});

describe('client router hooks', () => {
  it('usePathname follows the URL without the trailing slash', () => {
    setURL('/blog/hello/');
    const pathname = krateHook(usePathname());
    expect(pathname()).toBe('/blog/hello');
    setURL('/');
    navigated();
    expect(pathname()).toBe('/');
    setURL('/about/?x=1');
    navigated();
    expect(pathname()).toBe('/about');
  });

  it('is reactive: an effect runs again after a navigation, back and forward included', () => {
    setURL('/a/');
    const seen: string[] = [];
    createRoot(() => {
      const pathname = krateHook(usePathname());
      createEffect(() => {
        seen.push(pathname());
      });
    });
    setURL('/b/');
    navigated();
    krateFlushSync();
    setURL('/a/');
    navigated();
    krateFlushSync();
    expect(seen).toEqual(['/a', '/b', '/a']);
  });

  it('useSearchParams reads the live query', () => {
    setURL('/s/?q=one&tag=a&tag=b');
    const search = krateHook(useSearchParams());
    expect(search().get('q')).toBe('one');
    expect(search().getAll('tag')).toEqual(['a', 'b']);
    setURL('/s/?q=two');
    navigated();
    expect(search().get('q')).toBe('two');
    expect(search().has('tag')).toBe(false);
  });

  it('useParams matches the route pattern of the page', () => {
    setURL('/blog/hello/', '/blog/[slug]');
    const params = krateHook(useParams());
    expect(params()).toEqual({ slug: 'hello' });
    setURL('/blog/caf%C3%A9/', '/blog/[slug]');
    navigated();
    expect(params()).toEqual({ slug: 'café' });
    setURL('/docs/a/b/c/', '/docs/[...rest]');
    navigated();
    expect(params()).toEqual({ rest: ['a', 'b', 'c'] });
    setURL('/shop/', '/shop/[[...path]]');
    navigated();
    expect(params()).toEqual({});
    setURL('/about/', '');
    navigated();
    expect(params()).toEqual({});
  });

  it('a hook result reads as its value when it was not lowered', () => {
    setURL('/blog/hello/', '/blog/[slug]');
    expect(`${usePathname()}`).toBe('/blog/hello');
    expect(JSON.stringify({ p: useParams() })).toBe('{"p":{"slug":"hello"}}');
    expect(String(useSearchParams())).toBe('');
  });

  it('useSelectedLayoutSegment(s) count from the layout depth', () => {
    setURL('/dash/settings/profile/');
    const all = krateHook(useSelectedLayoutSegments());
    const fromDash = krateHook(useSelectedLayoutSegments(undefined, 1));
    const first = krateHook(useSelectedLayoutSegment(undefined, 1));
    const own = krateHook(useSelectedLayoutSegment(undefined, 3));
    expect(all()).toEqual(['dash', 'settings', 'profile']);
    expect(fromDash()).toEqual(['settings', 'profile']);
    expect(first()).toBe('settings');
    expect(own()).toBeNull();
  });

  it('useRouter has push, replace, back, forward, refresh and prefetch', () => {
    const router = useRouter();
    for (const m of ['push', 'replace', 'back', 'forward', 'refresh', 'prefetch'] as const) {
      expect(typeof router[m]).toBe('function');
    }
  });
});

describe('router hooks while rendering a request', () => {
  function request(url: string, params: Record<string, string>, searchParams: Record<string, string> = {}): void {
    (globalThis as any)[REQUEST_STORE] = {
      getStore: () => ({ url, method: 'GET', headers: new Headers(), cookies: new Map(), params, searchParams }),
    };
  }

  it('return plain values for the request, from the client entry or the server entry', () => {
    request('/post/[id]', { id: 'abc' }, { tab: 'info' });
    for (const nav of [{ usePathname, useParams, useSearchParams }, serverNav]) {
      expect(nav.usePathname()).toBe('/post/abc');
      expect(nav.useParams()).toEqual({ id: 'abc' });
      expect(nav.useSearchParams().get('tab')).toBe('info');
    }
  });

  it('fill catch-all params as arrays and as path parts', () => {
    request('/docs/[...rest]', { rest: 'a/b' });
    expect(usePathname()).toBe('/docs/a/b');
    expect(useParams()).toEqual({ rest: ['a', 'b'] });
    request('/shop/[[...path]]', {});
    expect(usePathname()).toBe('/shop');
    expect(useParams()).toEqual({});
  });

  it('segments count from the layout depth and useRouter methods throw', () => {
    request('/dash/settings', {});
    expect(useSelectedLayoutSegments(undefined, 1)).toEqual(['settings']);
    expect(useSelectedLayoutSegment(undefined, 0)).toBe('dash');
    expect(useSelectedLayoutSegment(undefined, 2)).toBeNull();
    expect(() => useRouter().push('/x')).toThrow(/browser/);
  });
});
