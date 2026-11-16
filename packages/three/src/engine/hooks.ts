// Scene hooks. Semantics follow @react-three/fiber (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE.
//
// Krate components run once. A hook that returns state (useThree) returns a
// reactive read: in React-style modules the compiler reads it as a value, so
// `const size = useThree((s) => s.size)` stays current; in Krate modules it is
// a getter, `size().width`.

import type * as THREE from 'three';
import type { ConstructorRepresentation } from './extend.js';
import type { Instance } from './instance.js';
import { getCurrentStore } from './reconciler.js';
import { hasRuntime, rt } from './runtime.js';
import type { RenderCallback, RootState, RootStore } from './store.js';
import { buildGraph, is, isObject3D, type ObjectMap } from './utils.js';

/** Returns the root store of the scene the calling component renders in. */
export function useStore(): RootStore {
  const store = getCurrentStore();
  if (!store) throw new Error('@krate/three: Hooks can only be used within the Canvas component!');
  return store;
}

/**
 * Accesses the root state (renderer, camera, scene, size, ...). With a
 * selector, the result updates when the selected value changes.
 */
export function useThree<T = RootState>(
  selector: (state: RootState) => T = (state) => state as unknown as T,
  equalityFn?: <U>(state: U, newState: U) => boolean,
): T {
  return useStore()(selector, equalityFn as ((a: T, b: T) => boolean) | undefined);
}

function onUnmount(fn: () => void): void {
  if (hasRuntime()) rt().onCleanup(fn);
}

/**
 * Runs `callback` every frame before rendering, with the root state and the
 * frame delta. A positive `renderPriority` takes over rendering: the root no
 * longer renders on its own and the callback renders (callbacks run in
 * ascending priority order).
 */
export function useFrame(callback: RenderCallback, renderPriority = 0): null {
  const store = useStore();
  const ref = { current: callback };
  const unsubscribe = store.getState().internal.subscribe(ref, renderPriority, store);
  onUnmount(unsubscribe);
  return null;
}

/** Returns the named nodes, materials and meshes of an object tree. */
export function useGraph(object: THREE.Object3D): ObjectMap {
  return buildGraph(object);
}

/**
 * Exposes an object's instance descriptor (props, parent, handlers). An
 * escape hatch: expect it to change between versions.
 */
export function useInstanceHandle<T>(ref: { current: T | null }): { readonly current: Instance<T> } {
  return {
    get current() {
      return (ref.current as unknown as { __r3f: Instance<T> } | null)?.__r3f as Instance<T>;
    },
  };
}

// Loading

export type InputLike = string | string[] | string[][] | Readonly<string | string[] | string[][]>;
export type LoaderLike = THREE.Loader<any, InputLike>;
type GLTFLike = { scene: THREE.Object3D };
type LoaderInstance<T extends LoaderLike | ConstructorRepresentation<LoaderLike>> =
  T extends ConstructorRepresentation<LoaderLike> ? InstanceType<T> : T;

export type LoaderResult<T extends LoaderLike | ConstructorRepresentation<LoaderLike>> =
  Awaited<ReturnType<LoaderInstance<T>['loadAsync']>> extends infer R ? (R extends GLTFLike ? R & ObjectMap : R) : never;

export type Extensions<T extends LoaderLike | ConstructorRepresentation<LoaderLike>> = (loader: LoaderInstance<T>) => void;

const memoizedLoaders = new WeakMap<Function, unknown>();
const isConstructor = (value: unknown): value is ConstructorRepresentation =>
  typeof value === 'function' && (value as Function).prototype?.constructor === value;

function getLoader(Proto: unknown): any {
  if (isConstructor(Proto)) {
    let loader = memoizedLoaders.get(Proto);
    if (!loader) {
      loader = new Proto();
      memoizedLoaders.set(Proto, loader);
    }
    return loader;
  }
  return Proto;
}

