// Owner tree and error boundaries.
//
// Krate components run once, so there is no reconciler to unwind when code
// throws. This module gives each hydrated component instance an Owner: a node
// in a tree that mirrors React's fiber parent chain. Effects, cleanups and
// mount callbacks created while an owner is current belong to it (see
// OwnerHooks in signal.ts), so unmounting an owner disposes its whole subtree,
// and values thrown by its code are routed up the tree to the nearest owner
// with a boundary handler. Error boundaries install a handler; Suspense can
// install one that intercepts thrown promises the same way.
//
// Owners also follow conditional branches: an owner registered inside a
// branch of a compiled conditional is mounted while that branch is shown and
// unmounted (its cleanups run) when it is swapped out, so componentDidMount /
// componentWillUnmount and state resets match React. A dormant owner keeps its
// setup function and is remounted by running it again.

import { setOwnerHooks, untrack, onCleanup } from './signal.js';

/** Second argument of componentDidCatch. */
export interface ErrorInfo {
  componentStack: string;
}

/**
 * Intercepts a value thrown by a descendant of the owner that installed it.
 * Returns true when the value was handled (the walk stops), false to let the
 * next boundary up see it.
 */
export type BoundaryHandler = (thrown: unknown, info: ErrorInfo, source: Owner) => boolean;

/**
 * dormant: registered, not running (hidden branch, unmounted subtree);
 * mounted: setup ran and its effects are live;
 * disposed: removed for good (page teardown).
 */
export type OwnerState = 'dormant' | 'mounted' | 'disposed';

export interface Owner {
  readonly id: string;
  /** Component name, used for component stacks. */
  readonly name: string;
  readonly parent: Owner | null;
  /** Owner whose render created this one's element (null when unknown). */
  readonly creator: Owner | null;
  readonly children: Owner[];
  /** Pre-order registration index; orders siblings. */
  readonly order: number;
  state: OwnerState;
  /** Error/Suspense interception for descendants. */
  boundary: BoundaryHandler | null;
  /** Kept dormant after a caught error until its boundary renders again. */
  blocked: boolean;
  /** Boundary flag: an error was caught and the boundary has not re-rendered since. */
  inError: boolean;
  /** Conditional slot and branch ('c' or 'a') this owner was rendered in. */
  readonly cond: string;
  readonly branch: string;
  /** Re-runnable body (the component's hydration scope). */
  readonly setup: (() => void) | null;
  /** Free-form per-owner storage (class instances live here). */
  readonly data: Record<string, unknown>;
}

interface OwnerImpl extends Owner {
  cleanups: Array<() => void>;
}

let current: OwnerImpl | null = null;
let nextOrder = 0;
const byId = new Map<string, OwnerImpl>();
const condState = new Map<string, string>();
const condOwners = new Map<string, OwnerImpl[]>();
let resetRegistered = false;
let mountDepth = 0;
const deferredMounts: OwnerImpl[] = [];

function makeOwner(
  id: string,
  name: string,
  parent: OwnerImpl | null,
  creator: OwnerImpl | null,
  cond: string,
  branch: string,
  setup: (() => void) | null,
): OwnerImpl {
  const o: OwnerImpl = {
    id,
    name,
    parent,
    creator,
    children: [],
    order: nextOrder++,
    state: 'dormant',
    boundary: null,
    blocked: false,
    inError: false,
    cond,
    branch,
    setup,
    data: {},
    cleanups: [],
  };
  if (parent) parent.children.push(o);
  return o;
}

/** The owner whose code is running, or null. */
export function getOwner(): Owner | null {
  return current;
}

/**
 * Creates a mounted owner under parent (default: the current owner). Use it for
 * regions created at runtime; run code in it with runWithOwner.
 */
export function createOwner(name: string, parent: Owner | null = current): Owner {
  const o = makeOwner('', name, (parent as OwnerImpl) || null, (parent as OwnerImpl) || null, '', '', null);
  o.state = parent && parent.state !== 'mounted' ? 'dormant' : 'mounted';
  if (!parent) registerReset();
  return o;
}

/**
 * Runs fn with owner current: effects and cleanups it creates belong to the
 * owner and errors it throws are routed to the owner's boundaries. Returns
 * fn's value, or undefined when it threw or the owner is not mounted.
 */
export function runWithOwner<T>(owner: Owner | null, fn: () => T): T | undefined {
  if (!owner) return fn();
  const o = owner as OwnerImpl;
  if (o.state !== 'mounted') return undefined;
  const prev = current;
  current = o;
  try {
    return untrack(fn);
  } catch (err) {
    handleError(err, o);
    return undefined;
  } finally {
    current = prev;
  }
}

/** Registers fn to run when owner unmounts (immediately if it already has). */
export function onOwnerCleanup(owner: Owner, fn: () => void): void {
  const o = owner as OwnerImpl;
  if (o.state !== 'mounted') {
    safely(fn, o);
    return;
  }
  o.cleanups.push(fn);
}

