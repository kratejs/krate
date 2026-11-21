# @krate/3d design

`@krate/3d` renders 3D scenes with Google Filament (WebGL2 via the `filament`
npm package's WASM build) from Three.js-style JSX. The Krate compiler knows the
whole scene tree at build time, so it does the work a runtime reconciler such
as react-three-fiber does on every page load: it flattens static transforms,
batches identical meshes into instances, and turns reactive props into direct
typed-array writes.

## What lives where

| Piece | Location | Why |
| --- | --- | --- |
| JSX scene compiler pass, light-unit calibration, static flattening, auto-instancing | Go: `packages/compiler/internal/scene3d` | Small code that needs the AST and whole-program knowledge |
| `matc` download and cache, shader hashing | Go: `packages/compiler/internal/scene3d/matc` | Small code; the `matc` binary itself is downloaded on demand, never embedded |
| Pre-compiled uber-materials (`*.filamat`) | npm: `@krate/3d/materials/`, copied to `dist/assets/3d/materials-<version>/` by the build | Binary assets; kept out of the `krate` binary |
| Material sources (`*.mat`) + rebuild script | npm: `@krate/3d/materials/src`, `materials/build.mjs` | Inputs to `matc` |
| Runtime bridge (Canvas, managers, transform sync, picking, glTF) | npm: `@krate/3d/runtime` | Ships to the browser |
| JSX intrinsic element types | npm: `@krate/3d/types` | Editor and `tsc` support |
| Filament WASM (`filament.js`, `filament.wasm`) | peer dependency `filament`; copied to `dist/assets/3d/filament-<version>/` by the build, loaded by a runtime `<script>` | Large; installed by the app, never bundled and never imported |

Nothing large is embedded in the `krate` binary: no `.filamat`, no `matc`, no
WASM.

## Engine and asset delivery

The engine is never bundled. `filament.js` has Node branches that
`require("fs")`, `"path"` and `"crypto"`, which a browser bundle cannot resolve,
so no module imports `filament`. When a build contains an `@krate/3d` scene
(compiled, or a `<Canvas>` left on the runtime path), the bundler's
`scene3dAssets` (called from the scene pass) resolves the app's own `filament`
package from the project root and registers, as ordinary site assets:

- `filament.js` and `filament.wasm` at
  `/assets/3d/filament-<filament package version>/` (the wasm must sit next to
  the script: filament.js derives its directory from `document.currentScript`);
- the six packaged `.filamat` files at `/assets/3d/materials-<Filament version>/`
  (Krate's bundler does not rewrite the `new URL("../../materials/x.filamat",
  import.meta.url)` fallback in `materials.ts`, so the Go side copies them), and
  custom shader `.filamat` files land in the same directory.

Both URLs carry the URL base path. The compiler passes them to the scene as the
`filamentUrl` and `materialBase` scene options (compiled scenes: in the
`__k3d.scene` options object; runtime-path Canvases: as `filamentUrl` and
`materialBase` props the pass adds to the element). `loadFilament` injects a
classic `<script src=filamentUrl>`, waits for the `Filament` global, then
calls `Filament.init([], cb)`; concurrent scenes share one load. A
`filament` / `materialBase` option or prop the author sets wins. With no
`filament` source and no `filamentUrl` the scene reports an error telling the
author to `npm install filament`.

If the `filament` package cannot be found from the project root, or its version
differs from `materials/VERSION` (the `.filamat` files are locked to it), the
build prints the warning `KR8702` (`diag.CodeScene3DAssets`) with the hint
`install filament` and emits the scene without URLs; it does not fail the build.

## Material tiers

- **Tier 1 (default):** `<meshStandardMaterial>`, `<meshPhysicalMaterial>` and
  `<meshBasicMaterial>` map to `standard.filamat`, `physical.filamat` and
  `unlit.filamat`. Static props are set once when the instance is created;
  reactive props become effects that call `MaterialInstance.setXxxParameter`.
  Each also has a `<name>_transparent.filamat` build (blending `transparent`,
  shader premultiplies by `opacity`) that the runtime picks when `transparent`
  is set. Both builds come from the same `.mat` source via `materials/build.mjs`
  (`-Pblending=transparent -DKRATE_TRANSPARENT=1`). The `stereo`, `vsm`, `fog`
  and `ssr` variants are filtered out to keep the files small (about 385 KB
  total); the runtime must not enable those features.
- **Tier 2 (custom shaders only):** `<shaderMaterial>` with inline shader code.
  The compiler hashes the generated `.mat` source (FNV-1a 64, hex), and if
  `dist/assets/3d/materials-<version>/<hash>.filamat` (or the build cache copy) is missing, runs
  `matc` from `~/.krate/tools/filament/<version>/bin/matc`, downloading the
  Filament release for the host platform on first use. `KRATE_FILAMENT_MATC`
  overrides the binary path; `KRATE_OFFLINE=1` makes a missing `matc` a
  diagnostic with a hint instead of a download. The compiled file is cached in
  `<project>/.krate/cache/3d/<Filament version>/` (a `.filamat` is only valid for the release that compiled it, so the version is part of the cache directory and of the asset URL), copied to `dist/assets/3d/materials-<version>/<hash>.filamat`, and
  loaded by the runtime as `scene.material("custom", { url, params })`.

  `<shaderMaterial>` props (all strings must be compile-time constants: a
  literal, a template literal without expressions, or a module-level `const`):

  | Prop | Meaning |
  | --- | --- |
  | `fragmentShader` | required; the statements of `void material(inout MaterialInputs material)` after `prepareMaterial(material);` |
  | `vertexShader` | optional; the statements of `void materialVertex(inout MaterialVertexInputs material)` |
  | `uniforms` | optional object: a constant (number: `float`, `[x,y,z]`: `float3`, `[x,y,z,w]`: `float4`) or `{ type: "float" \| "float3" \| "float4", value: expr }` with a possibly reactive `value`; read as `materialParams.<name>` |
  | `shadingModel` | `"unlit"` (default) or `"lit"` |
  | `transparent` | `blending : transparent`; unlit shaders get `material.baseColor.rgb *= material.baseColor.a` appended |

  The generated source is `material { name : krate_custom, shadingModel, blending,
  requires : [ uv0 ], parameters : [...] }` plus optional `vertex { }` and the
  `fragment { }` block. Failures (no `matc`, GLSL errors) are build errors
  positioned at the `<shaderMaterial>`. The runtime fallback path does not
  support `<shaderMaterial>`.

## Uber-material parameters

Shared names used by the `.mat` sources, the runtime and the compiler:

| Parameter | Type | Default | Notes |
| --- | --- | --- | --- |
| `baseColor` | float4 (linear RGBA) | 1,1,1,1 | sRGB hex/CSS colors converted at compile time; the Go and TS name tables (all 148 CSS names) are tested against each other |
| `roughness` | float | 1 | standard/physical |
| `metallic` | float | 0 | standard/physical |
| `emissive` | float3 | 0,0,0 | multiplied by `emissiveIntensity` |
| `emissiveIntensity` | float | 1 | |
| `opacity` | float | 1 | blending `transparent` variant reads it |
| `baseColorMap`, `normalMap`, `aoMap`, `roughnessMap`, `metallicMap`, `emissiveMap` | sampler2d | 1x1 white/flat placeholder | |
| `useBaseColorMap`, `useNormalMap`, `useAoMap`, ... | float (0/1) | 0 | uber-material switches; avoids variant explosion |
| `normalScale` | float | 1 | |
| `aoMapIntensity` | float | 1 | |
| `clearcoat`, `clearcoatRoughness`, `sheenColor`, `transmission`, `ior` | various | Three.js defaults | physical only |

## Transform buffer contract (compiler output -> runtime)

The compiler emits calls into `@krate/3d/runtime/compiled`. The runtime owns a
flat `Float32Array` with a fixed stride per dynamic entity and a `Uint32Array`
dirty bitmask:

```
STRIDE = 10
slot layout: [px, py, pz, qx, qy, qz, qw, sx, sy, sz]
```

Euler rotations are converted to quaternions in the emitted write (the
compiler emits `scene.rot(slot, x, y, z)` which writes slots 3..6), so the
per-frame loop only composes `T * R * S` and multiplies by the slot's static
parent matrix. Once per `requestAnimationFrame` the runtime walks the set bits,
composes each matrix and calls `TransformManager.setTransform` inside one
`openLocalTransformTransaction` / `commitLocalTransformTransaction`, then
clears the mask.

Emitted code shape for

```tsx
const [rotY, setRotY] = createSignal(0);
<Canvas shadows bloom>
  <group position={[0, 1, 0]}>
    <mesh position={[2, 0, 0]}><boxGeometry /><meshStandardMaterial color="orange" /></mesh>
    <mesh rotation={[0, rotY(), 0]} onClick={() => setRotY(rotY() + 0.5)}>
      <sphereGeometry args={[1, 32, 16]} />
      <meshStandardMaterial color="#4488ff" roughness={0.4} />
    </mesh>
  </group>
  <directionalLight intensity={1.5} position={[5, 5, 5]} castShadow />
</Canvas>
```

is roughly (the real output prefixes every generated name with `__`, wraps the
body in the `setup` prop of `<K3dCanvas>` from `@krate/3d/runtime/compiled-canvas`, and
uses `__k3d.effect` for `createEffect`; values are printed in compact form)

```js
import { __k3d } from "@krate/3d/runtime/compiled";
const scene = __k3d.scene(canvasEl, { slots: 1, view: { shadows: true, bloom: true, antialias: "fxaa", toneMapping: "aces" }, camera: { aperture: 16, shutter: 1 / 125, iso: 100 } });
const g0 = scene.geometry("box", [1, 1, 1]);
const g1 = scene.geometry("sphere", [1, 32, 16]);
const m0 = scene.material("standard", { baseColor: [1, 0.3763, 0, 1] });
const m1 = scene.material("standard", { baseColor: [0.0578, 0.2462, 1, 1], roughness: 0.4 });
// static: world matrix folded at compile time (group * mesh)
scene.staticMesh(g0, m0, [1,0,0,0, 0,1,0,0, 0,0,1,0, 2,1,0,1]);
// dynamic: slot 0, static parent matrix from the group
const e1 = scene.dynamicMesh(0, g1, m1, [1,0,0,0, 0,1,0,0, 0,0,1,0, 0,1,0,1], { pos: [0,0,0], quat: [0,0,0,1], scale: [1,1,1] }, { castShadow: true });
__k3d.effect(() => { scene.rot(0, 0, rotY(), 0); });
scene.on(e1, "click", () => setRotY(rotY() + 0.5));  // event names are lower-case, without "on"
scene.light("directional", { intensity: 165000, castShadows: true, position: [5,5,5], direction: [-0.577,-0.577,-0.577] });
```

(`scene.rot` writes the quaternion into the buffer and sets the dirty bit;
`scene.pos(slot, x, y, z)` and `scene.scale(slot, x, y, z)` likewise.)

Repeated identical (geometry, material) pairs with static transforms, either
written out or produced by `{CONST_ARRAY.map((item, i) => <mesh/>)}` (or a
`<For each={CONST_ARRAY}>` when a Krate package exports one; Krate itself has
no `For` component) over a module-level constant array whose item props fold to
constants, become one `scene.instanced(g, m, count, matrices)` call. Meshes with
event handlers are never merged. This is **not GPU instancing**: Filament
1.53.4's JS bindings expose `RenderableManager.Builder.instances(count)` (one
argument) but no `InstanceBuffer` (`Filament.InstanceBuffer` and
`InstanceBuffer$Builder` are undefined) and no per-instance transform call, so
`instances(n)` would draw n copies at one transform and the packaged materials
could not tell them apart (checked in Chromium and in node against the real
wasm). Instead the runtime creates `count` renderables that share one
vertex/index buffer pair and one material instance (geometry and material are
uploaded once; each copy is its own entity and draw call, with its own
transform set in one transform transaction). Real instancing would need an
InstanceBuffer binding in Filament's JS API, or a custom material that reads
per-instance matrices from a texture by `getInstanceIndex()`; neither is done. A
list over a signal is not known at build time and sends the Canvas to the
fallback (the runtime has no dynamic-list path in compiled code).

## Runtime API (`@krate/3d/runtime/compiled`)

Emitted code imports `__k3d` and calls it exactly as below. Handles are opaque
values returned by the scene and passed back to it.

```ts
__k3d.scene(canvas: HTMLCanvasElement, opts?: SceneOptions): Scene3D
// SceneOptions: { slots?: number; view?: ViewOptions; camera?: SceneCameraOptions;
//   filamentUrl?: string; materialBase?: string; filament?: FilamentSource; pixelRatio?: number;
//   fetchBytes?; onError? }
// ViewOptions: { shadows?, bloom?, ssao?, antialias?, toneMapping? }   (Canvas props)
// SceneCameraOptions: { position?, target?, fov?, near?, far?, aperture?, shutter?, iso?, exposure? }
__k3d.intensity(kind: "directional" | "point" | "spot" | "ambient", threeIntensity: number): number
__k3d.color(css: string | number): [r, g, b]            // sRGB -> linear
__k3d.effect(fn: () => void): () => void                // the page's createEffect
__k3d.aimLight(scene, h: LightHandle, kind: "directional" | "point" | "spot",
               position: Vec3, target?: Vec3 | null, parent?: ArrayLike<number> | null): void
                                                        // reactive light position/target: sets position and direction

scene.geometry(kind: "box" | "sphere" | "plane", args: number[]): GeometryHandle
scene.material(kind: "standard" | "physical" | "unlit", params: MaterialParams): MaterialHandle
scene.material(kind: "custom", spec: { url: string; params?: Record<string, number | number[]>; transparent?: boolean }): MaterialHandle
scene.setMaterialParam(m: MaterialHandle, name: string, value: number | boolean | string | number[]): void
scene.staticMesh(g, m, matrix16: ArrayLike<number>, flags?: MeshFlags): MeshHandle
scene.dynamicMesh(slot: number, g, m, parentMatrix16: ArrayLike<number>,
                  initial?: { pos?: Vec3; quat?: [x, y, z, w]; scale?: Vec3 }, flags?: MeshFlags): MeshHandle
scene.pos(slot, x, y, z): void                          // or scene.pos(slot, [x, y, z])
scene.rot(slot, x, y, z): void                          // Euler XYZ radians -> quaternion; array form too
scene.scale(slot, x, y, z): void                        // array form, or one number for uniform scale
scene.instanced(g, m, count: number, matrices: Float32Array, flags?: MeshFlags): MeshHandle
scene.light(kind: "directional" | "point" | "spot" | "ambient", props: LightProps): LightHandle
scene.setLight(h: LightHandle, name: "intensity" | "color" | "position" | "direction", value): void
scene.on(mesh: MeshHandle, event: "click" | "pointerover" | "pointerout" | "pointermove"
         | "pointerdown" | "pointerup", handler: (e: Pointer3DEvent) => void): void
scene.onFrame(cb: (deltaSeconds, timeSeconds) => void): () => void
scene.dispose(): void
```

- `MeshFlags = { castShadow?: boolean; receiveShadow?: boolean }`. Both default to
  false (like Three.js) and only take effect when the Canvas has `shadows`.
- `LightProps` is in Filament units (the compiler applies the calibration
  below): `{ intensity, color?: [r, g, b] linear, position?, direction?,
  castShadows?, falloff?, inner?, outer?, sun? }`. `direction` is the way the
  light travels (a directional light at `position` aimed at the origin has
  direction `-normalize(position)`); `inner` and `outer` are spot cone angles in
  radians; `falloff` is the point/spot radius; `sun: true` selects Filament's
  SUN type instead of DIRECTIONAL. An ambient light has only `intensity` and
  `color` and is ignored while an `<Environment>` is active.
- `MaterialParams` keys are the uber-material parameter names above, already
  linear (`baseColor: [r, g, b, a]`, `emissive: [r, g, b]`); a CSS color string
  is also accepted and converted. A string value for a `*Map` parameter is a
  texture URL (png, jpg or ktx2): it is fetched, bound, and its `useXxxMap`
  switch set to 1. Parameters the material kind does not declare are dropped.
  `transparent: true` or `opacity < 1` at creation selects the `_transparent`
  `.filamat`; the variant cannot change afterwards, so a reactive opacity must
  also pass `transparent: true`.
- `matrix16` is column-major. `dynamicMesh` writes `initial` into the buffer
  immediately; the first frame composes it with `parentMatrix16`.

### Calls before Filament is ready

Filament initialises asynchronously (WASM). The scene **queues and replays**:
every method above may be called synchronously right after `__k3d.scene(...)`.
Handles are returned immediately and fill in when the engine is up;
`pos`/`rot`/`scale` write the transform buffer directly (dirty bits are kept,
so the first frame applies them), `setMaterialParam` records the value so it is
used when the material instance is created, and everything else is queued in
call order and replayed once the engine, view and camera exist. The compiler
therefore emits the straight-line code shown above, not a ready callback.
`scene.ready` is a promise that resolves after the replay; `scene.dispose()`
before then discards the queue and creates nothing.

## Light and exposure calibration

The default camera is the neutral physical exposure aperture f/16, shutter
1/125 s, ISO 100 (EV100 = log2(16^2 / (1/125)) = about 15), which puts
direct sunlight (about 100,000 lux) near middle grey. Three.js-scale
intensities are mapped at compile time:

| Three.js | Filament | Mapping |
| --- | --- | --- |
| `directionalLight intensity` | sun/directional illuminance (lux) | `intensity * 110000` |
| `pointLight intensity` | luminous power (lumens) | `intensity * 4 * pi * 1000` (candela to lumen, scaled for EV15) |
| `spotLight intensity` | luminous power (lumens) | same as point |
| `ambientLight intensity` | IBL intensity (when no `<Environment>`) | `intensity * 30000` |

The constants live in one Go file and one TS file and are tested against each
other, so a reactive intensity (computed at runtime) matches a static one.

## Canvas props -> Filament View

`shadows` -> the JS `View` has no `setShadowingEnabled`, so it gates the
`castShadows` flag of lights and the `castShadows`/`receiveShadows` flags of
renderables (shadow maps are built only for lights with the flag); `bloom`
(bool or options) -> `setBloomOptions`; `ssao` -> `setAmbientOcclusionOptions`;
`antialias="taa" | "fxaa" | "msaa" | "none"` -> `setTemporalAntiAliasingOptions`
/ `setAntiAliasing` / `setMultiSampleAntiAliasingOptions` (default `fxaa`, msaa
uses 4 samples); `toneMapping="aces" | "filmic" | "agx" | "linear"` ->
`ColorGrading.Builder.toneMapping` (default `aces`; the 1.53 JS enum has no AGX,
so `agx` falls back to `FILMIC` until the bindings expose it). The view is
translucent with a transparent clear colour, like react-three-fiber's default
`alpha` canvas. Fog, SSR, VSM shadows and stereo are never enabled.

## Events

`onClick`, `onPointerOver`, `onPointerOut`, `onPointerMove`, `onPointerDown`,
`onPointerUp` register the entity in a picking table (`scene.on(mesh, "click" |
"pointerover" | "pointerout" | "pointermove" | "pointerdown" | "pointerup", fn)`). The runtime calls
`view.pick(x, y, callback)` on pointer events (throttled to one in-flight pick
per frame) and dispatches by entity id; over/out are derived by comparing the
previous hovered entity.

## Fallback

A scene the compiler cannot analyse (spread props, components that return
scene nodes it cannot see through, dynamic tag names) is compiled to the
runtime reconciler path (`@krate/3d/runtime` JSX components), which creates
the same Filament objects at runtime without the compile-time optimisations.
The build prints the warning `KR8701` (`diag.CodeScene3DFallback`; the
diagnostic type has no info severity) naming the element and the reason so
authors can see when they leave the fast path. Beyond the cases above, a
reactive `<group>` transform falls back too, because `dynamicMesh` takes one
static parent matrix and the runtime has no parent chaining; so do an expression
child (`{cond && <mesh/>}`), a `.map` or `<For>` over a non-constant list,
reactive geometry `args`, reactive spot `angle`/`penumbra`/`distance`, and a
non-constant shader string.

The compile pass lives in `packages/compiler/internal/scene3d`. It runs in the
bundler right after a TSX module is parsed (before imports are collected) and
rewrites `<Canvas>` in place to `<K3dCanvas setup={(canvas) => {...}} />`, so
IR, SSR and hydration see an ordinary component with one function prop.

`Canvas` (from `@krate/3d`) is a `@renderer`-tier component. Its children
arrive as element descriptors in the shape `@krate/three` consumes:
`{ __k3: 1, type, props, children }` for intrinsic tags, `{ __k3: 2, type, props }`
for component calls (called once), plus `__kv` vnodes and `__ka` lists.
`mountSceneTree` walks them and drives the same `Scene3D` API as compiled code
(one dynamic slot per mesh, groups folded into a static parent matrix when
mounted). A prop whose value is a zero-argument function is a reactive thunk
and is followed with an effect; scene children are not diffed, so a region
function is evaluated once. `OrbitControls`, `Environment`, `useFrame` and
`useGLTF` attach to the most recently created scene and work on both paths.

The runtime code reads the Krate runtime (`createSignal`, `createEffect`,
`onCleanup`, `untrack`) from the global object, like `@krate/three`'s engine,
so it never bundles a second copy; `vitest.setup.ts` installs it for tests.

## Filament facts (1.53.4 JS bindings)

- `filament.js` replaces its global with the instantiated module inside
  `Filament.init`, so a module handed in before init can be the pre-init stub;
  the loader falls back to `globalThis.Filament` after init.
- Real-engine quirks found by the browser test: the builder methods
  `SurfaceOrientation$Builder.normals/uvs/positions/triangles*` return
  `undefined` (do not chain them, and call `build()` on the same JS object);
  `view.pick` calls back with the hit `Entity` object (0 or null for a miss),
  not the number its typings promise; `Engine.create` defaults to an opaque
  context, so the scene passes `{ alpha: true }` to let the translucent clear
  colour show the page.
- The runtime reads the Krate runtime through literal `globalThis.createEffect`
  style reads: the build's runtime shaker only keeps the exports it sees read
  that way.
  Apps with a custom wasm location pass an already initialised module as
  `<Canvas filament={...}>` / `SceneOptions.filament`.
- glTF KTX2 textures need no extra setup: `loadResources` registers the
  `Ktx2Provider`. IBL must be a KTX1 cubemap from `cmgen` (`*_ibl.ktx`); the
  skybox is the sibling `*_skybox.ktx`.
- Uniform ambient light is a one-band `IndirectLight` (SH band 0 =
  `2 * sqrt(pi) * color`); the constant is not yet verified against a real
  render.
