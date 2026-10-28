// Portals: `createPortal(children, container)` and the built-in `<Portal>`.
//
// The compiler emits a portal as an inert anchor in the component's place:
//
//   <template data-krate-portal="ID" data-krate-portal-target="#modal-root">
//     ...portal children (hydration markers included)...
//   </template>
//
// Like React, the content is not part of the server-rendered page; after the
// page's bindings are installed, krateMountPortals() moves the template's
// nodes into the container (document.body unless a target selector or a
// registered resolver says otherwise). The anchor stays in the component's
// place: React events bubble from portal content through the anchor to the
// component's ancestors, a hidden conditional branch takes its portals with it
// (kratePortalsLeave), and showing the branch again mounts them
// (kratePortalsEnter). Removing the anchor through a binding drops the content.

import { krateEventRoot } from './events.js';
import { krateTrackNode } from './dom.js';

type Anchor = HTMLTemplateElement & {
  /** Nodes currently mounted in the container. */
  __kpn?: Node[];
  /** Whether dispose tracking is installed. */
  __kpd?: boolean;
};

/** Portal content nodes point back to their anchor (React parent). */
export type KratePortalNode = Node & { __kpp?: Node };

const SELECTOR = 'template[data-krate-portal]';
const resolvers = new Map<string, () => unknown>();
const mounted = new Set<Anchor>();

/**
 * Registers the container of portal `id` as an expression evaluated in the
 * component's hydration scope (anything other than a literal selector).
 */
export function kratePortalTarget(id: string, get: () => unknown): void {
  resolvers.set(id, get);
}

function isContainer(v: unknown): v is Element | DocumentFragment {
  return !!v && typeof v === 'object' && ((v as Node).nodeType === 1 || (v as Node).nodeType === 11);
}

function containerOf(a: Anchor): Element | DocumentFragment | null {
  const id = a.getAttribute('data-krate-portal') || '';
  const get = resolvers.get(id);
  if (get) {
    let v: unknown = null;
    try {
      v = get();
    } catch {
      v = null;
    }
    return isContainer(v) ? v : null;
  }
  const sel = a.getAttribute('data-krate-portal-target');
  if (!sel || sel === 'body') return document.body;
  try {
    return document.querySelector(sel);
  } catch {
    return null;
  }
}

/** Moves one portal's content into its container. */
export function kratePortalMount(a: Anchor): void {
  if (a.__kpn || !a.isConnected) return;
  const target = containerOf(a);
  if (!target) return;
  const nodes = Array.from(a.content.childNodes);
  const frag = document.createDocumentFragment();
  for (const n of nodes) {
    (n as KratePortalNode).__kpp = a;
    frag.appendChild(n);
  }
  target.appendChild(frag);
  a.__kpn = nodes;
  mounted.add(a);
  // Delegated React events need a listener on containers outside the root.
  const root = document.getElementById('root');
  if (target.nodeType === 1 && !(root && root.contains(target))) krateEventRoot(target);
  if (!a.__kpd) {
    a.__kpd = true;
    krateTrackNode(a, () => kratePortalUnmount(a, true));
  }
}

/**
 * Takes a portal's content out of its container: back into the anchor (the
 * branch may be shown again) or, with `drop`, discarded.
 */
export function kratePortalUnmount(a: Anchor, drop?: boolean): void {
  const nodes = a.__kpn;
  if (!nodes) return;
  a.__kpn = undefined;
  mounted.delete(a);
  for (const n of nodes) {
    if (drop) {
      if (n.parentNode) n.parentNode.removeChild(n);
    } else {
      a.content.appendChild(n);
    }
    (n as KratePortalNode).__kpp = undefined;
  }
}

function anchorsIn(root: ParentNode): Anchor[] {
  const out: Anchor[] = [];
  if ((root as Element).matches?.(SELECTOR)) out.push(root as Anchor);
  root.querySelectorAll(SELECTOR).forEach((a) => out.push(a as Anchor));
  return out;
}

/** Mounts every portal whose anchor is in the document (in tree order). */
export function krateMountPortals(root?: ParentNode): void {
  if (typeof document === 'undefined') return;
  for (const a of anchorsIn(root || document)) kratePortalMount(a);
}

/** Collects the portal anchors of nodes about to enter the document. */
export function kratePortalsIn(nodes: ParentNode): Anchor[] {
  return anchorsIn(nodes);
}

/** Mounts anchors collected by kratePortalsIn once they are in the document. */
export function kratePortalsEnter(anchors: Anchor[]): void {
  for (const a of anchors) kratePortalMount(a);
}

/** Unmounts the portals of nodes leaving the document (a hidden branch). */
export function kratePortalsLeave(nodes: ParentNode): void {
  for (const a of anchorsIn(nodes)) kratePortalUnmount(a);
}

/**
 * Visits the content of every portal template under `roots`, including
 * portals nested in other templates, so hydration can index their markers.
 */
export function kratePortalScan(roots: ParentNode[], visit: (content: DocumentFragment) => void): void {
  const queue = roots.slice();
  const seen = new Set<Node>();
  while (queue.length) {
    const r = queue.shift() as ParentNode;
    r.querySelectorAll('template').forEach((t) => {
      if (seen.has(t)) return;
      seen.add(t);
      const tpl = t as HTMLTemplateElement;
      if (tpl.hasAttribute('data-krate-portal')) visit(tpl.content);
      queue.push(tpl.content);
    });
  }
}

/**
 * Finds the first element matching `selector` in portal content: mounted
 * portals first, then portal templates under `roots` (not yet mounted).
 */
export function kratePortalFind(selector: string, roots: ParentNode[]): Element | null {
  for (const a of mounted) {
    for (const n of a.__kpn || []) {
      if (n.nodeType !== 1) continue;
      const el = n as Element;
      if (el.matches(selector)) return el;
      const inner = el.querySelector(selector);
      if (inner) return inner;
    }
  }
  let found: Element | null = null;
  kratePortalScan(roots, (content) => {
    if (!found) found = content.querySelector(selector);
  });
  return found;
}

/**
 * Forgets the portal state of the previous page (SPA navigation). Portals
 * whose anchor is still in the document (inside a layout the router kept
 * mounted) stay mounted.
 */
export function kratePortalReset(): void {
  const live = new Set<string>();
  for (const a of Array.from(mounted)) {
    if (!a.isConnected) kratePortalUnmount(a, true);
    else live.add(a.getAttribute('data-krate-portal') || '');
  }
  for (const id of Array.from(resolvers.keys())) if (!live.has(id)) resolvers.delete(id);
}
