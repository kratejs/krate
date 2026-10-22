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
}

// A null entry is an untracked scope (see untrack): reads inside it subscribe
// nothing.
const context: Array<EffectState | null> = [];
const mountQueue: Array<() => void> = [];
let mountScheduled = false;
const allEffects = new Set<EffectState>();
// Top-level onCleanup registrations (called outside an effect body) are scoped
// to the page/root and run when disposeAll() executes on route change. This
// gives component-level cleanup semantics: a component's top-level onCleanup
// registers once when its hydration IIFE runs and is invoked on unmount.
const rootCleanups: Array<() => void> = [];

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
}

function newEffectState(fn: () => void): EffectState {
  return { fn, cleanups: [], disposed: false, sources: new Set(), running: false, rerun: false, queued: false, owner: currentOwner };
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
const disposeHooks: Array<() => void> = [];

/** Registers fn to run before every effect flush starts (internal). */
export function krateOnBeforeFlush(fn: () => void): void {
  beforeFlushHooks.push(fn);
}

/** Registers fn to run after every settled effect flush (internal). */
export function krateOnAfterFlush(fn: () => void): void {
  afterFlushHooks.push(fn);
}

/** Registers fn to run at the start of disposeAll (internal). */
export function krateOnDispose(fn: () => void): void {
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

export function disposeAll(): void {
  for (const hook of disposeHooks) {
    try {
      hook();
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
      cleanup();
    } catch {
      // Cleanup errors must not prevent remaining cleanups or DOM swap.
    }
  }
  rootCleanups.length = 0;
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
  flushScheduled = false;
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

  const read = (): T => {
    const current = context[context.length - 1];
    if (current && !current.disposed) {
      subs.add(current);
      current.sources.add(subs);
    }
    return value;
  };

  const notify = (): void => {
    const list = Array.from(subs);
    for (const effect of list) {
      if (effect.disposed) {
        // Lazy cleanup for effects that were disposed without unsubscribing.
        subs.delete(effect);
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
    scheduleFlush();
  };

  const write = (next: T | ((prev: T) => T)): void => {
    const resolved = typeof next === 'function' ? (next as (prev: T) => T)(value) : next;
    // Object.is bail-out (React semantics): writing the same value notifies
    // nothing, and NaN written over NaN is "the same value".
    if (!Object.is(resolved, value)) {
      value = resolved;
      if (persist) {
        writePersisted(persist, value);
      }
      notify();
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

// Getters returned by createSignal, so the React compatibility hooks can read
// a dependency that is a signal getter instead of comparing the function.
const signalGetters = new WeakSet<object>();

/** Reports whether fn is a getter returned by createSignal. */
export function isSignalGetter(fn: unknown): boolean {
  return typeof fn === 'function' && signalGetters.has(fn);
}

function scheduleFlush(): void {
  if (!flushScheduled) {
    flushScheduled = true;
    queueMicrotask(flushEffects);
  }
}

function flushEffects(): void {
  flushScheduled = false;
  for (const hook of beforeFlushHooks) {
    hook();
  }
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
  // Writes made by these effects scheduled another flush; the commit runs
  // once that cascade settles, so it sees the final DOM.
  if (pending.length === 0) {
    runAfterFlush();
  }
}

/**
 * Runs the after-flush hooks now. Used when no signal flush is pending (for
 * example, right after hydration registered effects).
 */
export function runAfterFlush(): void {
  for (const hook of afterFlushHooks) {
    hook();
  }
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
  // Run cleanups from previous execution
  for (const cleanup of effect.cleanups) {
    cleanup();
  }
  effect.cleanups.length = 0;

  context.push(effect);
  const prevOwner = currentOwner;
  currentOwner = effect.owner;
  try {
    // Support Solid-style `return () => {...}` cleanup functions. Many
    // components (Slider, Dropdown, Dialog, Tooltip) register their document
    // listeners in an effect and return a cleanup that removes them; without
    // capturing the return value those listeners leak and the component never
    // "unsubscribes" (e.g. a slider that can't stop dragging).
    const ret = effect.fn();
    if (typeof ret === 'function') {
      effect.cleanups.push(ret as () => void);
    }
  } finally {
    currentOwner = prevOwner;
    context.pop();
    effect.running = false;
  }
}

export function createEffect(fn: () => void): () => void {
  const effect = newEffectState(fn);
  allEffects.add(effect);
  runEffect(effect);

  return () => {
    disposeEffect(effect);
    allEffects.delete(effect);
  };
}

export function onCleanup(fn: () => void): void {
  const current = context[context.length - 1];
  if (current) {
    current.cleanups.push(fn);
  } else {
    // No active effect: treat as a root/component-level cleanup, run on
    // disposeAll() (route change / page teardown).
    rootCleanups.push(fn);
  }
}

export function onMount(fn: () => void): void {
  mountQueue.push(fn);
  if (!mountScheduled) {
    mountScheduled = true;
    queueMicrotask(flushMounts);
  }
}

function flushMounts(): void {
  mountScheduled = false;
  const queue = mountQueue.splice(0);
  for (const fn of queue) {
    fn();
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
  const parent = context[context.length - 1];
  if (parent) {
    parent.cleanups.push(() => {
      if (!effect.disposed) {
        disposeEffect(effect);
        allEffects.delete(effect);
      }
    });
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
  init?: (arg: I) => S,
): [() => S, (action: A) => void] {
  // React's lazy initializer: useReducer(reducer, arg, init) starts at init(arg).
  const start = (typeof init === 'function' ? init(initial) : initial) as S;
  const [get, set] = createSignal<S>(start);
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
