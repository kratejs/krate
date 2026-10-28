// React-style effect scheduling on top of signals.
//
// Krate components run once, so there is no render to hang effects on.
// Instead each lowered useEffect/useLayoutEffect/useInsertionEffect is a
// "hook effect": a tiny tracker effect re-reads the dependency thunk whenever a
// signal it reads changes and, when the dependency array differs (Object.is
// per element), marks the hook dirty. Dirty hooks run in a commit that follows
// the signal flush, after the DOM bindings have been updated:
//
//   mutation  per component (children before parents): insertion cleanup,
//             insertion setup, layout cleanup
//   layout    layout setups, children before parents
//   passive   (a macrotask later, after paint) all passive cleanups, then all
//             passive setups, children before parents
//
// A pending passive phase is always flushed before the next update is
// processed, as React does. Unmounting a component runs its cleanups parent before child:
// insertion and layout cleanups immediately, passive cleanups in the passive
// phase. Component boundaries come from owners: hydration scripts declare
// them with krateOwner and runtime-created components get one from h().

import {
  createEffect,
  krateGetOwner,
  krateSetOwner,
  krateOnAfterFlush,
  krateOnBeforeFlush,
  krateOnDispose,
  kratePendingFlush,
  runAfterFlush,
  untrack,
  krateCommitting,
  krateDisposeOwner,
  krateEpoch,
  kratePruneSnapshots,
  krateReadAt,
  krateTakeSyncLane,
  krateCurrentScope,
  krateTagOwner,
} from './signal.js';

/** Effect phase: React's useEffect, useLayoutEffect and useInsertionEffect. */
export type EffectPhase = 'passive' | 'layout' | 'insertion';

type Cleanup = (() => void) | null;

interface Owner {
  id: string | null;
  parent: Owner | null;
  children: Owner[];
  hooks: HookEffect[];
  disposed: boolean;
  /** False while the component sits in a hidden conditional branch. */
  shown: boolean;
  /** Conditional branches (slot id + branch) the component renders inside. */
  conds: Array<[string, string]>;
  /** Content slot whose replacement unmounts this hydrated component. */
  slot: string | null;
  /** Set for components mounted through krateMount (remountable). */
  record: MountRecord | null;
  /** Route scope (layout id) the component belongs to; null for page code. */
  scope: string | null;
}

/**
 * A remountable hydrated component (krateMount): its scope function runs on
 * every mount, creating fresh state, bindings and effects.
 */
interface MountRecord {
  id: string;
  parentId: string | null;
  conds: Array<[string, string]>;
  slot: string | null;
  fn: () => void;
  owner: Owner | null;
}

interface HookEffect {
  owner: Owner;
  phase: EffectPhase;
  fn: () => unknown;
  deps: (() => unknown) | null;
  prev: unknown[] | undefined;
  hasPrev: boolean;
  cleanup: Cleanup;
  /** True after setup ran and until its cleanup ran. */
  mounted: boolean;
  /** Commit whose state snapshot the last setup (and so its cleanup) saw. */
  epoch: number;
  dirty: boolean;
  seq: number;
  stop: () => void;
  /** Route scope of a hook registered without a component owner. */
  scope: string | null;
}

const root: Owner = newOwner(null, null);
const byId = new Map<string, Owner>();
// Last branch shown by each dynamic conditional slot, as reported by kbindCond.
const condShown = new Map<string, string>();
// Content slots whose server-rendered content was replaced on the client.
const replacedSlots = new Set<string>();

const dirty = new Set<HookEffect>();
let seq = 0;
let commitQueued = false;
let passiveTimer: ReturnType<typeof setTimeout> | null = null;
// Passive work of the last commit, run in order by flushPassive.
let passiveCleanups: Array<() => void> = [];
let passiveSetups: Array<[HookEffect, number]> = [];
const records: MountRecord[] = [];
// Mounted hooks per snapshot epoch, so old snapshots can be pruned.
const liveEpochs = new Map<number, number>();
const unmountHooks: Array<(owner: unknown, id: string | null) => void> = [];

