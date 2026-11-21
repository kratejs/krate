// The runtime half of the compile-time scene optimizer. The Krate compiler
// replaces the provably static parts of a <Canvas> (meshes and groups with
// constant geometry, material and transform props) with one <CompiledScene>
// element whose `build` function calls the Builder below. The Builder makes the
// three objects directly: shared geometries and materials, matrices computed at
// build time with matrixAutoUpdate off, instanced meshes, and direct writes for
// reactive transforms. Everything else in the Canvas stays on the reconciler.

import * as THREE from 'three';
import { removeInteractivity } from './events.js';
import type { Instance } from './instance.js';
import { getCurrentStore } from './reconciler.js';
import { rt } from './runtime.js';
import type { RootStore } from './store.js';
import { applyProps } from './utils.js';

/** A compile-time matrix: the 16 elements, `true` for the identity, null when not static. */
export type StaticMatrix = ArrayLike<number> | true | null | undefined;
type Vec3 = ArrayLike<number>;

const GEOMETRIES: Record<string, new (...args: any[]) => THREE.BufferGeometry> = {
  box: THREE.BoxGeometry,
  sphere: THREE.SphereGeometry,
  plane: THREE.PlaneGeometry,
  cylinder: THREE.CylinderGeometry,
  torus: THREE.TorusGeometry,
  circle: THREE.CircleGeometry,
};

const MATERIALS: Record<string, new () => THREE.Material> = {
  meshStandardMaterial: THREE.MeshStandardMaterial,
  meshPhysicalMaterial: THREE.MeshPhysicalMaterial,
  meshBasicMaterial: THREE.MeshBasicMaterial,
  meshLambertMaterial: THREE.MeshLambertMaterial,
  meshPhongMaterial: THREE.MeshPhongMaterial,
};

// Geometries and materials shared by every compiled subtree of a root.
const caches = new WeakMap<RootStore, Map<string, unknown>>();
const warmed = new WeakSet<RootStore>();

function cached<T>(store: RootStore, key: string, make: () => T): T {
  let map = caches.get(store);
  if (!map) caches.set(store, (map = new Map()));
  let value = map.get(key) as T | undefined;
  if (value === undefined) map.set(key, (value = make()));
  return value;
}

const _m = new THREE.Matrix4();
const _pm = new THREE.Matrix4();
const _p = new THREE.Vector3();
const _s = new THREE.Vector3();
const _q = new THREE.Quaternion();
const _e = new THREE.Euler();

type Handlers = Record<string, ((event: any) => void) | undefined>;

/** Writes the matrices of one InstancedMesh and uploads only what changed. */
export class InstanceWriter {
  private lo = Infinity;
  private hi = -1;
  private readonly array: Float32Array;
  private readonly parent: THREE.Matrix4 | null;

  constructor(
    readonly mesh: THREE.InstancedMesh,
    parent: ArrayLike<number> | null,
    private readonly invalidate: () => void,
  ) {
    this.array = mesh.instanceMatrix.array as Float32Array;
    this.parent = parent ? new THREE.Matrix4().fromArray(parent as number[]) : null;
    mesh.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
    // The bounding sphere is computed once from the initial matrices.
    mesh.frustumCulled = false;
    mesh.onBeforeRender = () => this.flush();
  }

  /** Writes instance `i` from three numbers each; no allocation. */
  setv(i: number, px: number, py: number, pz: number, rx: number, ry: number, rz: number, sx: number, sy: number, sz: number): void {
    _p.set(px, py, pz);
    _e.set(rx, ry, rz);
    _q.setFromEuler(_e);
    _s.set(sx, sy, sz);
    this.put(i);
  }

  /** Writes instance `i` from position/rotation arrays and a scale number or array. */
  set(i: number, p?: Vec3 | null, r?: Vec3 | null, s?: Vec3 | number | null): void {
    if (p) _p.set(p[0], p[1], p[2]);
    else _p.set(0, 0, 0);
    if (r) _e.set(r[0], r[1], r[2]);
    else _e.set(0, 0, 0);
    _q.setFromEuler(_e);
    if (typeof s === 'number') _s.set(s, s, s);
    else if (s) _s.set(s[0], s[1], s[2]);
    else _s.set(1, 1, 1);
    this.put(i);
  }

