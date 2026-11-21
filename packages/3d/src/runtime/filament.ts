// The subset of the Filament 1.53 JS API this package uses, as structural
// interfaces. The real `filament` package is an optional peer dependency, so we
// never import its types; a test can pass a fake that satisfies FilamentModule.

export type Mat4 = ArrayLike<number>;
export type Vec3Like = ArrayLike<number>;

export interface FEntity {
  getId(): number;
  delete?(): void;
}
export interface FInstance {
  delete?(): void;
}
export interface FBox {
  center: number[];
  halfExtent: number[];
}
export interface FBuffer {}
export interface FVertexBuffer extends FBuffer {
  setBufferAt(engine: FEngine, index: number, data: ArrayBufferView): void;
}
export interface FIndexBuffer extends FBuffer {
  setBuffer(engine: FEngine, data: ArrayBufferView): void;
}
export interface FTexture extends FBuffer {}
export interface FMaterialInstance {
  setFloatParameter(name: string, v: number): void;
  setFloat3Parameter(name: string, v: ArrayLike<number>): void;
  setFloat4Parameter(name: string, v: ArrayLike<number>): void;
  setBoolParameter(name: string, v: boolean): void;
  setTextureParameter(name: string, t: FTexture, sampler: unknown): void;
}
export interface FMaterial {
  createInstance(): FMaterialInstance;
}
export interface FRenderableBuilder {
  geometry(slot: number, type: number, vb: FVertexBuffer, ib: FIndexBuffer): FRenderableBuilder;
  material(slot: number, mi: FMaterialInstance): FRenderableBuilder;
  boundingBox(box: FBox): FRenderableBuilder;
  castShadows(on: boolean): FRenderableBuilder;
  receiveShadows(on: boolean): FRenderableBuilder;
  build(engine: FEngine, entity: FEntity): void;
}
export interface FLightBuilder {
  color(rgb: number[]): FLightBuilder;
  direction(v: number[]): FLightBuilder;
  position(v: number[]): FLightBuilder;
  intensity(v: number): FLightBuilder;
  falloff(v: number): FLightBuilder;
  spotLightCone(inner: number, outer: number): FLightBuilder;
  castShadows(on: boolean): FLightBuilder;
  shadowOptions(o: Record<string, unknown>): FLightBuilder;
  build(engine: FEngine, entity: FEntity): void;
}
export interface FLightManager {
  getInstance(e: FEntity): FInstance;
  setIntensity(i: FInstance, v: number): void;
  setColor(i: FInstance, v: number[]): void;
  setPosition(i: FInstance, v: number[]): void;
  setDirection(i: FInstance, v: number[]): void;
}
export interface FRenderableManager {
  destroy(e: FEntity): void;
}
export interface FTransformManager {
  create(e: FEntity): void;
  destroy(e: FEntity): void;
  getInstance(e: FEntity): FInstance;
  setTransform(i: FInstance, m: Mat4): void;
  openLocalTransformTransaction(): void;
  commitLocalTransformTransaction(): void;
}
export interface FCamera {
  setProjectionFov(fov: number, aspect: number, near: number, far: number, dir: number): void;
  lookAt(eye: number[], center: number[], up: number[]): void;
  setExposure(aperture: number, shutter: number, iso: number): void;
}
export interface FScene {
  addEntity(e: FEntity): void;
  addEntities(e: FEntity[]): void;
  remove(e: FEntity): void;
  removeEntities(e: FEntity[]): void;
  setIndirectLight(ibl: FIndirectLight | null): void;
  setSkybox(sky: FSkybox | null): void;
}
export interface FIndirectLight {
  setIntensity(v: number): void;
}
export interface FIndirectLightBuilder {
  irradianceSh(bands: number, data: number[]): FIndirectLightBuilder;
  intensity(v: number): FIndirectLightBuilder;
  build(engine: FEngine): FIndirectLight;
}
export interface FSkybox {}
export interface FColorGrading {}
export interface FColorGradingBuilder {
  toneMapping(t: number): FColorGradingBuilder;
  build(engine: FEngine): FColorGrading;
}
export interface FPickResult {
  /** An Entity in the real binding (its typings say number); 0 or null when nothing was hit. */
  renderable: number | FEntity | null;
  depth: number;
  fragCoords: number[];
}
export interface FView {
  setScene(s: FScene): void;
  setCamera(c: FCamera): void;
  setViewport(v: number[]): void;
  setColorGrading(c: FColorGrading): void;
  setBloomOptions(o: Record<string, unknown>): void;
  setAmbientOcclusionOptions(o: Record<string, unknown>): void;
  setAntiAliasing(a: number): void;
  setTemporalAntiAliasingOptions(o: Record<string, unknown>): void;
  setMultiSampleAntiAliasingOptions(o: Record<string, unknown>): void;
  setBlendMode(m: number): void;
  pick(x: number, y: number, cb: (r: FPickResult) => void): void;
}
export interface FRenderer {
  render(swapChain: FSwapChain, view: FView): void;
  setClearOptions(o: { clearColor?: number[]; clear?: boolean; discard?: boolean }): void;
}
export interface FSwapChain {}
export interface FAsset {
  loadResources(
    onDone: (() => void) | null,
    onFetched: ((name: string) => void) | null,
    basePath: string | null,
    asyncInterval?: number | null,
    options?: object
  ): void;
  getEntities(): FEntity[];
  getRenderableEntities(): FEntity[];
  getRoot(): FEntity;
}
export interface FAssetLoader {
  createAsset(data: ArrayBufferView): FAsset | null;
  destroyAsset(a: FAsset): void;
  delete(): void;
}
export interface FSurfaceOrientation {
  getQuats(count: number): ArrayBufferView;
  delete(): void;
}
export interface FSurfaceOrientationBuilder {
  vertexCount(n: number): FSurfaceOrientationBuilder;
  normals(a: Float32Array, stride: number): FSurfaceOrientationBuilder;
  uvs(a: Float32Array, stride: number): FSurfaceOrientationBuilder;
  positions(a: Float32Array, stride: number): FSurfaceOrientationBuilder;
  triangleCount(n: number): FSurfaceOrientationBuilder;
  triangles16(a: Uint16Array): FSurfaceOrientationBuilder;
  triangles32(a: Uint32Array): FSurfaceOrientationBuilder;
  build(): FSurfaceOrientation;
}

