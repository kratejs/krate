import { resetContexts } from './context.js';
import {
  resolvePersist,
  readDurableState,
  readPersisted,
  writePersisted,
  subscribePersist,
  type PersistSpec,
  type ResolvedPersist,
} from './storage.js';

/** Options accepted by createSignal. */
export interface SignalOptions<T> {
  /**
   * Persist the signal's value. A string is a storage key; an object customizes
   * the key, store (local/session/memory), serialization, and cross-tab sync.
   */
  persist?: PersistSpec<T>;
  /**
   * React state semantics (set by the compiler for useState/useReducer).
   * Outside Krate's render phase (event handlers, effect bodies, cleanups)
   * the getter returns the value of the last commit, so a handler sees its
   * render's snapshot and a cleanup sees the render its effect ran for.
   * Writes still apply immediately to functional updaters, and an update made
   * outside a discrete event commits in a later task, as React schedules it.
   */
  react?: boolean;
}

/** Signal options that give a signal React state semantics. */
export const krateHookState: { readonly react: true } = { react: true };

// A null entry is an untracked scope (see untrack): reads inside it subscribe
// nothing.
const context: Array<EffectState | null> = [];
// Each queued mount callback remembers the owner (effect or root) that was
// current when it was registered, so an onCleanup inside the callback belongs
// to that owner instead of the page root, and the error-boundary owner its
// errors are routed to.
const mountQueue: Array<{ fn: () => void; owner: EffectState | null | undefined; boundary: unknown }> = [];
let mountScheduled = false;

/**
 * Owner integration (installed by boundary.ts). An owner is the component
 * instance that is current while code runs; effects, cleanups and mount
 * callbacks created under it are tied to it, and errors they throw are routed
 * to it (error boundaries, Suspense). With no hooks installed every code path
 * below behaves exactly as before.
 */
export interface OwnerHooks {
  /** The owner active right now, or null. */
  current(): unknown;
  /** Runs fn with owner current, routing anything it throws. */
  run(owner: unknown, fn: () => void): void;
  /** Registers fn to run when owner is unmounted. */
  cleanup(owner: unknown, fn: () => void): void;
  /** Routes an error thrown by owner's code; true when handled. */
  error(owner: unknown, err: unknown): boolean;
}

let ownerHooks: OwnerHooks | null = null;

/** Installs (or with null removes) the owner integration hooks. */
export function setOwnerHooks(hooks: OwnerHooks | null): void {
  ownerHooks = hooks;
}

// Callbacks that run once the effect queue has fully drained (see afterFlush).
const afterQueue: Array<() => void> = [];
const allEffects = new Set<EffectState>();
// Top-level onCleanup registrations (called outside an effect body) are scoped
// to the page/root and run when disposeAll() executes on route change. This
// gives component-level cleanup semantics: a component's top-level onCleanup
// registers once when its hydration IIFE runs and is invoked on unmount.
const rootCleanups: Array<{ fn: () => void; scope: string | null }> = [];

// Route scopes (SPA layout persistence). Hydration code of a layout runs
// between krateEnterScope(id) and krateLeaveScope(), so every effect, cleanup
// and component owner it creates is tagged with the layout's id. On a soft
// navigation that keeps the layout mounted, the router disposes everything
// except the kept scopes (krateDisposeExcept) and the new page's hydration
// script skips the kept layouts' code (krateEnterScope returns false).
let routeScope: string | null = null;
let keptScopes = new Set<string>();
// Scope of each owner key used by ownerEffects/ownerCleanups.
const ownerScopes = new Map<unknown, string | null>();

/**
 * Enters the route scope `id` for the hydration code that follows. Returns
 * false when the SPA router kept that layout mounted: its bindings and
 * effects are still live and must not be created twice.
 */
export function krateEnterScope(id: string): boolean {
  if (keptScopes.has(id)) return false;
  routeScope = id;
  currentOwner = null;
  return true;
}

/** Leaves the current route scope (see krateEnterScope). */
export function krateLeaveScope(): void {
  routeScope = null;
  currentOwner = null;
}

/**
 * The route scope new effects and owners are tagged with (internal): the
 * running effect's scope, else the scope whose hydration code is running,
 * else the current component owner's scope (code running later, e.g. an
 * event handler of a layout component).
 */
export function krateCurrentScope(): string | null {
  const top = context[context.length - 1];
  if (top) return top.scope;
  if (routeScope !== null) return routeScope;
  if (currentOwner !== null && ownerScopes.has(currentOwner)) return ownerScopes.get(currentOwner) ?? null;
  return null;
}

/** Sets the layout scopes the router kept mounted for the next hydration (internal). */
export function krateKeepScopes(ids: string[]): void {
  keptScopes = new Set(ids);
}

/** Records the route scope of an owner key (internal; see effects.ts). */
export function krateTagOwner(owner: unknown, scope: string | null): void {
  if (owner !== null && !ownerScopes.has(owner)) ownerScopes.set(owner, scope);
}

// Pending effects scheduled to re-run in the next microtask. Writes batch
// subscriber re-runs into a single flush so N writes in a loop (or one write
// fanning out) produce one pass instead of N synchronous effect runs.
const pending: Array<EffectState> = [];
let flushScheduled = false;

interface EffectState {
  fn: () => void;
  cleanups: Array<() => void>;
  disposed: boolean;
  /** Subscriber Sets this effect is registered in, so dispose() can remove it. */
  sources: Set<Set<EffectState>>;
  /** True while this effect's fn is executing (guards re-entrant writes). */
  running: boolean;
  /** Set when a write dirtied this effect while it was already running. */
  rerun: boolean;
  /** Set while the effect is in the pending flush queue. */
  queued: boolean;
  /** Component owner current when the effect was created (see krateOwnerScope). */
  owner: unknown;
  /** Error-boundary owner current when the effect was created (see OwnerHooks), or null. */
  boundaryOwner: unknown;
  /**
   * Set on the observer of a derived value (krateDerived): a write to one of
   * its sources calls mark synchronously instead of queueing a re-run.
   */
  mark?: (reactWrite: boolean) => void;
  /**
   * Marks an ownership root (see createRoot). A root is pushed on the context
   * stack with `disposed` set so signal reads stay untracked, while effects,
   * memos and cleanups registered under it are collected into `cleanups`.
   */
  root?: boolean;
  /** Set once a root has been disposed. */
  dead?: boolean;
  /** Route scope (layout id) the effect belongs to; null for page code. */
  scope: string | null;
}