function loadingFn(extensions?: (loader: any) => void, onProgress?: (event: ProgressEvent<EventTarget>) => void) {
  return (Proto: unknown, ...input: unknown[]): Promise<unknown[]> => {
    const loader = getLoader(Proto);
    if (extensions) extensions(loader);
    return Promise.all(
      input.map(
        (item) =>
          new Promise((res, reject) =>
            loader.load(
              item,
              (data: any) => {
                if (isObject3D(data?.scene)) Object.assign(data, buildGraph(data.scene));
                res(data);
              },
              onProgress,
              (error: { message?: string } | undefined) => reject(new Error(`Could not load ${String(item)}: ${error?.message}`)),
            ),
          ),
      ),
    );
  };
}

export interface CacheEntry {
  keys: unknown[];
  promise: Promise<unknown>;
  value?: unknown;
  error?: unknown;
  status: 'pending' | 'fulfilled' | 'rejected';
}

const cache: CacheEntry[] = [];
const settledListeners = new Set<(entry: CacheEntry) => void>();

/** Calls `fn` with each cache entry whose load settles. Returns the unsubscribe function. */
export function onLoadSettled(fn: (entry: CacheEntry) => void): () => void {
  settledListeners.add(fn);
  return () => {
    settledListeners.delete(fn);
  };
}

const notifySettled = (entry: CacheEntry): void => {
  for (const fn of [...settledListeners]) fn(entry);
};

function findEntry(keys: unknown[]): CacheEntry | undefined {
  return cache.find((entry) => is.equ(keys, entry.keys));
}

// Suspends the caller (throws the pending promise) until fn(...keys) settles.
function suspend<T>(fn: (...keys: any[]) => Promise<T>, keys: unknown[], preloadOnly = false): T | undefined {
  let entry = findEntry(keys);
  if (!entry) {
    const created: CacheEntry = {
      keys,
      status: 'pending',
      promise: fn(...keys).then(
        (value) => {
          created.status = 'fulfilled';
          created.value = value;
          notifySettled(created);
        },
        (error) => {
          created.status = 'rejected';
          created.error = error;
          notifySettled(created);
        },
      ),
    };
    cache.push(created);
    entry = created;
  }
  if (preloadOnly) return undefined;
  if (entry.status === 'fulfilled') return entry.value as T;
  if (entry.status === 'rejected') throw entry.error;
  throw entry.promise;
}

/**
 * Starts (or finds) the cached load of `input` and returns its cache entry
 * without suspending.
 */
export function loadEntry(
  loader: unknown,
  input: InputLike,
  extensions?: (loader: any) => void,
  onProgress?: (event: ProgressEvent<EventTarget>) => void,
): CacheEntry {
  const keys = [loader, ...(Array.isArray(input) ? input : [input])];
  suspend(loadingFn(extensions, onProgress), keys, true);
  return findEntry(keys)!;
}

/**
 * Loads and caches assets with a three loader. The calling component
 * suspends while loading: the nearest `<Suspense>` inside the scene shows its
 * fallback, and without one the scene renders once everything has loaded.
 */
export function useLoader<I extends InputLike, L extends LoaderLike | ConstructorRepresentation<LoaderLike>>(
  loader: L,
  input: I,
  extensions?: Extensions<L>,
  onProgress?: (event: ProgressEvent<EventTarget>) => void,
): I extends any[] ? LoaderResult<L>[] : LoaderResult<L> {
  const keys = Array.isArray(input) ? input : [input];
  const results = suspend(loadingFn(extensions as (l: any) => void, onProgress), [loader, ...keys]) as unknown[];
  return (Array.isArray(input) ? results : results[0]) as I extends any[] ? LoaderResult<L>[] : LoaderResult<L>;
}

/** Starts loading an asset into the cache. */
useLoader.preload = function <I extends InputLike, L extends LoaderLike | ConstructorRepresentation<LoaderLike>>(
  loader: L,
  input: I,
  extensions?: Extensions<L>,
): void {
  const keys = Array.isArray(input) ? input : [input];
  suspend(loadingFn(extensions as (l: any) => void), [loader, ...keys], true);
};

/** Removes a loaded asset from the cache. */
useLoader.clear = function <I extends InputLike, L extends LoaderLike | ConstructorRepresentation<LoaderLike>>(
  loader: L,
  input: I,
): void {
  const keys = [loader, ...(Array.isArray(input) ? input : [input])];
  const entry = findEntry(keys);
  if (entry) cache.splice(cache.indexOf(entry), 1);
};