function newOwner(id: string | null, parent: Owner | null): Owner {
  // Components inherit the route scope of their parent component; top-level
  // hydrated components take the scope their hydration code runs in.
  const scope = parent && parent.parent !== null ? parent.scope : parent ? krateCurrentScope() : null;
  const owner: Owner = { id, parent, children: [], hooks: [], disposed: false, shown: true, conds: [], slot: null, record: null, scope };
  if (parent) krateTagOwner(owner, scope);
  return owner;
}

/**
 * Registers fn to run whenever a component owner unmounts (parent first),
 * after its effect cleanups and before its bindings are disposed. Lets other
 * lowerings (class component lifecycles) observe unmount and remount.
 */
export function krateOnUnmount(fn: (owner: unknown, id: string | null) => void): void {
  unmountHooks.push(fn);
}

/** The id of a component owner (null for runtime-created components). */
export function krateOwnerId(owner: unknown): string | null {
  return ownerOf(owner).id;
}

function ownerOf(value: unknown): Owner {
  return value && typeof value === 'object' && 'hooks' in (value as Owner) ? (value as Owner) : root;
}

function isActive(owner: Owner): boolean {
  for (let o: Owner | null = owner; o; o = o.parent) {
    if (o.disposed || !o.shown) return false;
  }
  return true;
}

function condVisible(owner: Owner): boolean {
  for (const [id, branch] of owner.conds) {
    const shown = condShown.get(id);
    if (shown !== undefined && shown !== branch) return false;
  }
  return true;
}

/**
 * Starts a hydrated component scope (emitted at the top of each component's
 * hydration script). `parentId` names the enclosing component, `conds` the
 * conditional branches `[slotId, "c" | "a", initiallyShown]` it renders
 * inside, and `slot` the content slot (list) whose client-side replacement
 * unmounts it. `krateOwner(null)` ends hydration scopes.
 */
export function krateOwner(
  id: string | null,
  parentId?: string | null,
  conds?: Array<[string, string, number | boolean]>,
  slot?: string | null,
): void {
  if (id == null) {
    krateSetOwner(null);
    return;
  }
  let owner = byId.get(id);
  if (!owner) {
    const parent = (parentId != null && byId.get(parentId)) || root;
    owner = newOwner(id, parent);
    // A component inside an unmounted one is unmounted too.
    for (let o: Owner | null = parent; o; o = o.parent) {
      if (o.disposed) owner.disposed = true;
    }
    parent.children.push(owner);
    byId.set(id, owner);
    for (const [cid, branch, initially] of conds || []) {
      owner.conds.push([cid, branch]);
      if (!condShown.has(cid)) {
        // No binding reported this conditional yet: trust the server markup.
        condShown.set(cid, initially ? branch : branch === 'c' ? 'a' : 'c');
      }
    }
    owner.shown = condVisible(owner);
    if (slot) {
      owner.slot = slot;
      // The list was already re-rendered by its binding: this server-rendered
      // instance no longer exists, so its effects must never run.
      if (replacedSlots.has(slot)) owner.disposed = true;
    }
  }
  krateSetOwner(owner);
}

/** Runs fn as the body of a new child component of the current owner. */
export function krateWithOwner<T>(fn: () => T): { value: T; owner: unknown } {
  const parent = ownerOf(krateGetOwner());
  const owner = newOwner(null, parent);
  parent.children.push(owner);
  const prev = krateSetOwner(owner);
  try {
    return { value: fn(), owner };
  } finally {
    krateSetOwner(prev);
  }
}

/**
 * Unmounts a component created by krateWithOwner/krateOwner: cleanups run
 * parent before child (insertion and layout now, passive in the passive
 * phase) and its hooks never run again.
 */
