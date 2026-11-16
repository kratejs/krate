// A DOM-like host for renderers that build trees through DOM calls, such as
// Preact. @krate/three runs inside Preact islands (a "use client" component of
// a server-rendered page) with this host: the island's <Canvas> renders its
// children with Preact into a SceneNode, Preact creates one SceneNode per
// element (document.createElementNS in the scene namespace) and inserts,
// moves and removes them, and the props of each element are applied from its
// vnode once Preact has diffed it. The instance semantics (args, attach,
// primitives, dispose, events) are the engine's, as in the Krate reconciler.

import type * as THREE from 'three';
import { removeInteractivity, swapInteractivity } from './events.js';
import type { Instance } from './instance.js';
import {
  RECONSTRUCT_PROPS,
  createObject,
  disposeOnIdle,
  link,
  needsReconstruct,
  resolveType,
  unlinkObject,
  validateInstance,
} from './reconciler.js';
import { setRef } from './runtime.js';
import type { RootStore } from './store.js';
import { applyProps, diffProps, findInitialRoot, invalidateInstance, isObject3D, prepare } from './utils.js';

/** The namespace of scene elements: Preact creates them with createElementNS. */
export const SCENE_NS = 'https://krate.js.org/ns/three';

type Child = SceneNode | { nodeType: number; parentNode?: unknown };

/**
 * One element of the scene, as Preact sees it: a node with the DOM methods
 * Preact calls. The three object lives in `instance` once the element has
 * props and is attached under the scene.
 */
export class SceneNode {
  readonly nodeType = 1;
  readonly namespaceURI = SCENE_NS;
  readonly localName: string;
  readonly nodeName: string;
  readonly style: Record<string, unknown> = {};
  parentNode: SceneNode | null = null;
  childNodes: Child[] = [];
  /** The engine instance (object, props, handlers) once materialized. */
  instance: Instance | null = null;
  /** The store of the scene this node renders in (set for roots). */
  store: RootStore | null = null;
  /** The props of the last diff. */
  pending: Record<string, unknown> | null = null;
  /** The element's ref (the three object is passed to it). */
  ref: unknown = null;
  private releaseRef: () => void = () => {};

  constructor(type: string) {
    this.localName = type;
    this.nodeName = type.toUpperCase();
  }

  get firstChild(): Child | null {
    return this.childNodes[0] ?? null;
  }

  get lastChild(): Child | null {
    return this.childNodes[this.childNodes.length - 1] ?? null;
  }

  get nextSibling(): Child | null {
    const p = this.parentNode;
    if (!p) return null;
    return p.childNodes[p.childNodes.indexOf(this) + 1] ?? null;
  }

  get previousSibling(): Child | null {
    const p = this.parentNode;
    if (!p) return null;
    return p.childNodes[p.childNodes.indexOf(this) - 1] ?? null;
  }

  insertBefore<T extends Child>(child: T, before: Child | null): T {
    if (!(child instanceof SceneNode)) {
      warnNonScene(child);
      return child;
    }
    if (child.parentNode) child.parentNode.detachChild(child, false);
    const at = before ? this.childNodes.indexOf(before) : -1;
    if (at === -1) this.childNodes.push(child);
    else this.childNodes.splice(at, 0, child);
    child.parentNode = this;
    const store = this.sceneStore();
    if (store && this.instance) {
      materialize(child, store);
      attachChild(this, child);
    }
    return child;
  }

  appendChild<T extends Child>(child: T): T {
    return this.insertBefore(child, null);
  }

  removeChild<T extends Child>(child: T): T {
    if (child instanceof SceneNode) this.detachChild(child, true);
    return child;
  }

  remove(): void {
    this.parentNode?.removeChild(this);
  }

  contains(node: unknown): boolean {
    for (let n: unknown = node; n; n = (n as SceneNode).parentNode) if (n === this) return true;
    return false;
  }

  setAttribute(): void {}
  removeAttribute(): void {}
  getAttribute(): null {
    return null;
  }
  addEventListener(): void {}
  removeEventListener(): void {}