  private put(i: number): void {
    _m.compose(_p, _q, _s);
    if (this.parent) _m.premultiply(this.parent);
    _m.toArray(this.array as unknown as number[], i * 16);
    if (i < this.lo) this.lo = i;
    if (i > this.hi) this.hi = i;
    this.invalidate();
  }

  private flush(): void {
    if (this.hi < this.lo) return;
    const attr = this.mesh.instanceMatrix as THREE.InstancedBufferAttribute & {
      addUpdateRange?: (start: number, count: number) => void;
      clearUpdateRanges?: () => void;
      updateRange?: { offset: number; count: number };
    };
    const start = this.lo * 16;
    const count = (this.hi - this.lo + 1) * 16;
    if (typeof attr.addUpdateRange === 'function') {
      attr.clearUpdateRanges?.();
      attr.addUpdateRange(start, count);
    } else if (attr.updateRange) {
      attr.updateRange.offset = start;
      attr.updateRange.count = count;
    }
    attr.needsUpdate = true;
    this.lo = Infinity;
    this.hi = -1;
  }
}

function isIdentityScene(scene: THREE.Object3D): boolean {
  return (
    !scene.parent &&
    scene.position.lengthSq() === 0 &&
    scene.quaternion.x === 0 &&
    scene.quaternion.y === 0 &&
    scene.quaternion.z === 0 &&
    scene.quaternion.w === 1 &&
    scene.scale.x === 1 &&
    scene.scale.y === 1 &&
    scene.scale.z === 1
  );
}

/** What a compiled subtree's `build` function calls to make its objects. */
export class Builder {
  /** The group the subtree is built under (mounted as a primitive). */
  readonly root = new THREE.Group();
  private readonly frozen: boolean;
  private readonly interactive: THREE.Object3D[] = [];
  private readonly instanced: THREE.InstancedMesh[] = [];

  constructor(
    private readonly store: RootStore,
    top: boolean,
  ) {
    // The whole chain up to the scene is static only for a subtree that sits
    // directly in a scene that is not itself transformed.
    this.frozen = top && isIdentityScene(store.getState().scene);
    this.place(this.root, true, true);
  }

  geo(kind: string, args?: number[]): THREE.BufferGeometry {
    return cached(this.store, `g:${kind}:${JSON.stringify(args ?? [])}`, () => new GEOMETRIES[kind](...(args ?? [])));
  }

  mat(kind: string, props?: Record<string, unknown>): THREE.Material {
    return cached(this.store, `m:${kind}:${JSON.stringify(props ?? {})}`, () => {
      const m = new MATERIALS[kind]();
      if (props) applyProps(m as never, props as never);
      return m;
    });
  }

  group(parent: THREE.Object3D, local: StaticMatrix, world?: StaticMatrix, props?: Record<string, unknown> | null): THREE.Group {
    const o = new THREE.Group();
    return this.finish(o, parent, local, world, props);
  }

  mesh(
    parent: THREE.Object3D,
    geometry: THREE.BufferGeometry,
    material: THREE.Material,
    local: StaticMatrix,
    world?: StaticMatrix,
    props?: Record<string, unknown> | null,
    handlers?: Handlers | null,
  ): THREE.Mesh {
    const o = this.finish(new THREE.Mesh(geometry, material), parent, local, world, props);
    if (handlers) this.listen(o, handlers);
    return o;
  }

  /** A mesh drawn once for every 16 numbers of `matrices` (a Float32Array). */
  instances(
    parent: THREE.Object3D,
    geometry: THREE.BufferGeometry,
    material: THREE.Material,
    matrices: Float32Array,
    props?: Record<string, unknown> | null,
  ): THREE.InstancedMesh {
    const o = new THREE.InstancedMesh(geometry, material, matrices.length / 16);
    (o.instanceMatrix.array as Float32Array).set(matrices);
    this.instanced.push(o);
    return this.finish(o, parent, true, true, props);
  }

