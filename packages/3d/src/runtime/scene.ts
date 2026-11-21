// One Filament engine, scene, view and frame loop per <canvas>. Calls made
// before the engine is ready (Filament initialises asynchronously) are queued
// and replayed in order, so compiled code can run synchronously.

import type { PointerHandler } from '../types.js';
import { lightIntensity } from './calibration.js';
import { Entities } from './entities.js';
import type {
  FAssetLoader,
  FCamera,
  FColorGrading,
  FEngine,
  FEntity,
  FIndirectLight,
  FRenderer,
  FScene,
  FSkybox,
  FSwapChain,
  FView,
  FilamentModule,
  FilamentSource,
  Mat4,
} from './filament.js';
import { loadFilament } from './filament.js';
import { buildGeometry, uploadGeometry, type GeometryKind, type GpuGeometry } from './geometry.js';
import { buildAmbient, buildLight, updateLight, type LightKind, type LightProps } from './lights.js';
import { MaterialManager, type CustomMaterialSpec, type MaterialHandle, type MaterialKind, type MaterialParams, type ParamValue } from './materials.js';
import { Picker, type PointerEventName } from './picking.js';
import { buildInstanced, buildMesh, type MeshFlags } from './renderables.js';
import { TransformBuffer } from './transforms.js';
import {
  applyExposure,
  applyLookAt,
  applyProjection,
  applyView,
  cameraState,
  type CameraState,
  type SceneCameraOptions,
  type ViewOptions,
} from './view.js';

export interface SceneOptions {
  /** Number of dynamic transform slots the compiled code uses. */
  slots?: number;
  view?: ViewOptions;
  camera?: SceneCameraOptions;
  /** Base URL of the compiled .filamat files; defaults to the package's own materials. */
  materialBase?: string;
  /** URL of the filament.js the build copied into the site (the compiler fills it in). */
  filamentUrl?: string;
  /** An already loaded Filament module (or loader) instead of the script at `filamentUrl`. */
  filament?: FilamentSource;
  pixelRatio?: number;
  /** Fetches bytes for materials and textures; defaults to fetch(). */
  fetchBytes?: (url: string) => Promise<Uint8Array>;
  onError?: (err: unknown) => void;
}

export interface GeometryHandle {
  kind: GeometryKind;
  args: number[];
  gpu?: GpuGeometry;
}
export interface MeshHandle {
  /** Filament entity id once built, else 0. */
  id: number;
  slot: number;
  entities: FEntity[];
  built: Array<() => void>;
}
export interface LightHandle {
  kind: LightKind;
  props: LightProps;
  entity?: FEntity;
  ibl?: FIndirectLight;
}
export interface InitialTransform {
  pos?: readonly number[];
  quat?: readonly number[];
  scale?: readonly number[];
}
export type FrameCallback = (delta: number, time: number) => void;

export class Scene3D {
  readonly canvas: HTMLCanvasElement;
  readonly opts: SceneOptions;
  readonly transforms: TransformBuffer;
  /** Resolves once the engine is up and queued calls have replayed. */
  readonly ready: Promise<void>;
  isReady = false;
  disposed = false;
  error: unknown;

  F!: FilamentModule;
  engine!: FEngine;
  fscene!: FScene;
  view!: FView;
  camera!: FCamera;
  entities!: Entities;
  materials!: MaterialManager;
  picker!: Picker;
  camState: CameraState;
  private cameraEntity!: FEntity;
  private renderer!: FRenderer;
  private swapChain!: FSwapChain;
  private grading?: FColorGrading;
  private loader?: FAssetLoader;
  private assets: Array<{ destroy(): void }> = [];
  private geometries = new Map<string, GeometryHandle>();
  private queue: Array<() => void> = [];
  private frameCallbacks: FrameCallback[] = [];
  private ambient?: FIndirectLight;
  private env?: { ibl?: FIndirectLight; sky?: FSkybox };
  private nextSlot: number;
  private nextId = 1;
  private cameraDirty = true;
  private resizeObserver?: ResizeObserver;
  private visibility?: IntersectionObserver;
  private visible = true;
  private raf = 0;
  private last = 0;

  constructor(canvas: HTMLCanvasElement, opts: SceneOptions = {}) {
    this.canvas = canvas;
    this.opts = opts;
    this.transforms = new TransformBuffer(opts.slots ?? 0);
    this.nextSlot = opts.slots ?? 0;
    this.camState = cameraState(opts.camera);
    this.ready = this.init().catch((err) => {
      this.error = err;
      (opts.onError ?? console.error)(err);
    });
  }

