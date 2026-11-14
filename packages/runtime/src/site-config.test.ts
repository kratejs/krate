import { afterEach, describe, expect, it } from 'vitest';
import { basePath } from './base-path.js';
import { siteConfig } from './site-config.js';

const g = globalThis as Record<string, unknown>;

function withBlock(text: string | null) {
  g.document = { getElementById: (id: string) => (id === '__krate_cfg__' && text !== null ? { textContent: text } : null) };
}

afterEach(() => {
  delete g.document;
  delete g.__KRATE_CFG__;
});

describe('site config JSON block', () => {
  it('reads the base path and flags from the head data block', () => {
    withBlock('{"basePath":"/docs","viewTransitions":false}');
    expect(siteConfig().viewTransitions).toBe(false);
    expect(basePath()).toBe('/docs');
  });

  it('falls back to the defaults without a block or with a malformed one', () => {
    withBlock(null);
    expect(siteConfig()).toEqual({});
    expect(basePath()).toBe('');
    withBlock('{nope');
    expect(siteConfig()).toEqual({});
  });

  it('prefers a config the server already set on globalThis', () => {
    g.__KRATE_CFG__ = { basePath: '/app' };
    withBlock('{"basePath":"/other"}');
    expect(basePath()).toBe('/app');
  });
});