function newEffectState(fn: () => void): EffectState {
  const scope = krateCurrentScope();
  const effect: EffectState = { fn, cleanups: [], disposed: false, sources: new Set(), running: false, rerun: false, queued: false, owner: currentOwner, boundaryOwner: ownerHooks ? ownerHooks.current() : null, scope };
  if (currentOwner !== null) {
    krateTagOwner(currentOwner, scope);
    let set = ownerEffects.get(currentOwner);
    if (!set) ownerEffects.set(currentOwner, (set = new Set()));
    set.add(effect);
  }
  return effect;
}

// Effects and top-level cleanups created while an owner was current, so the
// owner can be unmounted on its own (a component removed by a conditional).
const ownerEffects = new Map<unknown, Set<EffectState>>();
const ownerCleanups = new Map<unknown, Array<() => void>>();

/**
 * Disposes every effect created under `owner` and runs its top-level
 * onCleanup callbacks (internal, used when a component unmounts).
 */
export function krateDisposeOwner(owner: unknown): void {
  const effects = ownerEffects.get(owner);
  ownerEffects.delete(owner);
  if (effects) {
    for (const effect of effects) {
      disposeEffect(effect);
      allEffects.delete(effect);
    }
  }
  const cleanups = ownerCleanups.get(owner);
  ownerCleanups.delete(owner);
  ownerScopes.delete(owner);
  if (cleanups) {
    for (const c of cleanups) {
      try {
        c();
      } catch {
        // One failing cleanup must not keep the rest from running.
      }
    }
  }
}

// Render snapshots for React state (see SignalOptions.react).
//
// Krate's render phase is any tracked effect run (DOM bindings, dependency
// trackers, memo computations read by them): reads there see the latest
// value. Everywhere else a React state read returns the value as of the last
// commit, or as of `snapshotEpoch` while an effect setup/cleanup runs for the
// commit it belongs to.
let renderDepth = 0;
let snapshotEpoch: number | null = null;
let epoch = 0;

interface ReactCell {
  commit(e: number): void;
  prune(min: number): boolean;
}
const changedCells = new Set<ReactCell>();
const historyCells = new Set<ReactCell>();

/**
 * Runs fn as render work: React state reads see the latest values rather than
 * the last commit's (internal; the class component render phase).
 */
export function krateRenderPhase<T>(fn: () => T): T {
  renderDepth++;
  const prevEpoch = snapshotEpoch;
  snapshotEpoch = null;
  try {
    return fn();
  } finally {
    snapshotEpoch = prevEpoch;
    renderDepth--;
  }
}

/** Records the current React state values as commit `epoch` (internal). */
export function krateCommitSnapshot(): number {
  epoch++;
  for (const cell of changedCells) cell.commit(epoch);
  changedCells.clear();
  return epoch;
}

/** The epoch of the last commit snapshot (internal). */
export function krateEpoch(): number {
  return epoch;
}

/** Runs fn reading React state as of commit `at` (internal). */
export function krateReadAt<T>(at: number | null, fn: () => T): T {
  const prev = snapshotEpoch;
  snapshotEpoch = at;
  try {
    return fn();
  } finally {
    snapshotEpoch = prev;
  }
}

/** Drops snapshot history older than commit `min` (internal). */
export function kratePruneSnapshots(min: number): void {
  for (const cell of historyCells) {
    if (!cell.prune(min)) historyCells.delete(cell);
  }
}

// Update lanes. A write during a discrete event (click, key, input...) or a
// commit flushes in a microtask, and a discrete update also runs its passive
// effects at the end of its commit, like React's SyncLane. A React state
// write anywhere else (timeouts, promises) flushes in a later task, after
// the microtasks that code queued, like React's DefaultLane.
const DISCRETE = new Set(
  ('auxclick beforeinput blur cancel change click close compositionend compositionstart compositionupdate ' +
    'contextmenu copy cut dblclick dragend dragstart drop focusin focusout input invalid keydown keypress keyup ' +
    'mousedown mouseup paste pause play pointercancel pointerdown pointerup ratechange reset resize seeked ' +
    'select submit touchcancel touchend touchstart volumechange focus').split(' '),
);
let committing = 0;
let syncLane = false;

// Set while the event runtime dispatches a discrete native event.
let discreteDispatch = 0;

function inDiscreteEvent(): boolean {
  if (discreteDispatch > 0) return true;
  const ev = typeof window !== 'undefined' ? (window as unknown as { event?: Event }).event : undefined;
  return !!ev && DISCRETE.has(ev.type);
}

let transitionWrites = 0;

/**
 * Runs fn with its signal writes in the transition lane: they commit in a
 * later task, like React's transition and default updates, even for plain
 * signals. Used for the updates an async action makes when it settles
 * (pending flags, results), so the pending render stays on screen for at
 * least one task, as it does in React.
 */
export function krateTransitionWrites<T>(fn: () => T): T {
  transitionWrites++;
  try {
    return fn();
  } finally {
    transitionWrites--;
  }
}

/** Reports whether a native event type is discrete (React's SyncLane). */
export function krateIsDiscrete(type: string): boolean {
  return DISCRETE.has(type);
}

/**
 * Runs the handlers of one native event dispatch. For a discrete event
 * (click, input, keydown, submit, change, focus, ...) the updates they made
 * are flushed synchronously when `flush` is set, as React does at the end of
 * a discrete event: render, commit, layout and passive effects all run before
 * the browser event returns. Continuous events keep the normal batching.
 */