/** Installs a boundary handler on owner (null removes it). */
export function setBoundary(owner: Owner, handler: BoundaryHandler | null): void {
  owner.boundary = handler;
}

/** React-style component stack from source up to the root. */
export function componentStack(source: Owner | null): string {
  let out = '';
  for (let o = source; o; o = o.parent) {
    out += '\n    at ' + o.name;
  }
  return out;
}

/**
 * Routes a thrown value from source to the nearest mounted ancestor boundary
 * that handles it. An owner never catches its own errors: a boundary's own
 * render or lifecycle failure goes to the boundary above it, as in React.
 * Unhandled errors are reported and the failing subtree is unmounted.
 */
export function handleError(thrown: unknown, source: Owner | null = current): void {
  if (!source) throw thrown;
  const info: ErrorInfo = { componentStack: componentStack(source) };
  for (let b = source.parent as OwnerImpl | null; b; b = b.parent as OwnerImpl | null) {
    if (b.state !== 'mounted' || !b.boundary) continue;
    let handled = false;
    try {
      handled = b.boundary(thrown, info, source);
    } catch (next) {
      // The boundary itself failed (getDerivedStateFromError threw): the new
      // error propagates from the boundary.
      handleError(next, b);
      return;
    }
    if (handled) return;
  }
  reportUncaught(thrown, info, source as OwnerImpl);
}

// Uncaught errors. React 18 unmounts the whole root; Krate keeps the page's
// server-rendered HTML and unmounts only the component that threw (and its
// descendants), logging the error like React does.
function reportUncaught(thrown: unknown, info: ErrorInfo, source: OwnerImpl): void {
  const g = globalThis as { __krateErrs?: string[] };
  try {
    const msg = String((thrown as Error)?.stack || (thrown as Error)?.message || thrown);
    if (Array.isArray(g.__krateErrs)) g.__krateErrs.push(msg);
  } catch {
    // Reporting must never throw.
  }
  if (typeof console !== 'undefined') {
    console.error(thrown);
    console.error(
      'The above error occurred in the <' + source.name + '> component:' + info.componentStack +
        '\n\nAdd an error boundary (a class component with static getDerivedStateFromError) to recover from it.',
    );
  }
  source.blocked = true;
  unmountOwner(source);
}

function safely(fn: () => void, owner: OwnerImpl | null): void {
  try {
    fn();
  } catch (err) {
    if (owner && owner.parent) handleError(err, owner);
    else reportUncaughtLoose(err);
  }
}

function reportUncaughtLoose(err: unknown): void {
  if (typeof console !== 'undefined') console.error(err);
}

/**
 * Unmounts owner and its subtree: cleanups run parent-first (React calls
 * componentWillUnmount top-down) and the owners become dormant, so they can
 * be mounted again by running their setup.
 */
export function unmountOwner(owner: Owner): void {
  disposeTree(owner as OwnerImpl, 'dormant');
}

/** Mounts a dormant owner (and its mountable descendants). */
export function mountOwner(owner: Owner): void {
  const o = owner as OwnerImpl;
  if (o.state !== 'dormant') return;
  o.blocked = false;
  if (canMount(o)) mount(o);
}

/** Unmounts every child of owner (an error boundary dropping its subtree). */
export function unmountChildren(owner: Owner): void {
  for (const c of owner.children) disposeTree(c as OwnerImpl, 'dormant');
}

/**
 * Mounts owner's dormant children that their conditional branch allows,
 * clearing the block a caught error put on them.
 */
export function remountChildren(owner: Owner): void {
  if (owner.state !== 'mounted') return;
  for (const c of owner.children.slice() as OwnerImpl[]) {
    c.blocked = false;
    if (c.state === 'dormant' && canMount(c)) mount(c);
  }
}

function disposeTree(o: OwnerImpl, to: 'dormant' | 'disposed'): void {
  if (o.state === 'mounted') {
    o.state = to;
    const errors: unknown[] = [];
    for (const fn of o.cleanups.splice(0)) {
      try {
        fn();
      } catch (err) {
        errors.push(err);
      }
    }
    for (const c of o.children) disposeTree(c as OwnerImpl, to);
    // Errors thrown while unmounting go to the boundary above the owner.
    for (const err of errors) {
      if (o.parent) handleError(err, o);
      else reportUncaughtLoose(err);
    }
    return;
  }
  if (to === 'disposed' && o.state !== 'disposed') {
    o.state = 'disposed';
    for (const c of o.children) disposeTree(c as OwnerImpl, to);
  }
}

function condShows(o: OwnerImpl): boolean {
  if (!o.cond) return true;
  const shown = condState.get(o.cond);
  return shown === undefined || shown === o.branch;
}

