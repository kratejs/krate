// Object, prop and camera helpers. Ported from @react-three/fiber (MIT,
// Copyright (c) 2019-2025 Poimandres), see NOTICE.

import * as THREE from 'three';
import type { Instance } from './instance.js';
import type { Dpr, RootState, RootStore, Size } from './store.js';

export type NonFunctionKeys<P> = { [K in keyof P]-?: P[K] extends Function ? never : K }[keyof P];
export type Overwrite<P, O> = Omit<P, NonFunctionKeys<O>> & O;
export type Properties<T> = Pick<T, NonFunctionKeys<T>>;
export type Mutable<P> = { [K in keyof P]: P[K] | Readonly<P[K]> };
export type IsOptional<T> = undefined extends T ? true : false;
export type IsAllOptional<T extends any[]> = T extends [infer First, ...infer Rest]
  ? IsOptional<First> extends true
    ? IsAllOptional<Rest>
    : false
  : true;

export type Camera = (THREE.OrthographicCamera | THREE.PerspectiveCamera) & { manual?: boolean };

/** Named nodes, materials and meshes of an object tree (see buildGraph). */
export interface ObjectMap {
  nodes: { [name: string]: THREE.Object3D };
  materials: { [name: string]: THREE.Material };
  meshes: { [name: string]: THREE.Mesh };
}

export interface Disposable {
  type?: string;
  dispose?: () => void;
}

/** Returns the instance's initial (outmost) root. */
export function findInitialRoot<T>(instance: Instance<T>): RootStore {
  let root = instance.root;
  while (root.getState().previousRoot) root = root.getState().previousRoot!;
  return root;
}

export const isOrthographicCamera = (def: Camera): def is THREE.OrthographicCamera =>
  !!def && (def as THREE.OrthographicCamera).isOrthographicCamera;
export const isRef = (obj: any): obj is { current: unknown } =>
  !!obj && typeof obj === 'object' && Object.prototype.hasOwnProperty.call(obj, 'current');
export const isColorRepresentation = (value: unknown): value is THREE.ColorRepresentation =>
  value != null && (typeof value === 'string' || typeof value === 'number' || (value as THREE.Color).isColor);
export const noop = (): void => {};

/** Resolves a pixel ratio, clamping a `[min, max]` range to the device ratio. */
export function calculateDpr(dpr: Dpr): number {
  // Assume 2x when the ratio cannot be detected (workers define window without it).
  const target = typeof window !== 'undefined' ? (window.devicePixelRatio ?? 2) : 1;
  return Array.isArray(dpr) ? Math.min(Math.max(dpr[0], target), dpr[1]) : dpr;
}

/**
 * Calls `onChange` whenever window.devicePixelRatio changes (another display,
 * browser zoom). A resolution query only matches the current ratio, so it is
 * re-created after every change. Returns the unsubscribe function.
 */
export function watchDpr(onChange: () => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function' || !window.devicePixelRatio) {
    return () => {};
  }
  let query: MediaQueryList | undefined;
  const listen = (): void => {
    try {
      query = window.matchMedia(`(resolution: ${window.devicePixelRatio}dppx)`);
      if (typeof query?.addEventListener === 'function') query.addEventListener('change', handleChange);
      else (query as any)?.addListener?.(handleChange);
    } catch {
      query = undefined;
    }
  };
  const unlisten = (): void => {
    if (typeof query?.removeEventListener === 'function') query.removeEventListener('change', handleChange);
    else (query as any)?.removeListener?.(handleChange);
    query = undefined;
  };
  const handleChange = (): void => {
    unlisten();
    listen();
    onChange();
  };
  listen();
  return unlisten;
}

/** Returns the root state of an object managed by @krate/three. */
export function getRootState<T extends THREE.Object3D = THREE.Object3D>(obj: T): RootState | undefined {
  return (obj as unknown as { __r3f?: Instance }).__r3f?.root.getState();
}

export interface EquConfig {
  /** Compare arrays by reference (a === b) or shallowly (default). */
  arrays?: 'reference' | 'shallow';
  /** Compare objects by reference (default) or shallowly. */
  objects?: 'reference' | 'shallow';
  /** Whether both sides must have the same keys (default true). */
  strict?: boolean;
}