  get pixelRatio(): number {
    return this.opts.pixelRatio ?? (typeof window !== 'undefined' ? window.devicePixelRatio : 1) ?? 1;
  }

  private async init(): Promise<void> {
    const F = await loadFilament(this.opts.filament, this.opts.filamentUrl);
    if (this.disposed) return;
    this.F = F;
    this.sizeCanvas();
    const engine = (this.engine = F.Engine.create(this.canvas, { alpha: true }));
    this.entities = new Entities(F, engine);
    this.fscene = engine.createScene();
    this.swapChain = engine.createSwapChain();
    this.renderer = engine.createRenderer();
    this.view = engine.createView();
    this.renderer.setClearOptions({ clearColor: [0, 0, 0, 0], clear: true });
    this.view.setBlendMode(F.View$BlendMode.TRANSLUCENT);
    this.cameraEntity = this.entities.create();
    this.camera = engine.createCamera(this.cameraEntity);
    this.view.setCamera(this.camera);
    this.view.setScene(this.fscene);
    applyExposure(this.camera, this.opts.camera);
    this.grading = applyView(F, engine, this.view, this.opts.view ?? {});
    this.materials = new MaterialManager(F, engine, this.opts.materialBase, this.opts.fetchBytes);
    this.picker = new Picker(this.view, this.canvas, () => this.pixelRatio);
    if (typeof ResizeObserver !== 'undefined') {
      this.resizeObserver = new ResizeObserver(() => this.applySize());
      this.resizeObserver.observe(this.canvas);
    }
    if (typeof IntersectionObserver !== 'undefined') {
      this.visibility = new IntersectionObserver((entries) => {
        for (const e of entries) this.visible = e.isIntersecting;
      });
      this.visibility.observe(this.canvas);
    }
    this.isReady = true;
    this.applySize();
    for (const fn of this.queue.splice(0)) this.run(fn);
    this.raf = requestAnimationFrame(this.loop);
  }

  private run(fn: () => void): void {
    try {
      fn();
    } catch (err) {
      this.error = err;
      (this.opts.onError ?? console.error)(err);
    }
  }

  /** Runs fn now if the engine is up, else after it is. */
  whenReady(fn: () => void): void {
    if (this.disposed) return;
    if (this.isReady) this.run(fn);
    else this.queue.push(fn);
  }

  private sizeCanvas(): void {
    const dpr = this.pixelRatio;
    const w = Math.max(1, Math.round(this.canvas.clientWidth * dpr));
    const h = Math.max(1, Math.round(this.canvas.clientHeight * dpr));
    if (this.canvas.width !== w) this.canvas.width = w;
    if (this.canvas.height !== h) this.canvas.height = h;
  }

  /** Matches the drawing buffer to the CSS size and device pixel ratio. */
  applySize(): void {
    if (this.disposed || !this.isReady) return;
    this.sizeCanvas();
    const { width, height } = this.canvas;
    this.view.setViewport([0, 0, width, height]);
    applyProjection(this.F, this.camera, this.camState, width, height);
  }

  // Slots and handles

  allocSlot(): number {
    const s = this.nextSlot++;
    this.transforms.ensure(this.nextSlot);
    return s;
  }

  geometry(kind: GeometryKind, args: readonly number[] = []): GeometryHandle {
    const key = `${kind}:${args.join(',')}`;
    let h = this.geometries.get(key);
    if (!h) {
      h = { kind, args: [...args] };
      this.geometries.set(key, h);
    }
    return h;
  }

  private gpu(h: GeometryHandle): GpuGeometry {
    return (h.gpu ??= uploadGeometry(this.F, this.engine, buildGeometry(h.kind, h.args)));
  }

  material(kind: MaterialKind, params?: MaterialParams): MaterialHandle;
  /** A shaderMaterial compiled by the build: `url` is the hashed .filamat, `params` the initial uniforms. */
  material(kind: 'custom', spec: CustomMaterialSpec): MaterialHandle;
  material(kind: MaterialKind | 'custom', params: MaterialParams | CustomMaterialSpec = {}): MaterialHandle {
    const h =
      kind === 'custom'
        ? MaterialManager.customHandle(params as CustomMaterialSpec)
        : MaterialManager.handle(kind === ('basic' as string) ? 'unlit' : kind, params as MaterialParams);
    this.whenReady(() => this.materials.start(h));
    return h;
  }

  setMaterialParam(m: MaterialHandle, name: string, value: ParamValue): void {
    if (this.isReady) this.materials.set(m, name, value);
    else m.params[name] = value;
  }

  private newMesh(slot: number): MeshHandle {
    return { id: 0, slot, entities: [], built: [] };
  }

