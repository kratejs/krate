import { describe, it, expect, vi } from 'vitest';
import { clientBoundary, jsx, lazy, renderToString, renderToStringAsync } from './server';

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

  it('sends children as a tree, not HTML, when asked to (real-React islands)', async () => {
    const Box = clientBoundary('m7', 'Box', '/c.js', function Box() { return null; }, render, true, true) as any;
    const html = await renderToStringAsync(jsx(Box, { children: jsx('mesh', { position: [1, 2, 3] }) }));
    expect(html).toContain('__krateTree');
    expect(html).not.toContain('__krateChildren');
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

  const propsOf = (html: string) =>
    JSON.parse(html.match(/data-props="([^"]*)"/)![1].replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&#39;/g, "'").replace(/&amp;/g, '&'));

  it('hands an asChild component its server-component child as an element it can clone', async () => {
    const Trigger = clientBoundary('m8', 'Trigger', '/c.js', function Trigger() { return null; }, render) as any;
    const Avatar = clientBoundary('m9', 'Avatar', '/a.js', function Avatar() { return null; }, render) as any;
    async function Button(p: any) {
      await Promise.resolve();
      return jsx('button', { 'data-slot': 'button', ...p });
    }
    const html = await renderToStringAsync(
      jsx(Trigger, { asChild: true, children: jsx(Button, { 'aria-label': 'Account', children: [jsx(Avatar, { n: 1 }), 'Ada'] }) }),
    );
    const data = propsOf(html);
    expect(data.__krateChildren).toBeUndefined();
    expect(data.__krateTree).toEqual([
      {
        $: 'e', t: 'button', p: { 'data-slot': 'button', 'aria-label': 'Account' },
        c: [{ $: 'c', m: 'm9', e: 'Avatar', s: '/a.js', p: { n: 1 }, c: [] }, 'Ada'],
      },
    ]);
    expect(html.match(/<krate-island/g)).toHaveLength(1);
  });

  it('runs the server child of an asChild component nested in a client tree', async () => {
    const ns = clientBoundary('m10', 'Menu', '/c.js', { Root() { return null; }, Trigger() { return null; } }, render) as any;
    const Inner = () => jsx('b', { children: 'deep' });
    const Button = (p: any) => jsx('button', { id: 'b', children: [jsx(Inner, {}), p.children] });
    const html = await renderToStringAsync(
      jsx(ns.Root, { children: jsx(ns.Trigger, { asChild: true, children: jsx(Button, { children: 'go' }) }) }),
    );
    const [trigger] = propsOf(html).__krateTree;
    expect(trigger.p).toEqual({ asChild: true });
    // Only the top of the child is run; deeper server components stay HTML.
    expect(trigger.c).toEqual([{ $: 'e', t: 'button', p: { id: 'b' }, c: [{ $: 'h', h: '<b>deep</b>' }, 'go'] }]);
  });

  it('keeps server children as HTML without asChild', async () => {
    const ns = clientBoundary('m11', 'Menu', '/c.js', { Root() { return null; }, Trigger() { return null; } }, render) as any;
    const Button = () => jsx('button', { children: 'go' });
    const html = await renderToStringAsync(jsx(ns.Root, { children: jsx(ns.Trigger, { children: jsx(Button, {}) }) }));
    expect(propsOf(html).__krateTree[0].c).toEqual([{ $: 'h', h: '<button>go</button>' }]);
  });

  it('warns when an asChild child can only be sent as HTML', async () => {
    const Trigger = clientBoundary('m12', 'RawTrigger', '/c.js', function RawTrigger() { return null; }, render) as any;
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    try {
      await renderToStringAsync(jsx(Trigger, { asChild: true, children: { __raw: '<a>x</a>' } }));
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('<RawTrigger asChild> received a child rendered to HTML'));
    } finally {
      warn.mockRestore();
    }
  });
});

describe('lazy', () => {
  it('renders the loaded component in place on the server', async () => {
    const Draw = lazy(() => Promise.resolve({ default: (p: { n: number; children?: unknown }) => jsx('p', { id: 'd', children: ['n=' + p.n, p.children] }) }));
    const html = await renderToStringAsync(jsx('main', { children: jsx(Draw, { n: 2, children: jsx('b', { children: 'kid' }) }) }));
    expect(html).toBe('<main><p id="d">n=2<b>kid</b></p></main>');
  });

  it('accepts a loader that resolves to the component itself', async () => {
    const Hi = lazy(() => Promise.resolve(() => jsx('i', { children: 'hi' })));
    expect(await renderToStringAsync(jsx(Hi, {}))).toBe('<i>hi</i>');
  });
});

describe('React element types on the server', () => {
  const forwardRef = (render: (p: any, ref: any) => any) => ({ $$typeof: Symbol.for('react.forward_ref'), render });

  it('runs forwardRef, memo and Slot types, in sync and async renders', async () => {
    const Fwd = forwardRef((p, ref) => jsx('b', { 'data-ref': String(ref), children: p.children }));
    const Memo = { $$typeof: Symbol.for('react.memo'), type: Fwd };
    const Slot = Object.assign(forwardRef(() => null), { displayName: 'Slot.Slot' });
    const tree = jsx('div', {
      children: [
        jsx(Memo, { children: 'm' }),
        jsx(Slot, { className: 'x', 'data-slot': 'button', children: jsx('a', { className: 'y', href: '/h', children: 'go' }) }),
      ],
    });
    const want = '<div><b data-ref="null">m</b><a class="x y" data-slot="button" href="/h">go</a></div>';
    expect(renderToString(tree)).toBe(want);
    expect(await renderToStringAsync(tree)).toBe(want);
  });

  it('rejects an unknown element type instead of printing it as a tag', async () => {
    expect(() => renderToString(jsx({ nope: 1 } as any, {}))).toThrow(/Cannot render an element whose type is an object/);
    await expect(renderToStringAsync(jsx({ $$typeof: Symbol.for('react.portal') } as any, {}))).rejects.toThrow(/react\.portal/);
  });
});
