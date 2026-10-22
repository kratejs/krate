/**
 * Keyed list reconciliation for compiled `.map()` lists.
 *
 * Each list item is a "row": a pair of comment markers around the nodes the
 * map callback rendered, plus an ownership root that holds the row's effects
 * and cleanups. When the source array changes, rows are matched by key (the
 * `key` prop of the element the callback returns, compared as a string like
 * React; items without a key use their index). Matching rows keep their DOM
 * nodes, component state, effects and refs; only rows whose key disappeared
 * are disposed, only new keys render, and rows are moved with the minimum
 * number of DOM moves (rows on the longest increasing subsequence of old
 * positions stay put).
 *
 * In accessor mode the callback receives `item()` / `index()` getters backed
 * by per-row signals, so a new item object for an existing key updates the
 * row in place and the index updates after a move. In value mode (callbacks
 * the compiler cannot rewrite, such as `.map(renderRow)` or destructured
 * parameters) the callback receives plain values and a row whose item changed
 * is re-rendered.
 *
 * On hydration the server-rendered rows (`<!--k:ID:KEY-->...<!--/k:ID:KEY-->`)
 * are adopted by key: the callback runs in virtual-node mode and its output is
 * bound to the existing DOM instead of replacing it.
 */
import { createEffect, createRoot, createSignal, onCleanup, untrack } from './signal.js';
import { krateUnmount, krateWithOwner } from './effects.js';
import {
  adoptValue,
  appendChild,
  disposeNode,
  insertAt,
  removeRest,
  trackEffect,
  withDOM,
  withVNodes,
  type AdoptCursor,
  type KrateAdoptable,
} from './dom.js';

type KeyFn = ((item: unknown, index: number) => unknown) | null;
type RenderFn = (item: unknown, index: unknown) => unknown;

interface Row {
  key: string;
  item: unknown;
  index: number;
  start: Comment;
  end: Comment;
  setItem: ((v: () => unknown) => void) | null;
  setIndex: ((v: number) => void) | null;
  /** False for a server-rendered row that has not been adopted yet. */
  live: boolean;
}

interface ListState {
  start: Comment;
  end: Comment;
  rows: Row[];
  keyFn: KeyFn;
  render: RenderFn;
  accessor: boolean;
  usesIndex: boolean;
}

/** Canonical key: explicit keys are `$` + String(key); implicit keys are the index. */
function keyOf(keyFn: KeyFn, item: unknown, i: number): string {
  if (!keyFn) return String(i);
  const k = keyFn(item, i);
  return k == null ? String(i) : '$' + String(k);
}