export function krateRunEvent(type: string, flush: boolean, fn: () => void): void {
  const discrete = DISCRETE.has(type);
  if (!discrete) {
    fn();
    return;
  }
  discreteDispatch++;
  try {
    fn();
  } finally {
    discreteDispatch--;
    // An event dispatched by the flush itself (a blur fired by removing the
    // focused node) joins the running flush instead of committing on its own.
    if (flush && discreteDispatch === 0 && flushDepth === 0 && committing === 0 && (pending.length > 0 || syncLane)) {
      krateFlushSync();
    }
  }
}

/** Marks a commit in progress (internal): writes there flush in a microtask. */
export function krateCommitting(on: boolean): void {
  committing += on ? 1 : -1;
}

/** Reports and clears whether the pending commit came from a discrete event. */
export function krateTakeSyncLane(): boolean {
  const v = syncLane;
  syncLane = false;
  return v;
}

/** The effect currently collecting dependencies, ignoring untracked frames. */
function currentEffect(): EffectState | null {
  return context[context.length - 1] || null;
}

// The component owner whose body (or binding effect) is currently running.
// Opaque here; effects.ts gives it meaning (effect ordering and unmounting).
// Effects capture it at creation and restore it while they re-run, so
// components created by a binding re-run are parented correctly.
let currentOwner: unknown = null;

/** Returns the current component owner (internal, used by effects.ts). */
export function krateGetOwner(): unknown {
  return currentOwner;
}

/** Sets the current component owner and returns the previous one (internal). */
export function krateSetOwner(owner: unknown): unknown {
  const prev = currentOwner;
  currentOwner = owner;
  return prev;
}

// Hooks run after a flush settles (no pending effects) and when the page is
// torn down. effects.ts registers its commit and unmount here; the API is
// additive so other schedulers can hook in too.
const beforeFlushHooks: Array<() => void> = [];
const afterFlushHooks: Array<() => void> = [];
const disposeHooks: Array<(keep: Set<string> | null) => void> = [];

/** Registers fn to run before every effect flush starts (internal). */
export function krateOnBeforeFlush(fn: () => void): void {
  beforeFlushHooks.push(fn);
}

/** Registers fn to run after every settled effect flush (internal). */
export function krateOnAfterFlush(fn: () => void): void {
  afterFlushHooks.push(fn);
}

/**
 * Registers fn to run at the start of disposeAll (internal). `keep` is the
 * set of route scopes that stay mounted for a partial teardown
 * (krateDisposeExcept), null for a full one.
 */
export function krateOnDispose(fn: (keep: Set<string> | null) => void): void {
  disposeHooks.push(fn);
}

/** Reports whether signal writes are waiting for the next flush. */
export function kratePendingFlush(): boolean {
  return pending.length > 0;
}

/**
 * Runs fn without subscribing the surrounding effect to the signals it reads.
 */
export function untrack<T>(fn: () => T): T {
  context.push(null);
  try {
    return fn();
  } finally {
    context.pop();
  }
}

/**
 * Runs fn once after the pending effect queue has fully drained (including
 * effects scheduled while it drained) and the after-flush hooks ran, so the
 * DOM reflects every write made so far. Schedules a flush when none is
 * pending. Used by the class component commit phase.
 */
export function afterFlush(fn: () => void): void {
  afterQueue.push(fn);
  scheduleFlush();
}

function drainAfterQueue(): void {
  if (afterQueue.length === 0) return;
  const after = afterQueue.splice(0);
  for (const fn of after) {
    fn();
  }
}

/**
 * Same-value equality used for every React-facing comparison (state
 * bail-out, dependency arrays): Object.is, so NaN equals NaN and +0 differs
 * from -0.
 */
export function sameValue(a: unknown, b: unknown): boolean {
  return Object.is(a, b);
}

function disposeEffect(effect: EffectState): void {
  if (!effect.disposed) {
    effect.disposed = true;
    if (effect.owner !== null) ownerEffects.get(effect.owner)?.delete(effect);
    // Detach from every signal subscriber Set so a long-lived signal never
    // retains a disposed effect (and its closure graph).
    for (const set of effect.sources) {
      set.delete(effect);
    }
    effect.sources.clear();
    for (const cleanup of effect.cleanups) {
      cleanup();
    }
    effect.cleanups.length = 0;
  }
}

/**
 * Partial teardown for a soft navigation that keeps some layouts mounted:
 * disposes every effect, cleanup and component owner whose route scope is
 * not in `keep`. With an empty `keep` this is disposeAll().
 */
export function krateDisposeExcept(keep: string[]): void {
  if (keep.length === 0) {
    disposeAll();
    return;
  }
  const k = new Set(keep);
  const gone = (scope: string | null | undefined) => !(scope != null && k.has(scope));
  for (const hook of disposeHooks) {
    try {
      hook(k);
    } catch {
      // Unmount errors must not prevent the rest of the teardown.
    }
  }
  for (const effect of Array.from(allEffects)) {
    if (gone(effect.scope)) {
      disposeEffect(effect);
      allEffects.delete(effect);
    }
  }
  const kept: typeof rootCleanups = [];
  for (const cleanup of rootCleanups.splice(0)) {
    if (!gone(cleanup.scope)) {
      kept.push(cleanup);
      continue;
    }
    try {
      cleanup.fn();
    } catch {
      // Cleanup errors must not prevent remaining cleanups or DOM swap.
    }
  }
  rootCleanups.push(...kept);
  const owners = new Set<unknown>([...ownerCleanups.keys(), ...ownerEffects.keys()]);
  for (const owner of owners) {
    if (gone(ownerScopes.get(owner))) krateDisposeOwner(owner);
  }
  // Islands are only mounted by page code (layouts with islands are never kept).
  const g = globalThis as { __krate_island_cleanups?: Array<() => void> };
  const islands = g.__krate_island_cleanups;
  if (Array.isArray(islands)) {
    g.__krate_island_cleanups = [];
    for (const cleanup of islands) {
      try {
        cleanup();
      } catch {
        // Island unmount errors must not prevent the SPA swap.
      }
    }
  }
  routeScope = null;
  resetContexts();
}

