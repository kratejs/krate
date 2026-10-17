import { describe, it, expect } from 'vitest';
import { pageAssetURLs } from './router';

const HTML = `<!doctype html><html><head>
  <link rel="modulepreload" href="/chunks/runtime.abc123.js">
  <link rel="modulepreload" href="index.1234.js">
  <link rel="preload" as="script" href="/assets/extra-5678.js">
</head><body>
  <script src="/chunks/runtime.abc123.js"></script>
  <script src="index.1234.js"></script>
  <script src="/api/track.js"></script>
</body></html>`;

describe('pageAssetURLs', () => {
  it('extracts page scripts, excluding the shared runtime chunk', () => {
    const urls = pageAssetURLs(HTML, 'https://example.com/about/', new Set());
    expect(urls).toContain('https://example.com/about/index.1234.js');
    expect(urls).toContain('https://example.com/api/track.js');
    expect(urls).not.toContain('https://example.com/chunks/runtime.abc123.js');
  });

  it('dedupes across script and modulepreload tags', () => {
    const urls = pageAssetURLs(HTML, 'https://example.com/about/', new Set());
    expect(urls.filter((u) => u.endsWith('/index.1234.js')).length).toBe(1);
  });

  it('picks up preload/modulepreload-only assets', () => {
    const urls = pageAssetURLs(HTML, 'https://example.com/about/', new Set());
    expect(urls).toContain('https://example.com/assets/extra-5678.js');
  });

  it('skips already-loaded assets', () => {
    const loaded = new Set(['https://example.com/about/index.1234.js']);
    const urls = pageAssetURLs(HTML, 'https://example.com/about/', loaded);
    expect(urls).not.toContain('https://example.com/about/index.1234.js');
  });

  it('resolves relative srcs against the target page directory', () => {
    const urls = pageAssetURLs(HTML, 'https://example.com/about', new Set());
    expect(urls).toContain('https://example.com/index.1234.js');
  });
});