export interface FEngine {
  createCamera(e: FEntity): FCamera;
  createMaterial(data: ArrayBufferView): FMaterial;
  createRenderer(): FRenderer;
  createScene(): FScene;
  createSwapChain(): FSwapChain;
  createView(): FView;
  createAssetLoader(): FAssetLoader;
  createTextureFromPng(data: ArrayBufferView, options?: object): FTexture;
  createTextureFromJpeg(data: ArrayBufferView, options?: object): FTexture;
  createTextureFromKtx2(data: ArrayBufferView, options?: object): FTexture;
  createIblFromKtx1(data: ArrayBufferView): FIndirectLight;
  createSkyFromKtx1(data: ArrayBufferView): FSkybox;
  getLightManager(): FLightManager;
  getRenderableManager(): FRenderableManager;
  getTransformManager(): FTransformManager;
  destroySwapChain(x: FSwapChain): void;
  destroyRenderer(x: FRenderer): void;
  destroyView(x: FView): void;
  destroyScene(x: FScene): void;
  destroyCameraComponent(e: FEntity): void;
  destroyMaterial(x: FMaterial): void;
  destroyEntity(e: FEntity): void;
  destroyIndexBuffer(x: FIndexBuffer): void;
  destroyVertexBuffer(x: FVertexBuffer): void;
  destroyIndirectLight(x: FIndirectLight): void;
  destroyMaterialInstance(x: FMaterialInstance): void;
  destroySkybox(x: FSkybox): void;
  destroyTexture(x: FTexture): void;
  destroyColorGrading(x: FColorGrading): void;
}

/** Enum members used here; values come from the loaded module, never hard-coded. */
export type FEnum = Record<string, number>;

