// Component inspector data: the dev build's component table, the nodes each
// instance rendered, their rects, and the live signal and hydration globals.

export type Tier = 'static' | 'server' | 'client' | 'runtime' | 'island';

export const TIERS: Tier[] = ['static', 'server', 'client', 'runtime', 'island'];

export interface CompRow {
  n: number;
  key?: string;
  name: string;
  tier: Tier;
  file?: string;
  line?: number;
  parent: number;
  island?: string;
  region?: string;
  boundary?: number;
}

export const TABLE_ID = '__krate_dev_components';

/** The component table of the page, or null when the build wrote none. */
export function readTable(doc: Document = document): CompRow[] | null {
  const el = doc.getElementById(TABLE_ID);
  if (!el) return null;
  try {
    const rows = JSON.parse(el.textContent || '[]');
    if (!Array.isArray(rows)) return null;
    return rows.filter((r) => r && typeof r.name === 'string' && typeof r.n === 'number') as CompRow[];
  } catch {
    return null;
  }
}

export function tierCounts(rows: CompRow[]): Record<Tier, number> {
  const c: Record<Tier, number> = { static: 0, server: 0, client: 0, runtime: 0, island: 0 };
  for (const r of rows) if (r.tier in c) c[r.tier]++;
  return c;
}

/** The chain of instance names from the outermost to row, for the component path. */
export function componentPath(rows: CompRow[], row: CompRow): string[] {
  const out: string[] = [];
  const seen = new Set<number>();
  let cur: CompRow | undefined = row;
  while (cur && !seen.has(cur.n)) {
    seen.add(cur.n);
    out.unshift(cur.name);
    cur = cur.parent >= 0 ? rows[cur.parent] : undefined;
  }
  return out;
}

export interface SignalValue {
  name: string;
  value: unknown;
}

/** Current values of the signals a client instance registered. */
export function signalsOf(key: string | undefined, g: any = globalThis): SignalValue[] {
  if (!key) return [];
  const list = g.__krate_dev_signals;
  if (!Array.isArray(list)) return [];
  const out: SignalValue[] = [];
  for (const s of list) {
    if (!s || s.c !== key) continue;
    let value: unknown;
    try {
      value = typeof s.get === 'function' ? s.get() : undefined;
    } catch {
      value = undefined;
    }
    out.push({ name: String(s.name || '?'), value });
  }
  return out;
}

/** Hydration time of an instance in ms, when the page recorded it. */
export function hydrationOf(row: CompRow, g: any = globalThis): number | undefined {
  if (row.key && g.__krate_dev_hydration && typeof g.__krate_dev_hydration[row.key] === 'number') {
    return g.__krate_dev_hydration[row.key];
  }
  const isl = row.island && g.__krate_island_hydration ? g.__krate_island_hydration[row.island] : undefined;
  if (isl && typeof isl.ms === 'number') return isl.ms;
  return undefined;
}

/** A signal value as short text for the tooltip. */
export function formatValue(v: unknown): string {
  if (typeof v === 'string') return JSON.stringify(v.length > 40 ? v.slice(0, 40) + '...' : v);
  if (typeof v === 'function') return 'fn';
  if (v === undefined) return 'undefined';
  try {
    const s = JSON.stringify(v);
    if (s === undefined) return String(v);
    return s.length > 60 ? s.slice(0, 60) + '...' : s;
  } catch {
    return String(v);
  }
}

/** Instance numbers of an element's data-kd attribute, outermost first. */
export function kdNumbers(el: Element): number[] {
  const v = el.getAttribute('data-kd');
  if (!v) return [];
  return v
    .split(/\s+/)
    .map(Number)
    .filter((n) => Number.isInteger(n) && n >= 0);
}

/** Maps instance numbers to the elements they mark. */
export function indexElements(doc: Document = document): Map<number, Element[]> {
  const map = new Map<number, Element[]>();
  doc.querySelectorAll('[data-kd]').forEach((el) => {
    for (const n of kdNumbers(el)) {
      const list = map.get(n);
      if (list) list.push(el);
      else map.set(n, [el]);
    }
  });
  return map;
}

function comments(root: Node): Comment[] {
  const out: Comment[] = [];
  const doc = root.ownerDocument || (root as Document);
  const w = doc.createTreeWalker(root, 128 /* NodeFilter.SHOW_COMMENT */);
  let n = w.nextNode();
  while (n) {
    out.push(n as Comment);
    n = w.nextNode();
  }
  return out;
}

