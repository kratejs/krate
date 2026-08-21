import { createEffect } from './signal.js';

export type Component<P = {}> = (props: P) => Node | null | undefined;

export type PropsWithChildren<P = {}> = P & { children?: unknown[] };
export type RefCallback = (el: Element) => void;

/** Extracts the Props type from a Component. */
export type ComponentProps<C> = C extends Component<infer P> ? P : never;

// ─── per-node effect tracking ────────────────────────────────────────────────
// Reactive props/children create effects that must be disposed when the node is
// removed from the DOM (list items, conditional branches, SPA reconciliation).
// Without this, effects and their closures accumulate for the page's lifetime.

const effectsByNode = new WeakMap<Node, Array<() => void>>();

function trackEffect(node: Node, dispose: () => void): void {
  let arr = effectsByNode.get(node);
  if (!arr) {
    arr = [];
    effectsByNode.set(node, arr);
  }
  arr.push(dispose);
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

// ─── comment-marker DOM traversal ───────────────────────────────────────────

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

/** Coerce a value to a DOM node for insertion. */
function toNode(value: unknown): Node | null {
  if (value == null || value === false || value === true) return null;
  if (value instanceof Node) return value;
  return document.createTextNode(String(value));
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
    // No end marker found — append at end
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
    // Store handler reference on element for cleanup on re-render
    const eventKey = key.slice(2).toLowerCase();
    const handlerKey = `__krate_${eventKey}`;
    const prevHandler = (el as any)[handlerKey];
    if (prevHandler) {
      el.removeEventListener(eventKey, prevHandler);
    }
    el.addEventListener(eventKey, value as EventListener);
    (el as any)[handlerKey] = value;
  } else if (key === 'ref') {
    if (typeof value === 'function') value(el);
  } else if (key === 'key') {
    // key is metadata only, not set as attribute
  } else if (value !== null && value !== undefined && value !== false) {
    el.setAttribute(key, String(value));
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
    return tag({ ...(props || {}), children });
  }

  const el = document.createElement(tag);

  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (typeof value === 'function' && !key.startsWith('on')) {
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
    // Empty container — fall back to mount behavior
    el.appendChild(fn());
    return;
  }
  // SSR content exists — run the component to set up effects/signals.
  // The freshly-built tree is only used to establish reactive subscriptions;
  // it is not attached to the DOM, so dispose its effects to avoid leaks.
  const tree = fn();
  if (tree) disposeNode(tree);
}