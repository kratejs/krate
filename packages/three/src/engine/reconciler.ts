// Builds and updates the three scene from compiled Krate JSX.
//
// A renderer component (`@renderer`, such as Canvas) receives its children as a
// function compiled in the reactive h() form: intrinsic elements are
// `h('mesh', props, ...children)`, components are `h(Comp, props, ...)`, a
// reactive prop is a zero-argument thunk (`position: () => [x(), 0, 0]`), a
// reactive child is a function, and a `.map()` is a `krateList(...)` value.
//
// While the engine evaluates that code it installs its own `h`, which returns
// lazy descriptors for elements and components alike (like React elements).
// Mounting walks the descriptors: an element becomes an Instance (the object,
// its props applied, attached to its parent), a component runs then, with its
// own owner and under the store of the tree it is mounted in (so hooks inside
// a portal see the portal's state), a function child becomes a region
// re-evaluated when the signals it reads change, and a list reconciles rows by
// key. The instance semantics (attach, args, primitives, dispose, events)
// follow @react-three/fiber (MIT, Copyright (c) 2019-2025 Poimandres).

import * as THREE from 'three';
import { hasConstructor, resolveConstructor, toPascalCase } from './extend.js';
import { removeInteractivity, swapInteractivity } from './events.js';
import type { Instance } from './instance.js';
import { isPromiseLike } from './promise.js';
import { rt, setRef } from './runtime.js';
import type { RootState, RootStore } from './store.js';
import {
  applyProps,
  attach,
  detach,
  diffProps,
  findInitialRoot,
  invalidateInstance,
  is,
  isObject3D,
  prepare,
} from './utils.js';

/** A three element descriptor (the result of `h('mesh', ...)` in a scene). */
interface ElementNode {
  __k3: 1;
  type: string;
  props: Record<string, unknown>;
  children: unknown[];
}

/** A component call descriptor (`h(Box, ...)` in a scene). */
interface ComponentNode {
  __k3: 2;
  type: (props: Record<string, unknown>) => unknown;
  props: Record<string, unknown>;
}

/** The value createPortal returns. */
export interface PortalNode {
  __k3: 3;
  children: unknown;
  container: THREE.Object3D;
  state?: Record<string, unknown>;
}

/** A compiled `.map()` (krateList) exposes its arguments to custom renderers. */
interface ListValue {
  __ka: 1;
  items: () => unknown;
  key: ((item: unknown, index: number) => unknown) | null;
  render: (item: unknown, index: unknown) => unknown;
  mode: number;
}

/** A Krate runtime vnode (h() under withVNodes), accepted for completeness. */
interface KrateVNode {
  __kv: 1;
  tag: string;
  props: Record<string, unknown> | null;
  children: unknown[];
}

/**
 * A position in a parent instance's child list. An instance slot holds one
 * instance; a container slot (component, region, list, list row, boundary)
 * holds the slots it produced, in order.
 */
interface Slot {
  list: Slot[];
  up: Slot | null;
  inst?: Instance;
  items?: Slot[];
  /** Disposes what the slot owns (effects, component owner). */
  teardown?: () => void;
}

/** A context provider active for a subtree: the context and its value getter. */
type ContextEntry = [unknown, () => unknown];

/** Where values mount: the store hooks see, the owner and the active providers. */
interface Ctx {
  store: RootStore;
  owner: unknown;
  contexts: ContextEntry[];
}

interface Meta {
  slots: Slot[];
  dispose: () => void;
  releaseRef: () => void;
  ref: unknown;
}

const metas = new WeakMap<Instance, Meta>();

// The store of the tree being evaluated, for hooks (useThree, useFrame, ...).
let currentStore: RootStore | null = null;

/** Returns the store of the scene being evaluated, or null outside one. */
export function getCurrentStore(): RootStore | null {
  return currentStore;
}

const PREFIX_REGEX = /^three(?=[A-Z])/;

/** The element the compiler emits for `<Suspense>` inside a scene. */
const SUSPENSE_TAG = 'krate-suspense';
const EVENT_PROP = /^on[A-Z]/;

function sceneH(tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]): unknown {
  if (typeof tag === 'function') {
    const p = props || {};
    // As in React, a component called without children has no children prop.
    if (children.length > 0) p.children = children.length === 1 ? children[0] : children;
    return { __k3: 2, type: tag as ComponentNode['type'], props: p } satisfies ComponentNode;
  }
  return { __k3: 1, type: String(tag), props: props || {}, children } satisfies ElementNode;
}

