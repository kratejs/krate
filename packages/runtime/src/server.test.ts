import { describe, it, expect } from 'vitest';
import { clientBoundary, jsx, renderToString, renderToStringAsync } from './server';

describe('renderToString', () => {
  it('renders nested elements and escapes text', () => {
    const node = jsx('div', { class: 'x', children: [jsx('span', { children: 'a & b' })] });
    expect(renderToString(node)).toBe('<div class="x"><span>a &amp; b</span></div>');
  });

  it('drops event and internal props', () => {
    const node = jsx('button', { onClick: () => undefined, children: 'x' });
    expect(renderToString(node)).toBe('<button>x</button>');
  });

  it('blocks javascript: URLs and drops reserved props', () => {
    const html = renderToString(jsx('a', { href: 'javascript:alert(1)', ref: {}, key: 'k', children: 'x' }));
    expect(html).toContain('href="javascript:throw new Error(');
    expect(html).not.toContain(' ref=');
    expect(html).not.toContain(' key=');
  });
});

describe('server actions', () => {

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

describe('clientBoundary', () => {
  // The render callback stands in for the Preact render module: it echoes the
  // serialized props so the test sees exactly what travels to the client.
  const render = (_c: unknown, props: Record<string, unknown>) => `<i>${JSON.stringify(props)}</i>`;

  it('renders a stateless component as plain HTML without an island', async () => {
    const Icon = clientBoundary('m1', 'Icon', '/c.js', function Icon() { return null; }, render, false) as any;
    const html = await renderToStringAsync(jsx(Icon, { size: 4 }));
    expect(html).toBe('<i>{"size":4}</i>');
  });

  it('wraps a stateful component in a krate-island with its props', async () => {
    const Box = clientBoundary('m2', 'Box', '/c.js', function Box() { return null; }, render) as any;
    const html = await renderToStringAsync(jsx(Box, { id: 'x' }));
    expect(html).toContain('<krate-island');
    expect(html).toContain('data-module="m2"');
    expect(html).toContain('data-export="Box"');
  });

  it('wraps the members of a namespace export under dotted names', async () => {
    const ns = clientBoundary('m3', 'Dialog', '/c.js', { Root() { return null; }, helper() { return 1; } }, render) as any;
    expect(typeof ns.Root).toBe('function');
    expect(ns.helper()).toBe(1);
    const html = await renderToStringAsync(jsx(ns.Root, {}));
    expect(html).toContain('data-export="Dialog.Root"');
  });

  it('keeps nested client components in the parent tree so they can share context', async () => {
    const ns = clientBoundary('m4', 'Dialog', '/c.js', { Root() { return null; }, Trigger() { return null; } }, render) as any;
    const html = await renderToStringAsync(
      jsx(ns.Root, { children: [jsx('div', { children: jsx(ns.Trigger, { id: 't', children: 'Open' }) })] }),
    );
    const attr = html.match(/data-props="([^"]*)"/)![1].replace(/&quot;/g, '"');
    const data = JSON.parse(attr);
    expect(data.__krateChildren).toBeUndefined();
    expect(data.__krateTree).toEqual([
      { $: 'e', t: 'div', p: {}, c: [{ $: 'c', m: 'm4', e: 'Dialog.Trigger', s: '/c.js', p: { id: 't' }, c: ['Open'] }] },
    ]);
  });

  it('hydrates a stateless parent when a nested component has state', async () => {
    const Wrap = clientBoundary('m5', 'Wrap', '/c.js', function Wrap() { return null; }, render, false) as any;
    const Inner = clientBoundary('m5', 'Inner', '/c.js', function Inner() { return null; }, render, true) as any;
    const html = await renderToStringAsync(jsx(Wrap, { children: [jsx(Inner, {})] }));
    expect(html).toContain('<krate-island');
  });

  it('sends server-only children as HTML', async () => {
    const Box = clientBoundary('m6', 'Box', '/c.js', function Box() { return null; }, render) as any;
    const html = await renderToStringAsync(jsx(Box, { children: jsx('b', { children: 'note' }) }));
    expect(html).toContain('__krateChildren');
  });
});