function canMount(o: OwnerImpl): boolean {
  if (o.blocked || !condShows(o)) return false;
  return !o.parent || o.parent.state === 'mounted';
}

function mount(o: OwnerImpl): void {
  if (o.state !== 'dormant') return;
  o.state = 'mounted';
  mountDepth++;
  try {
    if (o.setup) runWithOwner(o, o.setup);
  } finally {
    mountDepth--;
  }
  if (o.state === 'mounted') {
    for (const c of o.children.slice() as OwnerImpl[]) {
      if (c.state === 'dormant' && canMount(c)) mount(c);
    }
  }
  if (mountDepth === 0 && deferredMounts.length > 0) {
    const list = deferredMounts.splice(0).sort((a, b) => a.order - b.order);
    for (const d of list) {
      if (d.state === 'dormant' && canMount(d)) mount(d);
    }
  }
}

function registerReset(): void {
  if (resetRegistered) return;
  resetRegistered = true;
  // Top-level registration: runs on disposeAll() (SPA navigation).
  onCleanup(resetOwners);
}

function resetOwners(): void {
  const roots: OwnerImpl[] = [];
  for (const o of byId.values()) if (!o.parent) roots.push(o);
  for (const o of roots) disposeTree(o, 'disposed');
  byId.clear();
  condState.clear();
  condOwners.clear();
  deferredMounts.length = 0;
  resetRegistered = false;
}

/** Compact registration metadata emitted by the compiler. */
export interface OwnMeta {
  /** Parent owner id (nearest enclosing hydrated component). */
  p?: string;
  /** Component name. */
  n?: string;
  /** Conditional slot id and branch the component renders in. */
  c?: string;
  b?: string;
  /** Owner id of the component whose render created this element. */
  k?: string;
}

/**
 * Hydration entry point: the compiler wraps each component's hydration scope
 * in `__krateOwn(id, meta, setup)` on pages that use the owner tree. The setup
 * runs now when the component is visible, or later when its branch is shown.
 */
export function __krateOwn(id: string, meta: OwnMeta | null, setup: () => void): void {
  const m = meta || {};
  const prior = byId.get(id);
  if (prior) disposeTree(prior, 'disposed');
  const parent = (m.p && byId.get(m.p)) || null;
  const creator = (m.k && byId.get(m.k)) || null;
  const o = makeOwner(id, m.n || id, parent, creator, m.c || '', m.b || '', setup);
  byId.set(id, o);
  registerReset();
  if (o.cond) {
    let list = condOwners.get(o.cond);
    if (!list) {
      list = [];
      condOwners.set(o.cond, list);
    }
    list.push(o);
  }
  // An owner rendered as part of a boundary's failed render stays down until
  // the boundary renders again.
  if (parent && parent.inError && condShows(o)) o.blocked = true;
  if (canMount(o)) mount(o);
}

/** Owner registered under id, if any (tests, devtools). */
export function __krateOwner(id: string): Owner | null {
  return byId.get(id) || null;
}

/**
 * Called by the hydration bootstrap before a conditional swaps branches: the
 * outgoing branch's owners unmount while their DOM is still attached.
 */
export function __krateCondPre(id: string, show: boolean): void {
  const prev = condState.get(id);
  const want = show ? 'c' : 'a';
  if (prev === undefined || prev === want) return;
  const list = condOwners.get(id);
  if (!list) return;
  for (const o of list) {
    if (o.branch === prev && o.state === 'mounted') disposeTree(o, 'dormant');
  }
}

/** Called after a conditional swapped: the incoming branch's owners mount. */
export function __krateCondPost(id: string, show: boolean): void {
  const want = show ? 'c' : 'a';
  const prev = condState.get(id);
  condState.set(id, want);
  if (prev === want) return;
  const list = condOwners.get(id);
  if (!list) return;
  for (const o of list) {
    if (o.branch !== want || o.state !== 'dormant' || !canMount(o)) continue;
    if (mountDepth > 0) deferredMounts.push(o);
    else mount(o);
  }
}

/**
 * Called by the hydration bootstrap's __safe wrapper. Routes an error thrown
 * while a component's hydration scope runs; nodes outside the document (a
 * hidden conditional branch React would not have rendered) are ignored.
 */
export function __krateOnError(err: unknown, node?: Node | null): boolean {
  if (!current) return false;
  if (node && node.isConnected === false) return false;
  handleError(err, current);
  return true;
}

setOwnerHooks({
  current: () => current,
  run: (owner, fn) => {
    runWithOwner(owner as Owner, fn);
  },
  cleanup: (owner, fn) => onOwnerCleanup(owner as Owner, fn),
  error: (owner, err) => {
    const o = owner as OwnerImpl;
    // Late errors from an unmounted subtree are dropped like React drops
    // updates to unmounted components.
    if (o.state === 'mounted') handleError(err, o);
    return true;
  },
});