export function krateUnmount(value: unknown): void {
  const owner = ownerOf(value);
  if (owner === root || owner.disposed) return;
  const wasActive = isActive(owner);
  const passive: Array<() => void> = [];
  disposeTree(owner, wasActive, passive);
  if (owner.parent) {
    const list = owner.parent.children;
    const i = list.indexOf(owner);
    if (i >= 0) list.splice(i, 1);
  }
  if (passive.length) {
    passiveCleanups.push(...passive);
    schedulePassive();
  }
}

function disposeTree(owner: Owner, active: boolean, passive: Array<() => void>): void {
  // Disposed owners stay in byId so hydration scopes declared later inside
  // them resolve their (unmounted) parent instead of the root.
  owner.disposed = true;
  for (const hook of owner.hooks) {
    hook.stop();
    dirty.delete(hook);
    if (!active) {
      // Hidden, so its cleanup already ran (or never will): just release it.
      if (hook.mounted) {
        hook.mounted = false;
        hook.cleanup = null;
        releaseEpoch(hook.epoch);
      }
      continue;
    }
    if (hook.phase === 'passive') {
      const c = takeCleanup(hook);
      if (c) passive.push(c);
    } else {
      runCleanup(hook);
    }
  }
  for (const fn of unmountHooks) fn(owner, owner.id);
  // Bindings, refs and top-level cleanups of the component go with it.
  krateDisposeOwner(owner);
  if (owner.record && owner.record.owner === owner) owner.record.owner = null;
  for (const child of owner.children.slice()) {
    disposeTree(child, active && child.shown, passive);
  }
}

/**
 * Unmounts the hydrated component `id` and forgets it, so running its
 * hydration scope again (a remount by the class/boundary owner tree in
 * boundary.ts) creates a fresh owner whose effects mount again.
 */
export function krateForgetOwner(id: string): void {
  const owner = byId.get(id);
  if (!owner) return;
  krateUnmount(owner);
  byId.delete(id);
}

/**
 * Declares a remountable hydrated component (emitted for components inside
 * a conditional branch). `fn` is the component's hydration scope; it runs now
 * when the branch is shown, and again, as a fresh instance, every time the
 * branch is shown after being hidden. Hiding the branch unmounts it:
 * cleanups run, refs and bindings are released and its state is discarded.
 */
export function krateMount(
  id: string,
  parentId: string | null,
  conds: Array<[string, string, number | boolean]> | null,
  slot: string | null,
  fn: () => void,
): void {
  const rec: MountRecord = { id, parentId, conds: [], slot: slot || null, fn, owner: null };
  for (const [cid, branch, initially] of conds || []) {
    rec.conds.push([cid, branch]);
    if (!condShown.has(cid)) {
      condShown.set(cid, initially ? branch : branch === 'c' ? 'a' : 'c');
    }
  }
  records.push(rec);
  if (recordVisible(rec)) mountRecord(rec);
}

function recordParent(rec: MountRecord): Owner | null {
  if (rec.parentId == null) return root;
  const p = byId.get(rec.parentId);
  if (!p) return root;
  for (let o: Owner | null = p; o; o = o.parent) {
    if (o.disposed) return null;
  }
  return p;
}

function recordVisible(rec: MountRecord): boolean {
  if (rec.slot && replacedSlots.has(rec.slot)) return false;
  for (const [cid, branch] of rec.conds) {
    const shown = condShown.get(cid);
    if (shown !== undefined && shown !== branch) return false;
  }
  return recordParent(rec) !== null;
}

function mountRecord(rec: MountRecord): void {
  const parent = recordParent(rec) || root;
  const owner = newOwner(rec.id, parent);
  owner.record = rec;
  owner.slot = rec.slot;
  parent.children.push(owner);
  byId.set(rec.id, owner);
  rec.owner = owner;
  const prev = krateSetOwner(owner);
  try {
    // The scope may run inside a binding (a conditional swap); its reads must
    // not subscribe that binding.
    untrack(rec.fn);
  } finally {
    krateSetOwner(prev);
  }
}

/**
 * Reports that the hydrated content of slot `id` was replaced by its binding
 * (a list re-render): hydrated components inside it are unmounted.
 */
