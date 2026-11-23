// Island-preserving DOM swaps for the SPA router.
//
// A router swap (a navigation, router.refresh(), the refresh after a server
// action that revalidated) replaces server-rendered markup with the markup of
// the new document. Client islands in that markup are not replaced: an island
// mounted in the live DOM that has a counterpart in the new markup keeps its
// nodes and its mounted root, and is re-rendered in place with the new server
// props (state, DOM nodes and focus survive), as React keeps a mounted client
// component across a refresh. An island the swap really removes is unmounted
// before its markup goes, so its effects and portals are cleaned up.
//
// Islands are the ones the island loader lists in window.__krateIslands: a
// Preact island is the nodes between <!--krate-island:{m,e,s,p}--> and
// <!--/krate-island-->, a real-React island a <krate-island data-src> element
// (props in the JSON script before it). The loader puts each island's record
// on its open comment or element (__krateIsland); a mounted record carries the
// handle its chunk's mount returned: update(props, pool), unmount() and, for a
// Preact island, the server children (slots) it adopted from the document.
//
// Keying: islands are matched per container (the swapped region, or one
// server-children slot of a kept island), outermost islands only, in document
// order by module and export: the n-th island of a component in the live
// region pairs with the n-th one in the new markup. A navigation to another
// pathname only keeps an island whose server props are unchanged too (a
// layout's island); one whose props differ belongs to the other page and is
// mounted anew, as React remounts a page's components.
//
// The live ancestors of a kept island stay in place as long as the new markup
// has elements of the same tags on the same path (their attributes follow the
// new markup), so the island is never detached; everything else outside the
// islands is replaced by the new markup.

/** A server-children slot a Preact island adopted from the document. */
export interface IslandSlot {
  id: string;
  /** The slot's server HTML (outerHTML of its nodes) when it was adopted. */
  html: string;
  /** The live top-level nodes of the slot (the island keeps the array current). */
  set: Node[];
}

/** What an island chunk's mount returns. */
export interface IslandHandle {
  update(props: unknown, pool?: Record<string, Node[][]>): unknown;
  unmount(): void;
  slots?: IslandSlot[];
  /** Re-reads the slots' sets from the document (nested islands changed their nodes). */
  sync?(): void;
}

/** An island record of window.__krateIslands. */
export interface IslandRecord {
  status?: string;
  handle?: IslandHandle | null;
  gone?: boolean;
}

const OPEN = 'krate-island:';
const CLOSE = '/krate-island';
const OPEN_TAG = '<!--' + OPEN;
const CLOSE_TAG = '<!--' + CLOSE + '-->';
const SLOT_OPEN = /^krate-slot:([0-9a-z]+)$/;

interface Isl {
  key: string;
  start?: Comment;
  end?: Comment;
  el?: Element;
  /** Props as rendered: the open comment's JSON, or the props script's text. */
  json: string;
  /** Elements from the top of the scanned list down to the island's parent. */
  chain: Element[];
  rec?: IslandRecord;
}

function isOpen(n: Node): n is Comment {
  return n.nodeType === 8 && (n as Comment).data.indexOf(OPEN) === 0;
}

function isClose(n: Node): boolean {
  return n.nodeType === 8 && (n as Comment).data === CLOSE;
}

function isHost(n: Node): n is Element {
  return n.nodeType === 1 && (n as Element).localName === 'krate-island' && (n as Element).hasAttribute('data-src');
}

function closeOf(start: Node): Comment | null {
  let d = 0;
  for (let n = start.nextSibling; n; n = n.nextSibling) {
    if (isOpen(n)) d++;
    else if (isClose(n)) {
      if (!d) return n as Comment;
      d--;
    }
  }
  return null;
}

function rangeNodes(start: Node, end: Node): Node[] {
  const out: Node[] = [start];
  for (let n = start.nextSibling; n && n !== end; n = n.nextSibling) out.push(n);
  out.push(end);
  return out;
}

function nodesOf(i: Isl): Node[] {
  return i.el ? [i.el] : rangeNodes(i.start!, i.end!);
}

