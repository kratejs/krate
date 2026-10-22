/**
 * Suspense, use(), lazy hydration and async transitions.
 *
 * Krate components run once and update through signals, so React's
 * "throw a promise and re-render" model is mapped onto three pieces:
 *
 * - Boundaries. A Suspense boundary is a counter of outstanding work. It shows
 *   its fallback while any descendant is suspended and reveals its content
 *   once everything inside settled, so siblings reveal together and the
 *   innermost boundary wins (each suspension is attributed to the nearest
 *   boundary only). Compiled pages emit the boundary as a conditional slot
 *   (content and fallback, one of them in a hoisted template) and the
 *   boundary shows one of them between the slot markers (see boundaryDOM).
 * - Runs. The hydration code of a component inside a boundary runs through
 *   `__krateBoundary(id, fn)`. When `use(promise)` meets a pending promise
 *   during that first run, the run throws a {@link SuspenseSignal}; the runner
 *   holds the boundary and runs the component again when the promise settles.
 *   As in React, `use()` calls on the retry reuse the promise seen at the same
 *   position in the previous attempt, so an uncached `use(fetch(...))` does
 *   not loop.
 * - Transitions. `startTransition` marks the updates it makes (and the effect
 *   flush they cause) as a transition. A boundary that is already showing
 *   content keeps showing it when a transition update suspends it, and the
 *   transition stays pending until that work and any async action settle.
 */
import { createSignal, createEffect, onCleanup, krateGetOwner, krateSetOwner } from './signal.js';

// Thenables

/** A promise-like value carrying React-compatible status fields. */
export type Thenable<T> = PromiseLike<T> & {
  status?: 'pending' | 'fulfilled' | 'rejected';
  value?: T;
  reason?: unknown;
};

const tracked = new WeakSet<object>();

export function isThenable(value: unknown): value is Thenable<unknown> {
  return (
    value != null &&
    (typeof value === 'object' || typeof value === 'function') &&
    typeof (value as { then?: unknown }).then === 'function'
  );
}

/**
 * Starts tracking a thenable's settlement by writing `status`/`value`/`reason`
 * onto it (the same fields React uses, so libraries that pre-resolve promises
 * for React interoperate). The same object is cached by identity: once it is
 * fulfilled, reading it never suspends again.
 */
export function trackThenable<T>(t: Thenable<T>): Thenable<T> {
  if (tracked.has(t)) return t;
  tracked.add(t);
  if (t.status === 'fulfilled' || t.status === 'rejected') return t;
  t.status = 'pending';
  t.then(
    (value) => {
      if (t.status === 'pending') {
        t.status = 'fulfilled';
        t.value = value;
      }
    },
    (reason) => {
      if (t.status === 'pending') {
        t.status = 'rejected';
        t.reason = reason;
      }
    },
  );
  return t;
}

/** Thrown (internally) when a pending thenable is read. */
export class SuspenseSignal {
  readonly thenable: Thenable<unknown>;
  constructor(thenable: Thenable<unknown>) {
    this.thenable = thenable;
  }
}

export function isSuspenseSignal(value: unknown): value is SuspenseSignal {
  return value instanceof SuspenseSignal;
}

function noop(): void {}

const CONTEXT_MARKER = Symbol.for('krate.context');

/**
 * Replay scope: the thenables read in a previous attempt of the same
 * computation, by position. A read at position i reuses the earlier thenable
 * instead of a freshly created one (React's thenable state).
 */
export interface ReadScope {
  list: Thenable<unknown>[];
  i: number;
}

let readScope: ReadScope | null = null;

function withReadScope<T>(scope: ReadScope | null, fn: () => T): T {
  const prev = readScope;
  readScope = scope;
  try {
    return fn();
  } finally {
    readScope = prev;
  }
}

/**
 * Runs one attempt of a computation that may be replayed after suspending:
 * reads restart at position 0 and reuse the thenables of earlier attempts.
 */
export function withReplayScope<T>(scope: ReadScope, fn: () => T): T {
  scope.i = 0;
  return withReadScope(scope, fn);
}

/**
 * Unwraps a value the way React's `use()` does: a context yields its current
 * value, a fulfilled thenable its value, a rejected thenable throws its reason
 * and a pending one throws a {@link SuspenseSignal}. Other values pass
 * through unchanged.
 */
