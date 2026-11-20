// Fake mounted islands for the island-swap and router tests. They behave as
// the island chunks do (see the compiler's islandcontext.go and the mount of
// clientboundary.go): a Preact island adopts its server children (the slot
// markers go, the nodes stay, its slot set lists them, nested islands'
// nodes included), update() with new server children renders them in place
// of the old ones inside the island's range, sync() re-reads the nested
// islands' runs of its sets from the document. checkIslands() asserts the
// invariants a swap must keep.
import { vi, type Mock } from 'vitest';
import type { IslandHandle, IslandRecord } from './island-swap';

export interface FakeIsland extends IslandRecord {
  name: string;
  start?: Comment;
  end?: Comment;
  el?: Element;
  handle: IslandHandle & {
    update: Mock<IslandHandle['update']>;
    unmount: Mock<() => void>;
    sync: Mock<() => void>;
  };
  /** Whether the island's open comment (or element) was in the document when it unmounted. */
  connectedAtUnmount?: boolean;
}

const OPEN = 'krate-island:';
const isOpen = (n: Node) => n.nodeType === 8 && (n as Comment).data.startsWith(OPEN);
const isClose = (n: Node) => n.nodeType === 8 && (n as Comment).data === '/krate-island';

export function closeOf(start: Node): Node | null {
  let d = 0;
  for (let n = start.nextSibling; n; n = n.nextSibling) {
    if (isOpen(n)) d++;
    else if (isClose(n)) {
      if (!d) return n;
      d--;
    }
  }
  return null;
}

/** The chunk's __krateSlotSync: a set's nested islands' runs follow the document. */
function syncSet(set: Node[]): void {
  for (let i = 0; i < set.length; i++) {
    const s = set[i];
    if (!isOpen(s) || !s.isConnected) continue;
    const end = closeOf(s);
    const j = end ? set.indexOf(end, i + 1) : -1;
    if (j < 0) continue;
    const cur: Node[] = [];
    for (let n = s.nextSibling; n && n !== end; n = n.nextSibling) cur.push(n);
    set.splice(i + 1, j - i - 1, ...cur);
    i += cur.length + 1;
  }
}

function slotIds(v: any, out: string[] = []): string[] {
  if (Array.isArray(v)) v.forEach((x) => slotIds(x, out));
  else if (v && typeof v === 'object') {
    if (typeof v.__krateSlotRef === 'string') out.push(v.__krateSlotRef);
    for (const k in v) if (k !== '__krateSlotRef') slotIds(v[k], out);
  }
  return out;
}

/** Mounts every island marker in root not mounted yet, as the loader and the chunks do. */
export function mountIslands(root: Node, onUnmount?: (r: FakeIsland) => void): FakeIsland[] {
  const out: FakeIsland[] = [];
  const doc = root.ownerDocument || (root as Document);
  const w = doc.createTreeWalker(root, 129);
  const found: Node[] = [];
  for (let n = w.nextNode(); n; n = w.nextNode()) found.push(n);
  for (const n of found) {
    if ((n as any).__krateIsland || !n.isConnected) continue;
    let name = '';
    if (isOpen(n)) name = JSON.parse((n as Comment).data.slice(OPEN.length)).e;
    else if (n.nodeType === 1 && (n as Element).localName === 'krate-island') name = (n as Element).getAttribute('data-export')!;
    else continue;
    // The island's own slots, in document order: [id, set].
    let mine: Array<{ id: string; set: Node[]; html?: string }> = [];
    const rec: FakeIsland = {
      name,
      status: 'hydrated',
      handle: {
        slots: [],
        update: vi.fn<IslandHandle['update']>((props, pool) => {
          if (!pool || !rec.start) return;
          const ids = slotIds(props);
          ids.forEach((id, i) => {
            if (!pool[id] || !mine[i]) return;
            const old = mine[i].set;
            syncSet(old);
            const next = pool[id][0];
            const ref = old.find((x) => x.isConnected) || rec.end!;
            for (const x of next) ref.parentNode!.insertBefore(x, ref);
            for (const x of old) if (x.parentNode) x.parentNode.removeChild(x);
            mine[i] = { id, set: next };
          });
        }),
        unmount: vi.fn(() => {
          rec.connectedAtUnmount = n.isConnected;
          if (onUnmount) onUnmount(rec);
        }),
        sync: vi.fn(() => mine.forEach((s) => syncSet(s.set))),
      },
    };
    if (isOpen(n)) {
      rec.start = n as Comment;
      rec.end = closeOf(n) as Comment;
      mine = adoptSlots(n as Comment);
      rec.handle.slots = mine.map((s) => ({ id: s.id, html: s.html || '', set: s.set }));
    } else rec.el = n as Element;
    (n as any).__krateIsland = rec;
    out.push(rec);
  }
  return out;
}

