import { createEffect, onCleanup } from './signal.js';
import { krateWithOwner, krateUnmount } from './effects.js';
import { krateSanitizeAttr } from './slots.js';
import { krateOn, krateControl, krateSetControl } from './events.js';
import { krateAttachRef, type KrateRef } from './refs.js';

export {
  krateOn,
  krateControl,
  krateSetControl,
  krateSynthetic,
  krateEventRoot,
  krateEventName,
  krateMountFields,
} from './events.js';
export type { KrateSyntheticEvent } from './events.js';
export { krateAttachRef, krateSetRef, krateRefsEnter, krateRefsLeave, krateRefsFlush, krateRefsHold } from './refs.js';
export type { KrateRef } from './refs.js';

export type Component<P = {}> = (props: P) => Node | null | undefined;

export type PropsWithChildren<P = {}> = P & { children?: unknown[] };
export type RefCallback = (el: Element) => void;

/** Extracts the Props type from a Component. */
export type ComponentProps<C> = C extends Component<infer P> ? P : never;

// per-node effect tracking
// Reactive props/children create effects that must be disposed when the node is
// removed from the DOM (list items, conditional branches, SPA reconciliation).
// Without this, effects and their closures accumulate for the page's lifetime.

const effectsByNode = new WeakMap<Node, Array<() => void>>();

/** Register a disposer that runs when `node` (or an ancestor) is disposed. */
export function trackEffect(node: Node, dispose: () => void): void {
  krateTrackNode(node, dispose);
}

/** Registers `dispose` to run when `node` is disposed (removed by a binding). */
export function krateTrackNode(node: Node, dispose: () => void): void {
  let arr = effectsByNode.get(node);
  if (!arr) {
    arr = [];
    effectsByNode.set(node, arr);
  }
  arr.push(dispose);
}

function trackOwner(value: unknown, owner: unknown): void {
  const unmount = (): void => krateUnmount(owner);
  if (typeof DocumentFragment !== 'undefined' && value instanceof DocumentFragment) {
    // Fragment children are moved out on insertion; track each of them.
    for (let i = 0; i < value.childNodes.length; i++) trackEffect(value.childNodes[i], unmount);
  } else if (typeof Node !== 'undefined' && value instanceof Node) {
    trackEffect(value, unmount);
  } else if (Array.isArray(value)) {
    for (const v of value) trackOwner(v, owner);
  }
}

/**
 * Binds the ref returned by `get` to `el` (hydration `ref={...}`). The ref is
 * re-bound (old detached, new attached) when the value read by `get` changes
 * identity, and released when the owning scope or the node is disposed.
 * `assign` handles plain-variable targets.
 */
export function krateBindRef(el: Element, get: (() => KrateRef) | null, assign?: (v: unknown) => void): () => void {
  let first = true;
  const dispose = createEffect(() => {
    // The first binding happens during hydration and is queued until every
    // component has bound its DOM; a later re-bind attaches at once.
    const release = krateAttachRef(el, get ? get() : undefined, assign, first ? 'defer' : undefined);
    first = false;
    onCleanup(release);
  });
  trackEffect(el, dispose);
  return dispose;
}

/** Dispose all effects registered for a node and its subtree. */
export function disposeNode(node: Node): void {
  const arr = effectsByNode.get(node);
  if (arr) {
    for (const dispose of arr) dispose();
    arr.length = 0;
  }
  for (let i = 0; i < node.childNodes.length; i++) {
    disposeNode(node.childNodes[i]);
  }
}

// comment-marker DOM traversal

/**
 * Find the matching end comment marker for a start marker.
 * Searches forward among SIBLINGS ONLY (same parent/depth) for the next
 * <!--k--> comment. Nested markers inside child elements are skipped because
 * they are at a deeper level, not siblings.
 */