/** The sibling comment that closes `open`, skipping nested pairs. */
function closer(open: Comment, isOpen: (d: string) => boolean, isClose: (d: string) => boolean): Comment | null {
  let depth = 0;
  for (let n = open.nextSibling; n; n = n.nextSibling) {
    if (n.nodeType !== 8) continue;
    const d = (n as Comment).data;
    if (isOpen(d)) depth++;
    else if (isClose(d)) {
      if (depth === 0) return n as Comment;
      depth--;
    }
  }
  return null;
}

export interface CommentRange {
  start: Comment;
  end: Comment;
  /** The JSON a krate-island comment carries, when it parses. */
  data?: any;
}

/** The comment-delimited ranges of rows without an element of their own. */
export function commentRanges(rows: CompRow[], doc: Document = document): Map<number, CommentRange> {
  const out = new Map<number, CommentRange>();
  const wantBoundary = rows.some((r) => typeof r.boundary === 'number');
  const wantRegion = rows.some((r) => r.region);
  if (!wantBoundary && !wantRegion) return out;
  const all = doc.body ? comments(doc.body) : [];
  const islands = all.filter((c) => c.data.indexOf('krate-island:') === 0);
  for (const r of rows) {
    if (typeof r.boundary === 'number') {
      const open = islands[r.boundary];
      if (!open) continue;
      const end = closer(
        open,
        (d) => d.indexOf('krate-island:') === 0,
        (d) => d === '/krate-island',
      );
      if (!end) continue;
      let data: any;
      try {
        data = JSON.parse(open.data.slice('krate-island:'.length));
      } catch {
        data = undefined;
      }
      out.set(r.n, { start: open, end, data });
    } else if (r.region) {
      for (const prefix of ['region:', 'suspense:']) {
        const open = all.find((c) => c.data === prefix + r.region);
        if (!open) continue;
        const end = closer(
          open,
          () => false,
          (d) => d === '/' + prefix + r.region,
        );
        if (end) {
          out.set(r.n, { start: open, end });
          break;
        }
      }
    }
  }
  return out;
}

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

function union(rects: Box[]): Box | null {
  const live = rects.filter((r) => r.width > 0 || r.height > 0);
  if (!live.length) return null;
  let l = Infinity;
  let t = Infinity;
  let r = -Infinity;
  let b = -Infinity;
  for (const x of live) {
    l = Math.min(l, x.left);
    t = Math.min(t, x.top);
    r = Math.max(r, x.left + x.width);
    b = Math.max(b, x.top + x.height);
  }
  return { left: l, top: t, width: r - l, height: b - t };
}

/** The box of an element; a display:contents wrapper takes its children's. */
export function elementBox(el: Element): Box | null {
  const r = el.getBoundingClientRect();
  if (r.width > 0 || r.height > 0) return { left: r.left, top: r.top, width: r.width, height: r.height };
  let display = '';
  try {
    display = getComputedStyle(el).display;
  } catch {
    display = '';
  }
  if (display !== 'contents' && el.children.length === 0) return null;
  const kids: Box[] = [];
  for (const c of Array.from(el.children)) {
    const b = elementBox(c);
    if (b) kids.push(b);
  }
  return union(kids);
}

/** The box of the nodes between two comments. */
export function rangeBox(range: CommentRange): Box | null {
  try {
    const doc = range.start.ownerDocument;
    const rg = doc.createRange();
    rg.setStartAfter(range.start);
    rg.setEndBefore(range.end);
    const r = rg.getBoundingClientRect();
    if (r && (r.width > 0 || r.height > 0)) return { left: r.left, top: r.top, width: r.width, height: r.height };
  } catch {
    /* fall back to the elements in between */
  }
  const kids: Box[] = [];
  for (let n = range.start.nextSibling; n && n !== range.end; n = n.nextSibling) {
    if (n.nodeType === 1) {
      const b = elementBox(n as Element);
      if (b) kids.push(b);
    }
  }
  return union(kids);
}

export function boxContains(b: Box, x: number, y: number): boolean {
  return x >= b.left && x <= b.left + b.width && y >= b.top && y <= b.top + b.height;
}
