// The engine is bundled for the browser like an npm dependency, so it must not
// import a second copy of the Krate runtime. It uses the page runtime, which
// attaches its API to the global object, and resolves it on every call so the
// module can load before the runtime does (tests install it afterwards).

/** The subset of the Krate runtime the engine uses. */
export interface KrateRuntime {
  createSignal<T>(initial: T): [() => T, (next: T | ((prev: T) => T)) => void];
  createEffect(fn: () => void): () => void;
  createRoot<T>(fn: (dispose: () => void) => T): T;
  onCleanup(fn: () => void): void;
  untrack<T>(fn: () => T): T;
  krateDerived<T>(fn: () => T): () => T;
  krateWithOwner<T>(fn: () => T): { value: T; owner: unknown };
  krateUnmount(owner: unknown): void;
  krateGetOwner(): unknown;
  krateSetOwner(owner: unknown): unknown;
  krateSetRef(ref: unknown, value: unknown): () => void;
  h: (tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]) => unknown;
}

// The build ships only the runtime exports page code names, and it sees a
// name only as a literal global member, so each one is read as globalThis.x
// here rather than through an alias or a computed key.
function readRuntime(): Partial<KrateRuntime> {
  return {
    createSignal: globalThis.createSignal,
    createEffect: globalThis.createEffect,
    createRoot: globalThis.createRoot,
    onCleanup: globalThis.onCleanup,
    untrack: globalThis.untrack,
    krateDerived: globalThis.krateDerived,
    krateWithOwner: globalThis.krateWithOwner,
    krateUnmount: globalThis.krateUnmount,
    krateGetOwner: globalThis.krateGetOwner,
    krateSetOwner: globalThis.krateSetOwner,
    krateSetRef: globalThis.krateSetRef,
    h: globalThis.h,
  };
}

const REQUIRED = ['createSignal', 'createEffect', 'createRoot', 'onCleanup', 'untrack'] as const;
let cached: KrateRuntime | null = null;

/** Returns the page's Krate runtime, or throws when it is not loaded. */
export function rt(): KrateRuntime {
  if (cached && cached.createEffect === globalThis.createEffect) return cached;
  const g = readRuntime();
  for (const name of REQUIRED) {
    if (typeof g[name] !== 'function') {
      throw new Error(`@krate/three: the Krate runtime is not loaded (missing ${name}). @krate/three runs in the browser inside a Krate page.`);
    }
  }
  cached = g as KrateRuntime;
  return cached;
}

/** True when the Krate runtime is available (a browser page, or a test that installed it). */
export function hasRuntime(): boolean {
  return typeof globalThis.createEffect === 'function';
}

/** Reads without subscribing the current effect. */
export function untrack<T>(fn: () => T): T {
  return hasRuntime() ? rt().untrack(fn) : fn();
}

/** Attaches `value` to a ref (object or callback) and returns the detach function. */
export function setRef(ref: unknown, value: unknown): () => void {
  if (ref == null) return () => {};
  const set = globalThis.krateSetRef;
  if (typeof set === 'function') return set(ref, value);
  if (typeof ref === 'function') {
    const cleanup = (ref as (v: unknown) => unknown)(value);
    return () => {
      if (typeof cleanup === 'function') cleanup();
      else (ref as (v: unknown) => unknown)(null);
    };
  }
  (ref as { current: unknown }).current = value;
  return () => {
    if ((ref as { current: unknown }).current === value) (ref as { current: unknown }).current = null;
  };
}

declare global {
  // The page runtime's globals the engine reads (see readRuntime).
  var createSignal: KrateRuntime['createSignal'] | undefined;
  var createEffect: KrateRuntime['createEffect'] | undefined;
  var createRoot: KrateRuntime['createRoot'] | undefined;
  var onCleanup: KrateRuntime['onCleanup'] | undefined;
  var untrack: KrateRuntime['untrack'] | undefined;
  var krateDerived: KrateRuntime['krateDerived'] | undefined;
  var krateWithOwner: KrateRuntime['krateWithOwner'] | undefined;
  var krateUnmount: KrateRuntime['krateUnmount'] | undefined;
  var krateGetOwner: KrateRuntime['krateGetOwner'] | undefined;
  var krateSetOwner: KrateRuntime['krateSetOwner'] | undefined;
  var krateSetRef: KrateRuntime['krateSetRef'] | undefined;
  var h: KrateRuntime['h'] | undefined;
  var krateWithContexts: (<R>(entries: unknown[], fn: () => R) => R) | undefined;
  var krateFlushSync: (() => void) | undefined;
}