export function disposeAll(): void {
  routeScope = null;
  for (const hook of disposeHooks) {
    try {
      hook(null);
    } catch {
      // Unmount errors must not prevent the rest of the teardown.
    }
  }
  for (const effect of allEffects) {
    disposeEffect(effect);
  }
  allEffects.clear();
  for (const cleanup of rootCleanups) {
    try {
      cleanup.fn();
    } catch {
      // Cleanup errors must not prevent remaining cleanups or DOM swap.
    }
  }
  rootCleanups.length = 0;
  for (const owner of Array.from(ownerCleanups.keys())) {
    krateDisposeOwner(owner);
  }
  ownerEffects.clear();
  ownerScopes.clear();
  changedCells.clear();
  historyCells.clear();
  snapshotEpoch = null;
  // Unmount external Preact islands mounted by the page before the DOM swap.
  const g = globalThis as { __krate_island_cleanups?: Array<() => void> };
  const islands = g.__krate_island_cleanups;
  if (Array.isArray(islands)) {
    g.__krate_island_cleanups = [];
    for (const cleanup of islands) {
      try {
        cleanup();
      } catch {
        // Island unmount errors must not prevent the SPA swap.
      }
    }
  }
  pending.length = 0;
  afterQueue.length = 0;
  flushScheduled = false;
  flushMode = 0;
  flushToken++;
  syncLane = false;
  // Reset every context stack to its default so provider values from the page
  // being torn down cannot leak into the next SPA navigation.
  resetContexts();
}

/** ARIA role presets and raw overrides accepted by every CSS primitive. */
export interface CSSARIAOptions {
  /**
   * Accessibility semantics to synthesize. Defaults are native (radios,
   * checkboxes) and ship no JS. Presets that need synthesized state
   * (`tabs`, `listbox`, `accordion`, `disclosure`, `dialog`, `menu`,
   * `popover`) inject a tiny ARIA synchroniser automatically.
   */
  as?:
    | 'radiogroup'
    | 'tabs'
    | 'tablist'
    | 'listbox'
    | 'menu'
    | 'menubar'
    | 'accordion'
    | 'disclosure'
    | 'dialog'
    | 'modal'
    | 'popover'
    | 'switch'
    | 'checkbox';
  /** Accessible label for the scope container. */
  label?: string;
  /** Raw ARIA overrides (`{ role, 'aria-controls', panelRole }`); `false` opts out of all ARIA. */
  aria?: false | Record<string, string>;
}

/**
 * CSS custom properties driven by the selected option. Each property maps an
 * option to a value emitted verbatim (quote strings for `content:`). Every
 * choice/toggle scope also publishes the built-in `--krate-current`.
 *
 * ```ts
 * createCSSChoice('solo', { vars: { '--price': { solo: '"$9"', pro: '"$29"' } } })
 * ```
 */
export type CSSVars<T extends string | number> = Record<string, Partial<Record<T, string>>>;

/** Options accepted by the CSS state primitives. */
export interface CSSPrimitiveOptions<T extends string | number> extends CSSARIAOptions {
  vars?: CSSVars<T>;
  options?: readonly T[];
}

/**
 * Zero-JS mutually-exclusive state (tabs, segments). The Krate compiler rewrites
 * every `createCSSChoice` declaration at build time into hidden radio inputs +
 * `:has()` CSS, so it is **never** emitted to the client at runtime. Reading the
 * getter as JSX text (`{tab()}`) compiles to a live value driven by the
 * inherited `--krate-current` custom property - still zero JS. Custom `vars`
 * publish more inheritable properties for computed text/themes.
 *
 * If a component cannot be compiled to CSS the build does not fall back; it
 * fails with an error telling you to replace the call with `createSignal`.
 *
 * @param initial - The initially-selected option.
 * @param options - A literal option array, or an options object
 *   (`{ options, as, aria, vars, label }`). When omitted, the compiler infers
 *   options from every literal the setter is called with.
 */
export function createCSSChoice<T extends string | number>(
  initial: T,
  _options?: readonly T[] | CSSPrimitiveOptions<T>,
): CSSScope<T> {
  return withScopeHelpers(createSignal<T>(initial));
}

/**
 * Zero-JS boolean state (a single checkbox). Compiler-erased like
 * `createCSSChoice`; the runtime body only exists so type-checking and any
 * server-side evaluation succeed. `{on()}` renders live text via the
 * `--krate-current` property ("on"/"off").
 */
export function createCSSToggle(
  initial: boolean,
  _options?: CSSARIAOptions & { vars?: { on?: string; off?: string } },
): CSSScope<boolean> {
  return withScopeHelpers(createSignal<boolean>(initial));
}

/**
 * Zero-JS independent boolean flags (`createCSSFlags(['a','b'])`). Returns a
 * getter object keyed by flag name plus a setter. Compiler-erased like
 * `createCSSChoice`.
 */
export type CSSFlagsScope<K extends string> = [
  Record<K, () => boolean>,
  (flag: K, value: boolean) => void,
] & {
  trigger: (flag: K) => Record<string, unknown>;
  panel: (flag: K) => Record<string, unknown>;
  close: () => Record<string, unknown>;
};

export function createCSSFlags<K extends string>(
  flags: readonly K[],
  _options?: CSSARIAOptions & { vars?: Record<string, Partial<Record<K, string>>> },
): CSSFlagsScope<K> {
  const getters = {} as Record<K, () => boolean>;
  for (const flag of flags) {
    const [get] = createSignal(false);
    getters[flag] = get;
  }
  // No-op: the compiler compiles flags to checkboxes, so this body is only a
  // type-checking/server-evaluation shim.
  const set = (_flag: K, _value: boolean): void => {};
  const scope = [getters, set] as CSSFlagsScope<K>;
  scope.trigger = (flag: K) => ({ 'data-krate-scope': 'trigger', value: flag });
  scope.panel = (flag: K) => ({ 'data-krate-scope': 'panel', value: flag });
  scope.close = () => ({ 'data-krate-scope': 'close' });
  return scope;
}

