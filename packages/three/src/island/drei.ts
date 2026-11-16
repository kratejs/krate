// @krate/three/drei inside a Preact island: drei's OrbitControls,
// Environment, Lightformer and useGLTF on the island hooks. Ported from
// @react-three/drei 10 (MIT, Copyright (c) 2020 Poimandres), see NOTICE.

import { createElement, Fragment, type ComponentChildren, type VNode } from 'preact';
import { forwardRef, useImperativeHandle } from 'preact/compat';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks';
import * as THREE from 'three';
import {
  DreiOrbitControls as OrbitControlsImpl,
  lightformerColor,
  lightformerTarget,
  setEnvProps,
  type EnvironmentSceneProps,
} from '@krate/three/engine/drei';
import * as gltf from '@krate/three/engine/gltf';
import { createPortal, useFrame, useThree } from '@krate/three/island';

export type { GLTFResult } from '@krate/three/engine/gltf';

/** Loads a glTF (cached, suspending while it loads); see @react-three/drei's useGLTF. */
export const useGLTF = gltf.useGLTF;

type ControlsEvent = { type: string; target: OrbitControlsImpl };

/** Props of OrbitControls: drei's props plus any OrbitControls property. */
export interface OrbitControlsProps {
  makeDefault?: boolean;
  camera?: THREE.Camera;
  domElement?: HTMLElement;
  regress?: boolean;
  keyEvents?: boolean | HTMLElement;
  enableDamping?: boolean;
  onChange?: (e?: ControlsEvent) => void;
  onStart?: (e?: ControlsEvent) => void;
  onEnd?: (e?: ControlsEvent) => void;
  [key: string]: unknown;
}

/** drei's OrbitControls: orbit, zoom and pan with the pointer. */
export const OrbitControls = forwardRef<OrbitControlsImpl, OrbitControlsProps>((props, ref) => {
  const { makeDefault, camera, regress, domElement, enableDamping = true, keyEvents = false, onChange, onStart, onEnd, ...restProps } = props;
  const invalidate = useThree((state) => state.invalidate);
  const defaultCamera = useThree((state) => state.camera);
  const gl = useThree((state) => state.gl);
  const events = useThree((state) => state.events);
  const setEvents = useThree((state) => state.setEvents);
  const set = useThree((state) => state.set);
  const get = useThree((state) => state.get);
  const performance = useThree((state) => state.performance);
  const explCamera = camera || defaultCamera;
  const explDomElement = (domElement || events.connected || gl.domElement) as HTMLElement;
  const controls = useMemo(() => new OrbitControlsImpl(explCamera), [explCamera]);
  useFrame(() => {
    if (controls.enabled) controls.update();
  }, -1);
  useEffect(() => {
    if (keyEvents) controls.listenToKeyEvents(keyEvents === true ? explDomElement : keyEvents);
    controls.connect(explDomElement);
    return () => void controls.dispose();
  }, [keyEvents, explDomElement, regress, controls, invalidate]);
  useEffect(() => {
    const callback = (e: ControlsEvent): void => {
      invalidate();
      if (regress) performance.regress();
      if (onChange) onChange(e);
    };
    const onStartCb = (e: ControlsEvent): void => {
      if (onStart) onStart(e);
    };
    const onEndCb = (e: ControlsEvent): void => {
      if (onEnd) onEnd(e);
    };
    controls.addEventListener('change', callback as never);
    controls.addEventListener('start', onStartCb as never);
    controls.addEventListener('end', onEndCb as never);
    return () => {
      controls.removeEventListener('start', onStartCb as never);
      controls.removeEventListener('end', onEndCb as never);
      controls.removeEventListener('change', callback as never);
    };
  }, [onChange, onStart, onEnd, controls, invalidate, setEvents]);
  useEffect(() => {
    if (makeDefault) {
      const old = get().controls;
      set({ controls } as never);
      return () => set({ controls: old } as never);
    }
  }, [makeDefault, controls]);
  return createElement('primitive' as never, { ref, object: controls, enableDamping, ...restProps });
});

/** Props of Environment. */
export interface EnvironmentProps extends EnvironmentSceneProps {
  scene?: THREE.Scene | { current: THREE.Scene | null };
  background?: boolean | 'only';
  blur?: number;
  map?: THREE.Texture;
  resolution?: number;
  frames?: number;
  near?: number;
  far?: number;
  files?: never;
  preset?: never;
  ground?: never;
  children?: ComponentChildren;
}

function EnvironmentMap({ scene, background = false, map, ...config }: EnvironmentProps) {
  const defaultScene = useThree((state) => state.scene);
  useLayoutEffect(() => {
    if (map) return setEnvProps(background, scene, defaultScene, map, config);
  });
  return null;
}