/** A collection of type checks and a configurable equality. */
export const is = {
  obj: (a: any): boolean => a === Object(a) && !is.arr(a) && typeof a !== 'function',
  fun: (a: any): a is Function => typeof a === 'function',
  str: (a: any): a is string => typeof a === 'string',
  num: (a: any): a is number => typeof a === 'number',
  boo: (a: any): a is boolean => typeof a === 'boolean',
  und: (a: any): boolean => a === void 0,
  nul: (a: any): boolean => a === null,
  arr: (a: any): boolean => Array.isArray(a),
  equ(a: any, b: any, { arrays = 'shallow', objects = 'reference', strict = true }: EquConfig = {}): boolean {
    if (typeof a !== typeof b || !!a !== !!b) return false;
    if (is.str(a) || is.num(a) || is.boo(a)) return a === b;
    const isObj = is.obj(a);
    if (isObj && objects === 'reference') return a === b;
    const isArr = is.arr(a);
    if (isArr && arrays === 'reference') return a === b;
    if ((isArr || isObj) && a === b) return true;
    let i: any;
    for (i in a) if (!(i in b)) return false;
    if (isObj && arrays === 'shallow' && objects === 'shallow') {
      for (i in strict ? b : a) if (!is.equ(a[i], b[i], { strict, objects: 'reference' })) return false;
    } else {
      for (i in strict ? b : a) if (a[i] !== b[i]) return false;
    }
    if (is.und(i)) {
      if (isArr && a.length === 0 && b.length === 0) return true;
      if (isObj && Object.keys(a).length === 0 && Object.keys(b).length === 0) return true;
      if (a !== b) return false;
    }
    return true;
  },
};

/** Collects the named nodes, materials and meshes of an object tree. */
export function buildGraph(object: THREE.Object3D): ObjectMap {
  const data: ObjectMap = { nodes: {}, materials: {}, meshes: {} };
  if (object) {
    object.traverse((obj: any) => {
      if (obj.name) data.nodes[obj.name] = obj;
      if (obj.material && !data.materials[obj.material.name]) data.materials[obj.material.name] = obj.material;
      if (obj.isMesh && !data.meshes[obj.name]) data.meshes[obj.name] = obj;
    });
  }
  return data;
}

/** Disposes an object and every disposable property of it (scenes excepted). */
export function dispose<T extends Disposable>(obj: T): void {
  if (obj.type !== 'Scene') obj.dispose?.();
  for (const p in obj) {
    const prop = (obj as any)[p] as Disposable | undefined;
    if (prop?.type !== 'Scene') prop?.dispose?.();
  }
}

export const REACT_INTERNAL_PROPS = ['children', 'key', 'ref'];

/** Returns the props that describe the instance (without children, key, ref). */
export function getInstanceProps<T = any>(pendingProps: Record<string, unknown>): Instance<T>['props'] {
  const props: Record<string, unknown> = {};
  for (const key in pendingProps) {
    if (!REACT_INTERNAL_PROPS.includes(key)) props[key] = pendingProps[key];
  }
  return props as Instance<T>['props'];
}

/** Attaches an instance descriptor to an object (or returns the existing one). */
export function prepare<T = any>(target: T, root: RootStore, type: string, props: Instance<T>['props']): Instance<T> {
  const object = target as Instance<T>['object'];
  let instance = object?.__r3f;
  if (!instance) {
    instance = {
      root,
      type,
      parent: null,
      children: [],
      props: getInstanceProps(props as Record<string, unknown>),
      object,
      eventCount: 0,
      handlers: {},
      previousVisible: undefined,
      isHidden: false,
    };
    if (object) object.__r3f = instance;
  }
  return instance;
}

/** Resolves a possibly pierced key: `material-color` is `material.color`. */
export function resolve(root: any, key: string): { root: any; key: string; target: any } {
  if (!key.includes('-')) return { root, key, target: root[key] };
  // A key that exists as written (foo-bar) wins over piercing.
  if (key in root) return { root, key, target: root[key] };
  let target = root;
  const parts = key.split('-');
  for (const part of parts) {
    if (typeof target !== 'object' || target === null) {
      if (target !== undefined) {
        const remaining = parts.slice(parts.indexOf(part)).join('-');
        return { root: target, key: remaining, target: undefined };
      }
      return { root, key, target: undefined };
    }
    key = part;
    root = target;
    target = target[key];
  }
  return { root, key, target };
}

