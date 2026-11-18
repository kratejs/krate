import { describe, it, expect, vi } from 'vitest';
import { clientBoundary, jsx, lazy, renderToString, renderToStringAsync, Script, Style } from './server';

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

describe('Script and Style built-ins', () => {
  // A <Script> in an async (SSR) layout came out as escaped text without its attributes.
  it('render real elements with every attribute', async () => {
    async function Layout() {
      return jsx('body', {
        children: [jsx(Script, { src: 'https://a.test/s.js?a=1&b=2', 'data-website-id': 'w1', 'data-do-not-track': 'true', defer: true, strategy: 'afterInteractive' })],
      });
    }
    expect(await renderToStringAsync(jsx(Layout, {}))).toBe(
      '<body><script src="https://a.test/s.js?a=1&amp;b=2" data-website-id="w1" data-do-not-track="true" defer></script></body>',
    );
  });

  it('keep inline code as raw text', () => {
    expect(renderToString(jsx(Script, { id: 'ld', type: 'application/ld+json', children: '{"a":"<b>"}' }))).toBe(
      '<script id="ld" type="application/ld+json">{"a":"<b>"}</script>',
    );
    expect(renderToString(jsx(Style, { children: 'a > b { color: red }' }))).toBe('<style>a > b { color: red }</style>');
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
  // propsOf reads the props of the first island (its JSON script).
  const propsOf = (html: string) => JSON.parse(html.match(/<script type="application\/json" data-krate-props>(.*?)<\/script>/s)![1]);
  // slotRender renders like the Preact module: each slot token (children, a
  // JSX prop) inside the component's own markup, where the slot element goes.
  const slotRender = (_c: unknown, props: Record<string, any>) => {
    const slot = (v: unknown) => (typeof v === 'string' ? v : v && typeof v === 'object' && typeof (v as any).__krateSlotHTML === 'string' ? (v as any).__krateSlotHTML : '');
    return `<div>${slot(props.menu)}${slot(props.__krateChildren)}</div>`;
  };

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
    const data = propsOf(html);
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

  it('adopts server children it rendered instead of sending them again as HTML', async () => {
    const Box = clientBoundary('m6', 'Box', '/c.js', function Box() { return null; }, slotRender) as any;
    const html = await renderToStringAsync(jsx(Box, { children: jsx('b', { children: 'note' }) }));
    expect(html.match(/<b>note<\/b>/g)).toHaveLength(1);
    const data = propsOf(html);
    expect(Object.keys(data)).toEqual(['__krateChildren']);
    expect(data.__krateChildren).toEqual({ __krateSlotRef: expect.stringMatching(/^[0-9a-z]+$/) });
    expect(html).not.toContain('data-props=');
    expect(html).toMatch(/^<script type="application\/json" data-krate-props>[^<]*<\/script><krate-island /);
  });

  it('sends server children as HTML when the component did not render them', async () => {
    const Closed = clientBoundary('m13', 'Closed', '/c.js', function Closed() { return null; }, () => '<div></div>') as any;
    const html = await renderToStringAsync(jsx(Closed, { children: jsx('b', { children: 'later' }), menu: jsx('i', { children: 'm' }) }));
    expect(propsOf(html)).toEqual({ menu: { __krateSlotHTML: '<i>m</i>' }, __krateChildren: '<b>later</b>' });
  });

  it('adopts a JSX prop slot it rendered', async () => {
    const Shell = clientBoundary('m14', 'Shell', '/c.js', function Shell() { return null; }, slotRender) as any;
    const html = await renderToStringAsync(jsx(Shell, { menu: jsx('a', { id: 'who', children: 'Ada' }) }));
    expect(html.match(/id="who"/g)).toHaveLength(1);
    expect(propsOf(html).menu).toEqual({ __krateSlotRef: expect.stringMatching(/^[0-9a-z]+$/) });
  });

  it('puts the props of an island nested under client providers in the document once', async () => {
    const Providers = clientBoundary('m15', 'Providers', '/p.js', function Providers() { return null; }, slotRender) as any;
    const Frame = clientBoundary('m16', 'Frame', '/f.js', function Frame() { return null; }, slotRender) as any;
    const Grid = clientBoundary('m17', 'Grid', '/g.js', function Grid() { return null; }, () => '<ul></ul>') as any;
    const big = 'x'.repeat(50_000);
    async function Outer(p: any) { await Promise.resolve(); return jsx('section', { children: p.children }); }
    const AppShell = (p: any) => jsx(Frame, { menu: jsx('a', { children: 'menu' }), children: p.children });
    async function Inner() { await Promise.resolve(); return jsx('article', { children: jsx(Grid, { rows: big }) }); }
    const html = await renderToStringAsync(
      jsx(Providers, { children: jsx(AppShell, { children: jsx(Outer, { children: jsx(Inner, {}) }) }) }),
    );
    expect(html.split(big)).toHaveLength(2);
    expect(html.match(/<krate-island/g)).toHaveLength(3);
    expect(html.match(/data-krate-props/g)).toHaveLength(3);
    expect(html.match(/>menu</g)).toHaveLength(1);
    expect(html.length).toBeLessThan(big.length + 2_000);
  });

  it('escapes props so a value cannot end the props script', async () => {
    const Box = clientBoundary('m18', 'Box', '/c.js', function Box() { return null; }, () => '<p></p>') as any;
    const evil = '</script><script>alert(1)</script><!-- -->';
    const html = await renderToStringAsync(jsx(Box, { s: evil }));
    expect(html.match(/<\/script>/g)).toHaveLength(1);
    expect(html).not.toContain('<!--');
    expect(propsOf(html).s).toBe(evil);
  });

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
    // Only the top of the child is run; deeper server components stay
    // server-rendered, adopted from the document by id.
    expect(trigger.c).toEqual([{ $: 'e', t: 'button', p: { id: 'b' }, c: [{ $: 'h', r: expect.any(String) }, 'go'] }]);
    expect(html.match(/<b>deep<\/b>/g)).toHaveLength(1);
  });

  it('keeps server children as HTML without asChild', async () => {
    const ns = clientBoundary('m11', 'Menu', '/c.js', { Root() { return null; }, Trigger() { return null; } }, render) as any;
    const Button = () => jsx('button', { children: 'go' });
    const html = await renderToStringAsync(jsx(ns.Root, { children: jsx(ns.Trigger, { children: jsx(Button, {}) }) }));
    expect(propsOf(html).__krateTree[0].c).toEqual([{ $: 'h', r: expect.any(String) }]);
    // A tree slot the component did not render travels as HTML.
    const Closed = clientBoundary('m19', 'Menu', '/c.js', { Root() { return null; }, Trigger() { return null; } }, () => '<div></div>') as any;
    const closed = await renderToStringAsync(jsx(Closed.Root, { children: jsx(Closed.Trigger, { children: jsx(Button, {}) }) }));
    expect(propsOf(closed).__krateTree[0].c).toEqual([{ $: 'h', h: '<button>go</button>' }]);
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