/**
 * Zero-JS optional radio group (`createCSSGroup`) - an accordion, disclosure,
 * dialog, menu, or popover. `null` means "closed". Compiler-erased like the
 * other CSS primitives; the runtime body is a type-checking shim only.
 */
export function createCSSGroup<T extends string | number>(
  initial: T | null,
  _options?: readonly T[] | CSSPrimitiveOptions<T>,
): CSSScope<T | null> {
  return withScopeHelpers(createSignal<T | null>(initial));
}

/**
 * Zero-JS discrete range (`createCSSRange`) - a stepped slider, stepper, or
 * progress indicator. Values are integers from `min` to `max` by `step`.
 * Compiler-erased; the runtime body is a type-checking shim only.
 */
export function createCSSRange(
  initial: number,
  _options: CSSARIAOptions & { min?: number; max?: number; step?: number },
): CSSScope<number> {
  return withScopeHelpers(createSignal<number>(initial));
}

/**
 * A first-class CSS-scope handle. It is the `[getter, setter]` tuple returned by
 * the CSS primitives (so destructuring keeps working) with helpers that the Krate
 * compiler expands inline, anywhere in any component:
 *
 * ```tsx
 * const tab = createCSSChoice('a', ['a', 'b']);
 * <button {...tab.trigger('a')}>A</button>
 * <div   {...tab.panel('a')}>Panel A</div>
 * <button {...tab.close()}>Close</button>
 * ```
 *
 * The helpers return a marker spread at runtime; the compiler rewrites them to
 * the `<label for>` / `:has()`-toggled wrapper markup. A handle is a plain
 * value, so it can be passed to child components as a prop.
 */
export type CSSScope<T> = [() => T, (next: T) => void] & {
  /** Spread on the trigger element; selects `value`. */
  trigger: (value?: T) => Record<string, unknown>;
  /** Spread on a panel element; shows it when `value` is selected. */
  panel: (value?: T) => Record<string, unknown>;
  /** Spread on an element that closes the scope (groups/dialogs/popovers). */
  close: () => Record<string, unknown>;
  /** The current selected value (live, via `--krate-current`). */
  current: () => T;
};

function withScopeHelpers<T>(result: [() => T, (next: T) => void]): CSSScope<T> {
  const scope = result as CSSScope<T>;
  scope.trigger = (value?: T) => ({ 'data-krate-scope': 'trigger', value });
  scope.panel = (value?: T) => ({ 'data-krate-scope': 'panel', value });
  scope.close = () => ({ 'data-krate-scope': 'close' });
  scope.current = () => result[0]();
  return scope;
}

/** Actions destructured from a `createCSSStack` result. */
export interface CSSStackActions<K extends string> {
  push: (node: K) => void;
  pop: () => void;
  clear: () => void;
}

/**
 * Zero-JS navigation stack (`createCSSStack`) - a declared tree of nested
 * panels for drill-down menus and multi-level drawers. The compiler infers the
 * tree from where each literal `push('node')` appears; `top()` reads the
 * deepest open node. Compiler-erased; the runtime body is a shim only.
 */
export function createCSSStack<K extends string>(
  _nodes: readonly K[],
  _options?: CSSARIAOptions,
): [{ top: () => K; peek: () => K }, CSSStackActions<K>] {
  const top = (): K => _nodes[0];
  return [
    { top, peek: top },
    { push: (_node: K): void => {}, pop: (): void => {}, clear: (): void => {} },
  ];
}

export function createSignal<T>(
  initial: T,
  options?: SignalOptions<T>,
): [() => T, (next: T | ((prev: T) => T)) => void] {
  let value = initial;

  // Resolve persistence up front. The server-chosen durable state wins over
  // browser storage so hydration matches the server-rendered value.
  let persist: ResolvedPersist<T> | null = null;
  if (options?.persist) {
    persist = resolvePersist(options.persist);
    const durable = readDurableState<T>(persist.key, persist.deserialize);
    if (durable.found) {
      value = durable.value as T;
    } else {
      const stored = readPersisted<T>(persist);
      if (stored.found) {
        value = stored.value as T;
      }
    }
  }

  const subs = new Set<EffectState>();

  // React state: the committed value and its per-commit history.
  const react = !!options?.react;
  let committed = value;
  let history: Array<[number, T]> | null = null;
  const cell: ReactCell | null = react
    ? {
        commit(e: number): void {
          if (Object.is(committed, value)) return;
          if (!history) history = [[0, committed]];
          history.push([e, value]);
          committed = value;
          historyCells.add(cell as ReactCell);
        },
        prune(min: number): boolean {
          if (!history) return false;
          while (history.length > 1 && history[1][0] <= min) history.shift();
          if (history.length === 1) {
            history = null;
            return false;
          }
          return true;
        },
      }
    : null;
  const valueAt = (at: number): T => {
    if (!history) return committed;
    for (let i = history.length - 1; i >= 0; i--) {
      if (history[i][0] <= at) return history[i][1];
    }
    return history[0][1];
  };

  const read = (): T => {
    const current = context[context.length - 1];
    if (current && !current.disposed) {
      subs.add(current);
      current.sources.add(subs);
    }
    if (react) {
      if (snapshotEpoch !== null) return valueAt(snapshotEpoch);
      if (renderDepth === 0) return committed;
    }
    return value;
  };

  const notify = (reactWrite = false): void => notifySubs(subs, reactWrite);

  const write = (next: T | ((prev: T) => T)): void => {
    const resolved = typeof next === 'function' ? (next as (prev: T) => T)(value) : next;
    // Object.is bail-out (React semantics): writing the same value notifies
    // nothing, and NaN written over NaN is "the same value".
    if (!Object.is(resolved, value)) {
      value = resolved;
      if (persist) {
        writePersisted(persist, value);
      }
      if (cell) changedCells.add(cell);
      notify(react);
    }
  };

  // Cross-tab synchronization: another tab changing the same key updates this
  // signal's value (without re-persisting). Unsubscribed on page teardown.
  if (persist && persist.sync) {
    const p = persist;
    const unsubscribe = subscribePersist<T>(p, (raw) => {
      if (raw === null) return;
      try {
        const incoming = p.deserialize(raw);
        if (!Object.is(incoming, value)) {
          value = incoming;
          notify();
        }
      } catch {
        // Ignore malformed external updates.
      }
    });
    onCleanup(unsubscribe);
  }

  signalGetters.add(read);
  return [read, write];
}