  /** Builds the mesh once its material instance exists (immediately if it already does). */
  private build(m: MaterialHandle, fn: () => void): void {
    this.whenReady(() => {
      if (m.instance) fn();
      else m.waiters.push(() => this.run(fn));
    });
  }

  private finish(h: MeshHandle, entities: FEntity[]): void {
    h.entities = entities;
    h.id = entities[0].getId();
    this.fscene.addEntities(entities);
    for (const fn of h.built.splice(0)) fn();
  }

  staticMesh(g: GeometryHandle, m: MaterialHandle, matrix: ArrayLike<number>, flags?: MeshFlags): MeshHandle {
    const h = this.newMesh(-1);
    this.build(m, () => {
      const e = buildMesh(this.F, this.engine, this.entities, this.gpu(g), m.instance!, flags, !!this.opts.view?.shadows);
      const tcm = this.engine.getTransformManager();
      tcm.setTransform(tcm.getInstance(e), matrix as Mat4);
      this.finish(h, [e]);
    });
    return h;
  }

  dynamicMesh(
    slot: number,
    g: GeometryHandle,
    m: MaterialHandle,
    parent: ArrayLike<number>,
    initial?: InitialTransform,
    flags?: MeshFlags
  ): MeshHandle {
    const t = this.transforms;
    t.ensure(slot + 1);
    this.nextSlot = Math.max(this.nextSlot, slot + 1);
    t.setParent(slot, parent);
    if (initial) {
      const p = initial.pos;
      const q = initial.quat;
      const s = initial.scale;
      if (p) t.pos(slot, p[0], p[1], p[2]);
      if (s) t.scale(slot, s[0], s[1], s[2]);
      if (q) {
        const o = slot * 10 + 3;
        t.data[o] = q[0];
        t.data[o + 1] = q[1];
        t.data[o + 2] = q[2];
        t.data[o + 3] = q[3];
        t.markDirty(slot);
      }
    }
    const h = this.newMesh(slot);
    this.build(m, () => {
      const e = buildMesh(this.F, this.engine, this.entities, this.gpu(g), m.instance!, flags, !!this.opts.view?.shadows);
      t.instances[slot] = this.engine.getTransformManager().getInstance(e);
      t.markDirty(slot);
      this.finish(h, [e]);
    });
    return h;
  }

  /** Writes a slot's position; accepts (x, y, z) or one [x, y, z] array. */
  pos(slot: number, x: number | readonly number[], y?: number, z?: number): void {
    if (typeof x === 'number') this.transforms.pos(slot, x, y!, z!);
    else this.transforms.pos(slot, x[0], x[1], x[2]);
  }
  /** Writes a slot's Euler XYZ rotation; accepts (x, y, z) or one array. */
  rot(slot: number, x: number | readonly number[], y?: number, z?: number): void {
    if (typeof x === 'number') this.transforms.rot(slot, x, y!, z!);
    else this.transforms.rot(slot, x[0], x[1], x[2]);
  }
  /** Writes a slot's scale; accepts (x, y, z), one [x, y, z] array, or one number for uniform scale. */
  scale(slot: number, x: number | readonly number[], y?: number, z?: number): void {
    if (typeof x !== 'number') this.transforms.scale(slot, x[0], x[1], x[2]);
    else this.transforms.scale(slot, x, y ?? x, z ?? x);
  }

  /** `count` static copies of one mesh; matrices holds count column-major 4x4 matrices. */
  instanced(g: GeometryHandle, m: MaterialHandle, count: number, matrices: Float32Array, flags?: MeshFlags): MeshHandle {
    const h = this.newMesh(-1);
    this.build(m, () => {
      const es = buildInstanced(this.F, this.engine, this.entities, this.gpu(g), m.instance!, count, matrices, flags, !!this.opts.view?.shadows);
      this.finish(h, es);
    });
    return h;
  }

  light(kind: LightKind, props: LightProps = {}): LightHandle {
    const h: LightHandle = { kind, props: { ...props } };
    this.whenReady(() => {
      if (kind === 'ambient') {
        const old = this.ambient;
        h.ibl = this.ambient = buildAmbient(this.F, this.engine, h.props);
        this.refreshIndirect();
        if (old) this.engine.destroyIndirectLight(old);
        return;
      }
      h.entity = buildLight(this.F, this.engine, this.entities, kind, h.props, !!this.opts.view?.shadows);
      this.fscene.addEntity(h.entity);
    });
    return h;
  }

