// Root state store. Ported from @react-three/fiber (MIT, Copyright (c)
// 2019-2025 Poimandres), see NOTICE. R3F uses zustand; this store keeps the
// same API (getState, setState, subscribe, and the store as a selector hook)
// and makes selections reactive with Krate signals.

import * as THREE from 'three';
import type { DomEvent, EventManager, PointerCaptureTarget, ThreeEvent } from './events.js';
import { hasRuntime, rt } from './runtime.js';
import { calculateDpr, isOrthographicCamera, updateCamera, type Camera } from './utils.js';

export interface Intersection extends THREE.Intersection {
  eventObject: THREE.Object3D;
}

export type Subscription = {
  ref: { current: RenderCallback };
  priority: number;
  store: RootStore;
};

export type Dpr = number | [min: number, max: number];

export interface Size {
  width: number;
  height: number;
  top: number;
  left: number;
}

export type Frameloop = 'always' | 'demand' | 'never';

export interface Viewport extends Size {
  /** The initial pixel ratio */
  initialDpr: number;
  /** Current pixel ratio */
  dpr: number;
  /** size.width / viewport.width */
  factor: number;
  /** Camera distance */
  distance: number;
  /** Camera aspect ratio: width / height */
  aspect: number;
}

export type RenderCallback = (state: RootState, delta: number, frame?: XRFrame) => void;

export interface Performance {
  /** Current performance normal, between min and max */
  current: number;
  /** How low the performance can go, between 0 and max */
  min: number;
  /** How high the performance can go, between min and max */
  max: number;
  /** Time until current returns to max in ms */
  debounce: number;
  /** Sets current to min, puts the system in regression */
  regress: () => void;
}

export interface Renderer {
  render: (scene: THREE.Scene, camera: THREE.Camera) => any;
}

export interface InternalState {
  interaction: THREE.Object3D[];
  hovered: Map<string, ThreeEvent<DomEvent>>;
  subscribers: Subscription[];
  capturedMap: Map<number, Map<THREE.Object3D, PointerCaptureTarget>>;
  initialClick: [x: number, y: number];
  initialHits: THREE.Object3D[];
  lastEvent: { current: DomEvent | null };
  active: boolean;
  priority: number;
  frames: number;
  subscribe: (callback: { current: RenderCallback }, priority: number, store: RootStore) => () => void;
}

export interface XRManager {
  connect: () => void;
  disconnect: () => void;
}

type SetState<T> = (partial: T | Partial<T> | ((state: T) => T | Partial<T>), replace?: boolean) => void;
type GetState<T> = () => T;

export interface RootState {
  /** Set current state */
  set: SetState<RootState>;
  /** Get current state */
  get: GetState<RootState>;
  /** The instance of the renderer */
  gl: THREE.WebGLRenderer;
  /** Default camera */
  camera: Camera;
  /** Default scene */
  scene: THREE.Scene;
  /** Default raycaster */
  raycaster: THREE.Raycaster;
  /** Default clock */
  clock: THREE.Clock;
  /** Event layer interface, contains the event handler and the node they're connected to */
  events: EventManager<any>;
  /** XR interface */
  xr: XRManager;
  /** Currently used controls */
  controls: THREE.EventDispatcher | null;
  /** Normalized event coordinates */
  pointer: THREE.Vector2;
  /** @deprecated Normalized event coordinates, use "pointer" instead! */
  mouse: THREE.Vector2;
  legacy: boolean;
  /** Shortcut to gl.outputColorSpace = THREE.LinearSRGBColorSpace */
  linear: boolean;
  /** Shortcut to gl.toneMapping = NoTonemapping */
  flat: boolean;
  /** Render loop flags */
  frameloop: Frameloop;
  performance: Performance;
  /** Reactive pixel-size of the canvas */
  size: Size;
  /** Reactive size of the viewport in threejs units */
  viewport: Viewport & {
    getCurrentViewport: (
      camera?: Camera,
      target?: THREE.Vector3 | Parameters<THREE.Vector3['set']>,
      size?: Size,
    ) => Omit<Viewport, 'dpr' | 'initialDpr'>;
  };
  /** Flags the canvas for render, but doesn't render in itself */
  invalidate: (frames?: number) => void;
  /** Advance (render) one step */
  advance: (timestamp: number, runGlobalEffects?: boolean) => void;
  /** Shortcut to setting the event layer */
  setEvents: (events: Partial<EventManager<any>>) => void;
  /** Shortcut to manual sizing */
  setSize: (width: number, height: number, top?: number, left?: number) => void;
  /** Shortcut to manual setting the pixel ratio */
  setDpr: (dpr: Dpr) => void;
  /** Shortcut to setting frameloop flags */
  setFrameloop: (frameloop: Frameloop) => void;
  /** When the canvas was clicked but nothing was hit */
  onPointerMissed?: (event: MouseEvent) => void;
  /** If this state model is layered (via createPortal) then this contains the previous layer */
  previousRoot?: RootStore;
  /** Internals */
  internal: InternalState;
}