  /** The store of the scene above this node, if it is attached to one. */
  sceneStore(): RootStore | null {
    for (let n: SceneNode | null = this; n; n = n.parentNode) if (n.store) return n.store;
    return null;
  }

  /** Points the element's ref at its object (again, after a reconstruct). */
  bindRef(ref: unknown): void {
    if (ref === this.ref && this.instance) return;
    this.releaseRef();
    this.ref = ref;
    this.releaseRef = this.instance ? setRef(ref, this.instance.object) : () => {};
  }

  private detachChild(child: SceneNode, removed: boolean): void {
    const i = this.childNodes.indexOf(child);
    if (i !== -1) this.childNodes.splice(i, 1);
    child.parentNode = null;
    if (this.instance && child.instance) unlinkTree(this, child);
    if (removed && child.instance) {
      // Disposed when the browser is idle unless it was inserted again
      // meanwhile (compat Suspense moves suspended content out and back).
      disposeTree(child, () => child.parentNode !== null);
    }
  }
}

let warned = false;
function warnNonScene(child: unknown): void {
  if (warned) return;
  warned = true;
  const text = (child as { data?: string }).data;
  console.warn(
    text !== undefined
      ? `@krate/three: text is not allowed in the scene ("${text}")`
      : '@krate/three: DOM elements cannot be rendered in the scene; render them next to the <Canvas>',
  );
}

/** Creates the root node of a scene: its children are added to `root`. */
export function createSceneRoot(root: Instance, store: RootStore): SceneNode {
  const node = new SceneNode('scene');
  node.instance = root;
  node.store = store;
  return node;
}

/**
 * Creates the node of a portal target: children rendered into it are added
 * to `object` and see `store` (the portal's layered state).
 */
export function createPortalRoot(object: THREE.Object3D, store: RootStore): SceneNode {
  const node = new SceneNode('primitive');
  node.store = store;
  // Children attach through a stand-in instance, so an object that is itself
  // in the scene keeps its own descriptor.
  node.instance = {
    root: store,
    type: '',
    parent: null,
    children: [],
    props: {} as Instance['props'],
    object: object as Instance['object'],
    eventCount: 0,
    handlers: {},
    previousVisible: undefined,
    isHidden: false,
  };
  return node;
}

// Builds the objects of a subtree bottom-up (children before their parent, as
// React does), then links each child into its parent.
function materialize(node: SceneNode, store: RootStore): void {
  for (const child of node.childNodes) if (child instanceof SceneNode) materialize(child, store);
  if (!node.instance) {
    const props = node.pending ?? {};
    const type = resolveType(node.localName);
    validateInstance(type, props);
    if (type === 'primitive' && (props.object as { __r3f?: unknown }).__r3f) delete (props.object as { __r3f?: unknown }).__r3f;
    const inst = prepare(undefined as unknown, store, type, props);
    inst.object = createObject(inst) as Instance['object'];
    inst.object.__r3f = inst;
    node.instance = inst;
    applyProps(inst.object, inst.props);
    const ref = node.ref;
    node.ref = null;
    node.bindRef(ref);
  }
  for (const child of node.childNodes) if (child instanceof SceneNode && child.instance && child.instance.parent !== node.instance) attachChild(node, child);
}

function attachChild(parent: SceneNode, child: SceneNode): void {
  const p = parent.instance!;
  const c = child.instance!;
  if (c.parent === p) return;
  c.parent = p;
  // The next sibling that is already in the scene keeps the order.
  let before: Instance | null = null;
  for (let i = parent.childNodes.indexOf(child) + 1; i < parent.childNodes.length; i++) {
    const n = parent.childNodes[i];
    if (n instanceof SceneNode && n.instance && n.instance.parent === p) {
      before = n.instance;
      break;
    }
  }
  const at = before ? p.children.indexOf(before) : -1;
  if (at !== -1) p.children.splice(at, 0, c);
  else p.children.push(c);
  link(p, c, before);
  if (c.eventCount > 0 && isObject3D(c.object)) {
    const internal = findInitialRoot(c).getState().internal;
    if (!internal.interaction.includes(c.object)) internal.interaction.push(c.object);
  }
  invalidateInstance(c);
}

