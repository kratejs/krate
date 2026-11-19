import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import { swapPreservingIslands, maskIslands, unmountIslandsIn, type IslandHandle, type IslandRecord } from './island-swap';

// Island markup as the server renders it.
function island(e: string, props: unknown, html: string, m = 'cmod'): string {
  const json = JSON.stringify({ m, e, s: '/chunks/' + e + '.js', p: props }).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/--/g, '-\\u002d');
  return `<!--krate-island:${json}-->${html}<!--/krate-island-->`;
}

function slot(id: string, html: string): string {
  return `<!--krate-slot:${id}-->${html}<!--/krate-slot:${id}-->`;
}

function realIsland(e: string, props: unknown, html = ''): string {
  return `<script type="application/json" data-krate-props>${JSON.stringify(props)}</script><krate-island style="display:contents" data-module="rmod" data-export="${e}" data-src="/chunks/${e}.js">${html}</krate-island>`;
}

interface Fake extends IslandRecord {
  name: string;
  handle: IslandHandle & { update: Mock<IslandHandle['update']>; unmount: Mock<() => void> };
  /** Whether the island's nodes were still in the document when it unmounted. */
  connectedAtUnmount?: boolean;
  start?: Comment;
  el?: Element;
}

/**
 * Mounts fake islands on every island marker in root, as the island loader
 * and the island chunks do: the record goes on the open comment (or the
 * element), a Preact island adopts its server children (the slot markers go,
 * the nodes stay where they are).
 */
function mountAll(root: Node): Fake[] {
  const out: Fake[] = [];
  const w = document.createTreeWalker(root, 129);
  const found: Node[] = [];
  for (let n = w.nextNode(); n; n = w.nextNode()) found.push(n);
  for (const n of found) {
    let name = '';
    if (n.nodeType === 8 && (n as Comment).data.startsWith('krate-island:')) name = JSON.parse((n as Comment).data.slice(13)).e;
    else if (n.nodeType === 1 && (n as Element).localName === 'krate-island') name = (n as Element).getAttribute('data-export')!;
    else continue;
    if ((n as any).__krateIsland) continue;
    const rec: Fake = {
      name,
      status: 'hydrated',
      handle: {
        update: vi.fn<IslandHandle['update']>(),
        unmount: vi.fn(() => {
          rec.connectedAtUnmount = n.isConnected;
        }),
        slots: [],
      },
    };
    if (n.nodeType === 8) {
      rec.start = n as Comment;
      rec.handle.slots = adoptSlots(n as Comment);
    } else rec.el = n as Element;
    (n as any).__krateIsland = rec;
    out.push(rec);
  }
  return out;
}

function adoptSlots(start: Comment): NonNullable<IslandHandle['slots']> {
  const slots: NonNullable<IslandHandle['slots']> = [];
  let depth = 0;
  const all: Comment[] = [];
  for (let n = start.nextSibling; n; n = n.nextSibling) {
    if (n.nodeType === 8 && (n as Comment).data === '/krate-island') {
      if (!depth) break;
      depth--;
      continue;
    }
    if (n.nodeType === 8 && (n as Comment).data.startsWith('krate-island:')) depth++;
    if (depth) continue;
    if (n.nodeType === 8) all.push(n as Comment);
    if (n.nodeType === 1) {
      const w = document.createTreeWalker(n, 128);
      for (let c = w.nextNode(); c; c = w.nextNode()) all.push(c as Comment);
    }
  }
  for (const c of all) {
    const m = /^krate-slot:(\w+)$/.exec(c.data);
    if (!m) continue;
    const nodes: Node[] = [];
    let n = c.nextSibling;
    for (; n && !(n.nodeType === 8 && (n as Comment).data === '/krate-slot:' + m[1]); n = n.nextSibling) nodes.push(n);
    const html = nodes.map((x) => (x.nodeType === 1 ? (x as Element).outerHTML : x.nodeType === 8 ? '<!--' + (x as Comment).data + '-->' : x.textContent)).join('');
    c.parentNode!.removeChild(n!);
    c.parentNode!.removeChild(c);
    slots.push({ id: m[1], html, set: nodes });
  }
  return slots;
}

function fragment(html: string): Node[] {
  const t = document.createElement('template');
  t.innerHTML = html;
  return Array.from(document.importNode(t.content, true).childNodes);
}

let root: HTMLElement;

function render(html: string): Fake[] {
  root.innerHTML = html;
  return mountAll(root);
}

function swap(html: string, opts = {}): Promise<unknown>[] {
  return swapPreservingIslands(root, null, null, fragment(html), opts);
}

beforeEach(() => {
  document.body.innerHTML = '<div id="root"></div>';
  root = document.getElementById('root')!;
});

