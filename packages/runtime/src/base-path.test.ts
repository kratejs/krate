import { describe, it, expect, afterEach, vi } from 'vitest';
import { basePath, withBase } from './base-path';
import { h } from './dom';
import { Link, renderToString, jsx } from './server';
import { initRouter, destroyRouter, navigate, krateRouter } from './router';

function setBase(bp: string | undefined): void {
  (globalThis as any).__KRATE_CFG__ = bp === undefined ? undefined : { basePath: bp, viewTransitions: false };
}

describe('base path helpers', () => {
  afterEach(() => setBase(undefined));

  it('normalizes the configured value', () => {
    setBase(undefined);
    expect(basePath()).toBe('');
    setBase('/');
    expect(basePath()).toBe('');
    setBase('repo/');
    expect(basePath()).toBe('/repo');
  });

  it('prefixes only root-relative URLs', () => {
    setBase('/repo');
    expect(withBase('/a?x=1#y')).toBe('/repo/a?x=1#y');
    for (const u of ['//cdn.test/a', 'https://e.test/a', 'rel', '#top', '?q=1', '', 'mailto:a@b.c']) {
      expect(withBase(u)).toBe(u);
    }
  });

  it('leaves URLs alone at the root', () => {
    setBase('');
    expect(withBase('/a')).toBe('/a');
  });
});

describe('<Link> rendering under a base path', () => {
  afterEach(() => setBase(undefined));

  it('prefixes a client-created link', () => {
    setBase('/repo');
    const a = h('a', { href: '/about', 'data-krate-link': true }) as HTMLAnchorElement;
    expect(a.getAttribute('href')).toBe('/repo/about');
  });

  it('prefixes a reactive href each time', () => {
    setBase('/repo');
    const a = h('a', { href: () => '/x', 'data-krate-link': true }) as HTMLAnchorElement;
    expect(a.getAttribute('href')).toBe('/repo/x');
  });

  it('leaves a plain anchor as written', () => {
    setBase('/repo');
    const a = h('a', { href: '/about' }) as HTMLAnchorElement;
    expect(a.getAttribute('href')).toBe('/about');
  });

  it('prefixes a server-rendered link and leaves external ones', async () => {
    setBase('/repo');
    expect(await renderToString(jsx(Link as any, { href: '/about', children: 'a' }))).toContain('href="/repo/about"');
    expect(await renderToString(jsx(Link as any, { href: 'https://e.test/', children: 'a' }))).toContain('href="https://e.test/"');
  });
});

describe('navigation under a base path', () => {
  afterEach(() => {
    destroyRouter();
    setBase(undefined);
    vi.unstubAllGlobals();
  });

  it('prefixes a navigation that falls back to a document load', () => {
    destroyRouter();
    setBase('/repo');
    const assign = vi.fn();
    const replace = vi.fn();
    vi.stubGlobal('location', { assign, replace, href: 'http://x.test/repo/' });
    expect(navigate('/about')).toBe(true);
    expect(assign).toHaveBeenCalledWith('/repo/about');
    navigate('/about', { replace: true });
    expect(replace).toHaveBeenCalledWith('/repo/about');
    void krateRouter.push('/a');
    expect(assign).toHaveBeenCalledWith('/repo/a');
    navigate('https://e.test/');
    expect(assign).toHaveBeenCalledWith('https://e.test/');
  });

  it('scopes the speculation rules to the base path', () => {
    (HTMLScriptElement as any).supports = (t: string) => t === 'speculationrules';
    setBase('/repo');
    document.head.innerHTML = '<title>x</title>';
    document.body.innerHTML = '<div id="root"></div>';
    initRouter();
    const rules = JSON.parse(document.querySelector('script[type="speculationrules"]')!.textContent!);
    expect(rules.prefetch[0].where.and).toEqual([
      { href_matches: '/repo/*' },
      { not: { href_matches: '/repo/__krate/*' } },
      { not: { href_matches: '/repo/api/*' } },
    ]);
  });
});