function decodeKey(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

function warnDuplicates(dups: string[]): void {
  const shown = dups.map((k) => (k.charAt(0) === '$' ? k.slice(1) : k));
  console.error(
    'Encountered two children with the same key, `' +
      shown.join('`, `') +
      '`. Keys should be unique so that components maintain their identity across updates. ' +
      'Krate keeps the first occurrence on its existing row and renders each duplicate as its own row.',
  );
}

function newRowMarkers(): [Comment, Comment] {
  return [document.createComment(''), document.createComment('')];
}

// Run the row's render inside its own ownership root and register the root's
// disposer on the start marker, so disposeNode(start) unmounts the row.
function renderInRoot(state: ListState, row: Row, item: unknown, i: number, body: (v: unknown) => void): void {
  createRoot((dispose) => {
    trackEffect(row.start, dispose);
    // The row is an effect owner: components it renders are its children, so
    // removing the row unmounts them (React effect cleanups) in one call.
    const { value, owner } = krateWithOwner(() => {
      if (!state.accessor) return state.render(item, i);
      const [getItem, setItem] = createSignal<unknown>(item);
      const [getIndex, setIndex] = createSignal<number>(i);
      row.setItem = setItem as (v: () => unknown) => void;
      row.setIndex = setIndex;
      return state.render(getItem, getIndex);
    });
    onCleanup(() => krateUnmount(owner));
    body(value);
  });
}

function createRow(state: ListState, key: string, item: unknown, i: number): DocumentFragment {
  const [start, end] = newRowMarkers();
  const row: Row = { key, item, index: i, start, end, setItem: null, setIndex: null, live: true };
  const frag = document.createDocumentFragment();
  frag.appendChild(start);
  withDOM(() => renderInRoot(state, row, item, i, (v) => appendChild(frag, v)));
  frag.appendChild(end);
  (frag as unknown as { __row: Row }).__row = row;
  return frag;
}

// Adopt a server-rendered row: render in virtual-node mode and bind the result
// to the nodes between its markers. If adoption throws (a component that
// manipulates its own output imperatively, for example), the row is rendered
// fresh in place.
function adoptRow(state: ListState, row: Row, item: unknown, i: number): void {
  row.item = item;
  row.index = i;
  row.live = true;
  const parent = row.start.parentNode as Node;
  try {
    withVNodes(() =>
      renderInRoot(state, row, item, i, (v) => {
        const c: AdoptCursor = { parent, next: row.start.nextSibling };
        withDOM(() => adoptValue(v, c));
        removeRest(c, row.end);
      }),
    );
  } catch (err) {
    console.error(err);
    disposeRange(row.start, row.end, false);
    disposeNode(row.start);
    const frag = createRow(state, row.key, item, i);
    const fresh = (frag as unknown as { __row: Row }).__row;
    parent.insertBefore(frag, row.start);
    parent.removeChild(row.start);
    parent.removeChild(row.end);
    Object.assign(row, fresh);
  }
}

/** Dispose and remove the nodes between start and end (and the markers when `markers`). */
function disposeRange(start: Node, end: Node, markers: boolean): void {
  const parent = start.parentNode;
  if (!parent) return;
  let n = start.nextSibling;
  while (n && n !== end) {
    const next = n.nextSibling;
    disposeNode(n);
    parent.removeChild(n);
    n = next;
  }
  if (markers) {
    disposeNode(start);
    parent.removeChild(start);
    if (end.parentNode === parent) parent.removeChild(end);
  }
}

function removeRow(row: Row): void {
  disposeRange(row.start, row.end, true);
}

/** Move a row's nodes (markers included) before `ref`. */
function moveRow(row: Row, ref: Node): void {
  const parent = ref.parentNode as Node;
  if (row.end.nextSibling === ref) return;
  let n: Node | null = row.start;
  while (n) {
    const next: Node | null = n === row.end ? null : n.nextSibling;
    parent.insertBefore(n, ref);
    n = next;
  }
}

/**
 * Indices (into `seq`) of a longest strictly increasing subsequence of the
 * non-negative entries. O(n log n).
 */
export function longestIncreasingSubsequence(seq: ArrayLike<number>): number[] {
  const n = seq.length;
  const prev = new Int32Array(n);
  const tails: number[] = [];
  for (let i = 0; i < n; i++) {
    const v = seq[i];
    if (v < 0) continue;
    let lo = 0;
    let hi = tails.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (seq[tails[mid]] < v) lo = mid + 1;
      else hi = mid;
    }
    prev[i] = lo > 0 ? tails[lo - 1] : -1;
    tails[lo] = i;
  }
  const out = new Array<number>(tails.length);
  let k = tails.length ? tails[tails.length - 1] : -1;
  for (let j = tails.length - 1; j >= 0; j--) {
    out[j] = k;
    k = prev[k];
  }
  return out;
}

function toArray(raw: unknown): unknown[] {
  if (raw == null) return [];
  if (Array.isArray(raw)) return raw;
  if (typeof (raw as { [Symbol.iterator]?: unknown })[Symbol.iterator] === 'function') {
    return Array.from(raw as Iterable<unknown>);
  }
  return [];
}

// Focus and selection survive row moves: a browser blurs an element that is
// detached even momentarily, so the active element is restored after the
// update when it is still in the document (React does the same on commit).
function captureFocus(scope: Node | null): (() => void) | null {
  if (typeof document === 'undefined') return null;
  const active = document.activeElement as HTMLElement | null;
  if (!active || active === document.body || !scope || !scope.contains(active)) return null;
  let sel: [number | null, number | null, string | null] | null = null;
  try {
    const input = active as HTMLInputElement;
    if (typeof input.selectionStart === 'number') {
      sel = [input.selectionStart, input.selectionEnd, input.selectionDirection];
    }
  } catch {
    sel = null;
  }
  return () => {
    if (document.activeElement === active || !active.isConnected) return;
    active.focus({ preventScroll: true });
    if (sel) {
      try {
        (active as HTMLInputElement).setSelectionRange(sel[0], sel[1], (sel[2] || undefined) as 'forward' | 'backward' | 'none' | undefined);
      } catch {
        // Not every input type supports selection ranges.
      }
    }
  };
}