export interface FilamentModule {
  init(assets: string[], onready?: (() => void) | null): void;
  Engine: { create(canvas: HTMLCanvasElement, options?: object): FEngine; destroy(e: FEngine): void };
  EntityManager: { get(): { create(): FEntity } };
  RenderableManager: { Builder(count: number): FRenderableBuilder };
  LightManager: { Builder(type: number): FLightBuilder };
  IndirectLight: { Builder(): FIndirectLightBuilder };
  ColorGrading: { Builder(): FColorGradingBuilder };
  SurfaceOrientation$Builder: new () => FSurfaceOrientationBuilder;
  VertexBuffer: { Builder(): FVertexBufferBuilder };
  IndexBuffer: { Builder(): FIndexBufferBuilder };
  Texture: { Builder(): FTextureBuilder };
  TextureSampler: new (min: number, mag: number, wrap: number) => object;
  PixelBuffer(data: ArrayBufferView, format: number, type: number): unknown;
  LightManager$Type: FEnum;
  RenderableManager$PrimitiveType: FEnum;
  VertexAttribute: FEnum;
  VertexBuffer$AttributeType: FEnum;
  IndexBuffer$IndexType: FEnum;
  Camera$Fov: FEnum;
  ColorGrading$ToneMapping: FEnum & { AGX?: number };
  View$AntiAliasing: FEnum;
  View$BlendMode: FEnum;
  View$BloomOptions$BlendMode?: FEnum;
  View$QualityLevel?: FEnum;
  Texture$Sampler: FEnum;
  Texture$InternalFormat: FEnum;
  PixelDataFormat: FEnum;
  PixelDataType: FEnum;
  MinFilter: FEnum;
  MagFilter: FEnum;
  WrapMode: FEnum;
}
export interface FVertexBufferBuilder {
  vertexCount(n: number): FVertexBufferBuilder;
  bufferCount(n: number): FVertexBufferBuilder;
  attribute(a: number, buf: number, type: number, offset: number, stride: number): FVertexBufferBuilder;
  normalized(a: number): FVertexBufferBuilder;
  build(engine: FEngine): FVertexBuffer;
}
export interface FIndexBufferBuilder {
  indexCount(n: number): FIndexBufferBuilder;
  bufferType(t: number): FIndexBufferBuilder;
  build(engine: FEngine): FIndexBuffer;
}
export interface FTextureBuilder {
  width(n: number): FTextureBuilder;
  height(n: number): FTextureBuilder;
  levels(n: number): FTextureBuilder;
  sampler(s: number): FTextureBuilder;
  format(f: number): FTextureBuilder;
  build(engine: FEngine): FTexture;
}

export type FilamentSource = FilamentModule | (() => FilamentModule | Promise<FilamentModule>);

type FilamentGlobal = { Filament?: FilamentModule };

function ready(mod: FilamentModule): Promise<FilamentModule> {
  return new Promise((resolve) => {
    mod.init([], () => {
      // filament.js replaces its own global with the instantiated module, so
      // the object we were given may be the pre-init stub.
      const live = (mod as Partial<FilamentModule>).Engine ? mod : ((globalThis as FilamentGlobal).Filament ?? mod);
      resolve(live);
    });
  });
}

const scripts = new Map<string, Promise<FilamentModule>>();

/**
 * Loads filament.js as a classic script. It defines the global `Filament` and
 * finds filament.wasm next to the script (it derives the directory from
 * document.currentScript), so the build copies both into one directory and
 * the page never bundles the engine.
 */
function loadScript(url: string): Promise<FilamentModule> {
  let p = scripts.get(url);
  if (!p) {
    p = new Promise<FilamentModule>((resolve, reject) => {
      const existing = (globalThis as FilamentGlobal).Filament;
      if (existing && typeof existing.init === 'function') {
        resolve(existing);
        return;
      }
      const el = document.createElement('script');
      el.src = url;
      el.async = true;
      el.onload = () => {
        const mod = (globalThis as FilamentGlobal).Filament;
        if (mod) resolve(mod);
        else reject(new Error(`@krate/3d: ${url} did not define the Filament global`));
      };
      el.onerror = () => reject(new Error(`@krate/3d: could not load Filament from ${url}`));
      document.head.appendChild(el);
    });
    p.catch(() => scripts.delete(url));
    scripts.set(url, p);
  }
  return p;
}

/**
 * Resolves the Filament module: an injected one (tests, custom wasm location)
 * or the script the build copied to `url`. The engine is never imported, so no
 * bundler has to resolve filament.js and its Node-only branches.
 */
export async function loadFilament(source?: FilamentSource, url?: string): Promise<FilamentModule> {
  const mod = typeof source === 'function' ? await source() : source;
  if (mod) return ready(mod);
  if (!url) {
    throw new Error(
      '@krate/3d: the Filament engine is not available. Run `npm install filament` so the build can copy filament.js and filament.wasm into the site, or pass a `filament` prop.'
    );
  }
  return ready(await loadScript(url));
}