function adoptSlots(start: Comment): Array<{ id: string; set: Node[]; html: string }> {
  const slots: Array<{ id: string; set: Node[]; html: string }> = [];
  let depth = 0;
  const all: Comment[] = [];
  const end = closeOf(start);
  const see = (c: Comment) => {
    if (isOpen(c)) depth++;
    else if (isClose(c)) depth--;
    else if (!depth) all.push(c);
  };
  for (let n = start.nextSibling; n && n !== end; n = n.nextSibling) {
    if (n.nodeType === 8) see(n as Comment);
    else if (n.nodeType === 1) {
      const w = document.createTreeWalker(n, 128);
      for (let c = w.nextNode(); c; c = w.nextNode()) see(c as Comment);
    }
  }
  for (const c of all) {
    const m = /^krate-slot:(\w+)$/.exec(c.data);
    if (!m) continue;
    const nodes: Node[] = [];
    let n = c.nextSibling;
    for (; n && !(n.nodeType === 8 && (n as Comment).data === '/krate-slot:' + m[1]); n = n.nextSibling) nodes.push(n);
    const html = (nodes.map((x) => (x.nodeType === 1 ? (x as Element).outerHTML : x.nodeType === 8 ? '<!--' + (x as Comment).data + '-->' : x.textContent)).join(''));
    c.parentNode!.removeChild(n!);
    c.parentNode!.removeChild(c);
    slots.push({ id: m[1], set: nodes, html });
  }
  return slots;
}

/**
 * The invariants after a swap: every mounted island's markers are in the
 * document and balanced, its slot sets (synced) hold only nodes inside its
 * range, no server-children marker is left in the document, and an unmounted island's
 * markup is gone.
 */
export function checkIslands(recs: FakeIsland[]): string[] {
  const problems: string[] = [];
  for (const r of recs) {
    if (r.gone) {
      if (r.start ? r.start.isConnected : r.el!.isConnected) problems.push(`unmounted ${r.name} still in the document`);
      continue;
    }
    if (r.el) {
      if (!r.el.isConnected) problems.push(`mounted ${r.name} detached`);
      continue;
    }
    if (!r.start!.isConnected || !r.end!.isConnected) {
      problems.push(`mounted ${r.name} detached`);
      continue;
    }
    if (closeOf(r.start!) !== r.end) problems.push(`unbalanced range of ${r.name}`);
    // As the island's next render does (an island mounted since may have
    // taken its own slot markers out).
    r.handle.sync.getMockImplementation()!();
    for (const s of r.handle.slots || []) {
      for (const n of s.set) {
        const inside = n.isConnected && r.start!.compareDocumentPosition(n) & 4 && r.end!.compareDocumentPosition(n) & 2;
        if (!inside) problems.push(`slot node of ${r.name} outside its range`);
      }
    }
  }
  const w = document.createTreeWalker(document.body, 128);
  for (let c = w.nextNode(); c; c = w.nextNode()) if (/^\/?krate-slot:/.test((c as Comment).data)) problems.push(`slot marker ${(c as Comment).data} in the document`);
  return problems;
}

/** Island markup as the server renders it. */
export function island(e: string, props: unknown, html: string, m = 'cmod'): string {
  const json = JSON.stringify({ m, e, s: '/chunks/' + e + '.js', p: props }).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/--/g, '-\\u002d');
  return `<!--krate-island:${json}-->${html}<!--/krate-island-->`;
}

export function slot(id: string, html: string): string {
  return `<!--krate-slot:${id}-->${html}<!--/krate-slot:${id}-->`;
}

/** A wrapper island with one server-children slot. */
export function wrapper(e: string, props: Record<string, unknown>, id: string, children: string, own = ''): string {
  return island(e, { ...props, __krateChildren: { __krateSlotRef: id } }, own + slot(id, children));
}