export function krateSlotReplaced(id: string): void {
  if (replacedSlots.has(id)) return;
  replacedSlots.add(id);
  for (const owner of Array.from(byId.values())) {
    if (owner.slot === id) krateUnmount(owner);
  }
}

/**
 * Reports which branch ("c" consequent, "a" alternate) conditional slot `id`
 * shows. Components in the hidden branch keep their state but have their
 * effects cleaned up (React's Activity semantics); showing the branch again
 * re-runs every effect setup in the next commit.
 */
export function krateCondActivity(id: string, branch: string): void {
  if (condShown.get(id) === branch) return;
  condShown.set(id, branch);
  // Remountable components: unmount the ones that left (parents first), then
  // mount fresh instances of the ones that entered (parents first).
  for (const rec of records) {
    if (rec.owner && !rec.owner.disposed && !recordVisible(rec)) krateUnmount(rec.owner);
  }
  for (const rec of records) {
    if (!rec.owner && recordVisible(rec)) mountRecord(rec);
  }
  const affected: Owner[] = [];
  for (const owner of byId.values()) {
    if (!owner.record && owner.conds.some(([cid]) => cid === id)) affected.push(owner);
  }
  let passive: Array<() => void> = [];
  for (const owner of affected) {
    const visible = condVisible(owner);
    if (visible === owner.shown) continue;
    const before = isActive(owner);
    owner.shown = visible;
    const after = isActive(owner);
    if (before && !after) hideTree(owner, passive);
    else if (!before && after) showTree(owner);
  }
  if (passive.length) {
    passiveCleanups.push(...passive);
    passive = [];
    schedulePassive();
  }
  scheduleCommit();
}

function hideTree(owner: Owner, passive: Array<() => void>): void {
  for (const hook of owner.hooks) {
    dirty.delete(hook);
    if (hook.phase === 'passive') {
      const c = takeCleanup(hook);
      if (c) passive.push(c);
    } else {
      runCleanup(hook);
    }
  }
  for (const child of owner.children) {
    if (child.shown && !child.disposed) hideTree(child, passive);
  }
}

function showTree(owner: Owner): void {
  for (const hook of owner.hooks) {
    markDirty(hook);
  }
  for (const child of owner.children) {
    if (child.shown && !child.disposed) showTree(child);
  }
}

function depsEqual(a: unknown[] | undefined, b: unknown[] | undefined): boolean {
  if (!a || !b || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (!Object.is(a[i], b[i])) return false;
  }
  return true;
}

function toDeps(value: unknown): unknown[] | undefined {
  if (value == null) return undefined;
  return Array.isArray(value) ? value : Array.from(value as Iterable<unknown>);
}

function markDirty(hook: HookEffect): void {
  hook.dirty = true;
  dirty.add(hook);
  scheduleCommit();
}

function scheduleCommit(): void {
  if (commitQueued) return;
  commitQueued = true;
  queueMicrotask(() => {
    // A pending signal flush runs the commit itself once it settles.
    if (!kratePendingFlush()) {
      flushPassive();
      runAfterFlush();
    }
  });
}

/**
 * Registers a React effect. `deps` returns the dependency array (read inside
 * a tracking scope, so signal getters it calls are subscribed); the effect
 * re-runs in the next commit when any element changed by Object.is. A null
 * thunk means "no dependency array" and the effect runs only on mount here;
 * the compiler synthesizes the component's state and props as dependencies
 * for that case instead. `fn` may return a cleanup function.
 */
export function createEffectDeps(
  fn: () => unknown,
  deps?: (() => unknown) | null,
  phase: EffectPhase = 'passive',
): void {
  const owner = ownerOf(krateGetOwner());
  const hook: HookEffect = {
    owner,
    phase,
    fn,
    deps: deps || null,
    prev: undefined,
    hasPrev: false,
    cleanup: null,
    mounted: false,
    epoch: 0,
    dirty: false,
    seq: seq++,
    stop: () => {},
    scope: owner === root ? krateCurrentScope() : owner.scope,
  };
  owner.hooks.push(hook);
  if (owner.disposed) return;
  hook.stop = createEffect(() => {
    const next = hook.deps ? toDeps(hook.deps()) : [];
    if (hook.hasPrev && depsEqual(hook.prev, next)) return;
    hook.prev = next;
    hook.hasPrev = true;
    if (isActive(hook.owner)) markDirty(hook);
  });
}