function propsText(el: Element): string {
  if (el.hasAttribute('data-props')) return el.getAttribute('data-props') || '';
  const s = el.previousElementSibling;
  return s && s.localName === 'script' && s.hasAttribute('data-krate-props') ? s.textContent || '' : '';
}

/** The outermost islands in list, in document order. */
function scan(list: ArrayLike<Node>, chain: Element[] = [], out: Isl[] = []): Isl[] {
  for (let i = 0; i < list.length; i++) {
    const n = list[i];
    if (isOpen(n)) {
      const end = closeOf(n);
      let p: { m?: string; e?: string } | null = null;
      try {
        p = JSON.parse(n.data.slice(OPEN.length));
      } catch {
        p = null;
      }
      if (!end || !p) continue;
      out.push({ key: p.m + '#' + p.e, start: n, end, json: n.data.slice(OPEN.length), chain, rec: (n as any).__krateIsland });
      while (i < list.length - 1 && list[i] !== end) i++;
    } else if (isHost(n)) {
      out.push({
        key: n.getAttribute('data-module') + '#' + n.getAttribute('data-export'),
        el: n,
        json: propsText(n),
        chain,
        rec: (n as any).__krateIsland,
      });
    } else if (n.nodeType === 1) {
      scan(n.childNodes, chain.concat(n as Element), out);
    }
  }
  return out;
}

function mounted(rec: IslandRecord | undefined): rec is IslandRecord & { handle: IslandHandle } {
  const h = rec && rec.handle;
  return !!rec && !rec.gone && rec.status === 'hydrated' && !!h && typeof h.update === 'function' && typeof h.unmount === 'function';
}

/** Calls fn on every slot id referenced in props (server children adopted from the document). */
function eachSlotRef(v: any, fn: (id: string) => string): any {
  if (Array.isArray(v)) return v.map((x) => eachSlotRef(x, fn));
  if (!v || typeof v !== 'object') return v;
  const out: any = {};
  for (const k in v) {
    const x = v[k];
    if (k === '__krateSlotRef' && typeof x === 'string') out[k] = fn(x);
    else if (k === 'r' && v.$ === 'h' && typeof x === 'string') out[k] = fn(x);
    else out[k] = eachSlotRef(x, fn);
  }
  return out;
}

function parse(json: string): any {
  try {
    return JSON.parse(json);
  } catch {
    return undefined;
  }
}

/** The server props of an island, with slot ids (random per render) blanked out. */
function propsKey(i: Isl): string {
  const v = parse(i.json);
  return JSON.stringify(eachSlotRef(i.el ? v : v && v.p, () => ''));
}

/** Serializes nodes as the island chunks snapshot a slot's server HTML. */
export function nodesHTML(nodes: ArrayLike<Node>): string {
  let s = '';
  for (let i = 0; i < nodes.length; i++) {
    const n = nodes[i];
    s += n.nodeType === 1 ? (n as Element).outerHTML : n.nodeType === 8 ? '<!--' + (n as Comment).data + '-->' : n.textContent || '';
  }
  return s;
}

/** The next real-React island element (one with data-src) in html from i, or -1. */
function hostAt(html: string, i: number): number {
  for (let h = html.indexOf('<krate-island ', i); h >= 0; h = html.indexOf('<krate-island ', h + 1)) {
    if (/\sdata-src=/.test(html.slice(h, html.indexOf('>', h)))) return h;
  }
  return -1;
}

/**
 * HTML with every island replaced by a marker naming its component, so two
 * renders compare equal when only the islands (their props, or what they
 * rendered since) differ.
 */