export function readValue<T>(value: unknown): T {
  const ctx = value as { $$typeof?: symbol; useContext?: () => T } | null;
  // Krate contexts carry a marker; objects with a useContext() method (other
  // context shapes) count as contexts unless they are thenables.
  // Contexts are callable (React 19 `<Ctx value>`), so functions count too.
  if (ctx && (typeof ctx === 'object' || typeof ctx === 'function') && typeof ctx.useContext === 'function') {
    if (ctx.$$typeof === CONTEXT_MARKER || !isThenable(value)) return ctx.useContext();
  }
  if (!isThenable(value)) return value as T;
  let t = value as Thenable<T>;
  const scope = readScope;
  if (scope) {
    const idx = scope.i++;
    const prev = scope.list[idx] as Thenable<T> | undefined;
    if (prev !== undefined && prev !== t) {
      // An uncached promise created again on replay: keep the first one.
      t.then(noop, noop);
      t = prev;
    } else {
      scope.list[idx] = t;
    }
  }
  trackThenable(t);
  if (t.status === 'fulfilled') return t.value as T;
  if (t.status === 'rejected') throw t.reason;
  throw new SuspenseSignal(t);
}

// Error reporting

/** Receives errors from suspended work (rejected use(), failed lazy chunks). */
export type SuspenseErrorHandler = (error: unknown, boundary: Boundary | null) => boolean | void;

let errorHandler: SuspenseErrorHandler | null = null;

/**
 * Installs the handler for errors raised inside Suspense work. An error
 * boundary implementation registers here (or defines a global
 * `__krateReportError(error, anchor)`); returning `true` marks the error as
 * handled. Unhandled errors are logged with `console.error`.
 */
export function setSuspenseErrorHandler(handler: SuspenseErrorHandler | null): void {
  errorHandler = handler;
}

// Error boundaries (boundary.ts): the owner of the component whose work
// failed routes the error to its nearest boundary.
interface OwnerApi {
  getOwner: () => unknown;
  handleError: (error: unknown, owner: unknown) => void;
}

let ownerApi: OwnerApi | null = null;

/** Connects error boundaries explicitly (tests, the compat runtime). */
export function connectErrorBoundaries(api: OwnerApi | null): void {
  ownerApi = api;
}

function owners(): OwnerApi | null {
  if (ownerApi) return ownerApi;
  const g = globalThis as Partial<OwnerApi>;
  return typeof g.getOwner === 'function' && typeof g.handleError === 'function'
    ? { getOwner: g.getOwner, handleError: g.handleError }
    : null;
}

/** The error-boundary owner of the code running now, if any. */
function errorOwner(): unknown {
  const api = owners();
  return api ? api.getOwner() : null;
}

export function reportSuspenseError(error: unknown, boundary: Boundary | null, owner?: unknown): void {
  if (errorHandler && errorHandler(error, boundary) === true) return;
  const api = owner ? owners() : null;
  if (api) {
    api.handleError(error, owner);
    return;
  }
  const g = globalThis as { __krateReportError?: (e: unknown, anchor: Node | null) => boolean | void };
  if (typeof g.__krateReportError === 'function' && g.__krateReportError(error, boundary ? boundary.anchor : null) === true) {
    return;
  }
  console.error('[krate] uncaught error in suspended work:', error);
}

// Transitions

/** One startTransition call: settles when its action and its suspensions do. */
export interface Transition {
  /** True until the action and every suspension it caused have settled. */
  readonly pending: () => boolean;
  /** Resolves when the transition settles; rejects with the action's error. */
  readonly promise: Promise<void>;
}

class TransitionState implements Transition {
  count = 0;
  error: unknown = undefined;
  failed = false;
  readonly promise: Promise<void>;
  private settled = false;
  // Created on first read: a transition nobody observes writes no signal
  // (an extra write would only schedule another flush).
  private signal: [() => boolean, (v: boolean) => void] | null = null;
  private resolve!: () => void;
  private reject!: (e: unknown) => void;

  readonly pending = (): boolean => {
    if (!this.signal) this.signal = createSignal(!this.settled);
    return this.signal[0]();
  };

