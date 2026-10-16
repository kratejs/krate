import { describe, it, expect, beforeAll } from 'vitest';

interface Listener {
  (e: { data: string }): void;
}

class FakeEventSource {
  static last: FakeEventSource | null = null;
  listeners: Record<string, Listener[]> = {};
  constructor(public url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(type: string, fn: Listener): void {
    (this.listeners[type] ||= []).push(fn);
  }
  emit(type: string, data: unknown): void {
    for (const fn of this.listeners[type] || []) fn({ data: JSON.stringify(data) });
  }
}

let overlay: typeof import('./dev-overlay');

// File-level setup: globals must exist BEFORE the overlay module auto-inits on
// import, and the export is captured from the same module instance.
beforeAll(async () => {
  (globalThis as any).EventSource = FakeEventSource;
  (window as any).__KRATE_DEV__ = {
    sse: '/__krate/hotreload',
    errors: '/__krate/client-error',
    open: '/__krate/open',
    overlay: true,
    toolbar: true,
  };
  overlay = await import('./dev-overlay');
});

function devRoot(): ShadowRoot | null {
  const host = document.getElementById('krate-dev-root');
  return host ? host.shadowRoot : null;
}

describe('dev-overlay', () => {
  it('mounts a shadow-root host with a toolbar', () => {
    const shadow = devRoot();
    expect(shadow).toBeTruthy();
    expect(shadow!.querySelector('.toolbar')).toBeTruthy();
    expect(shadow!.querySelector('.overlay')).toBeTruthy();
  });

  it('renders structured build diagnostics with location, frame and hint', () => {
    FakeEventSource.last!.emit('build-error', {
      diagnostics: [
        {
          file: 'a.tsx',
          line: 3,
          col: 5,
          message: 'boom',
          hint: 'fix it',
          source: 'const x = 1;',
        },
      ],
      buildOk: false,
    });

    const overlay = devRoot()!.querySelector('.overlay') as HTMLElement;
    expect(overlay.hidden).toBe(false);
    const text = (devRoot()!.querySelector('.list') as HTMLElement).textContent || '';
    expect(text).toContain('boom');
    expect(text).toContain('a.tsx:3:5');
    expect(text).toContain('fix it');
    expect(text).toContain('^');
    const open = devRoot()!.querySelector('.open') as HTMLElement;
    expect(open).toBeTruthy();
    expect(open.textContent).toContain('Open in editor');
  });

  it('updates the toolbar route on SPA (history) navigation', () => {
    history.pushState({}, '', '/some/other/route');
    const route = devRoot()!.querySelector('.route') as HTMLElement;
    expect(route.textContent).toBe('/some/other/route');
  });

  it('shows runtime errors from client-error events', () => {
    FakeEventSource.last!.emit('client-error', {
      clientError: { message: 'kaboom', stack: 'at foo (a.tsx:1:1)' },
    });
    const text = (devRoot()!.querySelector('.list') as HTMLElement).textContent || '';
    expect(text).toContain('kaboom');
  });

  it('hides the overlay on Escape', () => {
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    const overlay = devRoot()!.querySelector('.overlay') as HTMLElement;
    expect(overlay.hidden).toBe(true);
  });
});

describe('routeMatches', () => {
  it('matches unprefixed patterns against a base-path URL', () => {
    expect(overlay.routeMatches('/about', '/docs/about', '/docs')).toBe(true);
    expect(overlay.routeMatches('/about', '/docs/other', '/docs')).toBe(false);
    expect(overlay.routeMatches('/', '/docs/', '/docs')).toBe(true);
  });

  it('matches root deployment and dynamic segments', () => {
    expect(overlay.routeMatches('/about', '/about', '')).toBe(true);
    expect(overlay.routeMatches('/blog/[slug]', '/blog/hello', '')).toBe(true);
    expect(overlay.routeMatches('/blog/[slug]', '/blog/hello/x', '')).toBe(false);
    expect(overlay.routeMatches('/docs/[...rest]', '/docs/a/b/c', '')).toBe(true);
  });
});

describe('source-map symbolication', () => {
  it('decodes Base64 VLQ deltas', () => {
    expect(overlay.decodeVLQ('AAAA')).toEqual([0, 0, 0, 0]);
    expect(overlay.decodeVLQ('CAAA')).toEqual([1, 0, 0, 0]);
  });

  it('decodes mappings with cumulative source line deltas', () => {
    const lines = overlay.decodeMappings('AAAA;AACA');
    expect(lines).toHaveLength(2);
    expect(lines[0][0]).toMatchObject({ genCol: 0, src: 0, srcLine: 0, srcCol: 0 });
    expect(lines[1][0]).toMatchObject({ genCol: 0, src: 0, srcLine: 1, srcCol: 0 });
  });

  it('parses V8 and path-only stack frames', () => {
    const a = overlay.parseStackFrame('    at foo (http://localhost:3000/index.abc.js:12:34)');
    expect(a).toMatchObject({ url: 'http://localhost:3000/index.abc.js', line: 12, col: 34 });
    const b = overlay.parseStackFrame('    at bar (/index.abc.js:5:6)');
    expect(b).toMatchObject({ url: '/index.abc.js', line: 5, col: 6 });
    expect(overlay.parseStackFrame('plain message')).toBeNull();
  });

  it('remaps a frame using a fetched map', async () => {
    (globalThis as any).fetch = async () => ({
      ok: true,
      json: async () => ({ version: 3, sources: ['src/foo.ts'], mappings: 'AAAA;AACA' }),
    });
    const out = await overlay.symbolicateStack('Error: x\n    at foo (http://localhost:3000/index.abc.js:2:1)');
    expect(out).toContain('src/foo.ts:2:1');
  });

  it('leaves the stack unchanged when no map is available', async () => {
    (globalThis as any).fetch = async () => ({ ok: false, json: async () => ({}) });
    const stack = '    at foo (http://localhost:3000/missing.js:1:1)';
    expect(await overlay.symbolicateStack(stack)).toBe(stack);
  });
});