function runSetup(hook: HookEffect, at: number): void {
  if (hook.owner.disposed) return;
  let ret: unknown;
  try {
    // The setup sees the state of the commit it belongs to.
    ret = krateReadAt(at, () => untrack(hook.fn));
  } catch (err) {
    reportHookError(err, hook.owner);
    return;
  }
  hook.cleanup = typeof ret === 'function' ? (ret as () => void) : null;
  hook.mounted = true;
  hook.epoch = at;
  liveEpochs.set(at, (liveEpochs.get(at) || 0) + 1);
}

// Error boundaries (boundary.ts) receive errors thrown by effect setups and
// cleanups, as React's do. Without a handler the error is rethrown.
let hookErrorHandler: ((err: unknown, ownerId: string | null) => boolean) | null = null;

/** Installs the handler for errors thrown by hook effects (internal). */
export function krateOnHookError(fn: ((err: unknown, ownerId: string | null) => boolean) | null): void {
  hookErrorHandler = fn;
}

function reportHookError(err: unknown, owner: Owner): void {
  let id: string | null = null;
  for (let o: Owner | null = owner; o && id === null; o = o.parent) id = o.id;
  if (!(hookErrorHandler && hookErrorHandler(err, id))) throw err;
}

function runGuarded(fn: () => void, owner: Owner): void {
  try {
    untrack(fn);
  } catch (err) {
    reportHookError(err, owner);
  }
}

// Detaches the cleanup of the last setup, bound to that setup's snapshot so
// it reads the values of the render it belongs to (React closure semantics).
function takeCleanup(hook: HookEffect): Cleanup {
  if (!hook.mounted) return null;
  hook.mounted = false;
  const at = hook.epoch;
  const c = hook.cleanup;
  hook.cleanup = null;
  if (!c) {
    releaseEpoch(at);
    return null;
  }
  // The snapshot stays live until the (possibly deferred) cleanup ran.
  return () => {
    try {
      krateReadAt(at, () => untrack(c));
    } finally {
      releaseEpoch(at);
    }
  };
}

function releaseEpoch(at: number): void {
  const n = (liveEpochs.get(at) || 1) - 1;
  if (n > 0) liveEpochs.set(at, n);
  else liveEpochs.delete(at);
}

function runCleanup(hook: HookEffect): void {
  const c = takeCleanup(hook);
  if (c) runGuarded(c, hook.owner);
}

function pruneSnapshots(): void {
  let min = krateEpoch();
  for (const e of liveEpochs.keys()) if (e < min) min = e;
  kratePruneSnapshots(min);
}

// Orders hooks children-before-parents (post-order over the owner tree),
// declaration order within a component.
function orderHooks(hooks: Set<HookEffect>): HookEffect[] {
  const rank = new Map<Owner, number>();
  let n = 0;
  const visit = (o: Owner): void => {
    for (const c of o.children) visit(c);
    rank.set(o, n++);
  };
  for (const c of root.children) visit(c);
  rank.set(root, n++);
  return Array.from(hooks).sort((a, b) => {
    const ra = rank.get(a.owner) ?? n;
    const rb = rank.get(b.owner) ?? n;
    return ra !== rb ? ra - rb : a.seq - b.seq;
  });
}

// Callbacks run in every commit's layout phase, after layout effects and
// before passive ones (class component lifecycles, see class-component.ts).
const layoutHooks: Array<() => void> = [];

/** Registers fn to run in the layout phase of every commit (internal). */
export function krateOnLayoutCommit(fn: () => void): void {
  layoutHooks.push(fn);
}