function findEndMarker(startMarker: Node): Node | null {
  let node = startMarker.nextSibling;
  while (node) {
    if (node.nodeType === 8 && node.nodeValue === 'k') return node;
    node = node.nextSibling;
  }
  return null;
}

/**
 * Clear all nodes between two markers (exclusive of the markers themselves).
 * @param start - start comment marker
 * @param end - end comment marker (must be a sibling of start)
 */
export function clearNodes(start: Node, end: Node): void {
  const parent = start.parentNode;
  if (!parent) return;
  let node = start.nextSibling;
  while (node && node !== end) {
    const next = node.nextSibling;
    disposeNode(node);
    parent.removeChild(node);
    node = next;
  }
}

/**
 * Insert content into a dynamic region delimited by two <!--k--> comment markers.
 * The start marker is located by the caller (comment-based slot lookup); the
 * end marker is found by searching forward among siblings. Content between the
 * markers is replaced.
 *
 * @param parent - parent node of the dynamic region
 * @param value - new content (string, number, Node, or array)
 * @param startMarker - the opening <!--k--> comment node
 */
export function insert(parent: Node, value: unknown, startMarker: Node): void {
  const endMarker = findEndMarker(startMarker);
  if (endMarker) {
    clearNodes(startMarker, endMarker);
    insertBefore(parent, value, endMarker);
  } else {
    // No end marker found - append at end
    insertBefore(parent, value, null);
  }
}

/** Insert a value (or array of values) before a reference node. */
function insertBefore(parent: Node, value: unknown, before: Node | null): void {
  if (value == null || value === false || value === true) return;
  if (typeof value === 'string' || typeof value === 'number') {
    parent.insertBefore(document.createTextNode(String(value)), before);
  } else if (value instanceof Node) {
    parent.insertBefore(value, before);
  } else if (Array.isArray(value)) {
    for (const item of value) insertBefore(parent, item, before);
  }
}

// Prop names whose DOM attribute differs from the JSX spelling.
const ATTR_ALIASES: Record<string, string> = { htmlFor: 'for' };

function setAttr(el: Element, key: string, value: unknown): void {
  if (key === 'class' || key === 'className') {
    // Guard against false/null/undefined before String() conversion.
    const next = value === false || value === null || value === undefined ? '' : String(value);
    if (el.getAttribute('class') !== next) {
      if (next === '' && !el.hasAttribute('class')) return;
      el.setAttribute('class', next);
    }
  } else if (key === 'style') {
    if (typeof value === 'string') {
      if (el.getAttribute('style') !== value) el.setAttribute('style', value);
    } else if (value && typeof value === 'object') {
      Object.assign((el as HTMLElement).style, value);
    } else if (value == null || value === false) {
      el.removeAttribute('style');
    }
  } else if (key.startsWith('on') && typeof value === 'function') {
    krateOn(el, key, value as Parameters<typeof krateOn>[2]);
  } else if (key === 'value' || key === 'checked' || key === 'selected' || key === 'defaultValue' || key === 'defaultChecked') {
    // Form-control state is a DOM property, not an attribute: setting the
    // attribute after user interaction has no effect.
    krateSetControl(el, key, value);
  } else if (key === 'ref') {
    // Attached now (the node is about to be inserted), detached when the node
    // is disposed or the rendering effect re-runs. Moving a list row does not
    // dispose it, so its refs stay attached.
    const release = krateAttachRef(el, value as KrateRef, undefined, 'now');
    trackEffect(el, release);
    onCleanup(release);
  } else if (key === 'key' || key === 'children' || key === 'dangerouslySetInnerHTML') {
    // key is reconciliation metadata; children are appended separately.
  } else {
    const name = ATTR_ALIASES[key] || key;
    if (value === null || value === undefined || value === false) {
      if (el.hasAttribute(name)) el.removeAttribute(name);
    } else {
      const next = value === true ? '' : krateSanitizeAttr(name, String(value));
      if (el.getAttribute(name) !== next) el.setAttribute(name, next);
    }
  }
}