/**
 * Runs fn with the scene `h` installed, `store` current for hooks, and the
 * providers of the subtree visible to context reads.
 */
export function evaluate<T>(store: RootStore, fn: () => T, contexts: ContextEntry[] = []): T {
  const prevH = globalThis.h;
  const prevStore = currentStore;
  globalThis.h = sceneH as typeof globalThis.h;
  currentStore = store;
  try {
    return contexts.length && typeof globalThis.krateWithContexts === 'function' ? globalThis.krateWithContexts(contexts, fn) : fn();
  } finally {
    globalThis.h = prevH;
    currentStore = prevStore;
  }
}

// A Krate context used as a provider element: `<Ctx.Provider value>` or `<Ctx value>`.
function providedContext(type: unknown): unknown {
  if (typeof type !== 'function') return null;
  const t = type as { __kctxOf?: unknown; $$typeof?: unknown };
  if (t.__kctxOf) return t.__kctxOf;
  if (t.$$typeof === Symbol.for('krate.context')) return type;
  return null;
}

function withOwner<T>(owner: unknown, fn: () => T): T {
  const runtime = rt();
  if (typeof runtime.krateSetOwner !== 'function') return fn();
  const prev = runtime.krateSetOwner(owner);
  try {
    return fn();
  } finally {
    runtime.krateSetOwner(prev);
  }
}

// Instance creation

export function resolveType(type: string): string {
  return hasConstructor(toPascalCase(type)) ? type : type.replace(PREFIX_REGEX, '');
}

export function validateInstance(type: string, props: Record<string, unknown>): void {
  const name = toPascalCase(type);
  const target = resolveConstructor(name);
  if (type !== 'primitive' && !target) {
    throw new Error(
      `@krate/three: ${name} is not part of the THREE namespace! Did you forget to extend? See: https://krate.js.org/docs/features/three/#extend`,
    );
  }
  if (type === 'primitive' && !props.object) throw new Error(`@krate/three: Primitives without 'object' are invalid!`);
  if (props.args !== undefined && !Array.isArray(props.args)) throw new Error('@krate/three: The args prop must be an array!');
}

export function createObject(inst: Instance): unknown {
  if (inst.props.object) return inst.props.object;
  const target = resolveConstructor(toPascalCase(inst.type))!;
  return new target(...((inst.props.args as unknown[] | undefined) ?? []));
}

function indexOfObject(parent: Instance, before: Instance | null): number {
  if (!before) return -1;
  return (parent.object as THREE.Object3D).children.indexOf(before.object as THREE.Object3D);
}

// Adds the child's object to its parent: through `attach`, or as an Object3D child.
export function link(parent: Instance, child: Instance, before: Instance | null): void {
  if (child.props.attach) {
    attach(parent, child);
  } else if (isObject3D(child.object) && isObject3D(parent.object)) {
    const childIndex = indexOfObject(parent, before);
    if (before && childIndex !== -1) {
      const existing = parent.object.children.indexOf(child.object);
      if (existing !== -1) {
        parent.object.children.splice(existing, 1);
        parent.object.children.splice(existing < childIndex ? childIndex - 1 : childIndex, 0, child.object);
      } else {
        child.object.parent = parent.object;
        parent.object.children.splice(childIndex, 0, child.object);
        child.object.dispatchEvent({ type: 'added' } as THREE.Object3DEventMap['added'] & { type: 'added' });
        parent.object.dispatchEvent({ type: 'childadded', child: child.object } as any);
      }
    } else {
      parent.object.add(child.object);
    }
  }
}

export function unlinkObject(parent: Instance, child: Instance): void {
  if (child.props.attach) detach(parent, child);
  else if (isObject3D(child.object) && isObject3D(parent.object)) parent.object.remove(child.object);
}

export function disposeOnIdle(object: { dispose?: () => void }, keep?: () => boolean): void {
  if (typeof object.dispose !== 'function') return;
  const run = (): void => {
    if (keep?.()) return;
    try {
      object.dispose!();
    } catch {
      // Disposal is best effort.
    }
  };
  // Freeing GPU resources off the critical path avoids frame stalls.
  const g = globalThis as { requestIdleCallback?: (cb: () => void) => void; IS_KRATE_THREE_TEST?: boolean };
  if (g.IS_KRATE_THREE_TEST) run();
  else if (typeof g.requestIdleCallback === 'function') g.requestIdleCallback(run);
  else setTimeout(run, 0);
}

export const RECONSTRUCT_PROPS = ['args', 'object', 'attach'];

