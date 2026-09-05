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
}

export function createSignal<T>(initial: T): [() => T, (next: T | ((prev: T) => T)) => void] {
  let value = initial;
  const subs = new Set<EffectState>();

  const read = (): T => {
    const current = context[context.length - 1];
    if (current && !current.disposed) {
      subs.add(current);
      current.sources.add(subs);
    }
    return value;
  };

  const write = (next: T | ((prev: T) => T)): void => {
    const resolved = typeof next === 'function' ? (next as (prev: T) => T)(value) : next;
    if (resolved !== value) {
      value = resolved;
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
    }
  };

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
