// Root configuration. Ported from @react-three/fiber (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE.

import * as THREE from 'three';
import type { ThreeElement } from './types.js';
import type { EventManager } from './events.js';
import { advance, invalidate } from './loop.js';
import type { OffscreenCanvasLike } from './roots.js';
import type { Dpr, Frameloop, Performance, Renderer, RootState, RootStore, Size } from './store.js';
import { isRenderer } from './gl.js';
import { applyProps, calculateDpr, is, prepare, watchDpr, type Camera, type Properties } from './utils.js';

export type DefaultGLProps = Omit<THREE.WebGLRendererParameters, 'canvas'> & {
  canvas: HTMLCanvasElement | OffscreenCanvasLike;
};

export type GLProps =
  | Renderer
  | ((defaultProps: DefaultGLProps) => Renderer)
  | ((defaultProps: DefaultGLProps) => Promise<Renderer>)
  | Partial<Properties<THREE.WebGLRenderer> | THREE.WebGLRendererParameters>;

export type CameraProps = (
  | Camera
  | Partial<
      ThreeElement<typeof THREE.Camera> &
        ThreeElement<typeof THREE.PerspectiveCamera> &
        ThreeElement<typeof THREE.OrthographicCamera>
    >
) & {
  /** Flags the camera as manual, putting projection into your own hands */
  manual?: boolean;
};

export interface RenderProps<TCanvas extends HTMLCanvasElement | OffscreenCanvasLike> {
  /** A threejs renderer instance or props that go into the default renderer */
  gl?: GLProps;
  /** Dimensions to fit the renderer to. Will measure canvas dimensions if omitted */
  size?: Size;
  /** Enables shadows (PCF soft by default); also 'basic' | 'percentage' | 'soft' | 'variance' or shadowMap options */
  shadows?: boolean | 'basic' | 'percentage' | 'soft' | 'variance' | Partial<THREE.WebGLShadowMap>;
  /** Disables three r139 color management */
  legacy?: boolean;
  /** Switch off automatic sRGB encoding and gamma correction */
  linear?: boolean;
  /** Use `THREE.NoToneMapping` instead of `THREE.ACESFilmicToneMapping` */
  flat?: boolean;
  /** Creates an orthographic camera */
  orthographic?: boolean;
  /** Render mode: `demand` renders on change only, `never` leaves rendering to you */
  frameloop?: Frameloop;
  /** Adaptive performance options */
  performance?: Partial<Omit<Performance, 'regress'>>;
  /** Target pixel ratio. Can clamp between a range: `[min, max]` */
  dpr?: Dpr;
  /** Props that go into the default raycaster */
  raycaster?: Partial<THREE.Raycaster>;
  /** A `THREE.Scene` instance or props that go into the default scene */
  scene?: THREE.Scene | Partial<THREE.Scene>;
  /** A `THREE.Camera` instance or props that go into the default camera */
  camera?: CameraProps;
  /** An event manager to manage elements' pointer events */
  events?: (store: RootStore) => EventManager<HTMLElement>;
  /** Callback after the canvas has rendered (but not yet committed) */
  onCreated?: (state: RootState) => void;
  /** Response for pointer clicks that have missed any target */
  onPointerMissed?: (event: MouseEvent) => void;
  /** @internal canvas marker */
  __canvas?: TCanvas;
}

type Canvas = DefaultGLProps['canvas'];
export type Configuration = RenderProps<Canvas>;

/** What a root has applied, so its next configuration applies only what changed */
export interface AppliedConfiguration {
  /** The last configuration applied in full */
  previous?: Configuration;
  /** The camera prop that set the current camera */
  camera?: Configuration['camera'];
}

const shallow = { objects: 'shallow' } as const;