  /** Reactive light update; intensity is in Filament units (see lightIntensity). */
  setLight(h: LightHandle, name: 'intensity' | 'color' | 'position' | 'direction', value: number | readonly number[]): void {
    (h.props as Record<string, unknown>)[name] = value;
    if (h.entity) updateLight(this.engine, h.entity, name, value);
    else if (h.ibl && name === 'intensity') h.ibl.setIntensity(value as number);
  }

  /** Three.js-scale intensity to Filament units. */
  intensity(kind: LightKind, threeIntensity: number): number {
    return lightIntensity(kind, threeIntensity);
  }

  /** Registers a pointer handler for a mesh; events: click, pointerover, pointerout, pointermove, pointerdown, pointerup. */
  on(mesh: MeshHandle, event: PointerEventName, handler: PointerHandler): void {
    const register = (): void => {
      for (const e of mesh.entities) this.picker.on(e.getId(), event, handler);
    };
    this.whenReady(() => (mesh.entities.length ? register() : mesh.built.push(register)));
  }

  // Environment and camera

  /** Replaces the image-based light (and optional skybox); ambient lights yield to it. */
  setEnvironment(ibl?: FIndirectLight, sky?: FSkybox): void {
    this.destroyEnv();
    this.env = { ibl, sky };
    this.fscene.setSkybox(sky ?? null);
    this.refreshIndirect();
  }

  private refreshIndirect(): void {
    this.fscene.setIndirectLight(this.env?.ibl ?? this.ambient ?? null);
  }

  private destroyEnv(): void {
    if (!this.env) return;
    if (this.env.ibl) this.engine.destroyIndirectLight(this.env.ibl);
    if (this.env.sky) this.engine.destroySkybox(this.env.sky);
    this.env = undefined;
  }

  /** Updates the camera; applied on the next frame. */
  setCamera(c: Partial<CameraState>): void {
    Object.assign(this.camState, c);
    this.cameraDirty = true;
  }

  getLoader(): FAssetLoader {
    return (this.loader ??= this.engine.createAssetLoader());
  }

  /** Registers something to destroy on dispose (glTF assets and the like). */
  track(destroy: () => void): void {
    this.assets.push({ destroy });
  }

  /** Per-frame callback, before transforms are flushed. Returns an unsubscribe function. */
  onFrame(cb: FrameCallback): () => void {
    this.frameCallbacks.push(cb);
    return () => {
      const i = this.frameCallbacks.indexOf(cb);
      if (i !== -1) this.frameCallbacks.splice(i, 1);
    };
  }

  // Frame loop

  private loop = (time: number): void => {
    if (this.disposed) return;
    this.raf = requestAnimationFrame(this.loop);
    if (!this.canvas.isConnected) {
      this.dispose();
      return;
    }
    if (this.visible) this.tick(time);
  };

  /** One frame: callbacks, camera, dirty transforms, pick, render. Public for tests. */
  tick(time: number): void {
    if (!this.isReady || this.disposed) return;
    const delta = this.last ? (time - this.last) / 1000 : 0;
    this.last = time;
    const cbs = this.frameCallbacks;
    for (let i = 0; i < cbs.length; i++) cbs[i](delta, time / 1000);
    if (this.cameraDirty) {
      this.cameraDirty = false;
      applyLookAt(this.camera, this.camState);
      applyProjection(this.F, this.camera, this.camState, this.canvas.width, this.canvas.height);
    }
    this.transforms.flush(this.engine.getTransformManager());
    this.picker.update();
    this.renderer.render(this.swapChain, this.view);
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.queue = [];
    this.frameCallbacks = [];
    if (this.raf) cancelAnimationFrame(this.raf);
    this.resizeObserver?.disconnect();
    this.visibility?.disconnect();
    if (!this.isReady) return;
    const engine = this.engine;
    this.picker.dispose();
    for (const a of this.assets.splice(0)) a.destroy();
    this.loader?.delete();
    this.destroyEnv();
    this.fscene.setIndirectLight(null);
    this.fscene.setSkybox(null);
    if (this.ambient) engine.destroyIndirectLight(this.ambient);
    engine.destroyCameraComponent(this.cameraEntity);
    this.entities.destroyAll();
    for (const g of this.geometries.values()) {
      if (!g.gpu) continue;
      engine.destroyVertexBuffer(g.gpu.vb);
      engine.destroyIndexBuffer(g.gpu.ib);
    }
    this.materials.dispose();
    if (this.grading) engine.destroyColorGrading(this.grading);
    engine.destroyView(this.view);
    engine.destroyRenderer(this.renderer);
    engine.destroyScene(this.fscene);
    engine.destroySwapChain(this.swapChain);
    this.F.Engine.destroy(engine);
  }

}