function applyProps(el: Element, props: Record<string, unknown>): void {
  for (const key in props) {
    const value = props[key];
    if (typeof value === 'function' && (key === 'value' || key === 'checked')) {
      trackEffect(el, krateControl(el, key, value as () => unknown));
    } else if (typeof value === 'function' && !key.startsWith('on') && key !== 'ref') {
      trackEffect(el, createEffect(() => setAttr(el, key, (value as () => unknown)())));
    } else {
      setAttr(el, key, value);
    }
  }
  const html = props.dangerouslySetInnerHTML as { __html?: unknown } | undefined;
  if (html && typeof html === 'object' && html.__html != null) {
    const next = String(html.__html);
    if (el.innerHTML !== next) el.innerHTML = next;
  }
}

// Virtual nodes
// While hydration adopts server-rendered list rows, h() returns lightweight
// descriptors instead of DOM nodes (JS evaluates children before parents, so
// the tree must exist before it can be matched against the DOM top-down).

/** A deferred element produced by h() while adopting server-rendered DOM. */
export interface KrateVNode {
  __kv: 1;
  tag: string;
  props: Record<string, unknown> | null;
  children: unknown[];
}

/** A value that knows how to mount itself or adopt server-rendered DOM. */
export interface KrateAdoptable {
  __ka: 1;
  mount(): Node;
  adopt(cursor: AdoptCursor): void;
}

/** Position inside a parent while adopting: `next` is the next unclaimed node. */
export interface AdoptCursor {
  parent: Node;
  next: Node | null;
}

let vnodeDepth = 0;

/** Evaluate `fn` with h() producing virtual nodes (see KrateVNode). */
export function withVNodes<T>(fn: () => T): T {
  const saved = vnodeDepth;
  vnodeDepth = saved + 1;
  try {
    return fn();
  } finally {
    vnodeDepth = saved;
  }
}

/** Evaluate `fn` with h() producing real DOM, even inside withVNodes. */
export function withDOM<T>(fn: () => T): T {
  const saved = vnodeDepth;
  vnodeDepth = 0;
  try {
    return fn();
  } finally {
    vnodeDepth = saved;
  }
}

function isVNode(v: unknown): v is KrateVNode {
  return !!v && typeof v === 'object' && (v as KrateVNode).__kv === 1;
}

function isAdoptable(v: unknown): v is KrateAdoptable {
  return !!v && typeof v === 'object' && (v as KrateAdoptable).__ka === 1;
}

function buildElement(tag: string, props: Record<string, unknown> | null, children: unknown[]): Element {
  const el = document.createElement(tag);
  if (props) applyProps(el, props);
  if (!props || !props.dangerouslySetInnerHTML) {
    for (const child of children) appendChild(el, child);
  }
  return el;
}

function materialize(v: KrateVNode): Node {
  return withDOM(() => buildElement(v.tag, v.props, v.children));
}

function isTextLike(v: unknown): boolean {
  return v == null || typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean' || typeof v === 'bigint';
}

function textOf(v: unknown): string {
  return v == null || typeof v === 'boolean' ? '' : String(v);
}

/** Append any renderable value (node, text, array, reactive function) to parent. */
export function appendChild(parent: Node, child: unknown): void {
  if (child == null || child === false || child === true) return;

  if (Array.isArray(child)) {
    for (const c of child) appendChild(parent, c);
    return;
  }

  if (typeof child === 'function') {
    createRegion(parent, child as () => unknown, null);
    return;
  }

  if (isVNode(child)) {
    parent.appendChild(materialize(child));
    return;
  }

  if (isAdoptable(child)) {
    parent.appendChild(child.mount());
    return;
  }

  parent.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
}

// Reactive regions
// A function child re-renders when the signals it reads change. A region that
// only ever renders text is a single text node updated in place; once it
// renders nodes it switches to a pair of empty comment markers so it can hold
// any number of nodes (elements, fragments, nested regions and lists).

