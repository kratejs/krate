// Entry points the compiler emits calls into (@krate/3d/runtime/compiled).
// Shape and the pre-ready queueing rule are documented in DESIGN.md.

import { aimLight } from './aim.js';
import { lightIntensity } from './calibration.js';
import { colorToLinear } from './color.js';
import { setCurrentScene } from './context.js';
import { rt } from './rt.js';
import { Scene3D, type SceneOptions } from './scene.js';

export const __k3d = {
  /**
   * Creates the engine for a canvas. Every method of the returned scene may be
   * called immediately: calls made before Filament is ready are queued and
   * replayed in order, and pos/rot/scale write the transform buffer directly.
   */
  scene(canvas: HTMLCanvasElement, opts: SceneOptions = {}): Scene3D {
    const scene = new Scene3D(canvas, opts);
    setCurrentScene(scene);
    rt().onCleanup(() => scene.dispose());
    return scene;
  },
  /** Three.js-scale light intensity to Filament units, for reactive intensities. */
  intensity: lightIntensity,
  /** sRGB CSS/hex color to a linear RGB triple, for reactive colors. */
  color: colorToLinear,
  /** Runs fn now and again when the signals it reads change (the page's createEffect). */
  effect(fn: () => void): () => void {
    return rt().createEffect(fn);
  },
  /** Light position and direction from a reactive position and target (see aim.ts). */
  aimLight,
};

export type { Scene3D, SceneOptions };
export type { GeometryHandle, MeshHandle, LightHandle, InitialTransform } from './scene.js';
export type { MaterialHandle, MaterialKind, MaterialParams } from './materials.js';
export type { LightProps } from './lights.js';
export type { MeshFlags } from './renderables.js';