export function maskIslands(html: string): string {
  let out = '';
  let i = 0;
  for (;;) {
    const a = html.indexOf(OPEN_TAG, i);
    const h = hostAt(html, i);
    if (a === -1 && h < 0) return out + html.slice(i);
    if (h >= 0 && (a === -1 || h < a)) {
      const e = html.indexOf('</krate-island>', h);
      if (e === -1) return out + html.slice(i);
      out += html.slice(i, h) + '<!--i:' + (/data-module="([^"]*)"/.exec(html.slice(h, html.indexOf('>', h))) || ['', ''])[1] + '-->';
      i = e + 15;
      continue;
    }
    const headEnd = html.indexOf('-->', a);
    if (headEnd === -1) return out + html.slice(i);
    const p = parse(html.slice(a + OPEN_TAG.length, headEnd));
    let j = headEnd + 3;
    let d = 0;
    for (;;) {
      const o = html.indexOf(OPEN_TAG, j);
      const c = html.indexOf(CLOSE_TAG, j);
      if (c === -1) return out + html.slice(i);
      if (o !== -1 && o < c) {
        d++;
        j = o + OPEN_TAG.length;
        continue;
      }
      j = c + CLOSE_TAG.length;
      if (!d) break;
      d--;
    }
    out += html.slice(i, a) + '<!--i:' + (p ? p.m + '#' + p.e : '') + '-->';
    i = j;
  }
}

/**
 * Every island record in nodes (nested ones included), in document order. An
 * island's open comment counts with the nodes now between its comments,
 * whether nodes lists them or not: a slot's set lists a nested island as its
 * two comments only.
 */
function recordsIn(nodes: ArrayLike<Node>): IslandRecord[] {
  const out: IslandRecord[] = [];
  const seen = new Set<Node>();
  const visit = (n: Node) => {
    if (seen.has(n)) return;
    seen.add(n);
    const r = (n as any).__krateIsland as IslandRecord | undefined;
    if (r && (isOpen(n) || isHost(n))) out.push(r);
    if (isOpen(n)) {
      const end = closeOf(n);
      if (end) for (let x = n.nextSibling; x && x !== end; x = x.nextSibling) visit(x);
      // A set out of the document keeps them on the open comment.
      const inner = (n as any).__krateInner as Node[] | null | undefined;
      if (inner) for (const x of inner) visit(x);
      return;
    }
    if (n.nodeType !== 1) return;
    for (let c = n.firstChild; c; c = c.nextSibling) visit(c);
  };
  for (let i = 0; i < nodes.length; i++) visit(nodes[i]);
  return out;
}

/**
 * Unmounts every island in nodes, innermost first, so their effects and
 * portals are cleaned up before the nodes leave the document. An island still
 * loading is marked gone (the loader unmounts it once its chunk mounted).
 */
export function unmountIslandsIn(nodes: ArrayLike<Node>): void {
  const recs = recordsIn(nodes);
  for (let i = recs.length - 1; i >= 0; i--) {
    const r = recs[i];
    if (r.gone) continue;
    r.gone = true;
    const h = r.handle;
    if (!h || typeof h.unmount !== 'function') continue;
    try {
      h.unmount();
    } catch (err) {
      console.error(err);
    }
  }
}

function syncAttrs(live: Element, next: Element): void {
  for (const a of Array.from(live.attributes)) if (!next.hasAttribute(a.name)) live.removeAttribute(a.name);
  for (const a of Array.from(next.attributes)) {
    if (live.getAttribute(a.name) === a.value) continue;
    if (a.namespaceURI) live.setAttributeNS(a.namespaceURI, a.name, a.value);
    else live.setAttribute(a.name, a.value);
  }
}

function insert(parent: Node, node: Node, ref: Node | null): void {
  // moveBefore keeps a connected node's state (focus, iframes, animations)
  // where the browser supports it.
  const mv = (parent as any).moveBefore;
  if (typeof mv === 'function' && node.isConnected && parent.isConnected && node.parentNode) {
    try {
      mv.call(parent, node, ref);
      return;
    } catch {
      /* fall back to a plain insert */
    }
  }
  parent.insertBefore(node, ref);
}

/**
 * Makes the children of parent between start and end (exclusive; the whole
 * child list when both are null) the nodes of want, in order: nodes not in
 * want are removed (and disposed), kept nodes stay where they are, the others
 * are inserted around them.
 */