  constructor() {
    this.promise = new Promise<void>((res, rej) => {
      this.resolve = res;
      this.reject = rej;
    });
    // A transition nobody awaits must not surface as an unhandled rejection;
    // the error is reported through reportSuspenseError instead.
    this.promise.catch(noop);
  }

  add(): void {
    this.count++;
  }

  release(): void {
    if (--this.count > 0) return;
    this.settled = true;
    this.signal?.[1](false);
    if (this.failed) this.reject(this.error);
    else this.resolve();
  }

  wait(t: PromiseLike<unknown>): void {
    this.add();
    const done = (): void => this.release();
    t.then(done, done);
  }
}

// The transition whose synchronous scope (or the effect flush that scope
// scheduled) is currently running.
let activeTransition: TransitionState | null = null;

/** Reports whether the code running now was started by startTransition. */
export function isInTransition(): boolean {
  return currentLane() !== null;
}

/**
 * Runs `fn` as a transition and returns its pending state. This is the
 * shared primitive behind `startTransition`, `useTransition` and the React 19
 * action hooks:
 *
 * - updates made synchronously by `fn`, and the effect flush they schedule,
 *   are transition updates;
 * - if `fn` returns a promise (an async action) the transition stays pending
 *   until it settles, including the awaited work;
 * - a boundary that is already revealed and suspends during the transition
 *   keeps its content, and the transition stays pending until it resolves.
 *
 * Updates made after an `await` inside `fn` are ordinary updates, as in
 * React 19; wrap them in another `startTransition` to mark them.
 */
export function runAsyncTransition(fn: () => unknown, options?: { report?: boolean }): Transition {
  const report = options?.report !== false;
  const tr = new TransitionState();
  // Join the action scope (actions.ts) so optimistic updates stay applied
  // and form/action status stays pending for the whole transition.
  const endAction = beginActionScope();
  tr.promise.then(endAction, endAction);
  tr.add();
  const prev = activeTransition;
  activeTransition = tr;
  let result: unknown;
  try {
    result = fn();
  } catch (error) {
    tr.failed = true;
    tr.error = error;
    activeTransition = prev;
    tr.release();
    if (report) reportSuspenseError(error, null);
    return tr;
  }
  activeTransition = prev;
  if (isThenable(result)) {
    tr.add();
    result.then(
      () => tr.release(),
      (error: unknown) => {
        tr.failed = true;
        tr.error = error;
        tr.release();
        if (report) reportSuspenseError(error, null);
      },
    );
  }
  // Effects scheduled by the synchronous part run in a microtask queued
  // before this one; keep the transition lane open until they ran so a
  // suspension they cause is attributed to this transition.
  laneTransition = tr;
  queueMicrotask(() => {
    if (laneTransition === tr) laneTransition = null;
    tr.release();
  });
  return tr;
}

type BeginAction = () => () => void;

let actionScope: BeginAction | null = null;

// beginActionScope opens a scope in the action runtime (the hydration
// bundle's __krateBeginAction global, or one connected explicitly).
function beginActionScope(): () => void {
  const begin = actionScope ?? (globalThis as { __krateBeginAction?: BeginAction }).__krateBeginAction;
  return typeof begin === 'function' ? begin() : noop;
}

/**
 * Makes this module the single transition scope for React 19 actions: form
 * actions and useActionState dispatches run through runAsyncTransition, so
 * they share pending state, transition lanes and Suspense behavior with
 * useTransition. Errors are left to the action runtime, which reports them.
 */
export function connectActionTransitions(api?: {
  setActionTransitionRunner?: (runner: (<T>(fn: () => T | Promise<T>) => Promise<T>) | null) => void;
  __krateBeginAction?: BeginAction;
}): void {
  const target = (api ?? globalThis) as {
    setActionTransitionRunner?: (runner: (<T>(fn: () => T | Promise<T>) => Promise<T>) | null) => void;
    __krateBeginAction?: BeginAction;
  };
  if (api?.__krateBeginAction) actionScope = api.__krateBeginAction;
  if (typeof target.setActionTransitionRunner !== 'function') return;
  target.setActionTransitionRunner(<T,>(fn: () => T | Promise<T>): Promise<T> => {
    let result: T | Promise<T> | undefined;
    let threw = false;
    let thrown: unknown;
    const tr = runAsyncTransition(
      () => {
        try {
          result = fn();
        } catch (error) {
          threw = true;
          thrown = error;
          throw error;
        }
        return result;
      },
      { report: false },
    );
    if (threw) return Promise.reject(thrown);
    // Resolve with the action's value once the transition (including any
    // suspension it caused) settled; reject with the action's error.
    return Promise.resolve(result as T | Promise<T>).then((value) => tr.promise.then(() => value));
  });
}

