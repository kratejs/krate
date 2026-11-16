// The browser engine of @krate/three. It is bundled for the page like an npm
// dependency (package.json "krate.vendor"), so it shares one `three` with the
// app and uses the page's Krate runtime.

import { getCurrentStore } from './reconciler.js';
import type { RootStore } from './store.js';

export * from './types.js';
export type {
  Intersection,
  IntersectionEvent,
  ThreeEvent,
  DomEvent,
  Events,
  EventHandlers,
  FilterFunction,
  ComputeFunction,
  EventManager,
  PointerCaptureTarget,
} from './events.js';
export { createEvents, swapInteractivity, removeInteractivity } from './events.js';
export { createPointerEvents, createPointerEvents as events } from './web-events.js';
export type { GlobalRenderCallback, GlobalEffectType } from './loop.js';
export { flushGlobalEffects, addEffect, addAfterEffect, addTail, invalidate, advance } from './loop.js';
export type { AttachFnType, AttachType, Args, InstanceProps, Instance } from './instance.js';
export type { ConstructorRepresentation, Catalogue } from './extend.js';
export { extend, toPascalCase, hasConstructor, resolveConstructor } from './extend.js';
export type { GLProps, CameraProps, RenderProps, DefaultGLProps, AppliedConfiguration, Configuration } from './configuration.js';
export type { ReconcilerRoot, Root, RootStateMachine } from './root.js';
export { _roots, createRoot, unmountComponentAtNode } from './root.js';
export { createPortal, hideInstance, unhideInstance, getCurrentStore } from './reconciler.js';
export type { PortalNode } from './reconciler.js';
export type {
  Subscription,
  Dpr,
  Size,
  Viewport,
  RenderCallback,
  Frameloop,
  Performance,
  Renderer,
  XRManager,
  RootState,
  RootStore,
  InternalState,
  StoreApi,
} from './store.js';
export { createStore } from './store.js';
export type { ObjectMap, Camera, Disposable, EquConfig, Properties, Overwrite, Mutable, NonFunctionKeys } from './utils.js';
export {
  applyProps,
  getRootState,
  dispose,
  buildGraph,
  calculateDpr,
  is,
  isRef,
  isOrthographicCamera,
  isColorRepresentation,
  updateCamera,
  findInitialRoot,
} from './utils.js';
export type { TrackedPromise } from './promise.js';
export { isRenderer, createRenderer, releaseRenderer } from './gl.js';
export { useStore, useThree, useFrame, useGraph, useLoader, useInstanceHandle } from './hooks.js';
export type { LoaderResult, Extensions } from './hooks.js';
export { mountCanvas } from './canvas.js';
export type { CanvasProps, CanvasElements, ResizeOptions } from './canvas.js';

/** The root store of the scene being rendered, for code that reads it like React context. */
export const context = {
  /** Returns the current scene's store (like `useContext(context)` in R3F). */
  useContext: (): RootStore | null => getCurrentStore(),
};

/**
 * Runs `fn` and applies the state changes it made synchronously (signal
 * writes are otherwise batched to the next microtask).
 */
export function flushSync<R>(fn: () => R): R {
  const result = fn();
  const g = globalThis as { krateFlushSync?: () => void };
  if (typeof g.krateFlushSync === 'function') g.krateFlushSync();
  return result;
}

/**
 * Test helper: runs `fn`, flushes state changes and waits for pending
 * microtasks (configuration, mounting), like React's act().
 */
export async function act<T>(fn: () => T | Promise<T>): Promise<T> {
  const result = await fn();
  for (let i = 0; i < 4; i++) {
    const g = globalThis as { krateFlushSync?: () => void };
    if (typeof g.krateFlushSync === 'function') g.krateFlushSync();
    await new Promise<void>((r) => setTimeout(r, 0));
  }
  return result;
}