export function needsReconstruct(inst: Instance, key: string, value: unknown): boolean {
  if (key === 'args') return !is.equ(value, inst.props.args);
  if (key === 'object') return value !== inst.props.object;
  if (key === 'attach') return value !== inst.props.attach && !(typeof value === 'function' && typeof inst.props.attach === 'function');
  return false;
}

// Replaces an instance's object (args, object or attach changed), keeping its
// children, handlers and position.
function reconstruct(inst: Instance): void {
  const parent = inst.parent;
  const meta = metas.get(inst);
  if (parent) {
    unlinkObject(parent, inst);
    for (const child of inst.children) unlinkObject(inst, child);
  }
  if (inst.isHidden) unhideInstance(inst);
  const prevObject = inst.object;
  if (prevObject.__r3f) delete prevObject.__r3f;
  if (inst.type !== 'primitive') disposeOnIdle(prevObject as { dispose?: () => void });

  inst.object = createObject(inst) as Instance['object'];
  inst.object.__r3f = inst;
  // Reapply attach defaults for the new object.
  if (inst.props.attach === 'geometry' || inst.props.attach === 'material') {
    if (!(inst.object as any).isBufferGeometry && !(inst.object as any).isMaterial) delete inst.props.attach;
  }
  if (meta) {
    meta.releaseRef();
    meta.releaseRef = setRef(meta.ref, inst.object);
  }
  swapInteractivity(findInitialRoot(inst), prevObject as unknown as THREE.Object3D, inst.object as unknown as THREE.Object3D);
  applyProps(inst.object, inst.props);
  if (parent) {
    link(parent, inst, null);
    for (const child of inst.children) link(inst, child, null);
    reorder(parent);
  }
  invalidateInstance(inst);
}

/** Hides an instance (used while a Suspense boundary shows its fallback). */
export function hideInstance(instance: Instance): void {
  if (instance.isHidden) return;
  if (instance.props.attach && instance.parent?.object) detach(instance.parent, instance);
  else if (isObject3D(instance.object)) {
    instance.previousVisible = instance.object.visible;
    instance.object.visible = false;
  }
  instance.isHidden = true;
  invalidateInstance(instance);
}

export function unhideInstance(instance: Instance): void {
  if (!instance.isHidden) return;
  if (instance.props.attach && instance.parent?.object) attach(instance.parent, instance);
  else if (isObject3D(instance.object)) instance.object.visible = instance.previousVisible ?? true;
  instance.previousVisible = undefined;
  instance.isHidden = false;
  invalidateInstance(instance);
}

function isThunk(key: string, value: unknown): value is () => unknown {
  return typeof value === 'function' && (value as Function).length === 0 && key !== 'ref' && !EVENT_PROP.test(key);
}

function mountElement(node: ElementNode, parent: Instance, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  const type = resolveType(node.type);
  const raw = node.props;
  const props: Record<string, unknown> = {};
  const thunks: Array<[string, () => unknown]> = [];
  let ref: unknown;
  for (const key in raw) {
    if (key === 'children' || key === 'key') continue;
    const value = raw[key];
    if (key === 'ref') ref = value;
    else if (isThunk(key, value)) thunks.push([key, value]);
    else props[key] = value;
  }

  let inst: Instance | null = null;
  let dispose: () => void = () => {};
  let initError: { err: unknown } | null = null;
  runtime.createRoot((d) => {
    dispose = d;
    for (const [key, thunk] of thunks) {
      if (initError) break;
      let first = true;
      const [retry, setRetry] = runtime.createSignal(0);
      runtime.createEffect(() => {
        retry();
        let value: unknown;
        try {
          value = thunk();
        } catch (err) {
          // On first mount the error (or suspension) propagates to the
          // enclosing region or Suspense boundary. Later, a prop that
          // suspends keeps its last value and is read again once the data
          // settles.
          if (first) {
            initError = { err };
          } else if (isPromiseLike(err)) {
            err.then(() => setRetry((n) => n + 1), () => setRetry((n) => n + 1));
          } else {
            console.error(err);
          }
          return;
        }
        if (first) {
          props[key] = value;
          return;
        }
        runtime.untrack(() => updateProp(inst!, key, value));
      });
      first = false;
    }
  });
  if (initError) {
    dispose();
    throw (initError as { err: unknown }).err;
  }

  try {
    validateInstance(type, props);
    if (type === 'primitive' && (props.object as { __r3f?: unknown }).__r3f) delete (props.object as { __r3f?: unknown }).__r3f;
    inst = prepare(undefined as unknown, ctx.store, type, props);
    inst.object = createObject(inst) as Instance['object'];
    inst.object.__r3f = inst;
  } catch (err) {
    dispose();
    throw err;
  }

  const before = nextInstance(slot);
  inst.parent = parent;
  const at = before ? parent.children.indexOf(before) : -1;
  if (at !== -1) parent.children.splice(at, 0, inst);
  else parent.children.push(inst);
  slot.inst = inst;

  const meta: Meta = { slots: [], dispose, releaseRef: () => {}, ref };
  metas.set(inst, meta);
  applyProps(inst.object, inst.props);
  link(parent, inst, before);
  meta.releaseRef = setRef(ref, inst.object);
  mountChildren(node.children, inst, meta.slots, null, ctx);
  invalidateInstance(inst);
}

