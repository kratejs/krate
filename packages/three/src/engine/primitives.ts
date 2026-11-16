// Krate-style scene primitives. They do what the hooks in hooks.ts do, in
// Krate's create* form: state comes back as an accessor (call it to read), and
// inputs may be accessors that the primitive follows.

import type * as THREE from 'three';
import type { ConstructorRepresentation } from './extend.js';
import {
  type CacheEntry,
  loadEntry,
  onLoadSettled,
  useLoader,
  useStore,
  type Extensions,
  type InputLike,
  type LoaderLike,
  type LoaderResult,
} from './hooks.js';
import { hasRuntime, rt, untrack } from './runtime.js';
import type { RenderCallback, RootState } from './store.js';
import { buildGraph, type ObjectMap } from './utils.js';

/** A reactive read: call it to get the current value. */
export type Accessor<T> = () => T;

/** A value, or an accessor of one. */
export type MaybeAccessor<T> = T | Accessor<T>;

const read = <T>(value: MaybeAccessor<T>): T =>
  typeof value === 'function' ? (value as Accessor<T>)() : value;

// A derived accessor that lives as long as the calling component.
function derive<T>(fn: () => T): Accessor<T> {
  if (!hasRuntime()) return fn;
  const runtime = rt();
  let release: () => void = () => {};
  const getter = runtime.createRoot((dispose) => {
    release = dispose;
    return runtime.krateDerived(fn);
  });
  runtime.onCleanup(release);
  return getter;
}

/**
 * Selects from the root state (renderer, camera, scene, size, ...) of the
 * enclosing `<Canvas>`. The accessor updates when the selected value changes:
 * `const size = createThree((s) => s.size); size().width`.
 */
export function createThree(): Accessor<RootState>;
export function createThree<T>(selector: (state: RootState) => T, equalityFn?: (a: T, b: T) => boolean): Accessor<T>;
export function createThree<T>(
  selector: (state: RootState) => T = (state) => state as unknown as T,
  equalityFn?: (a: T, b: T) => boolean,
): Accessor<T> {
  const store = useStore();
  if (!hasRuntime()) return () => selector(store.getState());
  return store(selector, equalityFn) as unknown as Accessor<T>;
}

/**
 * Runs `callback` before every frame of the enclosing `<Canvas>` until the
 * component unmounts or the returned function is called. `priority` works as
 * in useFrame (a positive priority takes over rendering); pass an accessor to
 * change it while mounted.
 */
export function createFrame(callback: RenderCallback, priority: MaybeAccessor<number> = 0): () => void {
  const store = useStore();
  const ref = { current: callback };
  let unsubscribe: (() => void) | undefined;
  let stopped = false;
  const subscribe = (value: number): void => {
    unsubscribe?.();
    unsubscribe = store.getState().internal.subscribe(ref, value, store);
  };
  let disposeEffect: (() => void) | undefined;
  if (typeof priority === 'function' && hasRuntime()) {
    disposeEffect = rt().createEffect(() => {
      const value = (priority as Accessor<number>)();
      if (!stopped) untrack(() => subscribe(value));
    });
  } else {
    subscribe(read(priority));
  }
  const stop = (): void => {
    if (stopped) return;
    stopped = true;
    disposeEffect?.();
    unsubscribe?.();
    unsubscribe = undefined;
  };
  if (hasRuntime()) rt().onCleanup(stop);
  return stop;
}

/** The accessor createLoader returns. */
export interface LoaderAccessor<T> {
  /** The loaded asset. Suspends the reading part of the scene while loading. */
  (): T;
  /** True while the current input is loading. */
  readonly loading: boolean;
  /** The load error, if the current input failed to load. */
  readonly error: unknown;
  /** 'loading', 'ready' or 'error'. */
  readonly state: 'loading' | 'ready' | 'error';
  /** The asset when it has loaded, otherwise undefined. Never suspends. */
  readonly latest: T | undefined;
}

type LoaderFor<I, L extends LoaderLike | ConstructorRepresentation<LoaderLike>> = I extends any[]
  ? LoaderResult<L>[]
  : LoaderResult<L>;

/**
 * Loads assets with a three loader, cached like useLoader. `input` may be an
 * accessor: when it changes, the new asset loads. Reading the accessor while
 * loading suspends the nearest `<Suspense>` in the scene; `loading`, `state`
 * and `latest` read the progress without suspending (for example inside
 * createFrame).
 */
export function createLoader<I extends InputLike, L extends LoaderLike | ConstructorRepresentation<LoaderLike>>(
  loader: L,
  input: MaybeAccessor<I>,
  extensions?: Extensions<L>,
  onProgress?: (event: ProgressEvent<EventTarget>) => void,
): LoaderAccessor<LoaderFor<I, L>> {
  // Readers re-run when the entry they read settles (or is replaced).
  let version: Accessor<number> = () => 0;
  let watched: CacheEntry | undefined;
  if (hasRuntime()) {
    const runtime = rt();
    const [get, set] = runtime.createSignal(0);
    version = get;
    let n = 0;
    runtime.onCleanup(onLoadSettled((settled) => settled === watched && set(++n)));
  }
  const entry = (): CacheEntry => {
    version();
    watched = loadEntry(loader, read(input), extensions as ((l: any) => void) | undefined, onProgress);
    return watched;
  };
  const pick = (value: unknown): LoaderFor<I, L> =>
    (Array.isArray(read(input)) ? value : (value as unknown[])[0]) as LoaderFor<I, L>;
  const accessor = (() => {
    entry();
    return useLoader(loader, read(input), extensions, onProgress);
  }) as LoaderAccessor<LoaderFor<I, L>>;
  Object.defineProperties(accessor, {
    loading: { get: () => entry().status === 'pending' },
    error: { get: () => entry().error },
    state: {
      get: () => {
        const status = entry().status;
        return status === 'pending' ? 'loading' : status === 'fulfilled' ? 'ready' : 'error';
      },
    },
    latest: {
      get: () => {
        const current = entry();
        return current.status === 'fulfilled' ? pick(current.value) : undefined;
      },
    },
  });
  return accessor;
}

/** Starts loading an asset into the cache (same cache as useLoader). */
createLoader.preload = useLoader.preload;
/** Removes a loaded asset from the cache. */
createLoader.clear = useLoader.clear;

/**
 * Returns `{ nodes, materials, meshes }` for an object tree, recomputed when
 * an accessor `object` changes.
 */
export function createGraph(object: MaybeAccessor<THREE.Object3D>): Accessor<ObjectMap> {
  return derive(() => buildGraph(read(object)));
}