// notifySubs schedules every subscriber of a changed source. Derived-value
// observers are marked stale synchronously (and forward the notification to
// their own subscribers), so anything that re-runs in the flush reads a
// recomputed value.
function notifySubs(subs: Set<EffectState>, reactWrite = false): void {
  const list = Array.from(subs);
  for (const effect of list) {
    if (effect.disposed) {
      // Lazy cleanup for effects that were disposed without unsubscribing.
      subs.delete(effect);
      continue;
    }
    if (effect.mark) {
      effect.mark(reactWrite);
      continue;
    }
    if (effect.running) {
      // A feedback loop that writes its own dependencies gets one
      // re-run after the current run; further iterations are dropped to
      // guarantee termination.
      effect.rerun = true;
      continue;
    }
    if (!effect.queued) {
      effect.queued = true;
      pending.push(effect);
    }
  }
  scheduleFlush(reactWrite);
}

const derivedGetters = new WeakSet<object>();

/**
 * A lazily evaluated derived value: the compile target of a React render-body
 * value (`const total = rows.reduce(...)`). The getter recomputes on read when
 * a signal (or another derived value) it read last time has changed since, so
 * every reader in a flush sees the same current value. Nothing is computed
 * until the first read, and a write only marks it stale. triggers, when
 * given, is read for its subscriptions only: an object or array literal built
 * in render gets a new identity whenever the component would re-render.
 */
export function krateDerived<T>(fn: () => T, triggers?: () => unknown): () => T {
  let value: T;
  let stale = true;
  const subs = new Set<EffectState>();
  const node = newEffectState(() => {});
  node.mark = (reactWrite: boolean) => {
    if (!stale) {
      stale = true;
      notifySubs(subs, reactWrite);
    }
  };
  allEffects.add(node);

  const read = (): T => {
    const current = context[context.length - 1];
    if (current && !current.disposed) {
      subs.add(current);
      current.sources.add(subs);
    }
    // Outside render (an event handler, an effect setup) React state reads
    // see a committed snapshot. When that snapshot can differ from the latest
    // values, compute from it without touching the cache, which always holds
    // the latest render value.
    if (
      (renderDepth === 0 || snapshotEpoch !== null) &&
      (changedCells.size > 0 || (snapshotEpoch !== null && historyCells.size > 0))
    ) {
      return untrack(fn);
    }
    if (stale && !node.disposed) {
      for (const set of node.sources) {
        set.delete(node);
      }
      node.sources.clear();
      // Cleared before computing so a source written during the computation
      // marks it stale again instead of being lost.
      stale = false;
      context.push(node);
      try {
        // Extra sources (a render literal's state and props) only subscribe.
        if (triggers) triggers();
        value = fn();
      } catch (err) {
        stale = true;
        throw err;
      } finally {
        context.pop();
      }
    } else if (node.disposed) {
      return untrack(fn);
    }
    return value;
  };

  // Disposed with the binding (or component) that created it.
  const parent = context[context.length - 1];
  if (parent) {
    parent.cleanups.push(() => {
      disposeEffect(node);
      allEffects.delete(node);
    });
  }
  derivedGetters.add(read);
  signalGetters.add(read);
  return read;
}

/** Reports whether fn is a getter returned by krateDerived. */
export function isDerivedGetter(fn: unknown): boolean {
  return typeof fn === 'function' && derivedGetters.has(fn);
}

/**
 * Normalizes a custom hook's return value to a getter. A hook compiled from
 * React source returns a derived getter for a render-dependent result; any
 * other value (a hook from a plain module, a constant) is wrapped so the call
 * site can read every hook result the same way.
 */
export function krateHook<T>(value: T | (() => T)): () => T {
  if (isDerivedGetter(value)) return value as () => T;
  return () => value as T;
}

// Getters returned by createSignal, so the React compatibility hooks can read
// a dependency that is a signal getter instead of comparing the function.
const signalGetters = new WeakSet<object>();

/** Reports whether fn is a getter returned by createSignal. */
export function isSignalGetter(fn: unknown): boolean {
  return typeof fn === 'function' && signalGetters.has(fn);
}

// 0: none, 1: microtask, 2: later task.
let flushMode = 0;
let flushToken = 0;

function scheduleFlush(reactWrite = false): void {
  // Transition writes never take the sync lane, even when the microtask that
  // makes them still sees the discrete event that started the action.
  const discrete = transitionWrites === 0 && inDiscreteEvent();
  if (discrete) syncLane = true;
  const later = reactWrite || transitionWrites > 0;
  const micro = !later || discrete || committing > 0 || renderDepth > 0 || typeof setTimeout !== 'function';
  if (flushMode === 1 || (!micro && flushMode === 2)) return;
  flushScheduled = true;
  const token = ++flushToken;
  const run = (): void => {
    if (token === flushToken) flushEffects();
  };
  if (micro) {
    flushMode = 1;
    queueMicrotask(run);
  } else {
    flushMode = 2;
    setTimeout(run, 0);
  }
}

let flushDepth = 0;

function flushEffects(): void {
  flushDepth++;
  try {
    flushEffectsInner();
  } finally {
    flushDepth--;
  }
}