function updateProp(inst: Instance, key: string, value: unknown): void {
  if (!metas.has(inst)) return;
  if (RECONSTRUCT_PROPS.includes(key)) {
    if (!needsReconstruct(inst, key, value)) return;
    inst.props[key] = value;
    reconstruct(inst);
    return;
  }
  const next = { ...inst.props, [key]: value };
  const changes = diffProps(inst, next as Instance['props']);
  // Only the changed key (and its pierced children) is updated here.
  const scoped: Record<string, unknown> = {};
  for (const k in changes as Record<string, unknown>) {
    if (k === key || k.startsWith(`${key}-`)) scoped[k] = (changes as Record<string, unknown>)[k];
  }
  inst.props[key] = value;
  if (Object.keys(scoped).length) applyProps(inst.object, scoped as Instance['props']);
}

// Slot ordering

function firstInstance(slot: Slot): Instance | null {
  if (slot.inst) return slot.inst;
  if (slot.items) {
    for (const s of slot.items) {
      const f = firstInstance(s);
      if (f) return f;
    }
  }
  return null;
}

// The first mounted instance after `slot` among its parent's children.
function nextInstance(slot: Slot): Instance | null {
  let cur: Slot | null = slot;
  while (cur) {
    const list: Slot[] = cur.list;
    for (let i = list.indexOf(cur) + 1; i < list.length; i++) {
      const f = firstInstance(list[i]);
      if (f) return f;
    }
    cur = cur.up;
  }
  return null;
}

function collectInstances(slots: Slot[], out: Instance[]): Instance[] {
  for (const s of slots) {
    if (s.inst) out.push(s.inst);
    else if (s.items) collectInstances(s.items, out);
  }
  return out;
}

// Restores the order of a parent's instances (and their Object3D children)
// after a list moved rows. Objects added outside the scene description keep
// their positions.
function reorder(parent: Instance): void {
  const meta = metas.get(parent) ?? rootMetas.get(parent);
  if (!meta) return;
  const ordered = collectInstances(meta.slots, []);
  const managed = new Set(parent.children);
  const rest = parent.children.filter((c) => !ordered.includes(c));
  parent.children.length = 0;
  parent.children.push(...ordered.filter((c) => managed.has(c)), ...rest);
  if (!isObject3D(parent.object)) return;
  const objects = ordered.filter((c) => !c.props.attach && isObject3D(c.object)).map((c) => c.object as THREE.Object3D);
  const set = new Set(objects);
  const kids = parent.object.children;
  let j = 0;
  for (let i = 0; i < kids.length; i++) {
    if (set.has(kids[i])) kids[i] = objects[j++];
  }
}

// Mounting values

/** Instances created by mount() for a root container (scene or portal target). */
const rootMetas = new WeakMap<Instance, Meta>();

function mountChildren(children: unknown, parent: Instance, list: Slot[], up: Slot | null, ctx: Ctx): void {
  if (Array.isArray(children)) {
    for (const c of children) mountChildren(c, parent, list, up, ctx);
    return;
  }
  mountValue(children, parent, list, up, ctx);
}

function newSlot(list: Slot[], up: Slot | null, container: boolean): Slot {
  const slot: Slot = container ? { list, up, items: [] } : { list, up };
  list.push(slot);
  return slot;
}