function place(parent: Node, want: Node[], start: Node | null, end: Node | null): void {
  const keep = new Set(want);
  const dispose = (globalThis as any).disposeNode;
  for (let n = start ? start.nextSibling : parent.firstChild; n && n !== end; ) {
    const next = n.nextSibling;
    // A kept node that moves elsewhere stays until it is moved, so it is
    // never detached on the way (moveBefore keeps its focus).
    if (!keep.has(n) && !(pinned && pinned.has(n))) {
      parent.removeChild(n);
      if (typeof dispose === 'function') dispose(n);
    }
    n = next;
  }
  let cur = start ? start.nextSibling : parent.firstChild;
  for (const n of want) {
    while (cur && cur !== end && cur !== n && pinned && pinned.has(cur) && !keep.has(cur)) cur = cur.nextSibling;
    if (n === cur) cur = cur.nextSibling;
    else insert(parent, n, cur);
  }
}

/** Live nodes a swap keeps (islands and their ancestors), while it runs. */
let pinned: Set<Node> | null = null;

function withPinned(p: Plan, fn: () => void): void {
  const prev = pinned;
  const set = new Set<Node>(prev || []);
  p.pairs.forEach((l) => nodesOf(l).forEach((n) => set.add(n)));
  p.map.forEach((l) => set.add(l));
  pinned = set;
  try {
    fn();
  } finally {
    pinned = prev;
  }
}

/** Islands of a live and a new node list, paired, and the plan to keep the live ones. */
interface Plan {
  /** Each paired new island, with the live island it keeps. */
  pairs: Map<Isl, Isl>;
  /** Paired new islands by their first node. */
  byStart: Map<Node, Isl>;
  /** New elements, with the live element kept in the place of each. */
  map: Map<Node, Element>;
  /** New elements on the path to a paired island. */
  carry: Set<Node>;
  /** Live islands without a counterpart. */
  gone: Isl[];
  live: Isl[];
  next: Isl[];
}

type Allow = (live: Isl, next: Isl) => boolean;

function pairIslands(liveList: ArrayLike<Node>, newList: ArrayLike<Node>, allow: Allow | null): Plan {
  const live = scan(liveList);
  const next = scan(newList);
  const pairs = new Map<Isl, Isl>();
  let from = 0;
  for (const n of next) {
    for (let j = from; j < live.length; j++) {
      if (live[j].key !== n.key) continue;
      from = j + 1;
      if (mounted(live[j].rec) && (!allow || allow(live[j], n))) pairs.set(n, live[j]);
      break;
    }
  }
  const kept = new Set(pairs.values());
  const byStart = new Map<Node, Isl>();
  pairs.forEach((_, n) => byStart.set(n.el || n.start!, n));
  return { pairs, byStart, map: new Map(), carry: new Set(), gone: live.filter((l) => !kept.has(l)), live, next };
}

/** Maps the ancestors of each paired new island onto the live ones (same tags, top down). */
function mapChains(p: Plan): void {
  const used = new Set<Node>(p.map.values());
  p.pairs.forEach((l, n) => {
    let ok = true;
    n.chain.forEach((ne, i) => {
      p.carry.add(ne);
      if (!ok) return;
      const le = l.chain[i];
      const cur = p.map.get(ne);
      if (cur) {
        ok = cur === le;
        return;
      }
      if (le && le.localName === ne.localName && !used.has(le)) {
        p.map.set(ne, le);
        used.add(le);
      } else ok = false;
    });
  });
}

/**
 * The nodes that take the place of the new list: kept live islands for their
 * counterparts, kept live ancestors (whose children are updated) for the new
 * elements mapped onto them, and the new nodes otherwise.
 */
function resolve(p: Plan, list: ArrayLike<Node>): Node[] {
  const out: Node[] = [];
  for (let i = 0; i < list.length; i++) {
    const n = list[i];
    const isl = p.byStart.get(n);
    if (isl) {
      out.push(...nodesOf(p.pairs.get(isl)!));
      if (isl.end) while (i < list.length - 1 && list[i] !== isl.end) i++;
      continue;
    }
    const live = p.map.get(n);
    if (live) {
      syncAttrs(live, n as Element);
      place(live, resolve(p, Array.from(n.childNodes)), null, null);
      out.push(live);
      continue;
    }
    if (p.carry.has(n)) place(n, resolve(p, Array.from(n.childNodes)), null, null);
    out.push(n);
  }
  return out;
}

/** Whether every island on both sides is paired (nothing to mount or unmount). */
function allPaired(p: Plan): boolean {
  return p.gone.length === 0 && p.pairs.size === p.next.length;
}