  /**
   * Like `instances` for `count` items whose matrices are written by effects
   * through the returned writer; `parentMatrix` is the static matrix of the
   * ancestors the items are composed against.
   */
  dynamicInstances(
    parent: THREE.Object3D,
    geometry: THREE.BufferGeometry,
    material: THREE.Material,
    count: number,
    parentMatrix?: ArrayLike<number> | null,
    props?: Record<string, unknown> | null,
  ): InstanceWriter {
    const o = new THREE.InstancedMesh(geometry, material, count);
    for (let i = 0; i < count; i++) o.setMatrixAt(i, _m.identity());
    this.instanced.push(o);
    this.finish(o, parent, true, true, props);
    return new InstanceWriter(o, parentMatrix ?? null, this.invalidate);
  }

  /** Runs `fn` as a reactive effect; each run requests a frame. */
  fx(fn: () => void): void {
    rt().createEffect(() => {
      fn();
      this.invalidate();
    });
  }

  /** Sets a vector or euler from an array, a number or another vector (position={...}). */
  vec(target: { set(...a: number[]): unknown }, value: unknown): void {
    applyProps({ target } as never, { target: value } as never);
  }

  readonly invalidate = (): void => {
    this.store.getState().invalidate();
  };

  /** Releases what the subtree owns; shared geometries and materials stay. */
  dispose(): void {
    for (const o of this.interactive) removeInteractivity(this.store, o);
    for (const o of this.instanced) o.dispose();
  }

  private finish<T extends THREE.Object3D>(o: T, parent: THREE.Object3D, local: StaticMatrix, world: StaticMatrix | undefined, props?: Record<string, unknown> | null): T {
    if (props) applyProps(o as never, props as never);
    this.place(o, local, world);
    parent.add(o);
    return o;
  }

  // Installs the matrices computed at compile time and stops three from
  // recomputing them. matrixWorld is only frozen when nothing above can move.
  private place(o: THREE.Object3D, local: StaticMatrix, world: StaticMatrix | undefined): void {
    // An omitted world matrix is the local one (a child of the identity).
    if (world === undefined) world = local;
    if (local) {
      if (local !== true) o.matrix.fromArray(local as number[]);
      o.matrixAutoUpdate = false;
    }
    if (world && this.frozen) {
      if (world !== true) o.matrixWorld.fromArray(world as number[]);
      o.matrixWorldAutoUpdate = false;
      o.matrixWorldNeedsUpdate = false;
    }
  }

  // Meshes with handlers join the event system like reconciler instances.
  private listen(o: THREE.Mesh, handlers: Handlers): void {
    const live: Handlers = {};
    for (const name in handlers) if (typeof handlers[name] === 'function') live[name] = handlers[name];
    const eventCount = Object.keys(live).length;
    if (eventCount === 0) return;
    const inst: Instance = {
      root: this.store,
      type: 'mesh',
      parent: null,
      children: [],
      props: {} as Instance['props'],
      object: o as Instance['object'],
      eventCount,
      handlers: live,
      previousVisible: undefined,
      isHidden: false,
    };
    (o as THREE.Mesh & { __r3f?: Instance }).__r3f = inst;
    this.store.getState().internal.interaction.push(o);
    this.interactive.push(o);
  }
}

// Compiles the shaders of everything in the scene before the first frame, so
// the first render does not stall on them. compileAsync (three r158+) uses
// parallel shader compilation where the browser has it.
function warmUp(store: RootStore): void {
  if (warmed.has(store)) return;
  warmed.add(store);
  queueMicrotask(() => {
    const state = store.getState();
    const gl = state.gl as unknown as {
      compileAsync?: (scene: THREE.Object3D, camera: THREE.Camera) => Promise<unknown>;
      compile?: (scene: THREE.Object3D, camera: THREE.Camera) => unknown;
    };
    try {
      if (typeof gl.compileAsync === 'function') void gl.compileAsync(state.scene, state.camera).catch(() => {});
      else if (typeof gl.compile === 'function') gl.compile(state.scene, state.camera);
    } catch {
      // A renderer that cannot precompile still renders on demand.
    }
  });
}

/** The element the compiler emits for the static part of a <Canvas>. */
export function CompiledScene(props: { build: (t: Builder) => void; top?: boolean }): unknown {
  const store = getCurrentStore();
  if (!store) throw new Error('@krate/three: a compiled scene can only be used within the Canvas component!');
  const t = new Builder(store, props.top === true);
  props.build(t);
  warmUp(store);
  rt().onCleanup(() => t.dispose());
  return { __k3: 1, type: 'primitive', props: { object: t.root }, children: [] };
}