// The transition whose effect flush is still queued.
let laneTransition: TransitionState | null = null;

function currentLane(): TransitionState | null {
  return activeTransition ?? laneTransition;
}

/** React's top-level `startTransition`. Async functions are supported. */
export function startTransition(fn: () => unknown): void {
  runAsyncTransition(fn);
}

/**
 * Returns a `startTransition` bound to a pending setter: the setter is set to
 * true when a transition starts and back to false once every transition
 * started through it has settled (overlapping transitions keep it pending).
 * Compiled `useTransition()` calls lower to this.
 */
export function __krateTransitionStarter(setPending: (v: boolean) => void): (fn: () => unknown) => void {
  let outstanding = 0;
  return (fn: () => unknown): void => {
    outstanding++;
    setPending(true);
    const tr = runAsyncTransition(fn);
    const done = (): void => {
      if (--outstanding === 0) setPending(false);
    };
    tr.promise.then(done, done);
  };
}

/** `const [isPending, startTransition] = useTransition()`; isPending is a getter. */
export function useTransition(): [() => boolean, (fn: () => unknown) => void] {
  const [pending, setPending] = createSignal(false);
  return [pending, __krateTransitionStarter(setPending)];
}

// Boundaries

/** A Suspense boundary: tracks outstanding work and shows content or fallback. */
export interface Boundary {
  readonly id: string;
  /** A node inside the boundary (its start marker), for error boundaries. */
  readonly anchor: Node | null;
  /** Number of suspensions currently hiding the content. */
  readonly pendingCount: number;
  /** Whether the content (rather than the fallback) is showing. */
  showingContent(): boolean;
  /** Suspends the boundary on `t` (see {@link suspendOn} for transitions). */
  suspend(t: PromiseLike<unknown>, inTransition?: boolean): void;
  /** Keeps the content hidden until the matching release (a run in progress). */
  hold(): void;
  release(): void;
}

// React throttles reveals: content replacing a fallback is committed no
// sooner than FALLBACK_THROTTLE_MS after the most recent fallback appeared,
// so nested and sequential reveals do not flash. Krate uses React 19's value.
let fallbackThrottleMs = 300;
let lastFallbackAt = -Infinity;

function now(): number {
  return typeof performance !== 'undefined' ? performance.now() : Date.now();
}

/** Records that a fallback became visible (starts the reveal throttle). */
export function noteFallbackShown(): void {
  lastFallbackAt = now();
}

/** Sets the reveal throttle (React's FALLBACK_THROTTLE_MS); 0 disables it. */
export function setFallbackThrottle(ms: number): void {
  fallbackThrottleMs = ms;
}

class BoundaryController implements Boundary {
  readonly id: string;
  anchor: Node | null;
  private pending = new Set<PromiseLike<unknown>>();
  private holds = 0;
  private content: boolean;
  private scheduled = false;
  private apply: (content: boolean) => void;

  constructor(id: string, anchor: Node | null, initialContent: boolean, apply: (content: boolean) => void) {
    this.id = id;
    this.anchor = anchor;
    this.content = initialContent;
    this.apply = apply;
  }

  get pendingCount(): number {
    return this.pending.size;
  }

  showingContent(): boolean {
    return this.content;
  }

  suspend(t: PromiseLike<unknown>, inTransition = false): void {
    if (inTransition && this.content) {
      // Already revealed content stays visible during a transition; the
      // transition waits for the work instead.
      currentLane()?.wait(t);
      return;
    }
    if (this.pending.has(t)) return;
    this.pending.add(t);
    this.update();
    const done = (): void => {
      this.pending.delete(t);
      this.schedule();
    };
    t.then(done, done);
  }

  hold(): void {
    this.holds++;
  }

  release(): void {
    if (this.holds > 0) this.holds--;
    this.schedule();
  }

  private schedule(): void {
    if (this.scheduled) return;
    this.scheduled = true;
    // Reveal after the current microtask batch: sibling retries and the
    // effect flush of resolved values land first, so content appears filled.
    queueMicrotask(() => {
      queueMicrotask(() => {
        this.scheduled = false;
        this.update();
      });
    });
  }