function mountValue(value: unknown, parent: Instance, list: Slot[], up: Slot | null, ctx: Ctx): void {
  if (value == null || typeof value === 'boolean') return;
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'bigint') {
    if (String(value).trim() !== '') console.warn(`@krate/three: text is not allowed in the scene ("${String(value)}")`);
    return;
  }
  if (Array.isArray(value)) {
    for (const v of value) mountValue(v, parent, list, up, ctx);
    return;
  }
  if (typeof value === 'function') {
    mountRegion(value as () => unknown, parent, newSlot(list, up, true), ctx);
    return;
  }
  const v = value as { __k3?: number; __ka?: number; __kv?: number };
  if (v.__k3 === 1) {
    const el = value as ElementNode;
    if (el.type === SUSPENSE_TAG) {
      // The compiler emits <Suspense> inside a scene as this element.
      const fallback = el.props.fallback;
      mountSuspense(
        {
          __k3: 2,
          type: () => null,
          props: {
            fallback:
              typeof fallback === 'function' && (fallback as Function).length === 0
                ? rt().untrack(() => evaluate(ctx.store, fallback as () => unknown, ctx.contexts))
                : fallback,
            children: el.children,
          },
        },
        parent,
        newSlot(list, up, true),
        ctx,
      );
      return;
    }
    mountElement(el, parent, newSlot(list, up, false), ctx);
    return;
  }
  if (v.__k3 === 2) {
    mountComponent(value as ComponentNode, parent, newSlot(list, up, true), ctx);
    return;
  }
  if (v.__k3 === 3) {
    mountPortal(value as PortalNode, parent, newSlot(list, up, true), ctx);
    return;
  }
  if (v.__ka === 1 && typeof (value as ListValue).render === 'function') {
    mountList(value as ListValue, parent, newSlot(list, up, true), ctx);
    return;
  }
  if (v.__kv === 1) {
    const k = value as KrateVNode;
    mountElement({ __k3: 1, type: k.tag, props: k.props || {}, children: k.children }, parent, newSlot(list, up, false), ctx);
    return;
  }
  if (typeof Node !== 'undefined' && value instanceof Node) {
    throw new Error('@krate/three: DOM elements cannot be rendered inside the scene. Render them outside <Canvas>.');
  }
  throw new Error(`@krate/three: cannot render ${Object.prototype.toString.call(value)} inside the scene`);
}

/** Removes everything a slot mounted. `top` invalidates the frame once. */
function unmountSlot(slot: Slot, parent: Instance, disposeObjects: boolean | undefined, top: boolean): void {
  if (slot.inst) {
    removeInstance(parent, slot.inst, disposeObjects, top);
    slot.inst = undefined;
    return;
  }
  const items = slot.items ? slot.items.splice(0) : [];
  for (let i = items.length - 1; i >= 0; i--) unmountSlot(items[i], parent, disposeObjects, top);
  const teardown = slot.teardown;
  slot.teardown = undefined;
  teardown?.();
}

function removeInstance(parent: Instance, child: Instance, disposeObjects: boolean | undefined, top: boolean): void {
  const meta = metas.get(child);
  const wasLinked = !!child.parent;
  child.parent = null;
  const idx = parent.children.indexOf(child);
  if (idx !== -1) parent.children.splice(idx, 1);
  if (meta) {
    meta.dispose();
    meta.releaseRef();
  }
  if (child.props.attach) {
    detach(parent, child);
  } else if (isObject3D(child.object) && isObject3D(parent.object)) {
    parent.object.remove(child.object);
    removeInteractivity(findInitialRoot(child), child.object);
  }
  // dispose={null} keeps the object (and its subtree) alive.
  const shouldDispose = child.props.dispose !== null && disposeObjects !== false;
  if (meta) {
    const slots = meta.slots.splice(0);
    for (let i = slots.length - 1; i >= 0; i--) unmountSlot(slots[i], child, shouldDispose, false);
    metas.delete(child);
  }
  child.children.length = 0;
  delete child.object.__r3f;
  // Primitives belong to the user, and scenes cannot be disposed.
  if (shouldDispose && child.type !== 'primitive' && (child.object as { type?: string }).type !== 'Scene') {
    disposeOnIdle(child.object as { dispose?: () => void });
  }
  if (top && wasLinked) {
    const state = child.root?.getState?.();
    if (state && state.internal.frames === 0) state.invalidate();
  }
}

function suspends(err: unknown): err is PromiseLike<unknown> {
  return isPromiseLike(err);
}