// Matches a dash-cased key that ends with an index (material-0).
const INDEX_REGEX = /-\d+$/;

/** Attaches a child to its parent through its `attach` prop. */
export function attach(parent: Instance, child: Instance): void {
  if (is.str(child.props.attach)) {
    if (INDEX_REGEX.test(child.props.attach)) {
      const index = child.props.attach.replace(INDEX_REGEX, '');
      const { root, key } = resolve(parent.object, index);
      if (!Array.isArray(root[key])) root[key] = [];
    }
    const { root, key } = resolve(parent.object, child.props.attach);
    child.previousAttach = root[key];
    root[key] = child.object;
  } else if (is.fun(child.props.attach)) {
    child.previousAttach = child.props.attach(parent.object, child.object);
  }
}

/** Reverts attach(). */
export function detach(parent: Instance, child: Instance): void {
  if (is.str(child.props.attach)) {
    const { root, key } = resolve(parent.object, child.props.attach);
    const previous = child.previousAttach;
    if (previous === undefined) delete root[key];
    else root[key] = previous;
  } else {
    child.previousAttach?.(parent.object, child.object);
  }
  delete child.previousAttach;
}

export const RESERVED_PROPS = [...REACT_INTERNAL_PROPS, 'args', 'dispose', 'attach', 'object', 'onUpdate'];

const MEMOIZED_PROTOTYPES = new Map<Function, any>();

function getMemoizedPrototype(root: any): any {
  let ctor = MEMOIZED_PROTOTYPES.get(root.constructor);
  try {
    if (!ctor) {
      ctor = new root.constructor();
      MEMOIZED_PROTOTYPES.set(root.constructor, ctor);
    }
  } catch {
    // A constructor that needs arguments has no default to restore.
  }
  return ctor;
}

/** Computes the changes that turn an instance's props into `newProps`. */
export function diffProps<T = any>(instance: Instance<T>, newProps: Instance<T>['props']): Instance<T>['props'] {
  const changedProps: Record<string, unknown> = {};
  const next = newProps as Record<string, unknown>;
  const prev = instance.props as Record<string, unknown>;
  for (const prop in next) {
    if (RESERVED_PROPS.includes(prop)) continue;
    if (is.equ(next[prop], prev[prop])) continue;
    changedProps[prop] = next[prop];
    // Re-apply pierced props of a replaced value (material then material-color).
    for (const other in next) {
      if (other.startsWith(`${prop}-`)) changedProps[other] = next[other];
    }
  }
  // Removed props go back to the class default where one can be determined.
  for (const prop in prev) {
    if (RESERVED_PROPS.includes(prop) || Object.prototype.hasOwnProperty.call(next, prop)) continue;
    const { root, key } = resolve(instance.object, prop);
    if (root.constructor && root.constructor.length === 0) {
      const ctor = getMemoizedPrototype(root);
      if (!is.und(ctor)) changedProps[prop] = ctor[key];
    } else {
      changedProps[prop] = 0;
    }
  }
  return changedProps as Instance<T>['props'];
}

// https://github.com/mrdoob/three.js/pull/27042
const colorMaps = new Set(['map', 'emissiveMap', 'sheenColorMap', 'specularColorMap', 'envMap']);
const EVENT_REGEX = /^on(Pointer|Click|DoubleClick|ContextMenu|Wheel)/;

