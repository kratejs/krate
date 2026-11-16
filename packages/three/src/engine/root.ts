// Roots: one per canvas. Ported from @react-three/fiber (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE.

import * as THREE from 'three';
import { applyRootConfiguration, followDpr, type RenderProps } from './configuration.js';
import { extend } from './extend.js';
import { createRenderer, isRenderer, releaseRenderer } from './gl.js';
import type { Instance } from './instance.js';
import { advance, invalidate } from './loop.js';
import { deferred, fulfilled, isPromiseLike, rejected, type TrackedPromise } from './promise.js';
import { mountTree, setPortalStoreFactory, type MountedTree } from './reconciler.js';
import { _roots, type OffscreenCanvasLike, type Root } from './roots.js';
import { createStore, createWithEqualityFn, type RootStore } from './store.js';
import { dispose } from './utils.js';

export { _roots } from './roots.js';
export type { Root, RootStateMachine } from './roots.js';

setPortalStoreFactory((init) => createWithEqualityFn(init) as RootStore);

// Every three class is available as an element by default, as in R3F's Canvas.
let extended = false;
function extendThree(): void {
  if (extended) return;
  extended = true;
  extend(THREE as unknown as Record<string, new (...args: any[]) => any>);
}

export interface ReconcilerRoot<TCanvas extends HTMLCanvasElement | OffscreenCanvasLike> {
  ready: TrackedPromise<unknown>;
  configure: (config?: RenderProps<TCanvas>) => TrackedPromise<ReconcilerRoot<TCanvas>>;
  render: (element: unknown) => RootStore;
  unmount: () => void;
}

type Transition = { type: 'use' } | { type: 'unmount'; token: symbol } | { type: 'dispose'; token: symbol };

function transitionRoot(root: Root, event: Transition): boolean {
  const state = root.state;
  switch (event.type) {
    case 'use':
      if (state.status === 'disposed') return false;
      root.state = { status: 'open' };
      return true;
    case 'unmount':
      if (state.status === 'disposed') return false;
      root.state = { status: 'closing', token: event.token };
      return true;
    case 'dispose':
      if (state.status !== 'closing' || state.token !== event.token) return false;
      root.state = { status: 'disposed' };
      return true;
  }
}

const logError = (error: unknown): void => {
  if (typeof reportError === 'function') reportError(error);
  else console.error(error);
};

/**
 * Creates a root for a canvas. `configure()` sets up the renderer, camera and
 * scene; `render(children)` mounts a scene description; `unmount()` tears it
 * all down.
 */
export function createRoot<TCanvas extends HTMLCanvasElement | OffscreenCanvasLike>(canvas: TCanvas): ReconcilerRoot<TCanvas> {
  extendThree();
  const prevRoot = _roots.get(canvas);
  if (prevRoot) console.warn('@krate/three: createRoot should only be called once per canvas!');
  const store = prevRoot?.store || createStore(invalidate, advance);
  const root: Root =
    prevRoot || { store, state: { status: 'open' }, ready: fulfilled(undefined), configuration: {}, ownsRenderer: false };
  if (!prevRoot) _roots.set(canvas, root);

  let tree: MountedTree | null = null;

  const api: ReconcilerRoot<TCanvas> = {
    get ready() {
      return root.ready;
    },
    configure(props = {}) {
      if (!transitionRoot(root, { type: 'use' })) {
        return rejected(new Error('@krate/three: Cannot configure a root after disposal has started.'));
      }
      const previous = root.ready;
      const { promise, resolve, reject } = deferred<ReconcilerRoot<TCanvas>>();
      root.ready = promise;
      const apply = (gl: unknown): void => {
        try {
          const state = store.getState();
          if (!state.gl) {
            root.ownsRenderer = !isRenderer(props.gl);
            state.set({ gl: gl as THREE.WebGLRenderer });
          }
          applyRootConfiguration(root, canvas, props as RenderProps<HTMLCanvasElement>);
          if (!root.unwatchDpr && (props.dpr === undefined || Array.isArray(props.dpr))) root.unwatchDpr = followDpr(root);
          resolve(api);
        } catch (error) {
          reject(error);
        }
      };
      const run = (): void => {
        try {
          const gl = store.getState().gl ?? createRenderer(canvas, props.gl);
          if (isPromiseLike(gl)) Promise.resolve(gl).then(apply).catch(reject);
          else apply(gl);
        } catch (error) {
          reject(error);
        }
      };
      if (previous.status === 'pending') previous.then(run, reject);
      else run();
      return promise;
    },
    render(children) {
      if (!transitionRoot(root, { type: 'use' })) return store;
      if (!store.getState().gl && root.ready.status === 'fulfilled') api.configure();
      if (root.ready.status === 'rejected') throw root.ready.reason;
      const commit = (): void => {
        if (root.state.status !== 'open' || root.ready.status === 'rejected') return;
        if (root.ready.status === 'pending') {
          root.ready.then(commit, logError);
          return;
        }
        root.unrender?.();
        const scene = store.getState().scene as THREE.Scene & { __r3f: Instance };
        tree = mountTree(children, scene.__r3f, store);
        root.unrender = () => {
          tree?.unmount();
          tree = null;
        };
        if (!store.getState().internal.active) {
          const state = store.getState();
          state.set((s) => ({ internal: { ...s.internal, active: true } }));
          root.configuration.previous?.onCreated?.(store.getState());
          if (!store.getState().events.connected) {
            store.getState().events.connect?.(canvas as unknown as HTMLElement);
          }
        }
        invalidate(store.getState());
      };
      commit();
      return store;
    },
    unmount() {
      if (_roots.get(canvas) === root) unmountComponentAtNode(canvas);
    },
  };
  return api;
}

function attempt<T>(step: () => T): T | undefined {
  try {
    return step();
  } catch (error) {
    console.warn('[@krate/three] Error while unmounting root; teardown may be incomplete:', error);
  }
}

/** Unmounts the root of a canvas and releases its renderer. */
export function unmountComponentAtNode<TCanvas extends HTMLCanvasElement | OffscreenCanvasLike>(
  canvas: TCanvas,
  callback?: (canvas: TCanvas) => void,
): void {
  const root = _roots.get(canvas);
  const token = Symbol('unmount');
  if (!root || !transitionRoot(root, { type: 'unmount', token })) return;
  const teardown = (): void => {
    if (root.ready.status === 'pending') {
      root.ready.then(teardown, teardown);
      return;
    }
    if (!transitionRoot(root, { type: 'dispose', token })) return;
    attempt(() => root.unrender?.());
    _roots.delete(canvas);
    const state = root.store.getState();
    state.internal.active = false;
    attempt(() => root.unwatchDpr?.());
    attempt(() => state.events.disconnect?.());
    if (state.gl?.xr) attempt(() => state.xr.disconnect());
    if (state.scene) attempt(() => dispose(state.scene as unknown as { dispose?: () => void; type?: string }));
    const disposal = attempt(() => releaseRenderer(state.gl, root.ownsRenderer));
    if (isPromiseLike(disposal)) disposal.then(() => callback?.(canvas));
    else callback?.(canvas);
  };
  // Teardown waits a tick so an unmount followed by a remount (a route that
  // re-renders the same canvas) can reclaim the root.
  queueMicrotask(teardown);
}