// A child function: re-evaluated (and its content replaced) when the signals
// it reads change. A value that suspends renders nothing until it settles.
function mountRegion(fn: () => unknown, parent: Instance, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  let owner: unknown = null;
  let disposeRoot: () => void = () => {};
  const clear = (): void => {
    const items = slot.items!.splice(0);
    for (let i = items.length - 1; i >= 0; i--) unmountSlot(items[i], parent, undefined, true);
    if (owner) runtime.krateUnmount(owner);
    owner = null;
  };
  runtime.createRoot((dispose) => {
    disposeRoot = dispose;
    const [retry, setRetry] = runtime.createSignal(0);
    runtime.createEffect(() => {
      retry();
      let value: unknown;
      let created: unknown = null;
      try {
        const r = withOwner(ctx.owner, () => runtime.krateWithOwner(() => evaluate(ctx.store, fn, ctx.contexts)));
        value = r.value;
        created = r.owner;
      } catch (err) {
        runtime.untrack(clear);
        if (suspends(err)) {
          err.then(() => setRetry((n) => n + 1), () => setRetry((n) => n + 1));
          return;
        }
        throw err;
      }
      runtime.untrack(() => {
        clear();
        owner = created;
        mountBoundary(() => mountValue(value, parent, slot.items!, slot, { store: ctx.store, owner: created, contexts: ctx.contexts }), slot, parent, () =>
          setRetry((n) => n + 1),
        );
      });
    });
  });
  slot.teardown = () => {
    disposeRoot();
    if (owner) runtime.krateUnmount(owner);
    owner = null;
  };
}

// Mounts content; when it suspends, removes what was mounted and calls retry
// once the data settles.
function mountBoundary(mount: () => void, slot: Slot, parent: Instance, retry: () => void): void {
  try {
    mount();
  } catch (err) {
    const items = slot.items!.splice(0);
    for (let i = items.length - 1; i >= 0; i--) unmountSlot(items[i], parent, undefined, true);
    if (suspends(err)) {
      err.then(retry, retry);
      return;
    }
    throw err;
  }
}

function isSuspense(type: unknown): boolean {
  return typeof type === 'function' && (type === (globalThis as { Suspense?: unknown }).Suspense || (type as { __krateThreeSuspense?: boolean }).__krateThreeSuspense === true);
}

function mountComponent(node: ComponentNode, parent: Instance, slot: Slot, ctx: Ctx): void {
  if (isSuspense(node.type)) {
    mountSuspense(node, parent, slot, ctx);
    return;
  }
  const provided = providedContext(node.type);
  if (provided) {
    // A provider inside the scene: its value is read live by the subtree.
    const props = node.props;
    const contexts: ContextEntry[] = [...ctx.contexts, [provided, () => props.value]];
    mountValue(props.children, parent, slot.items!, slot, { ...ctx, contexts });
    return;
  }
  const runtime = rt();
  let owner: unknown = null;
  let disposeRoot: () => void = () => {};
  slot.teardown = () => {
    disposeRoot();
    if (owner) runtime.krateUnmount(owner);
    owner = null;
  };
  const value = runtime.createRoot((dispose) => {
    disposeRoot = dispose;
    // Component bodies run once; their reactive reads live in thunks.
    return runtime.untrack(() =>
      withOwner(ctx.owner, () => {
        const r = runtime.krateWithOwner(() => evaluate(ctx.store, () => node.type(node.props), ctx.contexts));
        owner = r.owner;
        return r.value;
      }),
    );
  });
  mountValue(value, parent, slot.items!, slot, { store: ctx.store, owner, contexts: ctx.contexts });
}

// <Suspense fallback> inside the scene: children that suspend while mounting
// are replaced by the fallback until their data settles.
function mountSuspense(node: ComponentNode, parent: Instance, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  let disposed = false;
  const fallback = slot.items!;
  const content: Slot = { list: slot.items!, up: slot, items: [] };
  const showFallback = (): void => {
    for (const s of slot.items!.splice(0)) unmountSlot(s, parent, undefined, true);
    const f: Slot = { list: slot.items!, up: slot, items: [] };
    slot.items!.push(f);
    mountValue(node.props.fallback, parent, f.items!, f, ctx);
  };
  const attempt = (): void => {
    if (disposed) return;
    for (const s of slot.items!.splice(0)) unmountSlot(s, parent, undefined, true);
    content.items = [];
    slot.items!.push(content);
    try {
      mountValue(node.props.children, parent, content.items, content, ctx);
    } catch (err) {
      if (suspends(err)) {
        runtime.untrack(showFallback);
        err.then(attempt, attempt);
        return;
      }
      throw err;
    }
  };
  void fallback;
  slot.teardown = () => {
    disposed = true;
  };
  attempt();
}

