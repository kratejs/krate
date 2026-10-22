import { describe, it, expect } from 'vitest';
import { jsx, renderToString, goComponent } from './server';

describe('renderToString', () => {
  it('renders nested elements and escapes text', () => {
    const node = jsx('div', { class: 'x', children: [jsx('span', { children: 'a & b' })] });
    expect(renderToString(node)).toBe('<div class="x"><span>a &amp; b</span></div>');
  });

  it('renders void elements without a close tag', () => {
    expect(renderToString(jsx('img', { src: '/x.png', alt: 'y' }))).toBe('<img src="/x.png" alt="y">');
  });

  it('invokes function components', () => {
    const Comp = () => jsx('p', { children: 'hi' });
    expect(renderToString(jsx(Comp, {}))).toBe('<p>hi</p>');
  });

  it('drops event and internal props', () => {
    const node = jsx('button', { onClick: () => undefined, children: 'x' });
    expect(renderToString(node)).toBe('<button>x</button>');
  });

  it('omits null/false-valued attributes', () => {
    expect(renderToString(jsx('input', { disabled: false, value: null }))).toBe('<input>');
    expect(renderToString(jsx('input', { disabled: true }))).toBe('<input disabled>');
  });

  it('passes raw HTML through without escaping', () => {
    expect(renderToString({ __raw: '<b>hi</b>' } as any)).toBe('<b>hi</b>');
  });
});

describe('goComponent', () => {
  it('inlines baked HTML for an annotated server-tier call site', () => {
    const baked = { 'gc3_5_Badge': '<span class="badge">Hi</span>' };
    const node = goComponent('Badge', 'server', baked, { __krateGoId: 'gc3_5_Badge', label: 'Hi' });
    expect(renderToString(node)).toBe('<span class="badge">Hi</span>');
  });

  it('emits a marker for runtime-tier calls, encoding props + children', () => {
    const node = goComponent('Clock', 'runtime', null, { zone: 'UTC', children: ['a ', jsx('b', { children: 'c' })] }, 45);
    const html = renderToString(node);
    const m = html.match(/^<!--krate-go:Clock:(\d+):([A-Za-z0-9_-]+)-->$/);
    expect(m).not.toBeNull();
    expect(m![1]).toBe('45');
    const b64 = m![2].replace(/-/g, '+').replace(/_/g, '/');
    const payload = JSON.parse(Buffer.from(b64, 'base64').toString('utf8'));
    expect(payload.zone).toBe('UTC');
    expect(payload.children).toBe('a <b>c</b>');
  });

  it('falls back to a marker when a server call site is not in the baked map', () => {
    const node = goComponent('Badge', 'server', {}, { __krateGoId: 'missing', label: 'Hi' });
    expect(renderToString(node)).toMatch(/^<!--krate-go:Badge:0:/);
  });
});

describe('server actions', () => {
  it('renders an action function as a POST form endpoint', () => {
    const action = Object.assign(() => undefined, {
      actionUrl: '/base/__krate/action/a123',
    });
    const html = renderToString(jsx('form', { action, children: [] }));
    expect(html).toContain('action="/base/__krate/action/a123"');
    expect(html).toContain('method="post"');
    expect(html).toContain('data-krate-action');
  });

  it('renders a formAction function as a POST submit endpoint', () => {
    const action = Object.assign(() => undefined, {
      actionUrl: '/__krate/action/a456',
    });
    const html = renderToString(jsx('button', { formAction: action, children: 'go' }));
    expect(html).toContain('formaction="/__krate/action/a456"');
    expect(html).toContain('formmethod="post"');
    expect(html).toContain('data-krate-action');
  });
});