  private throttle: ReturnType<typeof setTimeout> | null = null;

  private update(): void {
    const want = this.pending.size === 0 && this.holds === 0;
    if (want === this.content) return;
    if (want) {
      const wait = lastFallbackAt + fallbackThrottleMs - now();
      if (wait > 0) {
        if (this.throttle === null) {
          this.throttle = setTimeout(() => {
            this.throttle = null;
            this.update();
          }, wait);
        }
        return;
      }
    } else {
      noteFallbackShown();
    }
    this.content = want;
    this.apply(want);
    // A reveal can expose a nested boundary that still shows its fallback.
    if (want && onReveal) onReveal(this);
  }
}

let onReveal: ((b: Boundary) => void) | null = null;

/**
 * Creates a boundary controller over an arbitrary show/hide function. Used by
 * the compiled boundary markers and the compat `<Suspense>` component, and
 * exported for tests and custom integrations.
 */
export function createBoundary(
  id: string,
  apply: (showContent: boolean) => void,
  options?: { anchor?: Node | null; initialContent?: boolean },
): Boundary {
  return new BoundaryController(id, options?.anchor ?? null, options?.initialContent ?? true, apply);
}

const boundaries = new Map<string, Boundary>();

/** The template/marker id of a compiled boundary's conditional slot. */
export function boundarySlotId(id: string): string {
  return 's' + id;
}

/**
 * Returns the boundary for a compiled `<!--suspense:ID-->` marker. The
 * compiler emits the boundary's two states as a conditional slot: the active
 * one between `<!--k:sID-->` markers and the other in a hoisted
 * `<template data-kc="sID" data-kb="c|a">` (c = content, a = fallback).
 */
export function getBoundary(id: string): Boundary {
  const slot = boundarySlotId(id);
  const g = globalThis as { findSlot?: (id: string) => Node | null };
  const start = typeof g.findSlot === 'function' ? g.findSlot(slot) : null;
  const existing = boundaries.get(id);
  // After an SPA navigation the same id can name a boundary of the new page.
  if (existing && (start === null || existing.anchor === start)) return existing;
  const tpl =
    typeof document !== 'undefined'
      ? (document.querySelector('template[data-kc="' + slot + '"]') as HTMLTemplateElement | null)
      : null;
  // A template holding the content means the build rendered the fallback.
  const initialContent = !tpl || tpl.getAttribute('data-kb') !== 'c';
  // The boundary is a conditional slot: report its branch to the effect
  // scheduler (when loaded) so effects of the content run in the commit after
  // it is revealed and are cleaned up while it is hidden again.
  const activity = (globalThis as { krateCondActivity?: (id: string, branch: string) => void }).krateCondActivity;
  const swap = start && start.nodeType === 8 ? boundaryDOM(start as Comment, tpl, initialContent) : null;
  const apply = (show: boolean): void => {
    if (!swap) return;
    if (activity && !show) activity(slot, 'a');
    swap(show);
    if (activity && show) activity(slot, 'c');
  };
  const b = createBoundary(id, apply, { anchor: start, initialContent });
  if (!initialContent && start && start.isConnected) noteFallbackShown();
  boundaries.set(id, b);
  return b;
}

onReveal = (revealed) => {
  for (const b of boundaries.values()) {
    if (b !== revealed && !b.showingContent() && b.anchor && b.anchor.isConnected) {
      noteFallbackShown();
      return;
    }
  }
};

/**
 * Shows the content or the fallback between a boundary's slot markers, the
 * way React does: the first reveal takes the content out of its template;
 * suspending revealed content again hides it in place (elements get
 * `display: none !important`, text is emptied) and shows the fallback after
 * it, so content state and DOM identity survive; revealing again restores it.
 */