interface Region {
  text: Text | null;
  start: Comment | null;
  end: Comment | null;
}

/** Move the disposers tracked on one node to another. */
function retrack(from: Node, to: Node): void {
  const arr = effectsByNode.get(from);
  if (!arr) return;
  effectsByNode.delete(from);
  for (const d of arr) trackEffect(to, d);
}

/** Remove (and dispose) every node strictly between start and end. */
export function clearBetween(start: Node, end: Node): void {
  const parent = start.parentNode;
  if (!parent) return;
  let node = start.nextSibling;
  while (node && node !== end) {
    const next = node.nextSibling;
    disposeNode(node);
    parent.removeChild(node);
    node = next;
  }
}

function regionToMarkers(r: Region): void {
  if (!r.text) return;
  const t = r.text;
  const p = t.parentNode as Node;
  r.start = document.createComment('');
  r.end = document.createComment('');
  p.insertBefore(r.start, t);
  p.insertBefore(r.end, t.nextSibling);
  retrack(t, r.start);
  p.removeChild(t);
  r.text = null;
}

function updateRegion(r: Region, v: unknown): void {
  if (isTextLike(v)) {
    const s = textOf(v);
    if (r.text) {
      if (r.text.data !== s) r.text.data = s;
      return;
    }
    clearBetween(r.start as Node, r.end as Node);
    if (s !== '') (r.end as Node).parentNode!.insertBefore(document.createTextNode(s), r.end);
    return;
  }
  regionToMarkers(r);
  clearBetween(r.start as Node, r.end as Node);
  const frag = document.createDocumentFragment();
  appendChild(frag, v);
  (r.end as Node).parentNode!.insertBefore(frag, r.end);
}

function createRegion(parent: Node, fn: () => unknown, before: Node | null): void {
  const r: Region = { text: null, start: null, end: null };
  let first = true;
  const dispose = createEffect(() => {
    const v = fn();
    if (!first) {
      updateRegion(r, v);
      return;
    }
    first = false;
    if (isTextLike(v)) {
      r.text = document.createTextNode(textOf(v));
      parent.insertBefore(r.text, before);
      return;
    }
    r.start = document.createComment('');
    r.end = document.createComment('');
    parent.insertBefore(r.start, before);
    const frag = document.createDocumentFragment();
    appendChild(frag, v);
    parent.insertBefore(frag, before);
    parent.insertBefore(r.end, before);
  });
  trackEffect((r.text || r.start) as Node, dispose);
}

// Adoption (hydration without re-rendering)
// adoptValue walks a rendered value (virtual nodes, text, arrays, regions,
// lists) against the server-rendered DOM at the cursor, binding props and
// effects to the existing nodes. A node that does not match is replaced by a
// freshly built one, so a mismatch degrades to a local client render.

function skipComments(c: AdoptCursor): void {
  while (c.next && c.next.nodeType === 8) c.next = c.next.nextSibling;
}

/** Insert a node at the cursor (before the next unclaimed node). */
export function insertAt(c: AdoptCursor, node: Node): void {
  c.parent.insertBefore(node, c.next);
}

function claimText(c: AdoptCursor, s: string): Text {
  skipComments(c);
  const n = c.next;
  if (s !== '' && n && n.nodeType === 3) {
    const t = n as Text;
    if (t.data.length > s.length && t.data.startsWith(s)) t.splitText(s.length);
    else if (t.data !== s) t.data = s;
    c.next = t.nextSibling;
    return t;
  }
  const t = document.createTextNode(s);
  insertAt(c, t);
  return t;
}

/** Remove (and dispose) the nodes from the cursor up to `end` (or the end). */
export function removeRest(c: AdoptCursor, end: Node | null): void {
  let n = c.next;
  while (n && n !== end) {
    const next = n.nextSibling;
    disposeNode(n);
    c.parent.removeChild(n);
    n = next;
  }
  c.next = end;
}

