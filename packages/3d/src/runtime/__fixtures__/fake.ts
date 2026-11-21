// A recording fake of the Filament module for tests. Every object it creates is
// tracked, so a test can assert that dispose destroyed all of them.

import type { FilamentModule } from '../filament.js';

type Rec = Record<string, unknown[][]>;
interface Obj {
  kind: string;
  [k: string]: unknown;
}

const enumOf = (names: string[]): Record<string, number> => Object.fromEntries(names.map((n, i) => [n, i]));

function builder(finish: (rec: Rec, ...args: unknown[]) => unknown): unknown {
  const rec: Rec = {};
  const p: unknown = new Proxy(
    {},
    {
      get: (_t, prop: string) =>
        prop === 'build'
          ? (...a: unknown[]) => finish(rec, ...a)
          : (...args: unknown[]) => {
              (rec[prop] ??= []).push(args);
              return p;
            },
    }
  );
  return p;
}

export function createFake() {
  const alive = new Set<Obj>();
  const log: string[] = [];
  const state = {
    renderables: [] as Array<{ entity: Obj; rec: Rec }>,
    lights: [] as Array<{ entity: Obj; type: number; rec: Rec }>,
    transforms: [] as Array<{ entity: Obj; m: number[] }>,
    picks: [] as Array<{ x: number; y: number; cb: (r: { renderable: number; depth: number; fragCoords: number[] }) => void }>,
    renders: 0,
    view: {} as Record<string, unknown[]>,
    camera: {} as Record<string, unknown[]>,
    materialBytes: [] as unknown[],
    instances: [] as Array<{ params: Record<string, unknown>; kind: string }>,
    destroyedKinds: [] as string[],
  };
  let nextId = 1;
  let engineObj: Obj;
  const make = (kind: string, extra: Record<string, unknown> = {}): Obj => {
    const o: Obj = { kind, ...extra };
    alive.add(o);
    return o;
  };
  const destroy = (o: Obj): void => {
    if (!alive.delete(o)) throw new Error(`destroying unknown or already destroyed ${o?.kind}`);
    state.destroyedKinds.push(o.kind);
  };
  const record = (store: Record<string, unknown[]>, name: string) => (...a: unknown[]) => {
    store[name] = a;
  };

  const tcm = {
    create: (e: Obj) => void (e.hasTransform = true),
    destroy: (e: Obj) => void (e.hasTransform = false),
    getInstance: (e: Obj) => ({ entity: e }),
    setTransform: (i: { entity: Obj }, m: ArrayLike<number>) => state.transforms.push({ entity: i.entity, m: Array.from(m) }),
    openLocalTransformTransaction: () => log.push('open'),
    commitLocalTransformTransaction: () => log.push('commit'),
  };
  const rm = { destroy: (e: Obj) => void (e.hasRenderable = false) };
  const lm = {
    getInstance: (e: Obj) => ({ entity: e }),
    setIntensity: (i: { entity: Obj }, v: number) => void (i.entity.intensity = v),
    setColor: (i: { entity: Obj }, v: number[]) => void (i.entity.color = v),
    setPosition: (i: { entity: Obj }, v: number[]) => void (i.entity.position = v),
    setDirection: (i: { entity: Obj }, v: number[]) => void (i.entity.direction = v),
  };

  const engine = {
    createCamera: (e: Obj) => {
      const cam = make('camera', { setProjectionFov: record(state.camera, 'fov'), lookAt: record(state.camera, 'lookAt'), setExposure: record(state.camera, 'exposure') });
      e.camera = cam;
      return cam;
    },
    createMaterial: (bytes: unknown) => {
      state.materialBytes.push(bytes);
      const mat = make('material');
      mat.createInstance = () => {
        const params: Record<string, unknown> = {};
        const mi = make('materialInstance', {
          setFloatParameter: (n: string, v: number) => (params[n] = v),
          setFloat3Parameter: (n: string, v: number[]) => (params[n] = Array.from(v)),
          setFloat4Parameter: (n: string, v: number[]) => (params[n] = Array.from(v)),
          setBoolParameter: (n: string, v: boolean) => (params[n] = v),
          setTextureParameter: (n: string, t: unknown) => (params[n] = t),
        });
        state.instances.push({ params, kind: new TextDecoder().decode(bytes as Uint8Array) });
        return mi;
      };
      return mat;
    },
    createRenderer: () => make('renderer', { render: () => void state.renders++, setClearOptions: () => {} }),
    createScene: () =>
      make('scene', {
        entities: new Set<Obj>(),
        addEntity(e: Obj) {
          (this.entities as Set<Obj>).add(e);
        },
        addEntities(es: Obj[]) {
          es.forEach((e) => (this.entities as Set<Obj>).add(e));
        },
        remove() {},
        removeEntities() {},
        setIndirectLight(l: Obj | null) {
          this.ibl = l;
        },
        setSkybox() {},
      }),
    createSwapChain: () => make('swapChain'),
    createView: () =>
      make('view', {
        setScene: () => {},
        setCamera: () => {},
        setViewport: record(state.view, 'viewport'),
        setColorGrading: record(state.view, 'colorGrading'),
        setBloomOptions: record(state.view, 'bloom'),
        setAmbientOcclusionOptions: record(state.view, 'ao'),
        setAntiAliasing: record(state.view, 'aa'),
        setTemporalAntiAliasingOptions: record(state.view, 'taa'),
        setMultiSampleAntiAliasingOptions: record(state.view, 'msaa'),
        setBlendMode: record(state.view, 'blend'),
        pick: (x: number, y: number, cb: never) => state.picks.push({ x, y, cb }),
      }),
    createAssetLoader: () => {
      const loader: Obj = make('assetLoader', {
        createAsset: () => make('asset', { loadResources: (done: () => void) => done(), getEntities: () => [] }),
        destroyAsset: destroy,
        delete: () => destroy(loader),
      });
      return loader;
    },
    createTextureFromPng: () => make('texture'),
    createTextureFromJpeg: () => make('texture'),
    createTextureFromKtx2: () => make('texture'),
    createIblFromKtx1: (_data?: unknown) => make('ibl', { setIntensity() {} }),
    createSkyFromKtx1: (_data?: unknown) => make('skybox'),
    getLightManager: () => lm,
    getRenderableManager: () => rm,
    getTransformManager: () => tcm,
    destroySwapChain: destroy,
    destroyRenderer: destroy,
    destroyView: destroy,
    destroyScene: destroy,
    destroyCameraComponent: (e: Obj) => destroy(e.camera as Obj),
    destroyMaterial: destroy,
    destroyEntity: destroy,
    destroyIndexBuffer: destroy,
    destroyVertexBuffer: destroy,
    destroyIndirectLight: destroy,
    destroyMaterialInstance: destroy,
    destroySkybox: destroy,
    destroyTexture: destroy,
    destroyColorGrading: destroy,
  };

  const module = {
    init: (_a: string[], cb?: () => void) => cb?.(),
    Engine: {
      create: () => {
        engineObj = make('engine');
        return engine;
      },
      destroy: () => {
        log.push('engineDestroy');
        destroy(engineObj);
      },
    },
    EntityManager: {
      get: () => ({
        create: () => {
          const id = ++nextId;
          return make('entity', { getId: () => id });
        },
      }),
    },
    RenderableManager: {
      Builder: () =>
        builder((rec, _engine, entity) => {
          (entity as Obj).hasRenderable = true;
          state.renderables.push({ entity: entity as Obj, rec });
        }),
    },
    LightManager: {
      Builder: (type: number) =>
        builder((rec, _engine, entity) => {
          state.lights.push({ entity: entity as Obj, type, rec });
        }),
    },
    IndirectLight: { Builder: () => builder((rec) => make('ibl', { rec, setIntensity() {} })) },
    ColorGrading: { Builder: () => builder((rec) => make('colorGrading', { rec })) },
    SurfaceOrientation$Builder: class {
      private n = 0;
      vertexCount(n: number) {
        this.n = n;
        return this;
      }
      normals() {
        return this;
      }
      uvs() {
        return this;
      }
      positions() {
        return this;
      }
      triangleCount() {
        return this;
      }
      triangles16() {
        return this;
      }
      triangles32() {
        return this;
      }
      build() {
        return { getQuats: (c: number) => new Int16Array(c * 4), delete() {}, n: this.n };
      }
    },
    VertexBuffer: { Builder: () => builder(() => make('vertexBuffer', { setBufferAt() {} })) },
    IndexBuffer: { Builder: () => builder((rec) => make('indexBuffer', { rec, setBuffer() {} })) },
    Texture: { Builder: () => builder(() => make('texture', { setImage() {} })) },
    TextureSampler: class {},
    PixelBuffer: () => ({}),
    LightManager$Type: enumOf(['SUN', 'DIRECTIONAL', 'POINT', 'FOCUSED_SPOT', 'SPOT']),
    RenderableManager$PrimitiveType: enumOf(['POINTS', 'LINES', 'LINE_STRIP', 'TRIANGLES']),
    VertexAttribute: { POSITION: 0, TANGENTS: 1, COLOR: 2, UV0: 3 },
    VertexBuffer$AttributeType: enumOf(['BYTE', 'FLOAT2', 'FLOAT3', 'SHORT4']),
    IndexBuffer$IndexType: enumOf(['USHORT', 'UINT']),
    Camera$Fov: enumOf(['VERTICAL', 'HORIZONTAL']),
    ColorGrading$ToneMapping: enumOf(['LINEAR', 'ACES_LEGACY', 'ACES', 'FILMIC']),
    View$AntiAliasing: enumOf(['NONE', 'FXAA']),
    View$BlendMode: enumOf(['OPAQUE', 'TRANSLUCENT']),
    View$BloomOptions$BlendMode: enumOf(['ADD', 'INTERPOLATE']),
    View$QualityLevel: enumOf(['LOW', 'MEDIUM', 'HIGH', 'ULTRA']),
    Texture$Sampler: enumOf(['SAMPLER_2D']),
    Texture$InternalFormat: enumOf(['RGBA8']),
    PixelDataFormat: enumOf(['RGBA']),
    PixelDataType: enumOf(['UBYTE']),
    MinFilter: enumOf(['NEAREST', 'LINEAR', 'LINEAR_MIPMAP_LINEAR']),
    MagFilter: enumOf(['NEAREST', 'LINEAR']),
    WrapMode: enumOf(['CLAMP_TO_EDGE', 'REPEAT']),
  };

  return { module: module as unknown as FilamentModule, alive, log, state, engine };
}

/** Fake byte fetch: returns the URL as the "file contents" so tests can see which file loaded. */
export const fakeFetch = (url: string): Promise<Uint8Array> => Promise.resolve(new TextEncoder().encode(url));
