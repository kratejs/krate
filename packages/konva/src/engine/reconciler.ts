// Builds and updates Konva nodes from compiled Krate JSX.
//
// <Stage> is a renderer component (`@renderer`): it receives its children as
// a function compiled in the reactive h() form. The react-konva elements are
// string constants used as components (`Rect === 'Rect'`), so they compile to
// `h(Rect, {get x() { return x(); }, onClick, ref}, ...children)`: a reactive
// prop is a getter, a function prop (event handler, dragBoundFunc, sceneFunc)
// is passed as is, a reactive child is a function and a `.map()` is a
// `krateList(...)` value.
//
// While the engine evaluates that code it installs its own `h`, which returns
// lazy descriptors. Mounting walks them: an element becomes a Konva node (its
// props applied with react-konva's rules, added to its parent in order), a
// component runs once with its own owner, a function child becomes a region
// re-evaluated when the signals it reads change, and a list reconciles rows by
// key. Every getter prop is read in its own effect and applied in place.

import { applyNodeProps, createNode, destroyNode, EVENTS_NAMESPACE, insertNode, updatePicture, type KonvaContainer, type KonvaNode } from './props.js';
import { rt, setRef } from './runtime.js';

/** An element descriptor (the result of `h(Rect, ...)` in a stage). */
interface ElementNode {
  __kk: 1;
  type: string;
  props: Record<string, unknown>;
  children: unknown[];
}

/** A component call descriptor (`h(Handles, ...)` in a stage). */
interface ComponentNode {
  __kk: 2;
  type: (props: Record<string, unknown>) => unknown;
  props: Record<string, unknown>;
}

/** A compiled `.map()` (krateList) exposes its arguments to custom renderers. */
interface ListValue {
  __ka: 1;
  items: () => unknown;
  key: ((item: unknown, index: number) => unknown) | null;
  render: (item: unknown, index: unknown) => unknown;
  mode: number;
}

/**
 * A position in a parent node's child list. A node slot holds one node; a
 * container slot (component, region, list, list row, boundary) holds the
 * slots it produced, in order.
 */
interface Slot {
  list: Slot[];
  up: Slot | null;
  node?: KonvaNode;
  items?: Slot[];
  teardown?: () => void;
}

type ContextEntry = [unknown, () => unknown];

/** Where values mount: the owner and the active providers. */
interface Ctx {
  owner: unknown;
  contexts: ContextEntry[];
}

interface Meta {
  slots: Slot[];
  /** The props applied so far (the base of the next update). */
  props: Record<string, unknown>;
  dispose: () => void;
  releaseRef: () => void;
}

const metas = new WeakMap<KonvaNode, Meta>();

/** The element the compiler emits for `<Suspense>` inside a renderer. */
const SUSPENSE_TAG = 'krate-suspense';

function stageH(tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]): unknown {
  if (typeof tag === 'function') {
    const p = props || {};
    if (children.length > 0) p.children = children.length === 1 ? children[0] : children;
    return { __kk: 2, type: tag as ComponentNode['type'], props: p } satisfies ComponentNode;
  }
  return { __kk: 1, type: String(tag), props: props || {}, children } satisfies ElementNode;
}