/** A zustand-compatible store API. */
export interface StoreApi<T> {
  getState: GetState<T>;
  setState: SetState<T>;
  getInitialState: GetState<T>;
  subscribe: (listener: (state: T, prevState: T) => void) => () => void;
}

/**
 * The root store. Calling it with a selector returns the selected value as a
 * reactive read (see useThree): in React-style modules the compiler reads it
 * as a value, in Krate modules it is a getter.
 */
export type RootStore = StoreApi<RootState> &
  (<U = RootState>(selector?: (state: RootState) => U, equalityFn?: (a: U, b: U) => boolean) => U);

/** Creates a store with the zustand API whose selections are reactive. */
export function createWithEqualityFn<T extends object>(
  init: (set: SetState<T>, get: GetState<T>, api: StoreApi<T>) => T,
): StoreApi<T> & (<U = T>(selector?: (state: T) => U, equalityFn?: (a: U, b: U) => boolean) => U) {
  let state: T;
  const listeners = new Set<(state: T, prev: T) => void>();
  // A version signal makes selections reactive; it is created on first use
  // so a store can exist before the runtime is installed.
  let version: [() => number, (v: number) => void] | null = null;
  let counter = 0;
  const bump = (): void => {
    counter++;
    if (version) version[1](counter);
  };

  const setState: SetState<T> = (partial, replace) => {
    const next = typeof partial === 'function' ? (partial as (s: T) => T | Partial<T>)(state) : partial;
    if (Object.is(next, state)) return;
    const prev = state;
    state = (replace ?? (typeof next !== 'object' || next === null)) ? (next as T) : Object.assign({}, state, next);
    bump();
    listeners.forEach((listener) => listener(state, prev));
  };
  const getState: GetState<T> = () => state;
  const subscribe = (listener: (state: T, prev: T) => void): (() => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  };

  const select = <U>(selector: (state: T) => U = (s) => s as unknown as U, equalityFn?: (a: U, b: U) => boolean): U => {
    if (!hasRuntime()) return selector(state);
    const runtime = rt();
    if (!version) version = runtime.createSignal(counter);
    const read = version[0];
    let has = false;
    let cached: U;
    // A derived getter: React-style call sites read it as a value, and
    // bindings that read it update when the selection changes. It lives in
    // its own root, released with the component (or effect) that selected.
    let release: () => void = () => {};
    const getter = runtime.createRoot((dispose) => {
      release = dispose;
      return runtime.krateDerived(() => {
        read();
        const next = selector(state);
        if (has && (equalityFn ? equalityFn(cached, next) : Object.is(cached, next))) return cached;
        has = true;
        cached = next;
        return next;
      });
    });
    runtime.onCleanup(release);
    return getter as unknown as U;
  };

  const api = Object.assign(select, { getState, setState, subscribe, getInitialState: () => initial }) as StoreApi<T> &
    (<U = T>(selector?: (state: T) => U, equalityFn?: (a: U, b: U) => boolean) => U);
  state = init(setState, getState, api);
  const initial = state;
  return api;
}