function adoptElement(v: KrateVNode, c: AdoptCursor): void {
  skipComments(c);
  const n = c.next;
  if (n && n.nodeType === 1 && (n as Element).localName.toLowerCase() === v.tag.toLowerCase()) {
    c.next = n.nextSibling;
    const el = n as Element;
    if (v.props) applyProps(el, v.props);
    if (!v.props || !v.props.dangerouslySetInnerHTML) {
      const inner: AdoptCursor = { parent: el, next: el.firstChild };
      for (const child of v.children) adoptValue(child, inner);
      removeRest(inner, null);
    }
    return;
  }
  insertAt(c, materialize(v));
}

function adoptRegion(fn: () => unknown, c: AdoptCursor): void {
  const r: Region = { text: null, start: null, end: null };
  let first = true;
  const dispose = createEffect(() => {
    if (!first) {
      updateRegion(r, fn());
      return;
    }
    first = false;
    const v = withVNodes(fn);
    if (isTextLike(v)) {
      r.text = claimText(c, textOf(v));
      return;
    }
    r.start = document.createComment('');
    insertAt(c, r.start);
    adoptValue(v, c);
    r.end = document.createComment('');
    insertAt(c, r.end);
  });
  trackEffect((r.text || r.start) as Node, dispose);
}

/** Adopt server-rendered DOM at the cursor for a rendered value. */
export function adoptValue(v: unknown, c: AdoptCursor): void {
  if (v == null || typeof v === 'boolean') return;
  if (Array.isArray(v)) {
    for (const x of v) adoptValue(x, c);
    return;
  }
  if (typeof v === 'function') {
    adoptRegion(v as () => unknown, c);
    return;
  }
  if (isVNode(v)) {
    adoptElement(v, c);
    return;
  }
  if (isAdoptable(v)) {
    v.adopt(c);
    return;
  }
  if (v instanceof Node) {
    insertAt(c, v);
    return;
  }
  claimText(c, String(v));
}

export function h(
  tag: string | ((props: Record<string, unknown>) => Node),
  props: Record<string, unknown> | null,
  ...children: unknown[]
): Node {
  if (typeof tag === 'function') {
    // Keep the props object (not a spread copy) so getter props stay live: a
    // list row passes `get item(){...}` accessors that read row signals. Each
    // component call gets an effect owner, so its React effects run
    // children-before-parents and are cleaned up when its nodes are disposed.
    const p = props || {};
    if (children.length > 0 || !('children' in p)) p.children = children;
    const { value, owner } = krateWithOwner(() => tag(p));
    trackOwner(value, owner);
    return value;
  }

  if (vnodeDepth > 0) {
    return { __kv: 1, tag, props, children } as unknown as Node;
  }
  return buildElement(tag, props, children);
}

function getContainer(container: string | HTMLElement): HTMLElement {
  if (typeof container === 'string') {
    const el = document.querySelector(container);
    if (!el) throw new Error(`Container "${container}" not found`);
    return el as HTMLElement;
  }
  return container;
}

export function mount(fn: () => Node, container: string | HTMLElement): void {
  const el = getContainer(container);
  let mounted = false;
  createEffect(() => {
    if (!mounted) {
      mounted = true;
      el.appendChild(fn());
    }
  });
}

export function hydrate(fn: () => Node, container: string | HTMLElement): void {
  const el = getContainer(container);
  if (el.childNodes.length === 0) {
    // Empty container - fall back to mount behavior
    el.appendChild(fn());
    return;
  }
  // SSR content exists - run the component to set up effects/signals.
  // The freshly-built tree is only used to establish reactive subscriptions;
  // it is not attached to the DOM, so dispose its effects to avoid leaks.
  const tree = fn();
  if (tree) disposeNode(tree);
}