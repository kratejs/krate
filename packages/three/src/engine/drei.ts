// Engine side of @krate/three/drei: the parts of @react-three/drei's
// OrbitControls, Environment and Lightformer that are plain three.js.
// Behavior follows @react-three/drei 10 (MIT, Copyright (c) 2020 Poimandres),
// see NOTICE. The classes come from three/examples instead of three-stdlib, so
// the page bundles one copy of each next to the app's own three.

import * as THREE from 'three';
import { applyProps } from './utils.js';

const EVENT_PROP = /^on[A-Z]/;

/**
 * Returns the props of a component call, minus `exclude`, in the form an
 * element in a scene takes: every value is a thunk that reads the prop, so a
 * prop the caller computes from state stays live on the element. Event
 * handlers and `ref` are passed as they are. A plain `{...props}` spread would
 * read every getter once.
 */
export function forwardProps(props: Record<string, unknown>, exclude: readonly string[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(props)) {
    if (key === 'children' || key === 'key' || exclude.includes(key)) continue;
    if (key === 'ref' || EVENT_PROP.test(key)) out[key] = props[key];
    else out[key] = () => props[key];
  }
  return out;
}

/** The scene props Environment sets, with drei's defaults. */
export interface EnvironmentSceneProps {
  backgroundBlurriness?: number;
  backgroundIntensity?: number;
  backgroundRotation?: THREE.Euler | [number, number, number];
  environmentIntensity?: number;
  environmentRotation?: THREE.Euler | [number, number, number];
}

type SceneLike = THREE.Scene | { current: THREE.Scene | null };

const resolveScene = (scene: SceneLike): THREE.Scene =>
  (scene as { current?: THREE.Scene | null }).current?.isScene
    ? ((scene as { current: THREE.Scene }).current as THREE.Scene)
    : (scene as THREE.Scene);

/**
 * Sets `texture` as the environment (and, with `background`, the background)
 * of `scene` or `defaultScene`, and returns the function that restores what
 * was there before.
 */
export function setEnvProps(
  background: boolean | 'only',
  scene: SceneLike | undefined,
  defaultScene: THREE.Scene,
  texture: THREE.Texture,
  sceneProps: EnvironmentSceneProps = {},
): () => void {
  const props: Record<string, unknown> = {
    backgroundBlurriness: 0,
    backgroundIntensity: 1,
    backgroundRotation: [0, 0, 0],
    environmentIntensity: 1,
    environmentRotation: [0, 0, 0],
  };
  for (const [key, value] of Object.entries(sceneProps)) if (value !== undefined) props[key] = value;
  const target = resolveScene(scene || defaultScene) as THREE.Scene & Record<string, any>;
  const oldbg = target.background;
  const oldenv = target.environment;
  const oldSceneProps = {
    backgroundBlurriness: target.backgroundBlurriness,
    backgroundIntensity: target.backgroundIntensity,
    backgroundRotation: target.backgroundRotation?.clone?.() ?? [0, 0, 0],
    environmentIntensity: target.environmentIntensity,
    environmentRotation: target.environmentRotation?.clone?.() ?? [0, 0, 0],
  };
  if (background !== 'only') target.environment = texture;
  if (background) target.background = texture;
  applyProps(target, props as never);
  return () => {
    if (background !== 'only') target.environment = oldenv;
    if (background) target.background = oldbg;
    applyProps(target, oldSceneProps as never);
  };
}

/**
 * The cube render target and camera of an Environment with children: the
 * children render into `scene`, and `update()` captures them into `texture`.
 */
export function createEnvironmentCapture(resolution: number) {
  const fbo = new THREE.WebGLCubeRenderTarget(resolution);
  fbo.texture.type = THREE.HalfFloatType;
  return {
    fbo,
    scene: new THREE.Scene(),
    /** Renders `scene` into the cube texture with `gl.autoClear` on. */
    update(gl: THREE.WebGLRenderer, camera: THREE.CubeCamera | undefined, scene: THREE.Scene): void {
      if (!camera) return;
      const autoClear = gl.autoClear;
      gl.autoClear = true;
      camera.update(gl, scene);
      gl.autoClear = autoClear;
    },
  };
}

/** Sets a Lightformer's emissive color (`color` times `intensity`). */
export function lightformerColor(mesh: THREE.Mesh | undefined, color: THREE.ColorRepresentation, intensity: number): void {
  const material = mesh?.material as THREE.MeshBasicMaterial | undefined;
  if (!material || !material.color) return;
  applyProps(material as never, { color } as never);
  material.color.multiplyScalar(intensity);
}

/** Points a Lightformer at its target when it has no rotation of its own. */
export function lightformerTarget(
  mesh: THREE.Mesh | undefined,
  target: boolean | THREE.Vector3 | [number, number, number] | undefined,
  rotated: boolean,
): void {
  if (!mesh || rotated) return;
  mesh.quaternion.identity();
  if (!target) return;
  if (typeof target === 'boolean') mesh.lookAt(0, 0, 0);
  else mesh.lookAt(Array.isArray(target) ? new THREE.Vector3(...target) : target);
}