describe('swapPreservingIslands', () => {
  it('keeps an island and its ancestors, updates the server content around it', () => {
    const [rec] = render(`<main><h1>Orders</h1><div class="bar">${island('Share', { id: 1 }, '<button id="share">Share</button>')}</div><p id="n">1 share</p></main>`);
    const button = document.getElementById('share');
    const bar = root.querySelector('.bar');
    swap(`<main><h1>Orders</h1><div class="bar wide">${island('Share', { id: 1 }, '<button id="share">Share</button>')}</div><p id="n">2 shares</p></main>`);
    expect(document.getElementById('share')).toBe(button);
    expect(root.querySelector('.bar')).toBe(bar);
    expect(bar!.className).toBe('bar wide');
    expect(document.getElementById('n')!.textContent).toBe('2 shares');
    expect(rec.handle.update).not.toHaveBeenCalled();
    expect(rec.handle.unmount).not.toHaveBeenCalled();
  });

  it('re-renders a kept island with its new props in place', () => {
    const [rec] = render(`<div>${island('Filters', { q: 'cv' }, '<input id="q" value="cv">')}</div>`);
    const input = document.getElementById('q');
    swap(`<div>${island('Filters', { q: '' }, '<input id="q" value="">')}</div>`);
    expect(document.getElementById('q')).toBe(input);
    expect(rec.handle.update).toHaveBeenCalledTimes(1);
    expect(rec.handle.update.mock.calls[0][0]).toEqual({ q: '' });
    // The live open comment carries the props it now renders.
    expect(rec.start!.data).toContain('"q":""');
  });

  it('unmounts an island the new markup drops, before its nodes leave the document', () => {
    const [a, b] = render(`<div>${island('Dialog', {}, '<button>open</button>')}</div><div>${island('Other', {}, '<i>x</i>')}</div>`);
    swap(`<div>${island('Other', {}, '<i>x</i>')}</div>`);
    expect(a.handle.unmount).toHaveBeenCalledTimes(1);
    expect(a.connectedAtUnmount).toBe(true);
    expect(a.gone).toBe(true);
    expect(b.handle.unmount).not.toHaveBeenCalled();
    expect(root.textContent).toBe('x');
  });

  it('matches islands by component in document order', () => {
    const [a1, a2, b] = render(`${island('A', { n: 1 }, '<b>1</b>')}${island('A', { n: 2 }, '<b>2</b>')}${island('B', {}, '<i></i>')}`);
    const nodes = Array.from(root.querySelectorAll('b'));
    swap(`${island('B', {}, '<i></i>')}${island('A', { n: 1 }, '<b>1</b>')}`);
    // B now comes first: the order-preserving match keeps B, and the As that
    // preceded it in the live DOM are unmounted (A mounts anew after B).
    expect(b.handle.unmount).not.toHaveBeenCalled();
    expect(a1.handle.unmount).toHaveBeenCalled();
    expect(a2.handle.unmount).toHaveBeenCalled();
    expect(root.querySelectorAll('b').length).toBe(1);
    expect(root.querySelector('b')).not.toBe(nodes[0]);
  });

  it('replaces an island by a different component (unmounting the old one)', () => {
    const [a] = render(`<section>${island('A', {}, '<b>a</b>')}</section>`);
    swap(`<section>${island('B', {}, '<b>b</b>')}</section>`);
    expect(a.handle.unmount).toHaveBeenCalled();
    expect(root.textContent).toBe('b');
  });

  it('keeps a mounted island across pages only when its props are unchanged', () => {
    const [nav, share] = render(`${island('Nav', { user: 'a' }, '<nav>n</nav>')}<main>${island('Share', { id: 1 }, '<b>1</b>')}</main>`);
    const navEl = root.querySelector('nav');
    swap(`${island('Nav', { user: 'a' }, '<nav>n</nav>')}<main>${island('Share', { id: 2 }, '<b>2</b>')}</main>`, { sameProps: true });
    expect(root.querySelector('nav')).toBe(navEl);
    expect(nav.handle.unmount).not.toHaveBeenCalled();
    expect(share.handle.unmount).toHaveBeenCalled();
    expect(root.querySelector('b')!.textContent).toBe('2');
  });

  it('keeps a real-React island element and re-renders it with the new props', () => {
    const [rec] = render(`<div>${realIsland('Viewer', { color: 'red' })}</div>`);
    rec.el!.innerHTML = '<canvas id="cv"></canvas>';
    const canvas = document.getElementById('cv');
    swap(`<div>${realIsland('Viewer', { color: 'blue' })}</div>`);
    expect(document.getElementById('cv')).toBe(canvas);
    expect(rec.handle.update).toHaveBeenCalledWith({ color: 'blue' });
    swap(`<p>gone</p>`);
    expect(rec.handle.unmount).toHaveBeenCalled();
    expect(rec.connectedAtUnmount).toBe(true);
  });

  it('leaves the live DOM alone when only the islands differ', () => {
    const [rec] = render(`<ul><li>a</li></ul>${island('C', { n: 1 }, '<b>1</b>')}`);
    const li = root.querySelector('li');
    swap(`<ul><li>a</li></ul>${island('C', { n: 2 }, '<b>2</b>')}`);
    expect(root.querySelector('li')).toBe(li);
    expect(rec.handle.update).toHaveBeenCalledWith({ n: 2 });
  });

  it('does not keep an island that is still loading', () => {
    const recs = render(`<div>${island('C', {}, '<b id="b">server</b>')}</div>`);
    recs[0].status = 'loading';
    const b = document.getElementById('b');
    swap(`<div>${island('C', {}, '<b id="b">server</b>')}</div>`);
    expect(document.getElementById('b')).not.toBe(b);
    expect(recs[0].gone).toBe(true);
  });
});