export const createStore = (
  invalidate: (state?: RootState, frames?: number) => void,
  advance: (timestamp: number, runGlobalEffects?: boolean, state?: RootState, frame?: XRFrame) => void,
): RootStore => {
  const rootStore = createWithEqualityFn<RootState>((set, get) => {
    const position = new THREE.Vector3();
    const defaultTarget = new THREE.Vector3();
    const tempTarget = new THREE.Vector3();

    function getCurrentViewport(
      camera: Camera = get().camera,
      target: THREE.Vector3 | Parameters<THREE.Vector3['set']> = defaultTarget,
      size: Size = get().size,
    ): Omit<Viewport, 'dpr' | 'initialDpr'> {
      const { width, height, top, left } = size;
      const aspect = width / height;
      if ((target as THREE.Vector3).isVector3) tempTarget.copy(target as THREE.Vector3);
      else tempTarget.set(...(target as Parameters<THREE.Vector3['set']>));
      const distance = camera.getWorldPosition(position).distanceTo(tempTarget);
      if (isOrthographicCamera(camera)) {
        return { width: width / camera.zoom, height: height / camera.zoom, top, left, factor: 1, distance, aspect };
      }
      const fov = (camera.fov * Math.PI) / 180;
      const h = 2 * Math.tan(fov / 2) * distance;
      const w = h * (width / height);
      return { width: w, height: h, top, left, factor: width / w, distance, aspect };
    }

    let performanceTimeout: ReturnType<typeof setTimeout> | undefined;
    const setPerformanceCurrent = (current: number): void =>
      set((state) => ({ performance: { ...state.performance, current } }));

    const pointer = new THREE.Vector2();

    const rootState: RootState = {
      set,
      get,
      // Configured by the root (see configuration.ts).
      gl: null as unknown as THREE.WebGLRenderer,
      camera: null as unknown as Camera,
      raycaster: null as unknown as THREE.Raycaster,
      events: { priority: 1, enabled: true, connected: false },
      scene: null as unknown as THREE.Scene,
      xr: null as unknown as XRManager,

      invalidate: (frames = 1) => invalidate(get(), frames),
      advance: (timestamp, runGlobalEffects) => advance(timestamp, runGlobalEffects, get()),

      legacy: false,
      linear: false,
      flat: false,
      controls: null,
      clock: new THREE.Clock(),
      pointer,
      mouse: pointer,

      frameloop: 'always',
      onPointerMissed: undefined,

      performance: {
        current: 1,
        min: 0.5,
        max: 1,
        debounce: 200,
        regress: () => {
          const state = get();
          if (performanceTimeout) clearTimeout(performanceTimeout);
          if (state.performance.current !== state.performance.min) setPerformanceCurrent(state.performance.min);
          performanceTimeout = setTimeout(() => setPerformanceCurrent(get().performance.max), state.performance.debounce);
        },
      },

      size: { width: 0, height: 0, top: 0, left: 0 },
      viewport: {
        initialDpr: 0,
        dpr: 0,
        width: 0,
        height: 0,
        top: 0,
        left: 0,
        aspect: 0,
        distance: 0,
        factor: 0,
        getCurrentViewport,
      },

      setEvents: (events) => set((state) => ({ ...state, events: { ...state.events, ...events } })),
      setSize: (width, height, top = 0, left = 0) => {
        const camera = get().camera;
        const size = { width, height, top, left };
        set((state) => ({ size, viewport: { ...state.viewport, ...getCurrentViewport(camera, defaultTarget, size) } }));
      },
      setDpr: (dpr) =>
        set((state) => {
          const resolved = calculateDpr(dpr);
          return { viewport: { ...state.viewport, dpr: resolved, initialDpr: state.viewport.initialDpr || resolved } };
        }),
      setFrameloop: (frameloop = 'always') => {
        const clock = get().clock;
        // With "never" the clock is driven by advance(timestamp).
        clock.stop();
        clock.elapsedTime = 0;
        if (frameloop !== 'never') {
          clock.start();
          clock.elapsedTime = 0;
        }
        set(() => ({ frameloop }));
      },

      previousRoot: undefined,
      internal: {
        interaction: [],
        hovered: new Map(),
        subscribers: [],
        initialClick: [0, 0],
        initialHits: [],
        capturedMap: new Map(),
        lastEvent: { current: null },

        active: false,
        frames: 0,
        priority: 0,
        subscribe: (ref, priority, store) => {
          const internal = get().internal;
          // A positive priority takes over rendering: while any such
          // subscriber exists the root does not render on its own.
          internal.priority = internal.priority + (priority > 0 ? 1 : 0);
          internal.subscribers.push({ ref, priority, store });
          // Lowest priority first, so the highest renders last (on top).
          internal.subscribers = internal.subscribers.sort((a, b) => a.priority - b.priority);
          return () => {
            const internal = get().internal;
            if (internal?.subscribers) {
              internal.priority = internal.priority - (priority > 0 ? 1 : 0);
              internal.subscribers = internal.subscribers.filter((s) => s.ref !== ref);
            }
          };
        },
      },
    };
    return rootState;
  }) as RootStore;

  const state = rootStore.getState();
  let oldSize = state.size;
  let oldDpr = state.viewport.dpr;
  let oldCamera = state.camera;
  rootStore.subscribe(() => {
    const { camera, size, viewport, gl, set } = rootStore.getState();
    // Resize camera and renderer on size and pixel ratio changes.
    if (size.width !== oldSize.width || size.height !== oldSize.height || viewport.dpr !== oldDpr) {
      oldSize = size;
      oldDpr = viewport.dpr;
      updateCamera(camera, size);
      if (viewport.dpr > 0) gl.setPixelRatio?.(viewport.dpr);
      const updateStyle = typeof HTMLCanvasElement !== 'undefined' && gl.domElement instanceof HTMLCanvasElement;
      gl.setSize?.(size.width, size.height, updateStyle);
    }
    // The viewport depends on the camera.
    if (camera !== oldCamera) {
      oldCamera = camera;
      set((state) => ({ viewport: { ...state.viewport, ...state.viewport.getCurrentViewport(camera) } }));
    }
  });

  // Any change requests a frame.
  rootStore.subscribe((state) => invalidate(state));
  return rootStore;
};
