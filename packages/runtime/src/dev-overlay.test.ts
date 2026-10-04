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

function devRoot(): ShadowRoot | null {
  const host = document.getElementById('krate-dev-root');
  return host ? host.shadowRoot : null;
}

describe('dev-overlay', () => {
  beforeAll(async () => {
    (globalThis as any).EventSource = FakeEventSource;
    (window as any).__KRATE_DEV__ = {
      sse: '/__krate/hotreload',
      errors: '/__krate/client-error',
      open: '/__krate/open',
      overlay: true,
      toolbar: true,
    };
    await import('./dev-overlay');
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