export interface SwapOptions {
  /** Only islands whose server props are unchanged are kept (a navigation to another page). */
  sameProps?: boolean;
}

/**
 * Replaces the live children of parent between start and end (exclusive;
 * all children when both are null) with newNodes (imported into the
 * document, detached), keeping matched islands mounted. Returns the pending
 * work of island updates (a re-render that loads more chunks first).
 */
export function swapPreservingIslands(
  parent: Node,
  start: Node | null,
  end: Node | null,
  newNodes: Node[],
  opts: SwapOptions = {},
): Promise<unknown>[] {
  const liveNodes: Node[] = [];
  for (let n = start ? start.nextSibling : parent.firstChild; n && n !== end; n = n.nextSibling) liveNodes.push(n);
  const jobs: Promise<unknown>[] = [];
  const p = pairIslands(liveNodes, newNodes, opts.sameProps ? (l, n) => propsKey(l) === propsKey(n) : null);
  if (p.pairs.size > 0 && allPaired(p) && maskIslands(nodesHTML(liveNodes)) === maskIslands(nodesHTML(newNodes))) {
    // Only the islands differ: the rest of the live DOM is left untouched.
    p.pairs.forEach((l, n) => updateIsland(l, n, jobs));
    return jobs;
  }
  for (const g of p.gone) unmountIslandsIn(nodesOf(g));
  mapChains(p);
  withPinned(p, () => place(parent, resolve(p, newNodes), start, end));
  p.pairs.forEach((l, n) => updateIsland(l, n, jobs));
  return jobs;
}

/** The server-children slots directly in a new island's markup, in document order. */
function slotsOf(i: Isl): Array<{ id: string; nodes: Node[] }> {
  const out: Array<{ id: string; nodes: Node[] }> = [];
  let depth = 0;
  let inner: Node[] = [];
  const see = (c: Node) => {
    if (isOpen(c)) return void depth++;
    if (isClose(c)) return void depth--;
    if (depth || c.nodeType !== 8) return;
    const m = SLOT_OPEN.exec((c as Comment).data);
    if (!m) return;
    // A slot inside a real-React island's element belongs to that island.
    const host = c.parentElement && c.parentElement.closest('krate-island');
    if (host && inner.some((x) => x === host || x.contains(host))) return;
    const close = '/krate-slot:' + m[1];
    const nodes: Node[] = [];
    let n = c.nextSibling;
    for (; n && !(n.nodeType === 8 && (n as Comment).data === close); n = n.nextSibling) nodes.push(n);
    if (n) out.push({ id: m[1], nodes });
  };
  inner = rangeNodes(i.start!, i.end!).slice(1, -1);
  for (const n of inner) {
    if (n.nodeType === 8) {
      see(n);
      continue;
    }
    if (n.nodeType !== 1) continue;
    const w = (n.ownerDocument || document).createTreeWalker(n, 128 /* SHOW_COMMENT */);
    for (let c = w.nextNode(); c; c = w.nextNode()) see(c);
  }
  return out;
}

/**
 * Brings a kept server-children slot up to date with the new markup without
 * replacing its top-level nodes (the island renders them): text and comments
 * take the new data, elements keep their identity and get the new attributes
 * and children, nested islands are kept. Returns false (changing nothing)
 * when the top-level shapes differ.
 */