function runLayoutHooks(): void {
  for (const fn of layoutHooks) fn();
}

function commit(): void {
  commitQueued = false;
  // runAfterFlush recorded the React state this commit rendered.
  const at = krateEpoch();
  const sync = krateTakeSyncLane();
  if (dirty.size === 0) {
    runLayoutHooks();
    if (sync) flushPassive();
    pruneSnapshots();
    return;
  }
  const hooks = orderHooks(dirty).filter((h) => isActive(h.owner));
  dirty.clear();
  for (const h of hooks) h.dirty = false;
  krateCommitting(true);
  try {
    // Mutation phase, per component in order: insertion cleanup + setup,
    // then layout cleanup.
    let i = 0;
    while (i < hooks.length) {
      const owner = hooks[i].owner;
      let j = i;
      while (j < hooks.length && hooks[j].owner === owner) j++;
      for (let k = i; k < j; k++) {
        if (hooks[k].phase === 'insertion') {
          runCleanup(hooks[k]);
          runSetup(hooks[k], at);
        }
      }
      for (let k = i; k < j; k++) {
        if (hooks[k].phase === 'layout') runCleanup(hooks[k]);
      }
      i = j;
    }
    // Layout phase.
    for (const h of hooks) {
      if (h.phase === 'layout') runSetup(h, at);
    }
    runLayoutHooks();
    // Passive phase, after paint.
    let queued = false;
    for (const h of hooks) {
      if (h.phase !== 'passive') continue;
      const c = takeCleanup(h);
      if (c) passiveCleanups.push(c);
      passiveSetups.push([h, at]);
      queued = true;
    }
    if (queued) schedulePassive();
    // A discrete event's update runs its passive effects before the event
    // returns (React's SyncLane), ahead of microtasks the handler queued.
    if (sync) flushPassive();
  } finally {
    krateCommitting(false);
  }
  pruneSnapshots();
}

function schedulePassive(): void {
  if (passiveTimer !== null) return;
  passiveTimer = setTimeout(flushPassive, 0);
}

/** Runs pending passive effect cleanups and setups now. */
export function flushPassive(): void {
  if (passiveTimer !== null) {
    clearTimeout(passiveTimer);
    passiveTimer = null;
  }
  // Loop: a passive cleanup or setup may unmount components, queueing more.
  while (passiveCleanups.length || passiveSetups.length) {
    const cleanups = passiveCleanups;
    const setups = passiveSetups;
    passiveCleanups = [];
    passiveSetups = [];
    krateCommitting(true);
    try {
      for (const c of cleanups) c();
      for (const [h, at] of setups) {
        if (isActive(h.owner)) runSetup(h, at);
      }
    } finally {
      krateCommitting(false);
    }
  }
}

/**
 * React's useMemo with a dependency array: the getter recomputes
 * synchronously, on read, when the dependency array changed (Object.is), so
 * the value is current within the same update. Signals read by `fn` are not
 * tracked; only the dependencies are.
 */
export function createMemoDeps<T>(fn: () => T, deps: () => unknown): () => T {
  let prev: unknown[] | undefined;
  let has = false;
  let value: T;
  return () => {
    const next = toDeps(deps()) || [];
    if (!has || !depsEqual(prev, next)) {
      prev = next;
      has = true;
      value = untrack(fn);
    }
    return value;
  };
}

/**
 * React's useCallback with a dependency array: a getter for the callback
 * whose identity only changes when the dependency array changes.
 */
export function createCallbackDeps<T>(fn: T, deps: () => unknown): () => T {
  // The source closure is created once, so a new identity per dependency
  // change is modelled by a fresh forwarding wrapper.
  let first = true;
  return createMemoDeps(() => {
    if (first) {
      first = false;
      return fn;
    }
    const base = fn as unknown as (...args: unknown[]) => unknown;
    return function (this: unknown, ...args: unknown[]) {
      return base.apply(this, args);
    } as unknown as T;
  }, deps);
}