function boundaryDOM(start: Comment, tpl: HTMLTemplateElement | null, initialContent: boolean): (show: boolean) => void {
  const endValue = '/' + (start.nodeValue || '');
  const end = (): Node | null => {
    for (let n = start.nextSibling; n; n = n.nextSibling) {
      if (n.nodeType === 8 && n.nodeValue === endValue) return n;
    }
    return null;
  };
  const between = (): Node[] => {
    const out: Node[] = [];
    const stop = end();
    for (let n = start.nextSibling; n && n !== stop; n = n.nextSibling) out.push(n);
    return out;
  };
  let content: Node[] = initialContent ? between() : [];
  let fallback: Node[] = initialContent ? [] : between();
  let tplTaken = false;
  const fromTemplate = (): Node[] => {
    if (!tpl || tplTaken) return [];
    tplTaken = true;
    return Array.from(tpl.content.childNodes);
  };
  const hidden = new Map<Node, [string, string] | string>();
  const insert = (nodes: Node[]): void => {
    const parent = start.parentNode;
    if (!parent) return;
    const before = end();
    for (const n of nodes) parent.insertBefore(n, before);
  };
  const detach = (nodes: Node[]): void => {
    for (const n of nodes) n.parentNode?.removeChild(n);
  };
  return (show: boolean): void => {
    if (show) {
      detach(fallback);
      if (content.length === 0) {
        content = fromTemplate();
        insert(content);
        return;
      }
      for (const n of content) {
        const saved = hidden.get(n);
        if (saved === undefined) continue;
        if (typeof saved === 'string') n.nodeValue = saved;
        else if (saved[0] === '') (n as HTMLElement).style.removeProperty('display');
        else (n as HTMLElement).style.setProperty('display', saved[0], saved[1]);
      }
      hidden.clear();
      return;
    }
    for (const n of content) {
      if (hidden.has(n)) continue;
      if (n.nodeType === 1 && (n as HTMLElement).style) {
        const style = (n as HTMLElement).style;
        hidden.set(n, [style.getPropertyValue('display'), style.getPropertyPriority('display')]);
        style.setProperty('display', 'none', 'important');
      } else if (n.nodeType === 3) {
        hidden.set(n, n.nodeValue || '');
        n.nodeValue = '';
      }
    }
    if (fallback.length === 0) fallback = fromTemplate();
    insert(fallback);
  };
}

/** Registers a boundary under its id so compiled runs can find it. */
export function registerBoundary(boundary: Boundary): void {
  boundaries.set(boundary.id, boundary);
}

/** Forgets compiled boundaries (SPA navigation re-hydrates a new page). */
export function resetBoundaries(): void {
  boundaries.clear();
}

/**
 * Suspends `boundary` on `t`. Inside a transition an already revealed
 * boundary keeps its content and the transition waits instead; with no
 * boundary the work only extends the active transition.
 */
export function suspendOn(boundary: Boundary | null, t: PromiseLike<unknown>): void {
  const inTransition = currentLane() !== null;
  if (boundary) boundary.suspend(t, inTransition);
  else if (inTransition) currentLane()?.wait(t);
}

// Component runs

interface RunState {
  boundary: Boundary | null;
  scope: ReadScope;
  disposers: Array<() => void>;
}

let currentRun: RunState | null = null;

/** The boundary of the component whose hydration is running, if any. */
export function currentBoundary(): Boundary | null {
  return currentRun ? currentRun.boundary : null;
}

/**
 * Runs a component's hydration code inside the boundary `id` (or none). A
 * pending `use()` during the run throws; the run is disposed and repeated
 * when the promise settles, with earlier promises reused by position. Other
 * errors go to the error handler (see {@link setSuspenseErrorHandler}).
 */
export function __krateBoundary(id: string | null, fn: () => void): void {
  const boundary = id ? getBoundary(id) : null;
  const state: RunState = { boundary, scope: { list: [], i: 0 }, disposers: [] };
  const errOwner = errorOwner();
  boundary?.hold();
  const attempt = (): void => {
    state.scope.i = 0;
    const prev = currentRun;
    currentRun = state;
    // A retry runs later, outside the hydration script: keep the effect
    // owner it declares from leaking into unrelated code.
    const owner = krateGetOwner();
    try {
      // Reads made directly in the body (not through __krateUse) replay too.
      withReadScope(state.scope, fn);
      currentRun = prev;
      krateSetOwner(owner);
      boundary?.release();
    } catch (error) {
      currentRun = prev;
      krateSetOwner(owner);
      for (const dispose of state.disposers.splice(0)) dispose();
      if (isSuspenseSignal(error)) {
        error.thenable.then(attempt, attempt);
        return;
      }
      boundary?.release();
      reportSuspenseError(error, boundary, errOwner);
    }
  };
  attempt();
}