function update(state: ListState, items: unknown[]): void {
  const { end, keyFn } = state;
  const parent = end.parentNode as Node;
  const oldRows = state.rows;
  const n = items.length;

  // Keys, with duplicates made unique: the first occurrence keeps the key.
  const keys = new Array<string>(n);
  const seen = new Map<string, number>();
  let dups: string[] | null = null;
  for (let i = 0; i < n; i++) {
    let k = keyOf(keyFn, items[i], i);
    const count = seen.get(k);
    if (count !== undefined) {
      (dups || (dups = [])).push(k);
      seen.set(k, count + 1);
      k = k + '\u0000' + count;
    } else {
      seen.set(k, 1);
    }
    keys[i] = k;
  }
  if (dups) warnDuplicates(dups);

  const oldByKey = new Map<string, number>();
  for (let i = 0; i < oldRows.length; i++) oldByKey.set(oldRows[i].key, i);

  // Server-rendered rows whose key no new item wants are adopted by position,
  // so a key the build could not compute still hydrates without re-rendering.
  for (let i = 0; i < n && i < oldRows.length; i++) {
    const row = oldRows[i];
    if (!row.live && !seen.has(row.key) && !oldByKey.has(keys[i])) {
      oldByKey.delete(row.key);
      row.key = keys[i];
      oldByKey.set(row.key, i);
    }
  }

  const restoreFocus = captureFocus(parent);
  const next = new Array<Row | null>(n);
  const sources = new Int32Array(n).fill(-1);
  const kept = new Uint8Array(oldRows.length);
  for (let i = 0; i < n; i++) {
    const oi = oldByKey.get(keys[i]);
    if (oi === undefined) continue;
    const row = oldRows[oi];
    const item = items[i];
    if (row.live && !state.accessor && (row.item !== item || (state.usesIndex && row.index !== i))) {
      // Value mode cannot update a row in place: re-render it.
      continue;
    }
    kept[oi] = 1;
    sources[i] = oi;
    next[i] = row;
  }

  // Unmount removed rows first, so their cleanups run before new rows mount.
  for (let i = 0; i < oldRows.length; i++) {
    if (!kept[i]) removeRow(oldRows[i]);
  }

  // Update or adopt the rows that stay.
  for (let i = 0; i < n; i++) {
    const row = next[i];
    if (!row) continue;
    const item = items[i];
    if (!row.live) {
      adoptRow(state, row, item, i);
      continue;
    }
    if (row.item !== item) {
      row.item = item;
      if (row.setItem) row.setItem(() => item);
    }
    if (row.index !== i) {
      row.index = i;
      if (row.setIndex) row.setIndex(i);
    }
  }

  // Render new rows in order (so mount effects run in order). Consecutive new
  // rows share one fragment and are inserted with a single DOM operation.
  const runs = new Array<DocumentFragment | null>(n);
  let run: DocumentFragment | null = null;
  for (let i = 0; i < n; i++) {
    if (next[i]) {
      run = null;
      continue;
    }
    const frag = createRow(state, keys[i], items[i], i);
    next[i] = (frag as unknown as { __row: Row }).__row;
    if (run) {
      run.appendChild(frag);
      runs[i - 1] = null;
    } else {
      run = frag;
    }
    runs[i] = run;
  }

  // Place rows from the end, keeping the longest run of rows already in order.
  const stay = new Uint8Array(n);
  for (const i of longestIncreasingSubsequence(sources)) stay[i] = 1;
  let anchor: Node = end;
  for (let i = n - 1; i >= 0; i--) {
    const row = next[i] as Row;
    const pending = runs[i];
    if (pending) {
      parent.insertBefore(pending, anchor);
    } else if (sources[i] >= 0 && !stay[i]) {
      moveRow(row, anchor);
    }
    anchor = row.start;
  }
  state.rows = next as Row[];
  if (restoreFocus) restoreFocus();
}