/** Applies props to a three object, with math, color and pierced-key semantics. */
export function applyProps<T = any>(object: Instance<T>['object'], props: Instance<T>['props']): Instance<T>['object'] {
  const instance = (object as any).__r3f as Instance<T> | undefined;
  const rootState = instance && findInitialRoot(instance).getState();
  const prevHandlers = instance?.eventCount;

  for (const prop in props as Record<string, unknown>) {
    const value = (props as Record<string, unknown>)[prop];
    if (RESERVED_PROPS.includes(prop)) continue;

    if (instance && EVENT_REGEX.test(prop)) {
      if (typeof value === 'function') (instance.handlers as any)[prop] = value;
      else delete (instance.handlers as any)[prop];
      instance.eventCount = Object.keys(instance.handlers).length;
      continue;
    }

    // Undefined values are ignored, like in R3F.
    if (value === undefined) continue;

    // Keep a hidden instance hidden: remember the value for when it shows.
    if (prop === 'visible' && instance?.isHidden && instance.previousVisible !== undefined) {
      instance.previousVisible = value as boolean;
      continue;
    }

    const { root, key, target } = resolve(object, prop);

    if (target === undefined && (typeof root !== 'object' || root === null)) {
      throw new Error(`@krate/three: Cannot set "${prop}". Ensure it is an object before setting "${key}".`);
    }

    if (target instanceof THREE.Layers && value instanceof THREE.Layers) {
      target.mask = value.mask;
    } else if (target instanceof THREE.Color && isColorRepresentation(value)) {
      target.set(value);
    } else if (
      target !== null &&
      typeof target === 'object' &&
      typeof target.set === 'function' &&
      typeof target.copy === 'function' &&
      value != null &&
      (value as object).constructor &&
      target.constructor === (value as object).constructor
    ) {
      target.copy(value);
    } else if (target !== null && typeof target === 'object' && typeof target.set === 'function' && Array.isArray(value)) {
      if (typeof target.fromArray === 'function') target.fromArray(value);
      else target.set(...value);
    } else if (target !== null && typeof target === 'object' && typeof target.set === 'function' && typeof value === 'number') {
      if (typeof target.setScalar === 'function') target.setScalar(value);
      else target.set(value);
    } else if (root instanceof THREE.ShaderMaterial && key === 'uniforms' && is.obj(value)) {
      // Uniform objects keep their identity so compiled programs see updates.
      if (!is.obj(root.uniforms)) root.uniforms = {};
      const uniforms = root.uniforms as Record<string, THREE.IUniform>;
      const nextUniforms = value as Record<string, THREE.IUniform>;
      for (const name in nextUniforms) {
        const uniform = nextUniforms[name];
        const targetUniform = uniforms[name];
        if (targetUniform) Object.assign(targetUniform, uniform);
        else uniforms[name] = { ...uniform };
      }
    } else {
      root[key] = value;
      // sRGB textures for built-in material color maps (RGBA8 only).
      if (
        rootState &&
        !rootState.linear &&
        colorMaps.has(key) &&
        root[key]?.isTexture &&
        root[key].format === THREE.RGBAFormat &&
        root[key].type === THREE.UnsignedByteType
      ) {
        root[key].colorSpace = THREE.SRGBColorSpace;
      }
    }
  }

  // Register the object with the event system when its handlers change.
  if (instance?.parent && rootState?.internal && (instance.object as any)?.isObject3D && prevHandlers !== instance.eventCount) {
    const obj = instance.object as unknown as THREE.Object3D;
    const index = rootState.internal.interaction.indexOf(obj);
    if (index !== -1) rootState.internal.interaction.splice(index, 1);
    if (instance.eventCount && (obj as any).raycast !== null) rootState.internal.interaction.push(obj);
  }

  // Geometries and materials attach to their parent mesh by default.
  if (instance && instance.props.attach === undefined) {
    if ((instance.object as any).isBufferGeometry) instance.props.attach = 'geometry';
    else if ((instance.object as any).isMaterial) instance.props.attach = 'material';
  }

  if (instance) invalidateInstance(instance);
  return object;
}

/** Calls onUpdate and requests a frame for an instance in the scene. */
export function invalidateInstance(instance: Instance): void {
  if (!instance.parent) return;
  instance.props.onUpdate?.(instance.object);
  const state = instance.root?.getState?.();
  if (state && state.internal.frames === 0) state.invalidate();
}

/** Fits a default camera to the canvas size (manual cameras are left alone). */
export function updateCamera(camera: Camera, size: Size): void {
  if (camera.manual) return;
  if (isOrthographicCamera(camera)) {
    camera.left = size.width / -2;
    camera.right = size.width / 2;
    camera.top = size.height / 2;
    camera.bottom = size.height / -2;
  } else {
    camera.aspect = size.width / size.height;
  }
  camera.updateProjectionMatrix();
}

export const isObject3D = (object: any): object is THREE.Object3D => object?.isObject3D;