function unlinkTree(parent: SceneNode, child: SceneNode): void {
  const p = parent.instance!;
  const c = child.instance!;
  const i = p.children.indexOf(c);
  if (i !== -1) p.children.splice(i, 1);
  if (c.parent === p) unlinkObject(p, c);
  c.parent = null;
  if (isObject3D(c.object)) removeInteractivity(findInitialRoot(c), c.object);
  const state = c.root?.getState?.();
  if (state && state.internal.frames === 0) state.invalidate();
}

function disposeTree(node: SceneNode, keep: () => boolean): void {
  const inst = node.instance;
  if (!inst || inst.props.dispose === null) return;
  for (const child of node.childNodes) if (child instanceof SceneNode) disposeTree(child, keep);
  if (inst.type === 'primitive' || (inst.object as { type?: string }).type === 'Scene') return;
  disposeOnIdle(inst.object as { dispose?: () => void }, keep);
}

/**
 * Applies the props of an element after Preact diffed it: the first props
 * create the object (once the element is under the scene), later ones update
 * it with R3F's rules (changed `args`, `object` or `attach` recreate it).
 */
export function commitSceneProps(node: SceneNode, props: Record<string, unknown>): void {
  const next: Record<string, unknown> = {};
  for (const key in props) if (key !== 'children' && key !== 'key' && key !== 'ref') next[key] = props[key];
  const inst = node.instance;
  // The default attach (geometry, material) is not a prop the element wrote.
  if (inst && next.attach === undefined) {
    const o = inst.object as { isBufferGeometry?: boolean; isMaterial?: boolean };
    if ((o.isBufferGeometry && inst.props.attach === 'geometry') || (o.isMaterial && inst.props.attach === 'material')) next.attach = inst.props.attach;
  }
  node.pending = next;
  if (!inst) {
    // Not yet in a scene: insertBefore materializes it.
    const store = node.parentNode?.sceneStore();
    if (store && node.parentNode?.instance) {
      materialize(node, store);
      attachChild(node.parentNode, node);
    }
    return;
  }
  let rebuild = false;
  for (const key of RECONSTRUCT_PROPS) {
    if (needsReconstruct(inst, key, next[key])) rebuild = true;
  }
  if (rebuild) {
    inst.props = { ...next } as Instance['props'];
    reconstruct(node);
    return;
  }
  const changes = diffProps(inst, next as Instance['props']);
  inst.props = { ...next } as Instance['props'];
  if (Object.keys(changes as object).length) {
    const before = inst.eventCount;
    applyProps(inst.object, changes);
    if (inst.eventCount !== before && inst.parent && isObject3D(inst.object)) {
      const internal = findInitialRoot(inst).getState().internal;
      const i = internal.interaction.indexOf(inst.object);
      if (inst.eventCount > 0 && i === -1) internal.interaction.push(inst.object);
      else if (inst.eventCount === 0 && i !== -1) internal.interaction.splice(i, 1);
    }
    invalidateInstance(inst);
  }
}

function reconstruct(node: SceneNode): void {
  const inst = node.instance!;
  const parent = inst.parent;
  if (parent) unlinkObject(parent, inst);
  for (const child of inst.children) unlinkObject(inst, child);
  const prev = inst.object;
  if (prev.__r3f) delete prev.__r3f;
  if (inst.type !== 'primitive') disposeOnIdle(prev as { dispose?: () => void });
  inst.object = createObject(inst) as Instance['object'];
  inst.object.__r3f = inst;
  swapInteractivity(findInitialRoot(inst), prev as unknown as THREE.Object3D, inst.object as unknown as THREE.Object3D);
  applyProps(inst.object, inst.props);
  if (parent) link(parent, inst, null);
  for (const child of inst.children) link(inst, child, null);
  const ref = node.ref;
  node.ref = null;
  node.bindRef(ref);
  invalidateInstance(inst);
}
