import { createEffect } from './signal.js';
import { disposeNode } from './dom.js';

/**
 * DOM-level list reconciliation.
 * Diffs old children against new items and mutates the DOM directly:
 * reuses keyed nodes, creates new ones, removes stale ones.
 *
 * @param parent - the parent DOM node
 * @param startMarker - comment boundary marking start of list region
 * @param endMarker - comment boundary marking end of list region
 * @param items - new array of items
 * @param mapFn - creates a DOM node for each item
 * @param keyFn - optional key extractor for keyed reconciliation
 */
export function reconcile<T>(
  parent: Node,
  startMarker: Node,
  endMarker: Node,
  items: T[],
  mapFn: (item: T, index: number) => Node,
  keyFn?: (item: T) => string | number
): void {
  // Collect existing child nodes between startMarker and endMarker
  const existing: Node[] = [];
  let node = startMarker.nextSibling;
  while (node && node !== endMarker) {
    existing.push(node);
    node = node.nextSibling;
  }

  if (keyFn) {
    // Keyed reconciliation
    const keyMap = new Map<string | number, Node>();
    for (const child of existing) {
      const key = (child as any).__k;
      if (key != null) keyMap.set(key, child);
    }

    const newNodes: Node[] = [];
    const usedKeys = new Set<string | number>();

    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      const key = keyFn(item);
      usedKeys.add(key);

      const newNode = mapFn(item, i);
      (newNode as any).__k = key;
      newNodes.push(newNode);
    }

    // Remove nodes whose keys are no longer present
    for (const [key, child] of keyMap) {
      if (!usedKeys.has(key) && child.parentNode === parent) {
        disposeNode(child);
        parent.removeChild(child);
      }
    }

    // Reorder: insert nodes in the correct order before endMarker
    for (let i = 0; i < newNodes.length; i++) {
      const newNode = newNodes[i];
      // Find the node that should be before this one
      const prevNode = i === 0 ? startMarker : newNodes[i - 1];
      if (newNode !== prevNode.nextSibling) {
        parent.insertBefore(newNode, prevNode.nextSibling);
      }
    }
  } else {
    // Unkeyed: simple index-based reconciliation
    const maxLen = Math.max(existing.length, items.length);

    for (let i = 0; i < maxLen; i++) {
      if (i < items.length) {
        const newNode = mapFn(items[i], i);
        if (i < existing.length) {
          // Replace existing node
          if (existing[i] !== newNode) {
            disposeNode(existing[i]);
            parent.replaceChild(newNode, existing[i]);
          }
        } else {
          // Append new node before endMarker
          parent.insertBefore(newNode, endMarker);
        }
      } else if (i < existing.length) {
        // Remove excess existing nodes
        if (existing[i].parentNode === parent) {
          disposeNode(existing[i]);
          parent.removeChild(existing[i]);
        }
      }
    }
  }
}

/**
 * Conditional branch swapping between two comment markers.
 * When the signal changes, tears down the current branch and inserts the new one.
 *
 * @param parent - the parent DOM node
 * @param startMarker - comment boundary marking start of conditional region
 * @param endMarker - comment boundary marking end of conditional region
 * @param test - reactive function returning boolean
 * @param trueContent - function returning DOM node for true branch
 * @param falseContent - function returning DOM node for false branch, or null for empty
 */
export function conditional(
  parent: Node,
  startMarker: Node,
  endMarker: Node,
  test: () => boolean,
  trueContent: () => Node,
  falseContent: (() => Node) | null
): void {
  let current: Node | null = null;

  createEffect(() => {
    const value = test();
    const newNode = value ? trueContent() : (falseContent ? falseContent() : null);

    if (current) {
      if (newNode) {
        disposeNode(current);
        parent.replaceChild(newNode, current);
      } else {
        disposeNode(current);
        parent.removeChild(current);
      }
    } else if (newNode) {
      parent.insertBefore(newNode, endMarker);
    }
    current = newNode;
  });
}

// ─── Tree reconciliation (SPA navigation) ───────────────────────────────────
// Diffs a live DOM subtree against a freshly-parsed one, patching only what
// changed. Unchanged nodes are kept in place so their state survives navigation:
// element listeners attached at hydration (`__krate_<event>` props), media
// playback, focus, scroll, CSS animations. Slot markers (`<!--k:id-->`) and
// `data-k` elements act as stable identity anchors between pages.

function nodeKey(n: Node): string | null {
  if (n.nodeType === Node.COMMENT_NODE) {
    const v = n.nodeValue || '';
    if (v.indexOf('k:') === 0) return v;
    return null;
  }
  if (n.nodeType === Node.ELEMENT_NODE) {
    const el = n as Element;
    const dk = el.getAttribute('data-k');
    if (dk) return dk;
    const id = el.getAttribute('id');
    if (id) return '#' + id;
  }
  return null;
}

function syncAttributes(oldEl: Element, newEl: Element): void {
  // Pass 1: apply (or update) attributes present on the new element.
  const newNames = new Set<string>();
  for (let i = 0; i < newEl.attributes.length; i++) {
    const name = newEl.attributes[i].name;
    const value = newEl.attributes[i].value;
    newNames.add(name);
    if (oldEl.getAttribute(name) !== value) {
      oldEl.setAttribute(name, value);
    }
  }
  // Pass 2: remove attributes no longer present.
  for (let i = 0; i < oldEl.attributes.length; i++) {
    const name = oldEl.attributes[i].name;
    if (!newNames.has(name)) {
      oldEl.removeAttribute(name);
    }
  }
}