/**
 * Dependency values of a component parameter: every own property of a props
 * object (read live, so prop getters subscribe) except children, or the
 * value itself for anything else (a custom hook argument).
 */
export function krateDepsOf(value: unknown): unknown[] {
  if (value && typeof value === 'object') {
    const proto = Object.getPrototypeOf(value);
    if (proto === Object.prototype || proto === null) {
      const out: unknown[] = [];
      for (const k of Object.keys(value as Record<string, unknown>)) {
        if (k !== 'children') out.push((value as Record<string, unknown>)[k]);
      }
      return out;
    }
  }
  return [value];
}

// Partial teardown (soft navigation keeping layouts mounted): unmount every
// component outside the kept route scopes and forget their slot state.
function unmountExcept(keep: Set<string>): void {
  const gone = (scope: string | null) => !(scope != null && keep.has(scope));
  const passive: Array<() => void> = [];
  for (const child of root.children.slice()) {
    if (!gone(child.scope)) continue;
    disposeTree(child, child.shown, passive);
    const i = root.children.indexOf(child);
    if (i >= 0) root.children.splice(i, 1);
  }
  const keptHooks: HookEffect[] = [];
  for (const hook of root.hooks) {
    if (!gone(hook.scope)) {
      keptHooks.push(hook);
      continue;
    }
    hook.stop();
    dirty.delete(hook);
    if (hook.phase === 'passive') {
      const c = takeCleanup(hook);
      if (c) passive.push(c);
    } else {
      runCleanup(hook);
    }
  }
  root.hooks = keptHooks;
  for (const [id, owner] of Array.from(byId)) {
    if (owner.disposed || gone(owner.scope)) byId.delete(id);
  }
  // Slot ids of a layout start with its scope id (the layout's ID prefix).
  const keptId = (id: string) => {
    for (const k of keep) if (id.startsWith(k)) return true;
    return false;
  };
  for (const id of Array.from(condShown.keys())) if (!keptId(id)) condShown.delete(id);
  for (const id of Array.from(replacedSlots)) if (!keptId(id)) replacedSlots.delete(id);
  for (let i = records.length - 1; i >= 0; i--) {
    if (!keptId(records[i].id)) records.splice(i, 1);
  }
  for (const hook of Array.from(dirty)) if (hook.owner.disposed) dirty.delete(hook);
  passiveSetups = passiveSetups.filter(([h]) => !h.owner.disposed && !(h.owner === root && gone(h.scope)));
  for (const c of passive) {
    try {
      untrack(c);
    } catch {
      // Keep tearing down.
    }
  }
  krateSetOwner(null);
}

function unmountAll(keep: Set<string> | null): void {
  if (keep) {
    unmountExcept(keep);
    return;
  }
  // Page teardown: everything unmounts now, layout before passive.
  const passive: Array<() => void> = [];
  for (const child of root.children.slice()) {
    disposeTree(child, child.shown, passive);
  }
  for (const hook of root.hooks) {
    hook.stop();
    if (hook.phase === 'passive') {
      const c = takeCleanup(hook);
      if (c) passive.push(c);
    } else {
      runCleanup(hook);
    }
  }
  root.children.length = 0;
  root.hooks.length = 0;
  byId.clear();
  condShown.clear();
  replacedSlots.clear();
  dirty.clear();
  passiveSetups = [];
  const pendingCleanups = passiveCleanups.concat(passive);
  passiveCleanups = [];
  if (passiveTimer !== null) {
    clearTimeout(passiveTimer);
    passiveTimer = null;
  }
  for (const c of pendingCleanups) {
    try {
      untrack(c);
    } catch {
      // Keep tearing down.
    }
  }
  commitQueued = false;
  krateSetOwner(null);
}

// React flushes the previous commit's passive effects before a new update
// is processed, so the flush that starts an update runs them first.
krateOnBeforeFlush(flushPassive);
krateOnAfterFlush(commit);
krateOnDispose(unmountAll);
