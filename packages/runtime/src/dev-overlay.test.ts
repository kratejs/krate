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
const beacons: Blob[] = [];

// File-level setup: globals must exist BEFORE the overlay module auto-inits on
// import, and the export is captured from the same module instance.
beforeAll(async () => {
  (globalThis as any).EventSource = FakeEventSource;
  // Error reports must not reach the (absent) dev server from the test.
  (navigator as any).sendBeacon = (_url: string, body: Blob) => {
    beacons.push(body);
    return true;
  };
  // What the inline head script buffers before this bundle loads.
  (window as any).__KRATE_EARLY_ERRORS__ = [
    new ErrorEvent('error', { message: 'thrown during first render', error: new Error('thrown during first render') }),
    { type: 'console', message: 'logged during first render' },
  ];
  (globalThis as any).__KRATE_WINDOW__ = 'view-7';
  (window as any).__KRATE_DEV__ = {
    sse: '/__krate/hotreload',
    errors: '/__krate/client-error',
    open: '/__krate/open',
    memory: '/__krate/dev/memory',
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
  it('reports errors thrown before the bundle loaded', async () => {
    const sent = await Promise.all(beacons.map((b) => b.text()));
    expect(sent.some((t) => t.includes('thrown during first render'))).toBe(true);
    expect((window as any).__KRATE_EARLY_ERRORS__).toBeUndefined();
  });

  it('reports early console.error calls with the window id', async () => {
    const sent = await Promise.all(beacons.map((b) => b.text()));
    const e = sent.map((t) => JSON.parse(t)).find((p) => p.message === 'logged during first render');
    expect(e).toMatchObject({ kind: 'console', window: 'view-7' });
  });

  it('reports console.error, unhandled rejections and failed scripts', async () => {
    const before = beacons.length;
    console.error('later console error', { a: 1 });
    const rej = new Event('unhandledrejection');
    (rej as any).reason = new Error('rejected later');
    window.dispatchEvent(rej);
    const script = document.createElement('script');
    script.src = '/nope.js';
    document.body.appendChild(script);
    script.dispatchEvent(new Event('error'));
    await new Promise((r) => setTimeout(r, 20));
    const sent = (await Promise.all(beacons.slice(before).map((b) => b.text()))).map((t) => JSON.parse(t));
    expect(sent.find((p) => p.kind === 'console')?.message).toContain('later console error');
    expect(sent.find((p) => p.kind === 'unhandledrejection')?.message).toBe('rejected later');
    expect(sent.find((p) => p.kind === 'resource')?.message).toContain('/nope.js');
  });

  it('decodes a one-field segment as the end of a mapping', () => {
    const lines = overlay.decodeMappings('AAAA,C;;C');
    expect(lines[0].map((s) => s.src)).toEqual([0, -1]);
    expect(lines[2].map((s) => s.src)).toEqual([-1]);
  });

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

describe('dev-overlay rebuild handling', () => {
  it('clears a build error when a rebuild of another route succeeds', () => {
    history.pushState({}, '', '/current');
    FakeEventSource.last!.emit('build-error', { diagnostics: [{ file: 'b.tsx', message: 'stale-build-error' }], buildOk: false });
    const overlayEl = devRoot()!.querySelector('.overlay') as HTMLElement;
    expect(overlayEl.hidden).toBe(false);
    expect((devRoot()!.querySelector('.list') as HTMLElement).textContent).toContain('stale-build-error');

    // The fix landed on a different route: no reload here, but the error goes.
    FakeEventSource.last!.emit('reload', { pages: ['/elsewhere'] });
    const text = (devRoot()!.querySelector('.list') as HTMLElement).textContent || '';
    expect(text).not.toContain('stale-build-error');
  });

  it('shows a runtime error once when the server echoes it back', () => {
    const before = ((devRoot()!.querySelector('.list') as HTMLElement).textContent || '').split('echo-dup-error').length - 1;
    window.dispatchEvent(new ErrorEvent('error', { message: 'echo-dup-error', filename: 'x.js', lineno: 1, colno: 1 }));
    FakeEventSource.last!.emit('client-error', { clientError: { message: 'echo-dup-error', stack: '' } });
    const text = (devRoot()!.querySelector('.list') as HTMLElement).textContent || '';
    expect(text.split('echo-dup-error').length - 1 - before).toBe(1);
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

describe('dev-overlay native transport', () => {
  function frame(obj: unknown, blobs: Uint8Array[] = []): Uint8Array {
    const json = new TextEncoder().encode(JSON.stringify(obj));
    let size = 4 + json.length + 2;
    for (const b of blobs) size += 4 + b.length;
    const out = new Uint8Array(size);
    const dv = new DataView(out.buffer);
    dv.setUint32(0, json.length);
    out.set(json, 4);
    let pos = 4 + json.length;
    dv.setUint16(pos, blobs.length);
    pos += 2;
    for (const b of blobs) {
      dv.setUint32(pos, b.length);
      out.set(b, pos + 4);
      pos += 4 + b.length;
    }
    return out;
  }
  function cat(...parts: Uint8Array[]): Uint8Array {
    const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
    let pos = 0;
    for (const p of parts) {
      out.set(p, pos);
      pos += p.length;
    }
    return out;
  }

  it('parses frames and keeps an incomplete tail', () => {
    const a = frame({ hmr: { type: 'connected' } });
    const b = frame({ hmr: { type: 'reload', pages: ['/'] } }, [new Uint8Array([1, 2, 3])]);
    const whole = overlay.parseFrames(cat(a, b));
    expect(whole.values).toEqual([{ hmr: { type: 'connected' } }, { hmr: { type: 'reload', pages: ['/'] } }]);
    expect(whole.rest.length).toBe(0);

    const cut = cat(a, b).subarray(0, a.length + b.length - 2);
    const part = overlay.parseFrames(cut);
    expect(part.values).toEqual([{ hmr: { type: 'connected' } }]);
    expect(part.rest.length).toBe(b.length - 2);
    const done = overlay.parseFrames(cat(part.rest, b.subarray(b.length - 2)));
    expect(done.values.length).toBe(1);
  });

  it('detects native origins and extracts host stacks', () => {
    expect(overlay.isNativeOrigin()).toBe(false);
    expect(overlay.hostStackOf({ code: 'internal', data: { stack: 'at f (src/lib/x.host.ts:3:9)' } })).toBe('at f (src/lib/x.host.ts:3:9)');
    expect(overlay.hostStackOf(new Error('plain'))).toBe('');
    expect(overlay.hostStackOf(null)).toBe('');
  });

  it('formats host events', () => {
    expect(overlay.hostEventLine({ seq: 1, kind: 'call', fn: 'tasks.Add', durNs: 3200000 })).toContain('call tasks.Add 3.2ms');
    expect(overlay.hostEventLine({ seq: 2, kind: 'call', fn: 'tasks.Add', code: 'not_found', message: 'nope' })).toContain('not_found: nope');
  });

  it('formats memory rows, with the JS heap unavailable on WebKit', () => {
    expect(overlay.formatBytes(1536)).toBe('2 KiB');
    expect(overlay.formatBytes(5 * 1048576)).toBe('5.0 MiB');
    expect(overlay.formatBytes(-1)).toBe('n/a');
    const rep = {
      procs: [{ pid: 7, role: 'webview:web', pss: 40 * 1048576, privateDirty: 25 * 1048576 }],
      total: { pss: 40 * 1048576, privateDirty: 25 * 1048576 },
    };
    const rows = overlay.memoryRows(rep, 321, null);
    expect(rows[0]).toEqual(['webview:web (7)', 'PSS 40.0 MiB / private dirty 25.0 MiB']);
    expect(rows).toContainEqual(['DOM nodes', '321']);
    expect(rows[rows.length - 1][1]).toContain('unavailable');
    expect(overlay.memoryRows(rep, 1, 2 * 1048576).pop()).toEqual(['JS heap', '2.0 MiB']);
    expect(overlay.memoryRows(null, 1, null)[0]).toEqual(['processes', 'unavailable']);
  });

  it('shows live numbers in the Memory panel and polls only while it is open', async () => {
    const shadow = devRoot()!;
    const calls: string[] = [];
    (globalThis as any).fetch = async (url: string) => {
      calls.push(String(url));
      return {
        ok: true,
        json: async () => ({
          procs: [{ pid: 11, role: 'shell', pss: 12 * 1048576, privateDirty: 9 * 1048576 }],
          total: { pss: 12 * 1048576, privateDirty: 9 * 1048576 },
        }),
      };
    };
    const btn = shadow.querySelector('.tb-btn.mem') as HTMLElement;
    expect(btn).toBeTruthy();
    expect(calls.length).toBe(0);
    btn.click();
    await new Promise((r) => setTimeout(r, 20));
    const panel = shadow.querySelector('.mempanel') as HTMLElement;
    expect(panel.hidden).toBe(false);
    expect(calls).toEqual(['/__krate/dev/memory']);
    expect(panel.textContent).toContain('shell (11)');
    expect(panel.textContent).toContain('PSS 12.0 MiB');
    expect(panel.textContent).toMatch(/DOM nodes\d+/);
    expect(panel.textContent).toContain('JS heap');

    (shadow.querySelector('.memclose') as HTMLElement).click();
    expect(panel.hidden).toBe(true);
    const n = calls.length;
    await new Promise((r) => setTimeout(r, 2300));
    expect(calls.length).toBe(n);
  });
});
