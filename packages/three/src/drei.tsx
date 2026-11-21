// @krate/three/drei: the @react-three/drei helpers that @krate/three provides
// natively, with drei's props and behavior: OrbitControls, Environment with
// Lightformer children (or a `map`), and useGLTF. `krate migrate nextjs` points
// these drei imports here.
//
// Behavior follows @react-three/drei 10 (MIT, Copyright (c) 2020 Poimandres),
// see NOTICE.

import { onCleanup, onMount } from '@krate/runtime';
import { createPortal, useStore } from '@krate/three/engine';
import {
  DreiOrbitControls as OrbitControlsImpl,
  createEnvironmentCapture,
  forwardProps,
  lightformerColor,
  lightformerTarget,
  setEnvProps,
  type EnvironmentSceneProps,
} from '@krate/three/engine/drei';
import * as gltf from '@krate/three/engine/gltf';
import * as THREE from 'three';
import type {} from './index.js';

export type { GLTFResult } from '@krate/three/engine/gltf';

/** Loads a glTF (cached, suspending while it loads); see @react-three/drei's useGLTF. */
export const useGLTF = gltf.useGLTF;

type ControlsEvent = { type: string; target: OrbitControlsImpl };

/** Props of OrbitControls: drei's props plus any OrbitControls property. */
export interface OrbitControlsProps extends Partial<Omit<OrbitControlsImpl, 'object' | 'domElement' | 'target'>> {
  /** Sets `controls` in the root state, as drei does. */
  makeDefault?: boolean;
  /** The camera to control; defaults to the scene camera. */
  camera?: THREE.Camera;
  /** The element pointer input is read from; defaults to the canvas event source. */
  domElement?: HTMLElement;
  /** Lowers the adaptive performance while the camera moves. */
  regress?: boolean;
  /** Listens to arrow keys on the dom element (`true`) or on the given element. */
  keyEvents?: boolean | HTMLElement;
  target?: THREE.Vector3 | [number, number, number];
  onChange?: (e?: ControlsEvent) => void;
  onStart?: (e?: ControlsEvent) => void;
  onEnd?: (e?: ControlsEvent) => void;
  ref?: unknown;
  [key: string]: unknown;
}

const ORBIT_OWN = ['makeDefault', 'camera', 'regress', 'domElement', 'enableDamping', 'keyEvents', 'onChange', 'onStart', 'onEnd'];

/**
 * Orbit, zoom and pan around a target with the pointer (three's
 * OrbitControls). `makeDefault` puts it in the root state as `controls`.
 */
export function OrbitControls(props: OrbitControlsProps) {
  const store = useStore();
  const state = store.getState;
  const controls = new OrbitControlsImpl(props.camera ?? state().camera);
  const unsubscribe = state().internal.subscribe(
    {
      current: () => {
        if (controls.enabled) controls.update();
      },
    },
    -1,
    store,
  );
  const onChange = (e: ControlsEvent): void => {
    state().invalidate();
    if (props.regress) state().performance.regress();
    props.onChange?.(e);
  };
  const onStart = (e: ControlsEvent): void => props.onStart?.(e);
  const onEnd = (e: ControlsEvent): void => props.onEnd?.(e);
  controls.addEventListener('change', onChange as never);
  controls.addEventListener('start', onStart as never);
  controls.addEventListener('end', onEnd as never);
  onMount(() => {
    const dom = props.domElement || (state().events.connected as HTMLElement | undefined) || state().gl.domElement;
    if (props.keyEvents) controls.listenToKeyEvents(props.keyEvents === true ? dom : props.keyEvents);
    controls.connect(dom);
    if (props.makeDefault) {
      const old = state().controls;
      state().set({ controls } as never);
      onCleanup(() => state().set({ controls: old } as never));
    }
  });
  onCleanup(() => {
    unsubscribe();
    controls.removeEventListener('change', onChange as never);
    controls.removeEventListener('start', onStart as never);
    controls.removeEventListener('end', onEnd as never);
    controls.dispose();
  });
  return <primitive object={controls} enableDamping={props.enableDamping ?? true} {...forwardProps(props, ORBIT_OWN)} />;
}

/** Props of Environment. */
export interface EnvironmentProps extends EnvironmentSceneProps {
  /** The scene to light; defaults to the canvas scene. */
  scene?: THREE.Scene | { current: THREE.Scene | null };
  /** Also use the environment as the background (`'only'`: just the background). */
  background?: boolean | 'only';
  /** Alias of backgroundBlurriness. */
  blur?: number;
  /** A texture to use as the environment, instead of children. */
  map?: THREE.Texture;
  /** Cube map size of an environment rendered from children. */
  resolution?: number;
  /** How many frames re-render the children into the cube map (Infinity: every frame). */
  frames?: number;
  near?: number;
  far?: number;
  /** Not supported by @krate/three/drei (HDR files and presets). */
  files?: never;
  preset?: never;
  ground?: never;
  children?: unknown;
}