function EnvironmentPortal({
  children,
  near = 0.1,
  far = 1000,
  resolution = 256,
  frames = 1,
  background = false,
  blur,
  backgroundBlurriness,
  backgroundIntensity,
  backgroundRotation,
  environmentIntensity,
  environmentRotation,
  scene,
}: EnvironmentProps) {
  const gl = useThree((state) => state.gl);
  const defaultScene = useThree((state) => state.scene);
  const camera = useRef<THREE.CubeCamera>(null);
  const [virtualScene] = useState(() => new THREE.Scene());
  const fbo = useMemo(() => {
    const target = new THREE.WebGLCubeRenderTarget(resolution);
    target.texture.type = THREE.HalfFloatType;
    return target;
  }, [resolution]);
  useEffect(() => () => fbo.dispose(), [fbo]);
  useLayoutEffect(() => {
    if (frames === 1 && camera.current) {
      const autoClear = gl.autoClear;
      gl.autoClear = true;
      camera.current.update(gl, virtualScene);
      gl.autoClear = autoClear;
    }
    return setEnvProps(background, scene, defaultScene, fbo.texture, {
      backgroundBlurriness: blur ?? backgroundBlurriness,
      backgroundIntensity,
      backgroundRotation,
      environmentIntensity,
      environmentRotation,
    });
  }, [children, virtualScene, fbo.texture, scene, defaultScene, background, frames, gl]);
  let count = 1;
  useFrame(() => {
    if (frames === Infinity || count < frames) {
      const autoClear = gl.autoClear;
      gl.autoClear = true;
      camera.current?.update(gl, virtualScene);
      gl.autoClear = autoClear;
      count++;
    }
  });
  return createPortal(
    createElement(Fragment, null, children as VNode, createElement('cubeCamera' as never, { ref: camera, args: [near, far, fbo] })),
    virtualScene,
  );
}

/**
 * drei's Environment: lights the scene with a cube map rendered from its
 * children (such as Lightformers), or with `map`. HDR `files`, `preset` and
 * `ground` are not supported.
 */
export function Environment(props: EnvironmentProps) {
  if (props.files || props.preset || props.ground) {
    throw new Error('@krate/three/drei: Environment supports children (Lightformers) and `map`; HDR files, presets and `ground` need @react-three/drei.');
  }
  return props.map ? createElement(EnvironmentMap, props) : props.children ? createElement(EnvironmentPortal, props) : null;
}

/** Props of Lightformer. */
export interface LightformerProps {
  form?: 'rect' | 'plane' | 'circle' | 'ring' | 'box' | ((props: { args?: unknown[] }) => VNode | null);
  intensity?: number;
  color?: THREE.ColorRepresentation;
  scale?: number | [number, number] | [number, number, number];
  target?: boolean | THREE.Vector3 | [number, number, number];
  args?: unknown[];
  map?: THREE.Texture;
  toneMapped?: boolean;
  light?: Record<string, unknown>;
  rotation?: unknown;
  material?: unknown;
  children?: ComponentChildren;
  [key: string]: unknown;
}

/** drei's Lightformer: a glowing shape for Environment. */
export const Lightformer = forwardRef<THREE.Mesh, LightformerProps>(
  ({ light, args, map, toneMapped = false, color = 'white', form: Form = 'rect', intensity = 1, scale = 1, target = [0, 0, 0], children, ...props }, forwardedRef) => {
    const ref = useRef<THREE.Mesh>(null);
    useImperativeHandle(forwardedRef, () => ref.current!, []);
    useLayoutEffect(() => {
      if (!children && !props.material) lightformerColor(ref.current ?? undefined, color, intensity);
    }, [color, intensity, children, props.material]);
    useLayoutEffect(() => {
      lightformerTarget(ref.current ?? undefined, target, !!props.rotation);
    }, [target, props.rotation]);
    const s = Array.isArray(scale) && scale.length === 2 ? [scale[0], scale[1], 1] : scale;
    const geometry =
      Form === 'circle'
        ? createElement('ringGeometry' as never, { args: args ?? [0, 0.5, 64] })
        : Form === 'ring'
          ? createElement('ringGeometry' as never, { args: args ?? [0.25, 0.5, 64] })
          : Form === 'rect' || Form === 'plane'
            ? createElement('planeGeometry' as never, { args: args ?? [1, 1] })
            : Form === 'box'
              ? createElement('boxGeometry' as never, { args: args ?? [1, 1, 1] })
              : createElement(Form as never, { args });
    return createElement(
      'mesh' as never,
      { ref, scale: s, ...props },
      geometry,
      children ? (children as VNode) : createElement('meshBasicMaterial' as never, { toneMapped, map, side: THREE.DoubleSide }),
      light ? createElement('pointLight' as never, { castShadow: true, ...light }) : null,
    );
  },
);
