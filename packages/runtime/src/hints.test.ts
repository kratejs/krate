import { afterEach, describe, expect, it } from 'vitest';
import { krateHintTags, preconnect, preinit, preinitModule, preload, prefetchDNS } from './hints';

afterEach(() => {
  document.head.innerHTML = '';
});

const links = (sel: string) => document.head.querySelectorAll(sel).length;

describe('resource hints in the browser', () => {
  it('adds one link per href', () => {
    preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous', type: 'application/wasm' });
    preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous', type: 'application/wasm' });
    expect(links('link[href="/x.wasm"]')).toBe(1);
    const el = document.head.querySelector('link[href="/x.wasm"]')!;
    expect(el.getAttribute('rel')).toBe('preload');
    expect(el.getAttribute('as')).toBe('fetch');
    expect(el.getAttribute('crossorigin')).toBe('');
    expect(el.getAttribute('type')).toBe('application/wasm');
  });

  it('adds nothing when the server HTML already has the link', () => {
    document.head.innerHTML = '<link rel="preload" href="/x.wasm" as="fetch" crossorigin="">';
    preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous' });
    preload(new URL('/x.wasm', document.baseURI).href, { as: 'fetch', crossOrigin: 'anonymous' });
    expect(document.head.querySelectorAll('link').length).toBe(1);
  });

  it('keeps preloads of the same href with another as apart', () => {
    document.head.innerHTML = '<link rel="preload" href="/a" as="fetch">';
    preload('/a', { as: 'image' });
    expect(links('link[href="/a"]')).toBe(2);
  });

  it('dedupes preconnect, dns-prefetch, preinit and preinitModule', () => {
    document.head.innerHTML =
      '<link rel="preconnect" href="https://cdn.example.com/"><link rel="dns-prefetch" href="https://dns.example.com"><link rel="stylesheet" href="/a.css"><script type="module" src="/m.js" async></script>';
    preconnect('https://cdn.example.com');
    prefetchDNS('https://dns.example.com');
    preinit('/a.css', { as: 'style' });
    preinitModule('/m.js');
    expect(document.head.children.length).toBe(4);
    preinit('/s.js', { as: 'script' });
    preinit('/s.js', { as: 'script' });
    expect(document.head.querySelectorAll('script[src="/s.js"]').length).toBe(1);
  });

  it('writes crossOrigin as React does', () => {
    preconnect('https://a.example', { crossOrigin: 'anonymous' });
    preconnect('https://b.example', { crossOrigin: 'use-credentials' });
    expect(document.head.querySelector('link[href="https://a.example"]')!.getAttribute('crossorigin')).toBe('');
    expect(document.head.querySelector('link[href="https://b.example"]')!.getAttribute('crossorigin')).toBe('use-credentials');
  });

  it('krateHintTags does nothing in the browser', () => {
    krateHintTags(['<link rel="preload" href="/y" as="fetch">']);
    expect(document.head.children.length).toBe(0);
  });
});
