import { describe, it, expect, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter } from './router';

function boot(head: string): void {
  destroyRouter();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  document.head.innerHTML = '<title>x</title>' + head;
  document.body.innerHTML = '<div id="root"></div>';
  initRouter();
}

describe('router speculation rules', () => {
  afterEach(() => {
    destroyRouter();
    vi.unstubAllGlobals();
  });

  it('injects the rules when the browser supports them and no CSP meta exists', () => {
    (HTMLScriptElement as any).supports = (t: string) => t === 'speculationrules';
    boot('');
    expect(document.querySelector('script[type="speculationrules"]')).not.toBeNull();
  });

  it('skips the inline rules under a CSP meta', () => {
    (HTMLScriptElement as any).supports = (t: string) => t === 'speculationrules';
    boot('<meta http-equiv="Content-Security-Policy" content="script-src \'self\'">');
    expect(document.querySelector('script[type="speculationrules"]')).toBeNull();
  });
});
