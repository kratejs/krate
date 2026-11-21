import { getCurrentScene } from './context.js';
import type { FrameCallback } from './scene.js';
import { rt } from './rt.js';

/** Runs `cb(delta, time)` (seconds) every frame of the current scene, before transforms are flushed. */
export function useFrame(cb: FrameCallback): void {
  const scene = getCurrentScene();
  if (!scene) throw new Error('@krate/3d: useFrame must be used inside a <Canvas>');
  rt().onCleanup(scene.onFrame(cb));
}