function flushEffectsInner(): void {
  flushScheduled = false;
  flushMode = 0;
  flushToken++;
  for (const hook of beforeFlushHooks) {
    hook();
  }
  // Effects re-queued by writes during a pass run in further passes of this
  // same flush, so the cascade settles and commits before any other task or
  // microtask can run. Otherwise code running between passes (an action's
  // settle callback) would read React state that is written but not yet
  // committed.
  for (let pass = 0; pending.length > 0 && pass < 100; pass++) {
    const effects = pending.splice(0);
    for (const effect of effects) {
      if (effect.disposed) {
        continue;
      }
      effect.queued = false;
      effect.rerun = false;
      runEffect(effect);
      if (effect.rerun && !effect.disposed) {
        effect.rerun = false;
        runEffect(effect);
      }
    }
  }
  if (pending.length === 0) {
    // The cascade is done: a flush the passes scheduled has nothing to do.
    flushMode = 0;
    flushScheduled = false;
    flushToken++;
  }
  // A cascade longer than the pass cap continues in the scheduled flush; the
  // commit runs once it settles, so it sees the final DOM.
  if (pending.length === 0) {
    // Settle hooks (the class component render phase) may write signals;
    // flush those in this same pass so the commit sees every DOM write.
    for (const hook of settleHooks) {
      hook();
    }
    if (pending.length > 0 && settleDepth < 50) {
      settleDepth++;
      try {
        flushEffects();
      } finally {
        settleDepth--;
      }
      return;
    }
    runAfterFlush();
  }
}

const settleHooks: Array<() => void> = [];
let settleDepth = 0;

/**
 * Registers fn to run when a flush has no pending effects, before the
 * after-flush hooks. Signal writes it makes are flushed first (internal).
 */
export function krateOnSettle(fn: () => void): void {
  settleHooks.push(fn);
}

/** Ensures a flush runs soon even when no signal was written (internal). */
export function krateRequestFlush(): void {
  scheduleFlush();
}

/**
 * Runs the after-flush hooks now. Used when no signal flush is pending (for
 * example, right after hydration registered effects).
 */
export function runAfterFlush(): void {
  // Every settled flush is a commit: record React state here, so committed
  // values advance even when no effect scheduler is loaded.
  if (changedCells.size > 0) krateCommitSnapshot();
  // React's commit reads document.activeElement (to restore focus and
  // selection). In Firefox that read applies the focus fixup rule, so a
  // focused button the commit disabled loses focus; reading it here gives
  // each browser the same focus outcome as under React.
  if (typeof document !== 'undefined') void document.activeElement;
  for (const hook of afterFlushHooks) {
    hook();
  }
  drainAfterQueue();
}

/**
 * Synchronously flushes pending signal effects and the commit that follows
 * them (the building block for flushSync and tests).
 */
export function krateFlushSync(): void {
  if (pending.length === 0) {
    runAfterFlush();
    return;
  }
  // flushEffects runs the after-flush hooks itself once nothing is pending.
  let guard = 0;
  while (pending.length > 0 && guard++ < 100) {
    flushEffects();
  }
}

function runEffect(effect: EffectState): void {
  effect.running = true;
  let pushed = false;
  const prevOwner = currentOwner;
  currentOwner = effect.owner;
  renderDepth++;
  // A tracked run is render work: it sees the latest values even when it is
  // started from inside an effect setup or cleanup.
  const prevEpoch = snapshotEpoch;
  snapshotEpoch = null;
  try {
    // Run cleanups from previous execution
    for (const cleanup of effect.cleanups.splice(0)) {
      cleanup();
    }
    context.push(effect);
    pushed = true;
    // Support Solid-style `return () => {...}` cleanup functions. Many
    // components (Slider, Dropdown, Dialog, Tooltip) register their document
    // listeners in an effect and return a cleanup that removes them; without
    // capturing the return value those listeners leak and the component never
    // "unsubscribes" (e.g. a slider that can't stop dragging).
    const ret = effect.fn();
    if (typeof ret === 'function') {
      effect.cleanups.push(ret as () => void);
    }
  } catch (err) {
    // An owned effect reports to its owner (error boundaries); unowned
    // effects keep the previous behavior and throw.
    if (!(effect.boundaryOwner && ownerHooks && ownerHooks.error(effect.boundaryOwner, err))) {
      throw err;
    }
  } finally {
    snapshotEpoch = prevEpoch;
    renderDepth--;
    currentOwner = prevOwner;
    if (pushed) context.pop();
    effect.running = false;
  }
}

export function createEffect(fn: () => void): () => void {
  const effect = newEffectState(fn);
  allEffects.add(effect);
  // An effect created while an ownership root is current is disposed with it,
  // and an effect created under a component owner is disposed when that
  // component unmounts.
  const owner = context[context.length - 1];
  const dispose = (): void => {
    disposeEffect(effect);
    allEffects.delete(effect);
  };
  if (effect.boundaryOwner && ownerHooks) {
    ownerHooks.cleanup(effect.boundaryOwner, dispose);
  }
  runEffect(effect);
  if (owner && owner.root) addOwnerCleanup(owner, dispose);
  return dispose;
}

export function onCleanup(fn: () => void): void {
  const current = currentEffect();
  if (current) {
    addOwnerCleanup(current, fn);
    return;
  }
  const owner = ownerHooks ? ownerHooks.current() : null;
  if (owner && ownerHooks) {
    // Component-level cleanup: runs when the owning component unmounts.
    ownerHooks.cleanup(owner, fn);
  } else {
    // No active effect: a component-level cleanup. It runs when its owner
    // unmounts, or on disposeAll() (route change / page teardown).
    if (currentOwner !== null) {
      krateTagOwner(currentOwner, krateCurrentScope());
      let list = ownerCleanups.get(currentOwner);
      if (!list) ownerCleanups.set(currentOwner, (list = []));
      list.push(fn);
    } else {
      rootCleanups.push({ fn, scope: krateCurrentScope() });
    }
  }
}

function addOwnerCleanup(owner: EffectState, fn: () => void): void {
  if (owner.dead) {
    // The root is already gone (a late mount callback): clean up at once.
    fn();
    return;
  }
  owner.cleanups.push(fn);
}

