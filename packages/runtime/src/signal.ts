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

const context: Array<EffectState> = [];
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
}

function newEffectState(fn: () => void): EffectState {
  return { fn, cleanups: [], disposed: false, sources: new Set(), running: false, rerun: false, queued: false };
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
 * inherited `--krate-current` custom property — still zero JS. Custom `vars`
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
): [() => T, (next: T) => void] {
  return createSignal<T>(initial);
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
): [() => boolean, (next: boolean) => void] {
  return createSignal<boolean>(initial);
}

/**
 * Zero-JS independent boolean flags (`createCSSFlags(['a','b'])`). Returns a
 * getter object keyed by flag name plus a setter. Compiler-erased like
 * `createCSSChoice`.
 */
export function createCSSFlags<K extends string>(
  flags: readonly K[],
  _options?: CSSARIAOptions & { vars?: Record<string, Partial<Record<K, string>>> },
): [Record<K, () => boolean>, (flag: K, value: boolean) => void] {
  const getters = {} as Record<K, () => boolean>;
  for (const flag of flags) {
    const [get] = createSignal(false);
    getters[flag] = get;
  }
  // No-op: the compiler compiles flags to checkboxes, so this body is only a
  // type-checking/server-evaluation shim.
  const set = (_flag: K, _value: boolean): void => {};
  return [getters, set];
}

/**
 * Zero-JS optional radio group (`createCSSGroup`) — an accordion, disclosure,
 * dialog, menu, or popover. `null` means "closed". Compiler-erased like the
 * other CSS primitives; the runtime body is a type-checking shim only.
 */
export function createCSSGroup<T extends string | number>(
  initial: T | null,
  _options?: readonly T[] | CSSPrimitiveOptions<T>,
): [() => T | null, (next: T | null) => void] {
  return createSignal<T | null>(initial);
}

/**
 * Zero-JS discrete range (`createCSSRange`) — a stepped slider, stepper, or
 * progress indicator. Values are integers from `min` to `max` by `step`.
 * Compiler-erased; the runtime body is a type-checking shim only.
 */
export function createCSSRange(
  initial: number,
  _options: CSSARIAOptions & { min?: number; max?: number; step?: number },
): [() => number, (next: number) => void] {
  return createSignal<number>(initial);
}

/** Actions destructured from a `createCSSStack` result. */
export interface CSSStackActions<K extends string> {
  push: (node: K) => void;
  pop: () => void;
  clear: () => void;
}

/**
 * Zero-JS navigation stack (`createCSSStack`) — a declared tree of nested
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
    if (resolved !== value) {
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
        if (incoming !== value) {
          value = incoming;
          notify();
        }
      } catch {
        // Ignore malformed external updates.
      }
    });
    onCleanup(unsubscribe);
  }

  return [read, write];
}

function scheduleFlush(): void {
  if (!flushScheduled) {
    flushScheduled = true;
    queueMicrotask(flushEffects);
  }
}

function flushEffects(): void {
  flushScheduled = false;
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

function runEffect(effect: EffectState): void {
  effect.running = true;
  // Run cleanups from previous execution
  for (const cleanup of effect.cleanups) {
    cleanup();
  }
  effect.cleanups.length = 0;

  context.push(effect);
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
 * Returns `[getter, dispatch]` — the getter contract matches `createSignal`, so
 * compiler slot bindings read it as `getter()` and hydration treats it like any
 * other signal. Used both directly and as the lowering target for React's
 * `useReducer`.
 */
export function createReducer<S, A>(
  reducer: (state: S, action: A) => S,
  initial: S,
): [() => S, (action: A) => void] {
  const [get, set] = createSignal<S>(initial);
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