/**
 * The compiled form of `const value = use(...)`: `__krateUse(() => expr)`
 * where every `use(x)` inside `expr` became `__krateRead(x)`. Returns a
 * getter.
 *
 * The first evaluation runs synchronously: a pending promise suspends the
 * whole component run (see {@link __krateBoundary}). Later re-evaluations (a
 * signal read by `expr` changed, for example a new promise in state) keep the
 * previous value while the new promise is pending and suspend the boundary:
 * outside a transition it shows the fallback again, inside a transition it
 * keeps the old content.
 */
export function __krateUse<T>(compute: () => T): () => T {
  const run = currentRun;
  const boundary = run ? run.boundary : null;
  const errOwner = errorOwner();
  const scope: ReadScope = run ? run.scope : { list: [], i: 0 };
  const start = scope.i;
  const initial = withReadScope(scope, compute);
  const own = scope.list.slice(start, scope.i);
  const [get, set] = createSignal<T>(initial);
  const [version, setVersion] = createSignal(0);
  const bump = (): void => setVersion((v) => v + 1);
  let first = true;
  const dispose = createEffect(() => {
    version();
    const replay: ReadScope | null = first ? { list: own, i: 0 } : null;
    first = false;
    try {
      const next = withReadScope(replay, compute);
      set(() => next);
    } catch (error) {
      if (isSuspenseSignal(error)) {
        error.thenable.then(bump, bump);
        suspendOn(boundary, error.thenable);
        return;
      }
      reportSuspenseError(error, boundary, errOwner);
    }
  });
  if (run) run.disposers.push(dispose);
  return get;
}

/** `use(x)` inside a `__krateUse` computation. */
export function __krateRead<T>(value: unknown): T {
  return readValue<T>(value);
}

// Lazy hydration chunks

const lazyLoads = new Map<string, Promise<unknown>>();

/**
 * Loads the hydration chunk of a `lazy()` component. The component's HTML was
 * rendered at build time, so the content stays visible while its code loads
 * (as with React hydrating server-rendered lazy content); the boundary is
 * held so a boundary that is still showing its fallback reveals together with
 * the lazy content. Loading starts when the page hydrates, the first point the
 * component is rendered on the client. A failed load is reported to the
 * nearest error boundary.
 */
export function __krateLazy(url: string, boundaryId?: string | null): Promise<unknown> {
  const boundary = boundaryId ? getBoundary(boundaryId) : null;
  const errOwner = errorOwner();
  boundary?.hold();
  let load = lazyLoads.get(url);
  if (!load) {
    // React starts loading a lazy component when it first renders: a boundary
    // inside a hidden branch waits until the branch is shown.
    const anchor = boundary ? boundary.anchor : null;
    load =
      anchor && !anchor.isConnected
        ? whenConnected(anchor).then(() => {
            // The fallback just became visible.
            noteFallbackShown();
            return importChunk(url);
          })
        : importChunk(url);
    lazyLoads.set(url, load);
  }
  load.then(
    () => boundary?.release(),
    (error: unknown) => {
      boundary?.release();
      reportSuspenseError(error, boundary, errOwner);
    },
  );
  return load;
}