/**
 * Run `fn` in a new ownership root. Effects, memos, onCleanup callbacks and
 * onMount callbacks registered while it runs (and the cleanups those mount
 * callbacks register) belong to the root and are torn down, in registration
 * order, when the returned `dispose` is called. Signal reads inside `fn` are
 * not tracked by any enclosing effect. Keyed list rows and keyed remounts use
 * one root per row so a removed row runs its cleanups without touching its
 * siblings.
 */
export function createRoot<T>(fn: (dispose: () => void) => T): T {
  const root = newEffectState(() => {});
  // `disposed` keeps signal reads inside the root untracked.
  root.disposed = true;
  root.root = true;
  const dispose = (): void => {
    if (root.dead) return;
    root.dead = true;
    const cleanups = root.cleanups.splice(0);
    for (const cleanup of cleanups) {
      try {
        cleanup();
      } catch (err) {
        console.error(err);
      }
    }
  };
  context.push(root);
  try {
    return fn(dispose);
  } finally {
    context.pop();
  }
}

export function onMount(fn: () => void): void {
  mountQueue.push({ fn, owner: context[context.length - 1], boundary: ownerHooks ? ownerHooks.current() : null });
  if (!mountScheduled) {
    mountScheduled = true;
    queueMicrotask(flushMounts);
  }
}

function flushMounts(): void {
  mountScheduled = false;
  const queue = mountQueue.splice(0);
  for (const { fn, owner, boundary } of queue) {
    const run = (): void => {
      if (!owner) {
        fn();
        return;
      }
      // The owner unmounted before its mount effects ran: skip them, as React
      // does for a component that never committed.
      if (owner.dead || (!owner.root && owner.disposed)) return;
      // Collect cleanups registered by the callback without tracking its reads.
      const collector = newEffectState(() => {});
      collector.disposed = true;
      context.push(collector);
      try {
        fn();
      } finally {
        context.pop();
      }
      for (const cleanup of collector.cleanups) addOwnerCleanup(owner, cleanup);
    };
    if (boundary && ownerHooks) ownerHooks.run(boundary, run);
    else run();
  }
}

// createMemo computes a derived value. Its computation effect is registered
// with the current parent context so it is disposed when the parent re-runs or
// is disposed (prevents leaks). A value-equality check suppresses propagation
// when the recomputed value is unchanged, avoiding thundering re-renders.
export function createMemo<T>(fn: () => T): () => T {
  const [get, set] = createSignal<T>(undefined as T);

  const effect = newEffectState(() => {
    const next = fn();
    set(next);
  });

  allEffects.add(effect);

  // Run the effect to establish subscriptions (pushes to context briefly for tracking)
  runEffect(effect);

  // Register cleanup on the parent context so this memo effect is disposed
  // when the parent re-runs or is disposed.
  const parent = currentEffect();
  const disposeMemo = (): void => {
    if (!effect.disposed) {
      disposeEffect(effect);
      allEffects.delete(effect);
    }
  };
  if (parent) {
    parent.cleanups.push(disposeMemo);
  } else if (effect.boundaryOwner && ownerHooks) {
    ownerHooks.cleanup(effect.boundaryOwner, disposeMemo);
  }

  return get;
}

/**
 * Reducer state primitive: a `createSignal` whose setter applies a reducer.
 * Returns `[getter, dispatch]` - the getter contract matches `createSignal`, so
 * compiler slot bindings read it as `getter()` and hydration treats it like any
 * other signal. Used both directly and as the lowering target for React's
 * `useReducer`.
 */
export function createReducer<S, A, I = S>(
  reducer: (state: S, action: A) => S,
  initial: I,
  init?: ((arg: I) => S) | SignalOptions<S> | null,
  options?: SignalOptions<S>,
): [() => S, (action: A) => void] {
  // React's lazy initializer: useReducer(reducer, arg, init) starts at init(arg).
  // An options object may take init's place (createReducer(r, s, opts)).
  if (init && typeof init === 'object') {
    options = init;
    init = undefined;
  }
  const start = (typeof init === 'function' ? init(initial) : initial) as S;
  const [get, set] = createSignal<S>(start, options);
  // dispatch is created once, so its identity is stable for the component's
  // lifetime. The signal's Object.is bail-out skips updates when the reducer
  // returns the current state.
  const dispatch = (action: A): void => set((prev) => reducer(prev, action));
  return [get, dispatch];
}

/** A mutable ref object. `current` starts as `initial` and is assigned the
 * DOM element when passed as `ref={refObj}` on an element. */
export interface RefObject<T> {
  current: T;
}

/** A ref that may be a plain callback or a `{current}` object. */
export type Ref<T> = RefCallback<T> | RefObject<T | null>;

/** A callback-style ref: invoked with the element (or null on cleanup). */
export type RefCallback<T> = (el: T) => void;

/** Result type of the {@link useRef} hook's component-level ref: an object
 * whose `current` is set by `ref={refObj}` bindings. */
export type UseRefResult<T> = RefObject<T>;

/**
 * Create a mutable ref object. Unlike React, `useRef` always allocates a
 * fresh `{current: initial}` object; Krate components run their body once per
 * mount (client) and once per evaluation (SSR), so the object naturally
 * persists for the component's lifetime without additional bookkeeping.
 *
 * Pass the object as `ref={myRef}` on a DOM element to have `.current` set to
 * the element, or read/write `.current` directly for imperative handles.
 */
export function useRef<T>(initial: T): UseRefResult<T> {
  return { current: initial };
}

/**
 * Pass `ref` through a forwarding component to the underlying element or
 * component. The ref may be either a callback or a `{current}` object; both
 * are invoked/assigned with the forwarded element.
 */
export function forwardRef<P>(
  renderFn: (props: P, ref: Ref<Element>) => Node | null | undefined
): (props: P & { ref?: Ref<Element> }) => Node | null | undefined {
  return (props: P & { ref?: Ref<Element> }) =>
    renderFn(props, props.ref || ((() => {}) as RefCallback<Element>));
}