function mountList(list: ListValue, parent: Instance, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  interface Row {
    key: string;
    item: unknown;
    slot: Slot;
    setItem: ((v: unknown) => void) | null;
    setIndex: ((v: number) => void) | null;
    owner: unknown;
    dispose: () => void;
  }
  const accessor = list.mode === 1;
  const usesIndex = list.render.length > 1;
  let rows = new Map<string, Row>();

  const renderRow = (row: Row, item: unknown, index: number): void => {
    runtime.createRoot((dispose) => {
      row.dispose = dispose;
      let itemArg: unknown = item;
      let indexArg: unknown = index;
      if (accessor) {
        const [getItem, setItem] = runtime.createSignal(item);
        row.setItem = (v) => setItem(() => v);
        itemArg = getItem;
        if (usesIndex) {
          const [getIndex, setIndex] = runtime.createSignal(index);
          row.setIndex = setIndex;
          indexArg = getIndex;
        }
      }
      const r = runtime.untrack(() =>
        withOwner(ctx.owner, () => runtime.krateWithOwner(() => evaluate(ctx.store, () => list.render(itemArg, indexArg), ctx.contexts))),
      );
      row.owner = r.owner;
      mountValue(r.value, parent, row.slot.items!, row.slot, { store: ctx.store, owner: r.owner, contexts: ctx.contexts });
    });
  };

  const removeRow = (row: Row): void => {
    unmountSlot(row.slot, parent, undefined, true);
    row.dispose();
    if (row.owner) runtime.krateUnmount(row.owner);
  };

  const update = (items: unknown[]): void => {
    const next = new Map<string, Row>();
    const order: Row[] = [];
    const seen = new Map<string, number>();
    items.forEach((item, i) => {
      let key = list.key ? String(list.key(item, i)) : String(i);
      const n = seen.get(key) ?? 0;
      seen.set(key, n + 1);
      if (n > 0) key = `${key}\u0000${n}`;
      let row = rows.get(key);
      if (row && !accessor && row.item !== item) {
        removeRow(row);
        row = undefined;
      }
      if (!row) {
        row = { key, item, slot: { list: slot.items!, up: slot, items: [] }, setItem: null, setIndex: null, owner: null, dispose: () => {} };
        (row as Row & { fresh?: boolean }).fresh = true;
      } else {
        rows.delete(key);
        row.item = item;
        row.setItem?.(item);
        row.setIndex?.(i);
      }
      next.set(key, row);
      order.push(row);
    });
    for (const row of rows.values()) removeRow(row);
    rows = next;
    slot.items!.length = 0;
    for (const row of order) slot.items!.push(row.slot);
    let moved = false;
    order.forEach((row, i) => {
      const fresh = (row as Row & { fresh?: boolean }).fresh;
      if (fresh) {
        delete (row as Row & { fresh?: boolean }).fresh;
        renderRow(row, row.item, i);
      } else {
        moved = true;
      }
    });
    if (moved) reorder(parent);
  };

  let disposeRoot: () => void = () => {};
  runtime.createRoot((dispose) => {
    disposeRoot = dispose;
    runtime.createEffect(() => {
      const raw = list.items();
      const items = Array.isArray(raw) ? raw : raw == null ? [] : Array.from(raw as Iterable<unknown>);
      runtime.untrack(() => update(items));
    });
  });
  slot.teardown = () => {
    disposeRoot();
    for (const row of rows.values()) {
      row.dispose();
      if (row.owner) runtime.krateUnmount(row.owner);
    }
    rows.clear();
  };
}

// Portals

type PortalOptions = {
  parentStore: RootStore;
  container: THREE.Object3D;
  raycaster: THREE.Raycaster;
  pointer: THREE.Vector2;
  events?: Record<string, unknown>;
  size?: Partial<RootState['size']>;
};

function inject(parentState: RootState, portalState: Partial<RootState>, options: PortalOptions): RootState {
  const { parentStore, container, raycaster, pointer, events, size } = options;
  let viewport: Partial<RootState['viewport']> | undefined;
  if (portalState.camera && size) {
    const camera = portalState.camera;
    viewport = parentState.viewport.getCurrentViewport(camera, new THREE.Vector3(), size as RootState['size']);
    if (camera !== parentState.camera) {
      // updateCamera without importing the store module's helpers twice.
      const c = camera as THREE.PerspectiveCamera & THREE.OrthographicCamera & { manual?: boolean };
      if (!c.manual) {
        if (c.isOrthographicCamera) {
          c.left = size.width! / -2;
          c.right = size.width! / 2;
          c.top = size.height! / 2;
          c.bottom = size.height! / -2;
        } else {
          c.aspect = size.width! / size.height!;
        }
        c.updateProjectionMatrix();
      }
    }
  }
  return {
    ...parentState,
    ...portalState,
    scene: container as THREE.Scene,
    raycaster,
    pointer,
    mouse: pointer,
    previousRoot: parentStore,
    events: { ...parentState.events, ...portalState.events, ...events },
    size: { ...parentState.size, ...size },
    viewport: { ...parentState.viewport, ...viewport },
  } as RootState;
}