function sceneProps(props: EnvironmentProps): EnvironmentSceneProps {
  return {
    backgroundBlurriness: props.blur ?? props.backgroundBlurriness,
    backgroundIntensity: props.backgroundIntensity,
    backgroundRotation: props.backgroundRotation,
    environmentIntensity: props.environmentIntensity,
    environmentRotation: props.environmentRotation,
  };
}

/**
 * Lights the scene with an environment map: rendered from its children (such
 * as Lightformers) into a cube map, or taken from `map`. HDR `files`,
 * `preset` and `ground` are not supported.
 */
export function Environment(props: EnvironmentProps) {
  if (props.files || props.preset || props.ground) {
    throw new Error('@krate/three/drei: Environment supports children (Lightformers) and `map`; HDR files, presets and `ground` need @react-three/drei.');
  }
  const store = useStore();
  const state = store.getState;
  if (props.map) {
    const map = props.map;
    onMount(() => onCleanup(setEnvProps(props.background ?? false, props.scene, state().scene, map, sceneProps(props))));
    return null;
  }
  const capture = createEnvironmentCapture(props.resolution ?? 256);
  const frames = props.frames ?? 1;
  let camera: THREE.CubeCamera | undefined;
  let count = 0;
  // drei renders the children into the cube map in a layout effect, once they
  // are mounted; here that is the first frame, before the scene renders.
  const unsubscribe = state().internal.subscribe(
    {
      current: () => {
        if (frames === Infinity || count < frames) {
          if (!camera) return;
          capture.update(state().gl, camera, capture.scene);
          count++;
        }
      },
    },
    0,
    store,
  );
  onMount(() => onCleanup(setEnvProps(props.background ?? false, props.scene, state().scene, capture.fbo.texture, sceneProps(props))));
  onCleanup(() => {
    unsubscribe();
    capture.fbo.dispose();
  });
  return createPortal(
    [props.children, <cubeCamera ref={(c: THREE.CubeCamera | null) => (camera = c ?? undefined)} args={[props.near ?? 0.1, props.far ?? 1000, capture.fbo]} />],
    capture.scene,
  );
}

/** Props of Lightformer. */
export interface LightformerProps {
  /** 'rect' (or 'plane'), 'circle', 'ring', 'box', or a geometry component. */
  form?: 'rect' | 'plane' | 'circle' | 'ring' | 'box' | ((props: { args?: unknown[] }) => JSX.Element | null);
  intensity?: number;
  color?: THREE.ColorRepresentation;
  scale?: number | [number, number] | [number, number, number];
  /** The point it faces when it has no `rotation`; `true` or omitted: the origin. */
  target?: boolean | THREE.Vector3 | [number, number, number];
  args?: unknown[];
  map?: THREE.Texture;
  toneMapped?: boolean;
  /** Adds a point light with these props. */
  light?: Record<string, unknown>;
  rotation?: unknown;
  children?: unknown;
  ref?: unknown;
  [key: string]: unknown;
}

const LIGHTFORMER_OWN = ['light', 'args', 'map', 'toneMapped', 'color', 'form', 'intensity', 'scale', 'target', 'ref'];

function lightformerScale(scale: LightformerProps['scale']): LightformerProps['scale'] {
  return Array.isArray(scale) && scale.length === 2 ? [scale[0], scale[1], 1] : scale;
}

/** A glowing shape for Environment: a mesh with an unlit, double-sided material. */
export function Lightformer(props: LightformerProps) {
  let mesh: THREE.Mesh | undefined;
  const form = props.form ?? 'rect';
  const setRef = (m: THREE.Mesh | null): void => {
    mesh = m ?? undefined;
    const ref = props.ref;
    if (typeof ref === 'function') (ref as (m: THREE.Mesh | null) => void)(m);
    else if (ref && typeof ref === 'object') (ref as { current: unknown }).current = m;
  };
  onMount(() => {
    if (!props.children && !props.material) lightformerColor(mesh, props.color ?? 'white', props.intensity ?? 1);
    lightformerTarget(mesh, props.target ?? [0, 0, 0], !!props.rotation);
  });
  const Form = typeof form === 'function' ? form : null;
  return (
    <mesh ref={setRef} scale={lightformerScale(props.scale ?? 1)} {...forwardProps(props, LIGHTFORMER_OWN)}>
      {form === 'circle' ? (
        <ringGeometry args={(props.args as never) ?? [0, 0.5, 64]} />
      ) : form === 'ring' ? (
        <ringGeometry args={(props.args as never) ?? [0.25, 0.5, 64]} />
      ) : form === 'rect' || form === 'plane' ? (
        <planeGeometry args={(props.args as never) ?? [1, 1]} />
      ) : form === 'box' ? (
        <boxGeometry args={(props.args as never) ?? [1, 1, 1]} />
      ) : Form ? (
        <Form args={props.args} />
      ) : null}
      {props.children || <meshBasicMaterial toneMapped={props.toneMapped ?? false} map={props.map} side={THREE.DoubleSide} />}
      {props.light ? <pointLight castShadow {...forwardProps(props.light, [])} /> : null}
    </mesh>
  );
}