function evaluate<T>(fn: () => T, contexts: ContextEntry[]): T {
  const g = globalThis as unknown as {
    h?: unknown;
    krateWithContexts?: <R>(entries: ContextEntry[], fn: () => R) => R;
  };
  const prevH = g.h;
  g.h = stageH;
  try {
    return contexts.length && typeof g.krateWithContexts === 'function' ? g.krateWithContexts(contexts, fn) : fn();
  } finally {
    g.h = prevH;
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

function isPromiseLike(v: unknown): v is PromiseLike<unknown> {
  return v != null && typeof (v as { then?: unknown }).then === 'function';
}

// Elements

function mountElement(el: ElementNode, parent: KonvaContainer, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  const raw = el.props;
  const props: Record<string, unknown> = {};
  const getters: Array<[string, () => unknown]> = [];
  for (const key of Object.keys(raw)) {
    if (key === 'children' || key === 'key') continue;
    const desc = Object.getOwnPropertyDescriptor(raw, key)!;
    if (desc.get) getters.push([key, desc.get.bind(raw)]);
    else props[key] = desc.value;
  }

  let node: KonvaNode | null = null;
  let meta: Meta | null = null;
  let dispose: () => void = () => {};
  let initError: { err: unknown } | null = null;
  runtime.createRoot((d) => {
    dispose = d;
    for (const [key, get] of getters) {
      if (initError) break;
      let first = true;
      runtime.createEffect(() => {
        let value: unknown;
        try {
          value = get();
        } catch (err) {
          if (first) initError = { err };
          else console.error(err);
          return;
        }
        if (first) props[key] = value;
        else runtime.untrack(() => updateProp(node!, key, value));
      });
      first = false;
    }
  });
  if (initError) {
    dispose();
    throw (initError as { err: unknown }).err;
  }

  try {
    node = createNode(el.type, props);
  } catch (err) {
    dispose();
    throw err;
  }
  const { ref, ...applied } = props;
  meta = { slots: [], props: applied, dispose, releaseRef: () => {} };
  metas.set(node, meta);
  slot.node = node;
  insertNode(parent, node, nextNode(slot));
  if (el.children.length) {
    if (typeof (node as KonvaContainer).add !== 'function' || !(node as KonvaContainer).children) {
      console.error(`@krate/konva: ${el.type} cannot have children`);
    } else {
      mountChildren(el.children, node as KonvaContainer, meta.slots, null, ctx);
    }
  }
  // As in React, refs are attached once the subtree is built.
  meta.releaseRef = setRef(ref, node);
}

function updateProp(node: KonvaNode, key: string, value: unknown): void {
  const meta = metas.get(node);
  if (!meta) return;
  if (key === 'ref') {
    meta.releaseRef();
    meta.releaseRef = setRef(value, node);
    return;
  }
  const next = { ...meta.props, [key]: value };
  applyNodeProps(node, next, meta.props);
  meta.props = next;
}

// Slot ordering

function firstNode(slot: Slot): KonvaNode | null {
  if (slot.node) return slot.node;
  if (slot.items) {
    for (const s of slot.items) {
      const f = firstNode(s);
      if (f) return f;
    }
  }
  return null;
}

// The first mounted node after `slot` among its parent's children.
function nextNode(slot: Slot): KonvaNode | null {
  let cur: Slot | null = slot;
  while (cur) {
    const list: Slot[] = cur.list;
    for (let i = list.indexOf(cur) + 1; i < list.length; i++) {
      const f = firstNode(list[i]);
      if (f) return f;
    }
    cur = cur.up;
  }
  return null;
}

function collectNodes(slots: Slot[], out: KonvaNode[]): KonvaNode[] {
  for (const s of slots) {
    if (s.node) out.push(s.node);
    else if (s.items) collectNodes(s.items, out);
  }
  return out;
}

// Restores the order of a parent's nodes after a list moved rows, in the
// positions they already take (nodes added outside the renderer stay put).
function reorder(parent: KonvaContainer): void {
  const meta = metas.get(parent as KonvaNode);
  if (!meta) return;
  const ordered = collectNodes(meta.slots, []).filter((n) => n.parent === parent);
  const positions = ordered.map((n) => n.getZIndex()).sort((a, b) => a - b);
  ordered.forEach((n, i) => {
    if (n.getZIndex() !== positions[i]) n.setZIndex(positions[i]);
  });
  updatePicture(parent);
}

// Mounting values

function mountChildren(children: unknown, parent: KonvaContainer, list: Slot[], up: Slot | null, ctx: Ctx): void {
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

function mountValue(value: unknown, parent: KonvaContainer, list: Slot[], up: Slot | null, ctx: Ctx): void {
  if (value == null || typeof value === 'boolean') return;
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'bigint') {
    if (String(value).trim() !== '') console.error(`Text components are not supported for now in ReactKonva. Your text is: "${String(value)}"`);
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
  const v = value as { __kk?: number; __ka?: number };
  if (v.__kk === 1) {
    const el = value as ElementNode;
    if (el.type === SUSPENSE_TAG) {
      const fallback = el.props.fallback;
      mountSuspense(
        typeof fallback === 'function' && (fallback as Function).length === 0
          ? rt().untrack(() => evaluate(fallback as () => unknown, ctx.contexts))
          : fallback,
        el.children,
        parent,
        newSlot(list, up, true),
        ctx,
      );
      return;
    }
    mountElement(el, parent, newSlot(list, up, false), ctx);
    return;
  }
  if (v.__kk === 2) {
    mountComponent(value as ComponentNode, parent, newSlot(list, up, true), ctx);
    return;
  }
  if (v.__ka === 1 && typeof (value as ListValue).render === 'function') {
    mountList(value as ListValue, parent, newSlot(list, up, true), ctx);
    return;
  }
  if (typeof Node !== 'undefined' && value instanceof Node) {
    throw new Error('@krate/konva: DOM elements cannot be rendered inside the Stage. Render them next to it.');
  }
  throw new Error(`@krate/konva: cannot render ${Object.prototype.toString.call(value)} inside the Stage`);
}

/** Removes everything a slot mounted; `top` destroys the nodes (descendants go with their parent). */
function unmountSlot(slot: Slot, top: boolean): void {
  if (slot.node) {
    removeNode(slot.node, top);
    slot.node = undefined;
    return;
  }
  const items = slot.items ? slot.items.splice(0) : [];
  for (let i = items.length - 1; i >= 0; i--) unmountSlot(items[i], top);
  const teardown = slot.teardown;
  slot.teardown = undefined;
  teardown?.();
}

function removeNode(node: KonvaNode, top: boolean): void {
  const meta = metas.get(node);
  if (meta) {
    meta.dispose();
    const slots = meta.slots.splice(0);
    for (let i = slots.length - 1; i >= 0; i--) unmountSlot(slots[i], false);
    meta.releaseRef();
    metas.delete(node);
  }
  if (top) destroyNode(node);
  else node.off(EVENTS_NAMESPACE);
}

function clearItems(slot: Slot): void {
  const items = slot.items!.splice(0);
  for (let i = items.length - 1; i >= 0; i--) unmountSlot(items[i], true);
}

// A child function: re-evaluated (and its content replaced) when the signals
// it reads change. A value that suspends renders nothing until it settles.
function mountRegion(fn: () => unknown, parent: KonvaContainer, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  let owner: unknown = null;
  let disposeRoot: () => void = () => {};
  const clear = (): void => {
    clearItems(slot);
    if (owner) runtime.krateUnmount(owner);
    owner = null;
  };
  runtime.createRoot((dispose) => {
    disposeRoot = dispose;
    const [retry, setRetry] = runtime.createSignal(0);
    const again = (): void => setRetry((n) => n + 1);
    runtime.createEffect(() => {
      retry();
      let value: unknown;
      let created: unknown = null;
      try {
        const r = withOwner(ctx.owner, () => runtime.krateWithOwner(() => evaluate(fn, ctx.contexts)));
        value = r.value;
        created = r.owner;
      } catch (err) {
        runtime.untrack(clear);
        if (isPromiseLike(err)) {
          err.then(again, again);
          return;
        }
        throw err;
      }
      runtime.untrack(() => {
        clear();
        owner = created;
        try {
          mountValue(value, parent, slot.items!, slot, { owner: created, contexts: ctx.contexts });
        } catch (err) {
          clearItems(slot);
          if (isPromiseLike(err)) err.then(again, again);
          else throw err;
        }
      });
    });
  });
  slot.teardown = () => {
    disposeRoot();
    if (owner) runtime.krateUnmount(owner);
    owner = null;
  };
}

function mountComponent(node: ComponentNode, parent: KonvaContainer, slot: Slot, ctx: Ctx): void {
  const provided = providedContext(node.type);
  if (provided) {
    // A provider inside the stage: its value is read live by the subtree.
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
    // Component bodies run once; their reactive reads live in getters and regions.
    return runtime.untrack(() =>
      withOwner(ctx.owner, () => {
        const r = runtime.krateWithOwner(() => evaluate(() => node.type(node.props), ctx.contexts));
        owner = r.owner;
        return r.value;
      }),
    );
  });
  mountValue(value, parent, slot.items!, slot, { owner, contexts: ctx.contexts });
}

// <Suspense fallback> inside the stage: children that suspend while mounting
// are replaced by the fallback until their data settles.
function mountSuspense(fallback: unknown, children: unknown, parent: KonvaContainer, slot: Slot, ctx: Ctx): void {
  let disposed = false;
  const attempt = (): void => {
    if (disposed) return;
    clearItems(slot);
    const content: Slot = { list: slot.items!, up: slot, items: [] };
    slot.items!.push(content);
    try {
      mountChildren(children, parent, content.items!, content, ctx);
    } catch (err) {
      if (!isPromiseLike(err)) throw err;
      rt().untrack(() => {
        clearItems(slot);
        const f: Slot = { list: slot.items!, up: slot, items: [] };
        slot.items!.push(f);
        mountValue(fallback, parent, f.items!, f, ctx);
      });
      err.then(attempt, attempt);
    }
  };
  slot.teardown = () => {
    disposed = true;
  };
  attempt();
}

function mountList(list: ListValue, parent: KonvaContainer, slot: Slot, ctx: Ctx): void {
  const runtime = rt();
  interface Row {
    item: unknown;
    slot: Slot;
    setItem: ((v: unknown) => void) | null;
    setIndex: ((v: number) => void) | null;
    owner: unknown;
    dispose: () => void;
    fresh?: boolean;
  }
  const accessor = list.mode === 1;
  const usesIndex = list.render.length > 1;
  let rows = new Map<string, Row>();

  const renderRow = (row: Row, index: number): void => {
    runtime.createRoot((dispose) => {
      row.dispose = dispose;
      let itemArg: unknown = row.item;
      let indexArg: unknown = index;
      if (accessor) {
        const [getItem, setItem] = runtime.createSignal(row.item);
        row.setItem = (v) => setItem(() => v);
        itemArg = getItem;
        if (usesIndex) {
          const [getIndex, setIndex] = runtime.createSignal(index);
          row.setIndex = setIndex;
          indexArg = getIndex;
        }
      }
      const r = runtime.untrack(() =>
        withOwner(ctx.owner, () => runtime.krateWithOwner(() => evaluate(() => list.render(itemArg, indexArg), ctx.contexts))),
      );
      row.owner = r.owner;
      mountValue(r.value, parent, row.slot.items!, row.slot, { owner: r.owner, contexts: ctx.contexts });
    });
  };

  const removeRow = (row: Row): void => {
    unmountSlot(row.slot, true);
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
        rows.delete(key);
        removeRow(row);
        row = undefined;
      }
      if (!row) {
        row = { item, slot: { list: slot.items!, up: slot, items: [] }, setItem: null, setIndex: null, owner: null, dispose: () => {}, fresh: true };
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
    let kept = false;
    order.forEach((row, i) => {
      if (row.fresh) {
        row.fresh = false;
        renderRow(row, i);
      } else {
        kept = true;
      }
    });
    if (kept) reorder(parent);
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

/** The mounted content of a container. */
export interface MountedTree {
  unmount: () => void;
}

/**
 * Mounts `children` (the compiled children function, or any renderable
 * value) into `container` (the Stage, or any Konva container).
 */
export function mountTree(children: unknown, container: KonvaContainer): MountedTree {
  const meta: Meta = { slots: [], props: {}, dispose: () => {}, releaseRef: () => {} };
  metas.set(container as KonvaNode, meta);
  const runtime = rt();
  const owner = runtime.krateGetOwner ? runtime.krateGetOwner() : null;
  // Providers around the renderer component, captured by the compiler
  // (krateContextRows), stay visible to the deferred parts of the stage.
  const contexts = ((children as { __kctx?: ContextEntry[] } | null)?.__kctx ?? []).slice();
  const ctx: Ctx = { owner, contexts };
  if (typeof children === 'function') mountRegion(children as () => unknown, container, newSlot(meta.slots, null, true), ctx);
  else mountChildren(children, container, meta.slots, null, ctx);
  return {
    unmount() {
      const slots = meta.slots.splice(0);
      for (let i = slots.length - 1; i >= 0; i--) unmountSlot(slots[i], true);
      metas.delete(container as KonvaNode);
    },
  };
}
