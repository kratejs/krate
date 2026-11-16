---
title: 3D with three.js
description: Build three.js scenes in JSX with @krate/three, which has the react-three-fiber API (Canvas, elements, useFrame, useThree, useLoader, events, portals).
order: 19
---

# 3D with three.js

`@krate/three` renders [three.js](https://threejs.org) scenes from JSX. It has
the API of [react-three-fiber](https://r3f.docs.pmnd.rs) (R3F): a `<Canvas>`,
every three class as a JSX element, `useFrame`, `useThree`, `useLoader`,
pointer events with raycasting, portals, `extend`, and `createRoot` for
rendering without a component. Code written for R3F runs with the import
changed from `@react-three/fiber` to `@krate/three`.

```sh
pnpm add @krate/three three
```

```tsx
import { useRef, useState } from "react";
import { Canvas, useFrame } from "@krate/three";
import type { Mesh } from "three";

function Box(props: { position: [number, number, number] }) {
  const ref = useRef<Mesh>(null);
  const [hovered, hover] = useState(false);
  const [clicked, click] = useState(false);
  useFrame((state, delta) => (ref.current!.rotation.x += delta));
  return (
    <mesh
      {...props}
      ref={ref}
      scale={clicked ? 1.5 : 1}
      onClick={() => click(!clicked)}
      onPointerOver={(event) => (event.stopPropagation(), hover(true))}
      onPointerOut={() => hover(false)}
    >
      <boxGeometry args={[1, 1, 1]} />
      <meshStandardMaterial color={hovered ? "hotpink" : "orange"} />
    </mesh>
  );
}

export default function Page() {
  return (
    <main style={{ height: "100vh" }}>
      <Canvas>
        <ambientLight intensity={Math.PI / 2} />
        <spotLight position={[10, 10, 10]} angle={0.15} penumbra={1} decay={0} intensity={Math.PI} />
        <Box position={[-1.2, 0, 0]} />
        <Box position={[1.2, 0, 0]} />
      </Canvas>
    </main>
  );
}
```

## How it works

`Canvas` is a *renderer component*: it is declared with the
[`@renderer`](/docs/core-concepts/component-tiers/#renderer-components)
decorator. The compiler does not render a renderer component's children to
HTML. It compiles them into a function that `Canvas` calls in the browser,
where `@krate/three` turns the elements into three objects:

- The page's HTML contains the canvas wrapper (`<div><div><canvas>`), so the
  layout is stable before JavaScript runs. Put a no-WebGL message in
  `fallback`; it renders inside the `<canvas>` element.
- The scene mounts once the canvas has a size. It is rendered again only where
  state it reads changes: a prop such as `scale={clicked ? 1.5 : 1}` updates
  that property of that object, `{show && <mesh />}` adds or removes the mesh,
  and a `.map()` keeps, adds, removes and reorders objects by `key`.
- Components inside the canvas run once, like every Krate component. State,
  context of the scene (`useThree`) and frame callbacks keep them current. React
  components (`useState`, `useRef`, `useEffect`, ...) work as they do elsewhere in
  Krate (see [React compatibility](/docs/guides/react-compatibility/)).
- The engine is bundled with the page like an npm dependency and shares one
  copy of `three` with your code, so `instanceof THREE.Mesh` and objects you
  create yourself (`new THREE.Vector3()`) work across both.

## Canvas

```tsx
<Canvas
  camera={{ position: [0, 0, 5], fov: 50 }}
  shadows
  dpr={[1, 2]}
  frameloop="demand"
  onCreated={(state) => state.gl.setClearColor("#101014")}
  onPointerMissed={() => setSelected(null)}
>
  ...
</Canvas>
```

| Prop | Description | Default |
|------|-------------|---------|
| `gl` | Props for the default `WebGLRenderer`, a renderer instance, or a (possibly async) factory `(defaults) => renderer` | `{}` |
| `camera` | A camera instance, or props for the default camera | `{ fov: 75, near: 0.1, far: 1000, position: [0, 0, 5] }` |
| `scene` | A `THREE.Scene` instance, or props for the default scene | `{}` |
| `shadows` | `true` (PCF soft), `'basic'`, `'percentage'`, `'soft'`, `'variance'`, or `gl.shadowMap` options | `false` |
| `raycaster` | Props for the default raycaster | `{}` |
| `frameloop` | `'always'`, `'demand'` (render when something changed or on `invalidate()`), or `'never'` (render with `advance()`) | `'always'` |
| `resize` | Measuring options: `{ scroll, debounce, offsetSize, polyfill }` | `{ scroll: true, debounce: { scroll: 50, resize: 0 } }` |
| `orthographic` | Create an `OrthographicCamera` | `false` |
| `dpr` | Pixel ratio, or a `[min, max]` range clamped to the device ratio | `[1, 2]` |
| `legacy` | Disable three's color management | `false` |
| `linear` | Linear output color space instead of sRGB | `false` |
| `flat` | `NoToneMapping` instead of `ACESFilmicToneMapping` | `false` |
| `events` | The event manager factory | `events` (DOM pointer events) |
| `eventSource` | The element (or ref) pointer events are read from | the wrapper div |
| `eventPrefix` | Which event coordinates map to the pointer: `'offset'`, `'client'`, `'page'`, `'layer'`, `'screen'` | `'offset'` |
| `performance` | Adaptive performance: `{ current, min, max, debounce }` | `{ current: 1, min: 0.5, max: 1, debounce: 200 }` |
| `onCreated` | Called with the root state once the scene is set up | |
| `onPointerMissed` | Called for clicks that hit nothing | |
| `fallback` | Content of the `<canvas>` element, shown when WebGL is unavailable | |
| `ref` | Receives the `<canvas>` element | |
| `id`, `class`, `style`, ... | Attributes of the wrapper div | |

Props that read state (`frameloop={paused ? "never" : "always"}`) reconfigure the
root when they change; only the changed inputs are applied.

## Objects, properties and args

Every three class is an element named in camelCase: `THREE.Mesh` is `<mesh>`,
`THREE.BoxGeometry` is `<boxGeometry>`. The few names that clash with HTML or
SVG take a `three` prefix: `<threeLine>`, `<threePath>`, `<threeAudio>`,
`<threeSource>`.

- **`args`** are the constructor arguments: `<boxGeometry args={[2, 2, 2]} />`
  is `new THREE.BoxGeometry(2, 2, 2)`. When `args` change, the object is
  recreated (children, handlers and refs move to the new object).
- **Properties** are set on the object. Math types are set, not replaced:
  `position={[1, 2, 3]}` calls `position.set(1, 2, 3)`, `scale={2}` calls
  `setScalar(2)`, `color="hotpink"` calls `color.set("hotpink")`, and passing an
  instance of the same class (`position={new THREE.Vector3()}`) copies it.
- **Pierced props** reach nested properties with dashes:
  `position-x={1}`, `material-color="red"`, `shadow-mapSize={[1024, 1024]}`.
- **`attach`** says where a child goes on its parent. Geometries attach to
  `geometry` and materials to `material` by default. `attach="material-0"`
  fills an array, `attach="fog"` sets a property, and a function
  `attach={(parent, self) => { ...; return () => { ...detach } }}` does anything.
- **`<primitive object={obj} />`** places an existing object in the scene. It
  is never disposed by the renderer.
- **`dispose={null}`** keeps an object (and its children) alive when it is
  removed. Otherwise removed objects are disposed when the browser is idle.
- **`ref`** receives the three object: `useRef<THREE.Mesh>(null)` in React
  style, or `let mesh: THREE.Mesh | undefined; <mesh ref={mesh} />` in Krate style.
- **`onUpdate={(self) => ...}`** runs after the object's props change.

```tsx
<mesh position-y={1} castShadow material-color="tomato">
  <sphereGeometry args={[0.5, 32, 32]} />
</mesh>

<directionalLight castShadow position={[5, 5, 5]} shadow-mapSize={[2048, 2048]}>
  <orthographicCamera attach="shadow-camera" args={[-10, 10, 10, -10]} />
</directionalLight>

<primitive object={gltf.scene} position={[0, -1, 0]} />
```

## Events

Objects with event handlers are raycast against the pointer. Handlers receive
the three intersection plus the DOM event's fields:

```tsx
<mesh
  onClick={(e) => console.log(e.object, e.point, e.distance)}
  onContextMenu={(e) => ...}
  onDoubleClick={(e) => ...}
  onWheel={(e) => ...}
  onPointerUp={(e) => ...}
  onPointerDown={(e) => e.target.setPointerCapture(e.pointerId)}
  onPointerOver={(e) => ...}
  onPointerOut={(e) => ...}
  onPointerEnter={(e) => ...}
  onPointerLeave={(e) => ...}
  onPointerMove={(e) => ...}
  onPointerMissed={() => ...}
  onPointerCancel={(e) => ...}
  onLostPointerCapture={(e) => ...}
/>
```

Events bubble up through the object's ancestors; `e.stopPropagation()` stops
them, and also stops objects behind the hit object from receiving the event.
The event has `object` (what was hit), `eventObject` (whose handler runs),
`point`, `distance`, `face`, `uv`, `intersections`, `pointer`, `ray`,
`camera`, `unprojectedPoint`, `delta` (pixels moved since pointer down) and
`nativeEvent`. `onPointerMissed` runs on objects (and on `<Canvas>`) when a click
hits nothing. Pointer capture works through `e.target.setPointerCapture()`.

Custom event managers (`events`, `createEvents`), `eventSource` (for example
the page root, to keep receiving events under HTML overlays), `eventPrefix`,
and `state.setEvents({ enabled, priority, filter, compute })` work as in R3F.

## Hooks

Hooks work in components rendered inside `<Canvas>` and throw elsewhere.

### useFrame

```tsx
useFrame((state, delta, xrFrame) => {
  ref.current.rotation.y += delta;
});
```

Runs before every frame is rendered and stops when the component unmounts. A
second argument, `useFrame(fn, 1)`, sets a priority: callbacks run in
ascending order, and any positive priority takes over rendering, so render
yourself (`state.gl.render(state.scene, state.camera)`) or with a composer.

### useThree

```tsx
const { camera, gl, scene, size, viewport } = useThree();
const width = useThree((state) => state.size.width);
const setSize = useThree((state) => state.setSize);
```

Returns the root state, or the part a selector picks; it updates when that part
changes. The state contains `gl`, `scene`, `camera`, `raycaster`, `pointer`,
`clock`, `size`, `viewport` (with `getCurrentViewport()`), `performance`,
`events`, `xr`, `controls`, `frameloop`, and the setters `set`, `get`,
`invalidate`, `advance`, `setSize`, `setDpr`, `setFrameloop`, `setEvents`.

In a React-style module (one that imports from `react`), Krate reads the result
as a value that stays current, exactly like R3F. In a Krate-style module the
result is a getter, as Krate state always is:

```tsx
// Krate style
const size = useThree((state) => state.size);
return <mesh scale={size().width / 100} />;
```

`useStore()` returns the store itself (`getState`, `setState`, `subscribe`) for
reads that should not update anything, such as inside `useFrame`.

### useLoader

```tsx
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { Suspense } from "react";

function Model() {
  const gltf = useLoader(GLTFLoader, "/model.glb");
  return <primitive object={gltf.scene} />;
}

<Canvas>
  <Suspense fallback={<mesh><boxGeometry /></mesh>}>
    <Model />
  </Suspense>
</Canvas>
```

Loads with any three loader (a class or an instance), caches the result by
loader and URL, and suspends the component while loading. A `<Suspense>` inside
the scene shows its fallback meanwhile; without one, the content waits until
everything inside has loaded. Pass an array of URLs to load several at once, a
third argument to configure the loader (`(loader) => loader.setDRACOLoader(draco)`),
and a fourth for progress events. `useLoader.preload(Loader, url)` starts
loading early and `useLoader.clear(Loader, url)` drops a cached result. A GLTF
result has `nodes`, `materials` and `meshes` maps.

### useGraph and useInstanceHandle

`useGraph(object)` returns `{ nodes, materials, meshes }` for any object tree,
for example a cloned GLTF scene. `useInstanceHandle(ref)` exposes the
renderer's descriptor of an object (props, parent, handlers); it is an escape
hatch whose shape may change.

## extend

The `three` namespace is available by default. Register other classes, such as
the controls and objects in `three/examples`, with `extend`:

```tsx
import { extend } from "@krate/three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";

extend({ OrbitControls });

function Controls() {
  const { camera, gl } = useThree();
  return <orbitControls args={[camera, gl.domElement]} />;
}
```

`extend(Class)` with a single class returns a tag for it:
`const Orbit = extend(OrbitControls); <Orbit args={[camera, gl.domElement]} />`.
For type checking, add the element to `ThreeElements`:

```ts
import type { ThreeElement } from "@krate/three";

declare module "@krate/three" {
  interface ThreeElements {
    orbitControls: ThreeElement<typeof OrbitControls>;
  }
}
```

## Portals

`createPortal(children, object, state?)` renders children into another object,
such as an off-screen scene for a render target, with its own camera, events
and size layered over the canvas state. Hooks inside the portal see the layered
state.

```tsx
const [scene] = useState(() => new THREE.Scene());
return <>{createPortal(<mesh><boxGeometry /></mesh>, scene, { camera: virtualCamera })}</>;
```

## Rendering on demand and by hand

- `frameloop="demand"` renders only after a change: an updated prop, a mounted
  or removed object, a resize, or a call to `invalidate()` (from
  `useThree((s) => s.invalidate)` or the `invalidate` export, which invalidates
  every canvas).
- `frameloop="never"` renders only when you call `advance(timestamp)`.
- `addEffect`, `addAfterEffect` and `addTail` register global callbacks that
  run before every frame, after it, and when the loop stops.
- `performance.regress()` lowers `performance.current` to `min` for
  `debounce` ms, for adaptive quality while the camera moves.

## Without a component: createRoot

```ts
import { createRoot, events, extend } from "@krate/three";
import * as THREE from "three";

extend(THREE);
const root = createRoot(document.querySelector("canvas")!);
await root.configure({ events, camera: { position: [0, 0, 50] }, size: { width: 400, height: 300, top: 0, left: 0 } });
root.render(scene); // the scene: compiled children, or objects from createPortal
root.unmount();
```

`unmountComponentAtNode(canvas)` also tears a root down; `_roots` maps canvases
to roots.

## Other exports

`applyProps(object, props)` applies props with the rules above to any object.
`getRootState(object)` returns the root state of an object in a scene.
`dispose(object)` disposes an object and its disposable properties.
`buildGraph(object)` is `useGraph` without a component. `flushSync(fn)` applies
the state changes made by `fn` at once, and `act(fn)` (for tests) also waits for
pending mounts. The types (`ThreeElements`, `ThreeElement`, `ThreeEvent`,
`RootState`, `Vector3`, `Euler`, `Color`, ...) match R3F's, and are also available
under the `ReactThreeFiber` and `KrateThree` namespaces.

## Differences from react-three-fiber

- **There is no React reconciler.** `reconciler` is not exported, and libraries
  built on `@react-three/fiber` (such as `@react-three/drei`) cannot be used inside
  a `@krate/three` canvas. Their building blocks are three classes you can
  `extend`. To use those libraries as they are, run `@react-three/fiber` itself in
  a [real-React island](/docs/guides/react-compatibility/#libraries-built-on-a-react-reconciler-real-react-islands),
  which ships React with the page.
- **Context crosses the canvas without a bridge.** The scene is compiled with
  the page, so providers around `<Canvas>` are visible inside it and stay live;
  R3F needs a context bridge for this.
- **DOM elements cannot be rendered in the scene.** Render HTML next to the
  `<Canvas>` (positioned over it) instead.
- **A zero-argument arrow written as a prop value is read as a value that
  changes.** `position={() => ...}` is not useful in either library; to pass a
  function as a property value (such as `raycast`), pass a named function
  (`raycast={meshBounds}`).