// whenConnected resolves once node is attached to the document.
function whenConnected(node: Node): Promise<void> {
  if (node.isConnected || typeof MutationObserver === 'undefined') return Promise.resolve();
  return new Promise((resolve) => {
    const observer = new MutationObserver(() => {
      if (!node.isConnected) return;
      observer.disconnect();
      resolve();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
  });
}

const lazyPayloads = new WeakMap<object, Thenable<unknown>>();

/**
 * The module promise of a lazy() whose loader is not a static import. The
 * loader is called once, when the component first hydrates; it must resolve
 * to a module whose default export is a component.
 */
export function __krateLazyPayload(loader: () => PromiseLike<unknown>): Thenable<unknown> {
  let payload = lazyPayloads.get(loader);
  if (!payload) {
    payload = Promise.resolve()
      .then(() => loader())
      .then((mod) => {
        const impl = mod && typeof mod === 'object' ? (mod as { default?: unknown }).default : undefined;
        if (typeof impl !== 'function') {
          throw new Error(
            'lazy: the loader did not resolve to a module with a component as its default export (resolve to { default: Component })',
          );
        }
        return mod;
      }) as Thenable<unknown>;
    trackThenable(payload);
    lazyPayloads.set(loader, payload);
  }
  return payload;
}

let chunkImporter: (url: string) => Promise<unknown> = (url) => import(/* @vite-ignore */ url);

/** Overrides how lazy chunks are fetched (tests, custom loaders). */
export function setLazyChunkImporter(fn: (url: string) => Promise<unknown>): void {
  chunkImporter = fn;
}

function importChunk(url: string): Promise<unknown> {
  try {
    return Promise.resolve(chunkImporter(url));
  } catch (error) {
    return Promise.reject(error);
  }
}

/** Entry point of a lazy hydration chunk: runs the chunk's component code. */
export function __krateLazyRun(fn: () => void): void {
  fn();
}

// Deferred values and external stores

function scheduleDeferred(fn: () => void): void {
  setTimeout(fn, 0);
}

/**
 * React's `useDeferredValue(value, initialValue?)` as a getter. During an
 * urgent update the getter keeps returning the previous value and catches up
 * in a later task (a `setTimeout(0)` after the update), so urgent DOM work
 * lands first. With `initialValue` the first value is `initialValue` and the
 * real value follows the same way (React 19). Updates made inside a
 * transition are not deferred again.
 */
export function createDeferredValue<T>(get: () => T, ...initial: [] | [T]): () => T {
  const hasInitial = initial.length > 0;
  let latest: T;
  let current: T;
  let timer = false;
  const [deferred, setDeferred] = createSignal<T>(undefined as T);
  const flush = (): void => {
    timer = false;
    if (!Object.is(latest, current)) {
      current = latest;
      setDeferred(() => latest);
    }
  };
  let first = true;
  createEffect(() => {
    latest = get();
    if (first) {
      first = false;
      current = hasInitial ? (initial[0] as T) : latest;
      setDeferred(() => current);
      if (!hasInitial || Object.is(current, latest)) return;
    }
    if (Object.is(latest, current)) return;
    if (isInTransition()) {
      flush();
      return;
    }
    if (!timer) {
      timer = true;
      scheduleDeferred(flush);
    }
  });
  return deferred;
}

/** React's `useDeferredValue` for compat callers: wraps a getter or a value. */
export function useDeferredValue<T>(value: T | (() => T), ...initial: [] | [T]): T | (() => T) {
  if (typeof value === 'function') return createDeferredValue(value as () => T, ...initial);
  return value;
}

/**
 * React's `useSyncExternalStore` as a getter. Subscribes once, re-reads the
 * snapshot on every store notification (and once right after subscribing, to
 * catch changes between the first read and the subscription), stores the
 * snapshot only when it changed by `Object.is`, and unsubscribes when the
 * component is torn down. `getServerSnapshot` is what the build renders;
 * the client hydrates from `getSnapshot`.
 */
export function createExternalStore<T>(
  subscribe: (onChange: () => void) => (() => void) | void,
  getSnapshot: () => T,
  _getServerSnapshot?: () => T,
): () => T {
  let value = getSnapshot();
  if (!Object.is(value, getSnapshot())) {
    console.error('[krate] The result of getSnapshot should be cached to avoid an infinite loop');
  }
  const [get, set] = createSignal<T>(value);
  const check = (): void => {
    const next = getSnapshot();
    if (!Object.is(next, value)) {
      value = next;
      set(() => next);
    }
  };
  const unsubscribe = subscribe(check);
  check();
  onCleanup(() => {
    if (typeof unsubscribe === 'function') unsubscribe();
  });
  return get;
}

/** React's `useSyncExternalStore` (getter result). */
export function useSyncExternalStore<T>(
  subscribe: (onChange: () => void) => (() => void) | void,
  getSnapshot: () => T,
  getServerSnapshot?: () => T,
): () => T {
  return createExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

connectActionTransitions();

// Lets createResource (in the core bundle) suspend the creating component's
// boundary without importing this module.
(globalThis as { __krateSuspenseHook?: unknown }).__krateSuspenseHook = {
  current: currentBoundary,
  suspend: (boundary: Boundary | null, promise: PromiseLike<unknown>) => suspendOn(boundary, promise),
};