let createPortalStore: ((init: (set: any, get: any) => any) => RootStore) | null = null;

/** Registers the store factory (set by root.ts to avoid an import cycle). */
export function setPortalStoreFactory(factory: (init: (set: any, get: any) => any) => RootStore): void {
  createPortalStore = factory;
}

/**
 * Creates the store of a portal: the parent's state with the portal's own
 * container (as `scene`), raycaster, pointer and the `state` overrides
 * layered over it, kept in sync with the parent. Returns the store and the
 * function that stops following the parent.
 */
export function createPortalState(
  parentStore: RootStore,
  container: THREE.Object3D,
  state?: Record<string, unknown>,
): { store: RootStore; unsubscribe: () => void } {
  const { events, size, ...rest } = (state ?? {}) as Record<string, any>;
  const options: PortalOptions = {
    parentStore,
    container,
    raycaster: new THREE.Raycaster(),
    pointer: new THREE.Vector2(),
    events,
    size,
  };
  const portalStore = createPortalStore!((set: any, get: any) => ({
    ...rest,
    set,
    get,
    setEvents: (e: Record<string, unknown>) => set((s: RootState) => ({ ...s, events: { ...s.events, ...e } })),
  }));
  portalStore.setState((portalState) => inject(parentStore.getState(), portalState, options));
  const unsubscribe = parentStore.subscribe((parentState) => {
    portalStore.setState((portalState) => inject(parentState, portalState, options));
  });
  return { store: portalStore, unsubscribe };
}

function mountPortal(node: PortalNode, parent: Instance, slot: Slot, ctx: Ctx): void {
  const { store: portalStore, unsubscribe } = createPortalState(ctx.store, node.container, node.state);

  // Children attach to the container through a stand-in instance, so a
  // container that is itself in the scene keeps its own descriptor.
  const host: Instance = {
    root: portalStore,
    type: '',
    parent,
    children: [],
    props: {} as Instance['props'],
    object: node.container as Instance['object'],
    eventCount: 0,
    handlers: {},
    previousVisible: undefined,
    isHidden: false,
  };
  const meta: Meta = { slots: [], dispose: () => {}, releaseRef: () => {}, ref: null };
  metas.set(host, meta);
  mountChildren(node.children, host, meta.slots, null, { store: portalStore, owner: ctx.owner, contexts: ctx.contexts });
  slot.teardown = () => {
    unsubscribe();
    const slots = meta.slots.splice(0);
    for (let i = slots.length - 1; i >= 0; i--) unmountSlot(slots[i], host, undefined, true);
    metas.delete(host);
  };
}

/**
 * Renders a portal: children render into `container` (any Object3D) with
 * their own layered state (camera, events, size) injected over the parent's.
 */
export function createPortal(children: unknown, container: THREE.Object3D, state?: Record<string, unknown>): PortalNode {
  return { __k3: 3, children, container, state };
}

// Roots

/** The mounted content of a root container (the scene). */
export interface MountedTree {
  unmount: () => void;
}

/**
 * Mounts `children` (the compiled children function, or any renderable
 * value) into `container`, the scene's instance.
 */
export function mountTree(children: unknown, container: Instance, store: RootStore): MountedTree {
  const meta: Meta = { slots: [], dispose: () => {}, releaseRef: () => {}, ref: null };
  rootMetas.set(container, meta);
  metas.set(container, meta);
  const runtime = rt();
  const owner = runtime.krateGetOwner ? runtime.krateGetOwner() : null;
  // Providers around the renderer component, captured by the compiler
  // (krateContextRows), stay visible to the deferred parts of the scene.
  const contexts = ((children as { __kctx?: ContextEntry[] } | null)?.__kctx ?? []).slice();
  const ctx: Ctx = { store, owner, contexts };
  if (typeof children === 'function') {
    mountRegion(children as () => unknown, container, newSlot(meta.slots, null, true), ctx);
  } else {
    const slot = newSlot(meta.slots, null, true);
    mountBoundary(() => mountChildren(children, container, slot.items!, slot, ctx), slot, container, () => {
      unmountSlot(slot, container, undefined, true);
      mountChildren(children, container, slot.items!, slot, ctx);
    });
  }
  return {
    unmount() {
      const slots = meta.slots.splice(0);
      for (let i = slots.length - 1; i >= 0; i--) unmountSlot(slots[i], container, undefined, true);
      rootMetas.delete(container);
      metas.delete(container);
    },
  };
}