describe('server children of a kept island', () => {
  it('updates changed server children in place and keeps the islands nested in them', () => {
    const page = (items: string, q: string) =>
      island(
        'Providers',
        { __krateChildren: { __krateSlotRef: 's' + q.length } },
        slot('s' + q.length, `<div class="shell">${island('Nav', {}, '<nav id="nav">nav</nav>')}<main><section class="filters">${island('Filters', { q }, `<input id="q" value="${q}">`)}</section><ul>${items}</ul></main></div>`) +
          '<section id="toaster"></section>',
      );
    const [providers, nav, filters] = render(page('<li>cv 1</li>', 'cv'));
    const shell = root.querySelector('.shell');
    const input = document.getElementById('q') as HTMLInputElement;
    const ul = root.querySelector('ul');
    swap(page('<li>cv 1</li><li>other</li>', ''));
    // The slot's nodes (rendered by Providers) keep their identity; nested
    // islands keep their nodes; the server content follows the new markup.
    expect(root.querySelector('.shell')).toBe(shell);
    expect(document.getElementById('q')).toBe(input);
    expect(root.querySelectorAll('li').length).toBe(2);
    expect(root.querySelector('ul')).not.toBe(ul);
    for (const r of [providers, nav, filters]) expect(r.handle.unmount).not.toHaveBeenCalled();
    // The slot id of the new render is mapped onto the adopted slot, so the
    // outer island's props did not change and it is not re-rendered.
    expect(providers.handle.update).not.toHaveBeenCalled();
    expect(filters.handle.update).toHaveBeenCalledWith({ q: '' });
    expect(providers.handle.slots![0].html).toContain('other');
  });

  it('leaves unchanged server children untouched', () => {
    const html = (id: string, n: number) =>
      island('Shell', { n, __krateChildren: { __krateSlotRef: id } }, slot(id, `<article><p>static</p>${island('Like', {}, '<button>like</button>')}</article>`));
    const [shell, like] = render(html('a1', 1));
    const p = root.querySelector('p');
    swap(html('b2', 2));
    expect(root.querySelector('p')).toBe(p);
    expect(shell.handle.update).toHaveBeenCalledTimes(1);
    expect(shell.handle.update.mock.calls[0][0]).toEqual({ n: 2, __krateChildren: { __krateSlotRef: 'a1' } });
    expect(like.handle.unmount).not.toHaveBeenCalled();
  });

  it('renders differently shaped server children anew, unmounting the islands in the old ones', () => {
    const [shell, like] = render(
      island('Shell', { __krateChildren: { __krateSlotRef: 'a' } }, slot('a', `<article>${island('Like', {}, '<button>like</button>')}</article>`)),
    );
    swap(island('Shell', { __krateChildren: { __krateSlotRef: 'b' } }, slot('b', `<p>one</p><p>two</p>`)));
    expect(like.handle.unmount).toHaveBeenCalled();
    expect(like.connectedAtUnmount).toBe(true);
    const [props, pool] = shell.handle.update.mock.calls[0];
    expect(props).toEqual({ __krateChildren: { __krateSlotRef: 'b' } });
    expect(Object.keys(pool!)).toEqual(['b']);
    expect(pool!.b[0].map((n: Node) => n.textContent)).toEqual(['one', 'two']);
  });
});

describe('maskIslands', () => {
  it('replaces islands (nested and real-React) by their component name', () => {
    const html = `<p>a</p>${island('A', { x: 1 }, `<b>${island('B', {}, 'b')}</b>`)}<i></i>${realIsland('R', { y: 1 }, 'fallback')}`;
    expect(maskIslands(html)).toBe(`<p>a</p><!--i:cmod#A--><i></i><script type="application/json" data-krate-props>{"y":1}</script><!--i:rmod-->`);
  });
});

describe('unmountIslandsIn', () => {
  it('unmounts nested islands before the islands around them', () => {
    const order: string[] = [];
    const recs = render(`<div>${island('Outer', {}, `<p>${island('Inner', {}, 'x')}</p>`)}</div>`);
    for (const r of recs) r.handle.unmount.mockImplementation(() => void order.push(r.name));
    unmountIslandsIn([root.firstChild!]);
    expect(order).toEqual(['Inner', 'Outer']);
  });
});