// Collect the server-rendered rows between the list markers. Anything else
// between the markers (stray text, unexpected nodes) is removed.
function collectServerRows(state: ListState): void {
  const { start, end } = state;
  const parent = start.parentNode as Node;
  const id = (start.nodeValue || '').slice(2);
  const prefix = 'k:' + id + ':';
  const rows: Row[] = [];
  let n: Node | null = start.nextSibling;
  while (n && n !== end) {
    const v = n.nodeType === 8 ? n.nodeValue || '' : '';
    if (id && v.indexOf(prefix) === 0) {
      const closeValue = '/' + v;
      let m: Node | null = n.nextSibling;
      while (m && m !== end && !(m.nodeType === 8 && m.nodeValue === closeValue)) m = m.nextSibling;
      if (m && m !== end) {
        rows.push({
          key: decodeKey(v.slice(prefix.length)),
          item: undefined,
          index: rows.length,
          start: n as Comment,
          end: m as Comment,
          setItem: null,
          setIndex: null,
          live: false,
        });
        n = m.nextSibling;
        continue;
      }
    }
    const nextNode: Node | null = n.nextSibling;
    disposeNode(n);
    parent.removeChild(n);
    n = nextNode;
  }
  // Duplicate server keys: keep the first, drop the rest.
  const seen = new Set<string>();
  state.rows = rows.filter((r) => {
    if (seen.has(r.key)) {
      removeRow(r);
      return false;
    }
    seen.add(r.key);
    return true;
  });
}

function run(state: ListState, getItems: () => unknown, adopt: boolean): void {
  let first = true;
  const dispose = createEffect(() => {
    const items = toArray(getItems());
    untrack(() => {
      if (first) {
        first = false;
        if (adopt) collectServerRows(state);
      }
      update(state, items);
    });
  });
  trackEffect(state.start, dispose);
}

function newState(start: Comment, end: Comment, keyFn: KeyFn, render: RenderFn, mode: number): ListState {
  return {
    start,
    end,
    rows: [],
    keyFn,
    render,
    accessor: mode === 1,
    usesIndex: render.length > 1,
  };
}

/**
 * Bind a server-rendered list: `start` is its `<!--k:ID-->` marker. Rows are
 * adopted by key and then reconciled whenever `getItems` changes.
 */
export function krateBindList(start: Comment, getItems: () => unknown, keyFn: KeyFn, render: RenderFn, mode: number): void {
  const closeValue = '/' + (start.nodeValue || '');
  let end: Node | null = start.nextSibling;
  while (end && !(end.nodeType === 8 && end.nodeValue === closeValue)) end = end.nextSibling;
  if (!end) {
    end = document.createComment(closeValue);
    start.parentNode!.insertBefore(end, start.nextSibling);
  }
  run(newState(start, end as Comment, keyFn, render, mode), getItems, true);
}

/**
 * A list nested in rendered JSX (a `.map()` inside a row or component). It
 * mounts as a fragment with its own markers, or adopts a server-rendered list
 * at the hydration cursor.
 */
export function krateList(getItems: () => unknown, keyFn: KeyFn, render: RenderFn, mode: number): KrateAdoptable {
  return {
    __ka: 1,
    mount(): Node {
      const frag = document.createDocumentFragment();
      const start = document.createComment('');
      const end = document.createComment('');
      frag.appendChild(start);
      frag.appendChild(end);
      run(newState(start, end, keyFn, render, mode), getItems, false);
      return frag;
    },
    adopt(c: AdoptCursor): void {
      // A server-rendered list is a `<!--k:ID-->` ... `<!--/k:ID-->` pair at
      // the cursor (closing markers of preceding slots are skipped).
      let n = c.next;
      while (n && n.nodeType === 8 && (n.nodeValue || '').indexOf('/k:') === 0) n = n.nextSibling;
      if (n && n.nodeType === 8 && (n.nodeValue || '').indexOf('k:') === 0) {
        const closeValue = '/' + n.nodeValue;
        let m: Node | null = n.nextSibling;
        while (m && !(m.nodeType === 8 && m.nodeValue === closeValue)) m = m.nextSibling;
        if (m) {
          c.next = m.nextSibling;
          withDOM(() => run(newState(n as Comment, m as Comment, keyFn, render, mode), getItems, true));
          return;
        }
      }
      insertAt(c, withDOM(() => this.mount()));
    },
  };
}