function replaceNode(oldNode: Node, newNode: Node): Node {
  const imported = document.importNode(newNode, true);
  if (oldNode.parentNode) {
    disposeNode(oldNode);
    oldNode.parentNode.replaceChild(imported, oldNode);
    return imported;
  }
  return imported;
}

function compatible(oldNode: Node, newNode: Node): boolean {
  if (oldNode.nodeType !== newNode.nodeType) return false;
  if (oldNode.nodeType === Node.ELEMENT_NODE) {
    return (oldNode as Element).tagName === (newNode as Element).tagName;
  }
  return true;
}

function reconcileNode(oldNode: Node, newNode: Node): Node {
  if (!compatible(oldNode, newNode)) return replaceNode(oldNode, newNode);

  if (oldNode.nodeType === Node.TEXT_NODE) {
    if (oldNode.nodeValue !== newNode.nodeValue) oldNode.nodeValue = newNode.nodeValue;
    return oldNode;
  }
  if (oldNode.nodeType === Node.COMMENT_NODE) {
    if (oldNode.nodeValue !== newNode.nodeValue) return replaceNode(oldNode, newNode);
    return oldNode;
  }
  if (oldNode.nodeType !== Node.ELEMENT_NODE) return oldNode;

  const oldEl = oldNode as Element;
  const newEl = newNode as Element;
  if (nodeKey(oldEl) !== nodeKey(newEl)) return replaceNode(oldEl, newEl);

  syncAttributes(oldEl, newEl);
  reconcileChildren(oldEl, newEl);
  return oldEl;
}

function reconcileChildren(parent: Node, newParent: Node): void {
  const oldKids = Array.from(parent.childNodes);
  const newKids = Array.from(newParent.childNodes);

  // Fast path: identical child counts and identical node keys in order. This
  // is the common case for static navigation, and it avoids building the
  // key maps and per-node bookkeeping entirely.
  if (oldKids.length === newKids.length) {
    let same = true;
    for (let i = 0; i < newKids.length; i++) {
      if (nodeKey(oldKids[i]) !== nodeKey(newKids[i])) {
        same = false;
        break;
      }
    }
    if (same) {
      for (let i = 0; i < newKids.length; i++) {
        reconcileNode(oldKids[i], newKids[i]);
      }
      return;
    }
  }

  const keyToIndex = new Map<string, number>();
  const unkeyedOld: number[] = [];
  oldKids.forEach((c, i) => {
    const k = nodeKey(c);
    if (k != null) {
      if (!keyToIndex.has(k)) keyToIndex.set(k, i);
    } else {
      unkeyedOld.push(i);
    }
  });

  const consumed = new Set<number>();
  let lastPlacedIndex = 0;
  let anchor: Node | null = null;
  let unkeyedPtr = 0;

  const nextUnkeyed = (): number => {
    while (unkeyedPtr < unkeyedOld.length && consumed.has(unkeyedOld[unkeyedPtr])) unkeyedPtr++;
    return unkeyedPtr < unkeyedOld.length ? unkeyedOld[unkeyedPtr] : -1;
  };

  for (const newChild of newKids) {
    const k = nodeKey(newChild);
    let placedNode: Node;

    if (k != null && keyToIndex.has(k)) {
      const idx = keyToIndex.get(k)!;
      keyToIndex.delete(k);
      consumed.add(idx);
      const oldChild = oldKids[idx];
      if (idx < lastPlacedIndex) {
        parent.insertBefore(oldChild, anchor);
      } else {
        lastPlacedIndex = idx;
      }
      placedNode = reconcileNode(oldChild, newChild);
    } else if (k == null) {
      const candIdx = nextUnkeyed();
      if (candIdx >= 0 && compatible(oldKids[candIdx], newChild)) {
        consumed.add(candIdx);
        const oldChild = oldKids[candIdx];
        if (oldChild.parentNode === parent && oldChild.nextSibling !== anchor) {
          parent.insertBefore(oldChild, anchor);
        }
        placedNode = reconcileNode(oldChild, newChild);
      } else {
        placedNode = document.importNode(newChild, true);
        parent.insertBefore(placedNode, anchor);
      }
    } else {
      // Keyed but not present in old tree → insert new
      placedNode = document.importNode(newChild, true);
      parent.insertBefore(placedNode, anchor);
    }
    anchor = placedNode.nextSibling;
  }

  oldKids.forEach((c, i) => {
    if (!consumed.has(i) && c.parentNode === parent) {
      disposeNode(c);
      parent.removeChild(c);
    }
  });
}

/**
 * Reconciles a live DOM subtree against freshly-parsed markup.
 * Keeps unchanged nodes (and their state), patches attributes/text, and only
 * inserts/replaces/removes nodes that actually differ. Used by the SPA router
 * on navigation so persistent components don't get destroyed and re-created.
 *
 * @param oldRoot - the live element currently in the document
 * @param newRoot - the parsed element from the navigated-to page
 */
export function reconcileTrees(oldRoot: Node, newRoot: Node): void {
  if (oldRoot.nodeType !== Node.ELEMENT_NODE || newRoot.nodeType !== Node.ELEMENT_NODE) {
    return;
  }
  const oldEl = oldRoot as Element;
  const newEl = newRoot as Element;
  if (oldEl.tagName !== newEl.tagName) {
    const imported = document.importNode(newRoot, true);
    if (oldEl.parentNode) {
      oldEl.parentNode.replaceChild(imported, oldEl);
    }
    return;
  }
  syncAttributes(oldEl, newEl);
  reconcileChildren(oldEl, newEl);
}