function computeInitialSize(canvas: Canvas, size?: Size): Size {
  if (!size && typeof HTMLCanvasElement !== 'undefined' && canvas instanceof HTMLCanvasElement && canvas.parentElement) {
    const { width, height, top, left } = canvas.parentElement.getBoundingClientRect();
    return { width, height, top, left };
  }
  if (!size && typeof OffscreenCanvas !== 'undefined' && canvas instanceof OffscreenCanvas) {
    return { width: canvas.width, height: canvas.height, top: 0, left: 0 };
  }
  return { width: 0, height: 0, top: 0, left: 0, ...size };
}

/**
 * Applies the inputs that changed since the last configuration applied in
 * full. Objects and arrays compare shallowly, so an equal inline value is
 * unchanged. The pixel ratio follows the device on every call.
 */
export function applyRootConfiguration(
  { store, configuration }: { store: RootStore; configuration: AppliedConfiguration },
  canvas: Canvas,
  props: Configuration,
): void {
  const {
    gl: glConfig,
    scene: sceneOptions,
    camera: cameraOptions,
    shadows = false,
    linear = false,
    flat = false,
    legacy = false,
    frameloop = 'always',
    dpr = [1, 2],
    performance,
    onPointerMissed,
  } = props;
  const size = computeInitialSize(canvas, props.size);
  const next: Configuration = { ...props, shadows, linear, flat, legacy, frameloop, size };
  const state = store.getState();
  const gl = state.gl;
  const last = configuration.previous;
  const changed = (key: keyof Configuration): boolean => !last || !is.equ(next[key], last[key], shallow);
  // A failure can leave inputs partly applied; the next call applies all of them.
  configuration.previous = undefined;

  let raycaster = state.raycaster;
  if (!raycaster) state.set({ raycaster: (raycaster = new THREE.Raycaster()) });
  if (changed('raycaster')) {
    const { params, ...options } = (props.raycaster ?? {}) as Partial<THREE.Raycaster>;
    applyProps(raycaster as any, { ...options, params: { ...raycaster.params, ...params } } as any);
  }

  // Create the default camera without overwriting a camera set by the user.
  if (!state.camera || (state.camera === (configuration.camera as unknown) && changed('camera'))) {
    configuration.camera = cameraOptions;
    const isCamera = (cameraOptions as THREE.Camera | undefined)?.isCamera;
    const camera = (isCamera
      ? cameraOptions
      : props.orthographic
        ? new THREE.OrthographicCamera(0, 0, 0, 0, 0.1, 1000)
        : new THREE.PerspectiveCamera(75, 0, 0.1, 1000)) as Camera;
    if (!isCamera) {
      camera.position.z = 5;
      if (cameraOptions) {
        applyProps(camera as any, cameraOptions as any);
        // Keep a user-defined frustum (pmndrs/react-three-fiber#3160).
        if (!camera.manual) {
          if ('aspect' in cameraOptions || 'left' in cameraOptions || 'right' in cameraOptions || 'bottom' in cameraOptions || 'top' in cameraOptions) {
            camera.manual = true;
            camera.updateProjectionMatrix();
          }
        }
      }
      if (!state.camera && !(cameraOptions as { rotation?: unknown } | undefined)?.rotation) camera.lookAt(0, 0, 0);
    }
    state.set({ camera });
    // pmndrs/react-xr#300
    raycaster.camera = camera;
  }

  if (!state.scene) {
    let scene: THREE.Scene;
    if ((sceneOptions as THREE.Scene | undefined)?.isScene) {
      scene = sceneOptions as THREE.Scene;
      prepare(scene, store, '', {} as any);
    } else {
      scene = new THREE.Scene();
      prepare(scene, store, '', {} as any);
      if (sceneOptions) applyProps(scene as any, sceneOptions as any);
    }
    state.set({ scene });
  }

  if (props.events && !state.events.handlers) state.set({ events: props.events(store) });
  if (changed('size')) state.setSize(size.width, size.height, size.top, size.left);
  if (state.viewport.dpr !== calculateDpr(dpr)) state.setDpr(dpr);
  if (changed('frameloop')) state.setFrameloop(frameloop);
  if (changed('onPointerMissed')) state.set({ onPointerMissed });
  // Performance options merge, so removing them keeps the current settings.
  if (performance && changed('performance')) {
    state.set((s) => ({ performance: { ...s.performance, ...performance } }));
  }

  if (!state.xr) {
    const handleXRFrame = (timestamp: number, frame?: XRFrame): void => {
      const s = store.getState();
      if (s.frameloop === 'never') return;
      advance(timestamp, true, s, frame);
    };
    const handleSessionChange = (): void => {
      const s = store.getState();
      s.gl.xr.enabled = s.gl.xr.isPresenting;
      s.gl.xr.setAnimationLoop(s.gl.xr.isPresenting ? handleXRFrame : null);
      if (!s.gl.xr.isPresenting) invalidate(s);
    };
    const xr = {
      connect() {
        const g = store.getState().gl;
        g.xr.addEventListener('sessionstart', handleSessionChange);
        g.xr.addEventListener('sessionend', handleSessionChange);
      },
      disconnect() {
        const g = store.getState().gl;
        g.xr.removeEventListener('sessionstart', handleSessionChange);
        g.xr.removeEventListener('sessionend', handleSessionChange);
      },
    };
    if (typeof gl.xr?.addEventListener === 'function') xr.connect();
    state.set({ xr });
  }

  if (gl.shadowMap && changed('shadows')) {
    const oldEnabled = gl.shadowMap.enabled;
    const oldType = gl.shadowMap.type;
    gl.shadowMap.enabled = !!shadows;
    if (is.boo(shadows)) {
      gl.shadowMap.type = THREE.PCFSoftShadowMap;
    } else if (is.str(shadows)) {
      const types = {
        basic: THREE.BasicShadowMap,
        percentage: THREE.PCFShadowMap,
        soft: THREE.PCFSoftShadowMap,
        variance: THREE.VSMShadowMap,
      };
      gl.shadowMap.type = types[shadows] ?? THREE.PCFSoftShadowMap;
    } else if (is.obj(shadows)) {
      Object.assign(gl.shadowMap, shadows);
    }
    if (oldEnabled !== gl.shadowMap.enabled || oldType !== gl.shadowMap.type) gl.shadowMap.needsUpdate = true;
  }

  // Explicit renderer options take precedence over the color shorthands.
  const glProps = glConfig && !is.fun(glConfig) && !isRenderer(glConfig) ? (glConfig as Record<string, unknown>) : undefined;
  if (changed('legacy')) {
    THREE.ColorManagement.enabled = !legacy;
    state.set({ legacy });
  }
  if (changed('linear')) {
    if (glProps?.outputColorSpace === undefined) gl.outputColorSpace = linear ? THREE.LinearSRGBColorSpace : THREE.SRGBColorSpace;
    state.set({ linear });
  }
  if (changed('flat')) {
    if (glProps?.toneMapping === undefined) gl.toneMapping = flat ? THREE.NoToneMapping : THREE.ACESFilmicToneMapping;
    state.set({ flat });
  }
  if (glProps && changed('gl')) applyProps(gl as any, glProps as any);
  configuration.previous = next;
}

/**
 * Follows window.devicePixelRatio (another display, zoom), which changes
 * without a resize. Returns the function that stops following.
 */
export function followDpr(root: { store: RootStore; configuration: AppliedConfiguration }): () => void {
  const xr = (): any => root.store.getState().gl?.xr;
  const follow = (): void => {
    const state = root.store.getState();
    // three restores its own ratio after an XR session ends.
    if (xr()?.isPresenting || xr()?.getSession?.()) {
      xr().addEventListener('sessionend', follow);
      return;
    }
    xr()?.removeEventListener?.('sessionend', follow);
    const last = root.configuration.previous;
    if (!last) return;
    const dpr = last.dpr ?? [1, 2];
    if (state.viewport.dpr !== calculateDpr(dpr)) state.setDpr(dpr);
  };
  const unwatch = watchDpr(follow);
  return () => {
    unwatch();
    xr()?.removeEventListener?.('sessionend', follow);
  };
}
