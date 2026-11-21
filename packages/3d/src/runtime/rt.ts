// Like @krate/three's engine, this code is bundled for the browser as an npm
// dependency and must not import a second copy of the Krate runtime. It uses
// the page runtime, which attaches its API to the global object.

export interface KrateRuntime {
  createSignal<T>(initial: T): [() => T, (next: T | ((prev: T) => T)) => void];
  createEffect(fn: () => void): () => void;
  onCleanup(fn: () => void): void;
  untrack<T>(fn: () => T): T;
}

/**
 * Returns the page's Krate runtime, or throws when it is not loaded. The reads
 * are written out as `globalThis.name` on purpose: the build keeps only the
 * runtime exports it can see the code read through the global object, and a
 * loop over names would hide them.
 */
export function rt(): KrateRuntime {
  // Direct reads, not through a local alias: the build's scan sees only
  // `globalThis.name` members.
  const api: KrateRuntime = {
    createSignal: (globalThis as unknown as KrateRuntime).createSignal,
    createEffect: (globalThis as unknown as KrateRuntime).createEffect,
    onCleanup: (globalThis as unknown as KrateRuntime).onCleanup,
    untrack: (globalThis as unknown as KrateRuntime).untrack,
  };
  for (const [name, fn] of Object.entries(api)) {
    if (typeof fn !== 'function') {
      throw new Error(`@krate/3d: the Krate runtime is not loaded (missing ${name}). @krate/3d runs in the browser inside a Krate page.`);
    }
  }
  return api;
}
