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
} from './events.js';
export type { KrateSyntheticEvent } from './events.js';
export { krateAttachRef, krateSetRef, krateRefsEnter, krateRefsLeave } from './refs.js';
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

function trackEffect(node: Node, dispose: () => void): void {
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
  const dispose = createEffect(() => {
    const release = krateAttachRef(el, get ? get() : undefined, assign);
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

function setAttr(el: Element, key: string, value: unknown): void {
  if (key === 'class' || key === 'className') {
    // Fix 2.8: Guard against false/null/undefined before String() conversion
    if (value === false || value === null || value === undefined) {
      el.className = '';
    } else {
      el.className = String(value);
    }
  } else if (key === 'style') {
    if (typeof value === 'string') {
      el.setAttribute('style', value);
    } else if (value && typeof value === 'object') {
      Object.assign((el as HTMLElement).style, value);
    }
  } else if (key.startsWith('on') && typeof value === 'function') {
    krateOn(el, key, value as Parameters<typeof krateOn>[2]);
  } else if (key === 'value' || key === 'checked' || key === 'selected' || key === 'defaultValue' || key === 'defaultChecked') {
    // Form-control state is a DOM property, not an attribute: setting the
    // attribute after user interaction has no effect.
    krateSetControl(el, key, value);
  } else if (key === 'ref') {
    // Attached now (the node is about to be inserted), detached when the node
    // is disposed or the rendering effect re-runs.
    const release = krateAttachRef(el, value as KrateRef, undefined, true);
    trackEffect(el, release);
    onCleanup(release);
  } else if (key === 'key') {
    // key is metadata only, not set as attribute
  } else if (value !== null && value !== undefined && (value !== false || /^(aria|data)-/.test(key))) {
    // aria-* and data-* stringify booleans (aria-selected={false} renders
    // "false"), as in React.
    el.setAttribute(key, krateSanitizeAttr(key, String(value)));
  }
}

function appendChild(parent: Node, child: unknown): void {
  if (child == null || child === false || child === true) return;

  if (Array.isArray(child)) {
    for (const c of child) appendChild(parent, c);
    return;
  }

  if (typeof child === 'function') {
    const text = document.createTextNode('');
    parent.appendChild(text);
    trackEffect(text, createEffect(() => { text.nodeValue = String(child()); }));
    return;
  }

  parent.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
}

export function h(
  tag: string | ((props: Record<string, unknown>) => Node),
  props: Record<string, unknown> | null,
  ...children: unknown[]
): Node {
  if (typeof tag === 'function') {
    // Each component call gets an effect owner, so its React effects run
    // children-before-parents and are cleaned up when its nodes are disposed.
    const { value, owner } = krateWithOwner(() => tag({ ...(props || {}), children }));
    trackOwner(value, owner);
    return value;
  }

  const el = document.createElement(tag);

  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (typeof value === 'function' && (key === 'value' || key === 'checked')) {
        trackEffect(el, krateControl(el, key, value as () => unknown));
      } else if (typeof value === 'function' && !key.startsWith('on') && key !== 'ref') {
        trackEffect(el, createEffect(() => setAttr(el, key, value())));
      } else {
        setAttr(el, key, value);
      }
    }
  }

  for (const child of children) {
    appendChild(el, child);
  }

  return el;
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