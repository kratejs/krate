// A DOM-like host for renderers that build trees through DOM calls, such as
// Preact. @krate/konva runs inside Preact islands (a "use client" component
// of a server-rendered page) with this host: the island's <Stage> renders its
// children with Preact into a host node wrapping the Konva.Stage, Preact
// creates one host node per element (document.createElementNS in the konva
// namespace) and inserts, moves and removes them, and each element's props
// are applied to its Konva node once Preact has diffed it. The node semantics
// are react-konva's host config (see props.ts).

import { applyNodeProps, createNode, destroyNode, EVENTS_NAMESPACE, insertNode, updatePicture, type KonvaContainer, type KonvaNode } from './props.js';

/** The namespace of stage elements: Preact creates them with createElementNS. */
export const KONVA_NS = 'https://krate.js.org/ns/konva';

type Child = KonvaHostNode | { nodeType: number; parentNode?: unknown; data?: string };

/**
 * One element of a stage, as Preact sees it: a node with the DOM methods
 * Preact calls. The Konva node lives in `konvaNode` once the element has had
 * its props.
 */
export class KonvaHostNode {
  readonly nodeType = 1;
  readonly namespaceURI = KONVA_NS;
  readonly localName: string;
  readonly nodeName: string;
  readonly style: Record<string, unknown> = {};
  parentNode: KonvaHostNode | null = null;
  childNodes: Child[] = [];
  /** The Konva node, once created. */
  konvaNode: KonvaNode | null;
  /** The props applied so far. */
  konvaProps: Record<string, unknown> = {};

  constructor(type: string, node: KonvaNode | null = null) {
    this.localName = type;
    this.nodeName = type.toUpperCase();
    this.konvaNode = node;
  }

  get firstChild(): Child | null {
    return this.childNodes[0] ?? null;
  }

  get lastChild(): Child | null {
    return this.childNodes[this.childNodes.length - 1] ?? null;
  }

  get nextSibling(): Child | null {
    const p = this.parentNode;
    return p ? (p.childNodes[p.childNodes.indexOf(this) + 1] ?? null) : null;
  }

  get previousSibling(): Child | null {
    const p = this.parentNode;
    return p ? (p.childNodes[p.childNodes.indexOf(this) - 1] ?? null) : null;
  }

  insertBefore<T extends Child>(child: T, before: Child | null): T {
    if (!(child instanceof KonvaHostNode)) {
      warnNonKonva(child);
      return child;
    }
    if (child.parentNode) child.parentNode.unlist(child);
    const at = before ? this.childNodes.indexOf(before) : -1;
    if (at === -1) this.childNodes.push(child);
    else this.childNodes.splice(at, 0, child);
    child.parentNode = this;
    if (this.konvaNode && child.konvaNode) place(this, child);
    return child;
  }

  appendChild<T extends Child>(child: T): T {
    return this.insertBefore(child, null);
  }

  removeChild<T extends Child>(child: T): T {
    if (child instanceof KonvaHostNode) {
      this.unlist(child);
      // Detached only: an unmounted element is destroyed by unmountHost, an
      // element moved out and back (compat Suspense) is reused.
      const node = child.konvaNode;
      if (node?.parent) {
        node.remove();
        if (this.konvaNode) updatePicture(this.konvaNode);
      }
    }
    return child;
  }

  remove(): void {
    this.parentNode?.removeChild(this);
  }

  contains(node: unknown): boolean {
    for (let n: unknown = node; n; n = (n as KonvaHostNode).parentNode) if (n === this) return true;
    return false;
  }

  setAttribute(): void {}
  removeAttribute(): void {}
  getAttribute(): null {
    return null;
  }
  addEventListener(): void {}
  removeEventListener(): void {}

  private unlist(child: KonvaHostNode): void {
    const i = this.childNodes.indexOf(child);
    if (i !== -1) this.childNodes.splice(i, 1);
    child.parentNode = null;
  }
}

let warned = false;
function warnNonKonva(child: Child): void {
  if (warned) return;
  warned = true;
  const text = (child as { data?: string }).data;
  console.error(
    text !== undefined
      ? `Text components are not supported for now in ReactKonva. Your text is: "${text}"`
      : '@krate/konva: DOM elements cannot be rendered inside the Stage; render them next to it',
  );
}

/** Creates the host node of a container that already exists (the Stage). */
export function createHostRoot(node: KonvaContainer): KonvaHostNode {
  return new KonvaHostNode(node.getClassName(), node as unknown as KonvaNode);
}

// Adds a child's node to its parent's, before the next sibling already there.
function place(parent: KonvaHostNode, child: KonvaHostNode): void {
  const p = parent.konvaNode as KonvaContainer;
  let before: KonvaNode | null = null;
  for (let i = parent.childNodes.indexOf(child) + 1; i < parent.childNodes.length; i++) {
    const n = parent.childNodes[i];
    if (n instanceof KonvaHostNode && n.konvaNode && n.konvaNode.parent === (p as unknown)) {
      before = n.konvaNode;
      break;
    }
  }
  insertNode(p, child.konvaNode!, before);
}

/**
 * Applies the props of an element after Preact diffed it: the first props
 * create the Konva node (with the children already built), later ones update
 * it with react-konva's rules.
 */
export function commitHostProps(host: KonvaHostNode, props: Record<string, unknown>): void {
  const next: Record<string, unknown> = {};
  for (const key in props) if (key !== 'children' && key !== 'ref' && key !== 'key') next[key] = props[key];
  const node = host.konvaNode;
  if (node) {
    applyNodeProps(node, next, host.konvaProps);
    host.konvaProps = next;
    return;
  }
  host.konvaNode = createNode(host.localName, next);
  host.konvaProps = next;
  for (const child of host.childNodes) {
    if (child instanceof KonvaHostNode && child.konvaNode) insertNode(host.konvaNode as KonvaContainer, child.konvaNode, null);
  }
  if (host.parentNode?.konvaNode) place(host.parentNode, host);
}

/** Destroys the node of an unmounted element and releases its listeners. */
export function unmountHost(host: KonvaHostNode): void {
  const node = host.konvaNode;
  if (!node) return;
  host.konvaNode = null;
  // Preact unmounts parents first: a destroyed parent took its children along.
  if (host.parentNode && !host.parentNode.konvaNode) node.off(EVENTS_NAMESPACE);
  else destroyNode(node);
}

const refWrappers = new WeakMap<object, (host: KonvaHostNode | null) => unknown>();

/**
 * Returns a ref that receives the Konva node of a host node instead of the
 * host node (stable per ref, so Preact does not reattach it every render).
 */
export function konvaRef(ref: unknown): unknown {
  if (ref == null || (typeof ref !== 'function' && typeof ref !== 'object')) return ref;
  let wrapper = refWrappers.get(ref as object);
  if (!wrapper) {
    wrapper = (host) => {
      const value = host ? host.konvaNode : null;
      if (typeof ref === 'function') return (ref as (v: unknown) => unknown)(value);
      (ref as { current: unknown }).current = value;
      return undefined;
    };
    refWrappers.set(ref as object, wrapper);
  }
  return wrapper;
}
