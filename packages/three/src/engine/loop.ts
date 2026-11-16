// The shared frame loop. Ported from @react-three/fiber (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE.

import { _roots } from './roots.js';
import type { RootState } from './store.js';

export type GlobalRenderCallback = (timestamp: number) => void;
type SubItem = { callback: GlobalRenderCallback };

function createSubs(callback: GlobalRenderCallback, subs: Set<SubItem>): () => void {
  const sub = { callback };
  subs.add(sub);
  return () => void subs.delete(sub);
}

const globalEffects = new Set<SubItem>();
const globalAfterEffects = new Set<SubItem>();
const globalTailEffects = new Set<SubItem>();

/** Adds a global render callback which is called each frame. */
export const addEffect = (callback: GlobalRenderCallback): (() => void) => createSubs(callback, globalEffects);

/** Adds a global after-render callback which is called each frame. */
export const addAfterEffect = (callback: GlobalRenderCallback): (() => void) => createSubs(callback, globalAfterEffects);

/** Adds a global callback which is called when rendering stops. */
export const addTail = (callback: GlobalRenderCallback): (() => void) => createSubs(callback, globalTailEffects);

function run(effects: Set<SubItem>, timestamp: number): void {
  if (!effects.size) return;
  for (const { callback } of effects.values()) callback(timestamp);
}

export type GlobalEffectType = 'before' | 'after' | 'tail';

export function flushGlobalEffects(type: GlobalEffectType, timestamp: number): void {
  switch (type) {
    case 'before':
      return run(globalEffects, timestamp);
    case 'after':
      return run(globalAfterEffects, timestamp);
    case 'tail':
      return run(globalTailEffects, timestamp);
  }
}

function update(timestamp: number, state: RootState, frame?: XRFrame): number {
  let delta = state.clock.getDelta();
  // With frameloop "never" the clock follows the timestamps passed to advance().
  if (state.frameloop === 'never' && typeof timestamp === 'number') {
    delta = timestamp - state.clock.elapsedTime;
    state.clock.oldTime = state.clock.elapsedTime;
    state.clock.elapsedTime = timestamp;
  }
  const subscribers = state.internal.subscribers;
  for (let i = 0; i < subscribers.length; i++) {
    const subscription = subscribers[i];
    subscription.ref.current(subscription.store.getState(), delta, frame);
  }
  // Render unless a subscriber took over rendering.
  if (!state.internal.priority && state.gl.render) state.gl.render(state.scene, state.camera);
  state.internal.frames = Math.max(0, state.internal.frames - 1);
  return state.frameloop === 'always' ? 1 : state.internal.frames;
}

let running = false;
let useFrameInProgress = false;
let frame = 0;

export function loop(timestamp: number): void {
  frame = requestAnimationFrame(loop);
  running = true;
  let repeat = 0;
  flushGlobalEffects('before', timestamp);
  useFrameInProgress = true;
  for (const root of _roots.values()) {
    const state = root.store.getState();
    if (
      state.internal.active &&
      (state.frameloop === 'always' || state.internal.frames > 0) &&
      !state.gl.xr?.isPresenting
    ) {
      repeat += update(timestamp, state);
    }
  }
  useFrameInProgress = false;
  flushGlobalEffects('after', timestamp);
  if (repeat === 0) {
    flushGlobalEffects('tail', timestamp);
    running = false;
    return cancelAnimationFrame(frame);
  }
}

/** Requests a frame. Invalidates every root unless a root's state is passed. */
export function invalidate(state?: RootState, frames = 1): void {
  if (!state) return _roots.forEach((root) => invalidate(root.store.getState(), frames));
  if (state.gl?.xr?.isPresenting || !state.internal.active || state.frameloop === 'never') return;
  if (frames > 1) {
    state.internal.frames = Math.min(60, state.internal.frames + frames);
  } else if (useFrameInProgress) {
    // Invalidated from useFrame: render this frame and the next one.
    state.internal.frames = 2;
  } else {
    state.internal.frames = 1;
  }
  if (!running) {
    running = true;
    requestAnimationFrame(loop);
  }
}

/** Advances the frame loop and renders, for manual rendering with frameloop="never". */
export function advance(timestamp: number, runGlobalEffects = true, state?: RootState, frame?: XRFrame): void {
  if (runGlobalEffects) flushGlobalEffects('before', timestamp);
  if (!state) for (const root of _roots.values()) update(timestamp, root.store.getState());
  else update(timestamp, state, frame);
  if (runGlobalEffects) flushGlobalEffects('after', timestamp);
}