function morphSlot(set: Node[], nodes: Node[], jobs: Promise<unknown>[]): boolean {
  const p = pairIslands(set, nodes, null);
  // Top-level units (a node, or a whole island) must line up one to one.
  type Unit = { node: Node; isl?: Isl };
  const units = (list: Node[], isl: Isl[]): Unit[] => {
    const top = new Map<Node, Isl>();
    for (const x of isl) if (x.chain.length === 0) top.set(x.el || x.start!, x);
    const out: Unit[] = [];
    for (let i = 0; i < list.length; i++) {
      const x = top.get(list[i]);
      out.push({ node: list[i], isl: x });
      if (x && x.end) while (i < list.length - 1 && list[i] !== x.end) i++;
    }
    return out;
  };
  const a = units(set, p.live);
  const b = units(nodes, p.next);
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.isl || y.isl) {
      if (!x.isl || !y.isl || p.pairs.get(y.isl) !== x.isl) return false;
    } else if (x.node.nodeType !== y.node.nodeType || (x.node.nodeType === 1 && (x.node as Element).localName !== (y.node as Element).localName)) {
      return false;
    }
  }
  for (const g of p.gone) unmountIslandsIn(nodesOf(g));
  for (let i = 0; i < a.length; i++) if (!a[i].isl && a[i].node.nodeType === 1) p.map.set(b[i].node, a[i].node as Element);
  mapChains(p);
  withPinned(p, () => {
    for (let i = 0; i < a.length; i++) {
      const x = a[i].node;
      const y = b[i].node;
      if (a[i].isl) continue;
      if (x.nodeType === 1) {
        syncAttrs(x as Element, y as Element);
        place(x, resolve(p, Array.from(y.childNodes)), null, null);
      } else if ((x as CharacterData).data !== (y as CharacterData).data) {
        (x as CharacterData).data = (y as CharacterData).data;
      }
    }
  });
  p.pairs.forEach((l, n) => updateIsland(l, n, jobs));
  return true;
}

function escapeComment(json: string): string {
  return json.replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/--/g, '-\\u002d');
}

/**
 * Brings a kept island up to date with its counterpart in the new markup:
 * its server-children slots follow the new server HTML (in place where they
 * can), and it re-renders with the new props when they changed.
 */
function updateIsland(l: Isl, n: Isl, jobs: Promise<unknown>[]): void {
  const h = l.rec!.handle!;
  const raw = parse(n.json);
  if (raw === undefined) return;
  if (l.el) {
    if (n.json === l.json) return;
    try {
      const r = h.update(raw);
      if (r && typeof (r as Promise<unknown>).then === 'function') jobs.push((r as Promise<unknown>).catch((e) => console.error(e)));
    } catch (err) {
      console.error(err);
    }
    return;
  }
  const rename: Record<string, string> = {};
  const pool: Record<string, Node[][]> = {};
  let retaken = false;
  const olds = h.slots || [];
  const news = slotsOf(n);
  const slots: IslandSlot[] = [];
  news.forEach((s, i) => {
    const o = olds.length === news.length ? olds[i] : undefined;
    const html = nodesHTML(s.nodes);
    if (o) {
      if (maskIslands(html) === maskIslands(o.html)) {
        const q = pairIslands(o.set, s.nodes, null);
        if (allPaired(q)) {
          q.pairs.forEach((ll, nn) => updateIsland(ll, nn, jobs));
          rename[s.id] = o.id;
          slots.push(o);
          return;
        }
      }
      if (morphSlot(o.set, s.nodes, jobs)) {
        rename[s.id] = o.id;
        o.html = html;
        slots.push(o);
        return;
      }
    }
    // The slot is rendered anew from the new nodes: the islands in its old
    // content go with it.
    if (o) unmountIslandsIn(o.set);
    pool[s.id] = [s.nodes];
    retaken = true;
    slots.push({ id: s.id, html, set: s.nodes });
  });
  if (olds.length !== news.length) for (const o of olds) unmountIslandsIn(o.set);
  h.slots = slots;
  const props = eachSlotRef(raw.p || {}, (id) => rename[id] || id);
  const old = parse(l.json);
  const changed = retaken || JSON.stringify(props) !== JSON.stringify(old ? old.p || {} : null);
  raw.p = props;
  // The island keeps the useId() prefix it mounted with.
  if (old && old.i !== undefined) raw.i = old.i;
  l.start!.data = OPEN + escapeComment(JSON.stringify(raw));
  // Nested islands in its slots may have replaced their nodes: the sets
  // follow the document before the island renders again.
  if (typeof h.sync === 'function') {
    try {
      h.sync();
    } catch (err) {
      console.error(err);
    }
  }
  if (!changed) return;
  try {
    const r = retaken ? h.update(props, pool) : h.update(props);
    if (r && typeof (r as Promise<unknown>).then === 'function') jobs.push((r as Promise<unknown>).catch((e) => console.error(e)));
  } catch (err) {
    console.error(err);
  }
}
