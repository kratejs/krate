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

const REQUIRED = ['createSignal', 'createEffect', 'createRoot', 'onCleanup', 'untrack'] as const;

/** Returns the page's Krate runtime, or throws when it is not loaded. */
export function rt(): KrateRuntime {
  const g = globalThis as unknown as KrateRuntime;
  for (const name of REQUIRED) {
    if (typeof (g as unknown as Record<string, unknown>)[name] !== 'function') {
      throw new Error(`@krate/three: the Krate runtime is not loaded (missing ${name}). @krate/three runs in the browser inside a Krate page.`);
    }
  }
  return g;
}

/** True when the Krate runtime is available (a browser page, or a test that installed it). */
export function hasRuntime(): boolean {
  return typeof (globalThis as Record<string, unknown>).createEffect === 'function';
}

/** Reads without subscribing the current effect. */
export function untrack<T>(fn: () => T): T {
  return hasRuntime() ? rt().untrack(fn) : fn();
}

/** Attaches `value` to a ref (object or callback) and returns the detach function. */
export function setRef(ref: unknown, value: unknown): () => void {
  if (ref == null) return () => {};
  const g = globalThis as unknown as Partial<KrateRuntime>;
  if (typeof g.krateSetRef === 'function') return g.krateSetRef(ref, value);
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
