import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { Link, renderToString } from './server';
import { h } from './dom';

beforeEach(() => {
  (globalThis as any).__KRATE_CFG__ = { basePath: '/app' };
});

afterEach(() => {
  delete (globalThis as any).__KRATE_CFG__;
});

describe('<Link basePath={false}>', () => {
  it('server render: adds the base path by default and not when opted out', () => {
    const inSite = renderToString(Link({ href: '/pricing', children: 'a' }));
    expect(inSite).toContain('href="/app/pricing"');
    expect(inSite).toContain('data-krate-link');

    const outside = renderToString(Link({ href: '/pricing', basePath: false, children: 'a' }));
    expect(outside).toContain('href="/pricing"');
    expect(outside).toContain('data-krate-external');
    expect(outside).toContain('data-krate-no-base');
    expect(outside).not.toContain('data-krate-link');
    expect(outside).not.toContain('basePath');
  });

  it('client render: data-krate-no-base keeps the href as written', () => {
    const link = h('a', { href: '/pricing', 'data-krate-link': true }, 'a') as unknown as Element;
    expect(link.getAttribute('href')).toBe('/app/pricing');

    const outside = h('a', { href: '/pricing', 'data-krate-external': true, 'data-krate-no-base': true }, 'a') as unknown as Element;
    expect(outside.getAttribute('href')).toBe('/pricing');
  });
});
