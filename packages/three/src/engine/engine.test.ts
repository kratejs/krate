import * as THREE from 'three';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  act,
  advance,
  createPortal,
  createPointerEvents,
  createFrame,
  createGraph,
  createLoader,
  createRoot,
  createThree,
  extend,
  invalidate,
  useFrame,
  useLoader,
  useThree,
  type ReconcilerRoot,
  type RootState,
  type RootStore,
} from './index.js';

// Compiled scene code calls the global `h`; the engine installs its own while
// it evaluates the scene.
const h = (tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]): unknown =>
  (globalThis as unknown as { h: (...a: unknown[]) => unknown }).h(tag, props, ...children);
const g = globalThis as unknown as {
  createSignal<T>(v: T): [() => T, (v: T) => void];
  krateFlushSync(): void;
  Suspense?: unknown;
};

// The object a compiled `.map()` produces (krateList), as a custom renderer sees it.
function list(items: () => unknown[], key: ((item: any, i: number) => unknown) | null, render: (item: any, i?: any) => unknown, mode = 1) {
  return { __ka: 1, items, key, render, mode };
}

function fakeGL(canvas: HTMLCanvasElement) {
  return {
    domElement: canvas,
    render: vi.fn(),
    setSize: vi.fn(),
    setPixelRatio: vi.fn(),
    shadowMap: { enabled: false, type: THREE.PCFShadowMap, needsUpdate: false },
    outputColorSpace: THREE.SRGBColorSpace,
    toneMapping: THREE.NoToneMapping,
    dispose: vi.fn(),
    forceContextLoss: vi.fn(),
  };
}

const roots: Array<ReconcilerRoot<HTMLCanvasElement>> = [];

async function mount(children: unknown, config: Record<string, unknown> = {}) {
  const canvas = document.createElement('canvas');
  const gl = fakeGL(canvas);
  const root = createRoot(canvas);
  roots.push(root);
  await root.configure({ gl: gl as any, size: { width: 100, height: 100, top: 0, left: 0 }, frameloop: 'never', ...config });
  const store = root.render(children);
  await act(() => {});
  return { root, store, gl, scene: store.getState().scene, canvas };
}

const flush = (): Promise<void> => act(() => {});

afterEach(async () => {
  for (const r of roots.splice(0)) r.unmount();
  await flush();
});

describe('elements', () => {
  it('creates objects, applies props and attaches geometry and material', async () => {
    const { scene } = await mount(() => [
      h('mesh', { name: 'box', position: () => [1, 2, 3], 'rotation-x': 0.5 }, h('boxGeometry', { args: () => [2, 2, 2] }), h('meshBasicMaterial', { color: 'red' })),
    ]);
    const mesh = scene.children[0] as THREE.Mesh;
    expect(mesh).toBeInstanceOf(THREE.Mesh);
    expect(mesh.name).toBe('box');
    expect(mesh.position.toArray()).toEqual([1, 2, 3]);
    expect(mesh.rotation.x).toBe(0.5);
    expect(mesh.geometry).toBeInstanceOf(THREE.BoxGeometry);
    expect((mesh.geometry as THREE.BoxGeometry).parameters.width).toBe(2);
    expect((mesh.material as THREE.MeshBasicMaterial).color.getHexString()).toBe('ff0000');
  });

  it('updates reactive props in place', async () => {
    const [x, setX] = g.createSignal(1);
    const [color, setColor] = g.createSignal('red');
    const { scene } = await mount(() => [h('mesh', { 'position-x': () => x() }, h('meshBasicMaterial', { color: () => color() }))]);
    const mesh = scene.children[0] as THREE.Mesh;
    const material = mesh.material;
    setX(4);
    setColor('blue');
    await flush();
    expect(scene.children[0]).toBe(mesh);
    expect(mesh.position.x).toBe(4);
    expect(mesh.material).toBe(material);
    expect((mesh.material as THREE.MeshBasicMaterial).color.getHexString()).toBe('0000ff');
  });

  it('reconstructs the object when args change and keeps its children', async () => {
    const [size, setSize] = g.createSignal(1);
    const { scene } = await mount(() => [
      h('group', { args: () => [], name: 'g' }, h('mesh', { args: () => [new THREE.BoxGeometry(size(), 1, 1)] })),
    ]);
    const group = scene.children[0] as THREE.Group;
    const mesh = group.children[0] as THREE.Mesh;
    const geometry = mesh.geometry;
    setSize(3);
    await flush();
    const next = group.children[0] as THREE.Mesh;
    expect(next).not.toBe(mesh);
    expect(next.geometry).not.toBe(geometry);
    expect((next.geometry as THREE.BoxGeometry).parameters.width).toBe(3);
  });

  it('supports pierced props, indexed attach and attach functions', async () => {
    const attached: unknown[] = [];
    const { scene } = await mount(() => [
      h('mesh', { 'material-color': 'lime' }),
      h(
        'mesh',
        { name: 'multi' },
        h('meshBasicMaterial', { attach: 'material-0', color: 'red' }),
        h('meshBasicMaterial', { attach: 'material-1', color: 'blue' }),
      ),
      h('group', {}, h('object3D', {
        attach: () => (parent: unknown, self: unknown) => {
          attached.push([parent, self]);
          return () => attached.push('detached');
        },
      })),
    ]);
    const [a, b] = scene.children as THREE.Mesh[];
    expect((a.material as THREE.MeshBasicMaterial).color.getHexString()).toBe('00ff00');
    expect(Array.isArray(b.material)).toBe(true);
    expect((b.material as THREE.MeshBasicMaterial[])[1].color.getHexString()).toBe('0000ff');
    expect(attached).toHaveLength(1);
  });

  it('renders primitives and does not dispose them', async () => {
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial());
    const spy = vi.spyOn(object.geometry, 'dispose');
    const [show, setShow] = g.createSignal(true);
    const { scene } = await mount(() => [() => show() && h('primitive', { object, position: () => [0, 1, 0] })]);
    expect(scene.children[0]).toBe(object);
    expect(object.position.y).toBe(1);
    setShow(false);
    await flush();
    expect(scene.children).toHaveLength(0);
    expect(spy).not.toHaveBeenCalled();
  });

  it('disposes removed objects unless dispose is null', async () => {
    const [show, setShow] = g.createSignal(true);
    const { scene } = await mount(() => [
      () => show() && h('mesh', { name: 'a' }, h('boxGeometry', {})),
      () => show() && h('mesh', { name: 'b', dispose: null }, h('boxGeometry', {})),
    ]);
    const [a, b] = scene.children as THREE.Mesh[];
    const da = vi.spyOn(a.geometry, 'dispose');
    const db = vi.spyOn(b.geometry, 'dispose');
    setShow(false);
    await flush();
    expect(scene.children).toHaveLength(0);
    expect(da).toHaveBeenCalled();
    expect(db).not.toHaveBeenCalled();
  });

  it('throws a helpful error for unknown elements', async () => {
    const canvas = document.createElement('canvas');
    const root = createRoot(canvas);
    roots.push(root);
    await root.configure({ gl: fakeGL(canvas) as any, size: { width: 10, height: 10, top: 0, left: 0 } });
    expect(() => root.render(() => [h('notAThing', {})])).toThrow(/NotAThing is not part of the THREE namespace/);
  });

  it('registers custom classes with extend', async () => {
    class Thing extends THREE.Object3D {
      constructor(public label = 'x') {
        super();
      }
    }
    extend({ Thing });
    const Named = extend(class Other extends THREE.Group {});
    const { scene } = await mount(() => [h('thing', { args: () => ['hi'] }), h(Named, {})]);
    expect((scene.children[0] as Thing).label).toBe('hi');
    expect(scene.children[1]).toBeInstanceOf(THREE.Group);
  });

  it('sets refs to the object', async () => {
    const ref = { current: null as THREE.Mesh | null };
    const fn = vi.fn();
    const [show, setShow] = g.createSignal(true);
    await mount(() => [() => show() && h('mesh', { ref }), h('group', { ref: fn })]);
    expect(ref.current).toBeInstanceOf(THREE.Mesh);
    expect(fn).toHaveBeenCalledWith(expect.any(THREE.Group));
    setShow(false);
    await flush();
    expect(ref.current).toBeNull();
  });
});

describe('regions and lists', () => {
  it('mounts and unmounts conditional content in order', async () => {
    const [show, setShow] = g.createSignal(false);
    const { scene } = await mount(() => [h('group', { name: 'first' }), () => show() && h('group', { name: 'middle' }), h('group', { name: 'last' })]);
    expect(scene.children.map((c) => c.name)).toEqual(['first', 'last']);
    setShow(true);
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['first', 'middle', 'last']);
    setShow(false);
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['first', 'last']);
  });

  it('reconciles keyed lists: keeps, adds, removes and reorders', async () => {
    const [items, setItems] = g.createSignal([1, 2, 3]);
    const { scene } = await mount(() => [
      list(items, (i) => i, (item: () => number) => h('group', { name: () => String(item()) })),
      h('group', { name: 'tail' }),
    ]);
    const names = (): string[] => scene.children.map((c) => c.name);
    expect(names()).toEqual(['1', '2', '3', 'tail']);
    const two = scene.children[1];
    setItems([3, 2, 4]);
    await flush();
    expect(names()).toEqual(['3', '2', '4', 'tail']);
    expect(scene.children[1]).toBe(two);
    setItems([]);
    await flush();
    expect(names()).toEqual(['tail']);
  });
});

describe('components and hooks', () => {
  it('runs components once and subscribes useFrame until unmount', async () => {
    const body = vi.fn();
    const frame = vi.fn();
    function Spinner(props: { speed: () => number }) {
      body();
      const ref = { current: null as THREE.Mesh | null };
      useFrame((_state, delta) => {
        frame(delta);
        if (ref.current) ref.current.rotation.y += props.speed();
      });
      return h('mesh', { ref });
    }
    const [show, setShow] = g.createSignal(true);
    const [speed] = g.createSignal(1);
    const { store, scene } = await mount(() => [() => show() && h(Spinner, { get speed() { return speed; } })]);
    expect(body).toHaveBeenCalledTimes(1);
    advance(1, true, store.getState());
    expect(frame).toHaveBeenCalledTimes(1);
    expect(scene.children[0].rotation.y).toBe(1);
    setShow(false);
    await flush();
    advance(2, true, store.getState());
    expect(frame).toHaveBeenCalledTimes(1);
    expect(store.getState().internal.subscribers).toHaveLength(0);
  });

  it('useThree returns the state and reactive selections', async () => {
    let state!: () => RootState;
    let width!: () => number;
    function Probe() {
      state = useThree() as unknown as () => RootState;
      width = useThree((s) => s.size.width) as unknown as () => number;
      return null;
    }
    const { store } = await mount(() => [h(Probe, {})]);
    expect(state().scene).toBe(store.getState().scene);
    expect(width()).toBe(100);
    store.getState().setSize(300, 200);
    expect(width()).toBe(300);
  });

  it('redraws a frameloop "never" root at its new size at once, with no time passing', async () => {
    const { store, gl } = await mount(() => [h('mesh', { name: 'm' })]);
    store.getState().advance(1);
    const clock = store.getState().clock.elapsedTime;
    gl.render.mockClear();
    gl.setSize.mockClear();
    store.getState().setSize(300, 200);
    // setSize clears the drawing buffer and nothing else draws a "never" root.
    expect(gl.setSize).toHaveBeenCalledWith(300, 200, true);
    expect(gl.render).toHaveBeenCalledTimes(1);
    expect(store.getState().clock.elapsedTime).toBe(clock);
    // A move without a size change does not redraw.
    store.getState().setSize(300, 200, 10, 10);
    expect(gl.render).toHaveBeenCalledTimes(1);
  });

  it('throws when hooks are used outside a scene', () => {
    expect(() => useThree()).toThrow(/within the Canvas/);
  });

  it('a positive useFrame priority takes over rendering', async () => {
    const order: string[] = [];
    function A() {
      useFrame(() => order.push('a'), 2);
      return null;
    }
    function B() {
      useFrame(() => order.push('b'), 1);
      return null;
    }
    const { store, gl } = await mount(() => [h(A, {}), h(B, {})]);
    gl.render.mockClear();
    advance(1, true, store.getState());
    expect(order).toEqual(['b', 'a']);
    expect(gl.render).not.toHaveBeenCalled();
  });

  it('renders on demand with frameloop="demand"', async () => {
    const raf = vi.spyOn(globalThis, 'requestAnimationFrame');
    const { store } = await mount(() => [h('group', {})], { frameloop: 'demand' });
    raf.mockClear();
    store.getState().internal.frames = 0;
    invalidate(store.getState());
    expect(store.getState().internal.frames).toBe(1);
    raf.mockRestore();
  });
});

describe('suspense and loading', () => {
  class FakeLoader extends THREE.Loader<string> {
    static resolvers: Array<() => void> = [];
    load(url: string, onLoad: (data: string) => void): void {
      FakeLoader.resolvers.push(() => onLoad(`data:${url}`));
    }
  }

  it('suspends the scene until useLoader resolves', async () => {
    function Model() {
      const data = useLoader(FakeLoader, '/model');
      return h('group', { name: data });
    }
    const { scene } = await mount(() => [h(Model, {})]);
    expect(scene.children).toHaveLength(0);
    FakeLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['data:/model']);
  });

  it('shows a Suspense fallback inside the scene', async () => {
    function Suspense(props: { fallback?: unknown; children?: unknown }) {
      return props.children;
    }
    (Suspense as unknown as { __krateThreeSuspense: boolean }).__krateThreeSuspense = true;
    function Model() {
      const data = useLoader(FakeLoader, '/other');
      return h('group', { name: data });
    }
    const { scene } = await mount(() => [h(Suspense, { fallback: h('group', { name: 'loading' }) }, h(Model, {})), h('group', { name: 'static' })]);
    expect(scene.children.map((c) => c.name)).toEqual(['loading', 'static']);
    FakeLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['data:/other', 'static']);
  });
});

describe('compiled Suspense', () => {
  class SlowLoader extends THREE.Loader<string> {
    static resolvers: Array<() => void> = [];
    load(url: string, onLoad: (data: string) => void): void {
      SlowLoader.resolvers.push(() => onLoad(`slow:${url}`));
    }
  }

  it('treats the krate-suspense element as a boundary with a thunk fallback', async () => {
    function Model() {
      return h('group', { name: useLoader(SlowLoader, '/a') });
    }
    const { scene } = await mount(() => [h('krate-suspense', { fallback: () => h('group', { name: 'wait' }) }, h(Model, {}))]);
    expect(scene.children.map((c) => c.name)).toEqual(['wait']);
    SlowLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['slow:/a']);
  });
});

describe('context', () => {
  type Ctx = { useContextLive(): () => string; Provider: unknown };
  const rtx = globalThis as unknown as {
    createContext(v: string): Ctx;
    krateContextRows(entries: unknown[], fn: () => unknown): () => unknown;
  };

  it('sees providers around the canvas, live, in deferred components', async () => {
    const Theme = rtx.createContext('default');
    const [color, setColor] = g.createSignal('red');
    function Inner() {
      const value = Theme.useContextLive();
      return h('group', { name: () => 'ctx:' + value() });
    }
    const { scene } = await mount(rtx.krateContextRows([[Theme, () => color()]], () => [h(Inner, {})]));
    expect(scene.children[0].name).toBe('ctx:red');
    setColor('blue');
    await flush();
    expect(scene.children[0].name).toBe('ctx:blue');
  });

  it('supports providers inside the scene', async () => {
    const Theme = rtx.createContext('default');
    const [color, setColor] = g.createSignal('green');
    function Inner() {
      const value = Theme.useContextLive();
      return h('group', { name: () => 'ctx:' + value() });
    }
    const { scene } = await mount(() => [
      h(Theme.Provider, { get value() { return color(); } }, h(Inner, {})),
      h(Inner, {}),
    ]);
    expect(scene.children.map((c) => c.name)).toEqual(['ctx:green', 'ctx:default']);
    setColor('pink');
    await flush();
    expect(scene.children[0].name).toBe('ctx:pink');
  });
});

describe('portals', () => {
  it('renders into another object with its own state', async () => {
    const target = new THREE.Scene();
    let portalScene: unknown;
    function Inside() {
      portalScene = (useThree((s) => s.scene) as unknown as () => THREE.Scene)();
      return h('mesh', { name: 'inside' });
    }
    await mount(() => [createPortal(h(Inside, {}), target)]);
    expect(target.children.map((c) => c.name)).toEqual(['inside']);
    expect(portalScene).toBe(target);
  });
});

describe('events', () => {
  it('raycasts pointer events to handlers with bubbling and misses', async () => {
    const clicked = vi.fn();
    const groupClicked = vi.fn();
    const missed = vi.fn();
    const { store, canvas } = await mount(
      () => [
        h(
          'group',
          { onClick: groupClicked },
          h('mesh', { onClick: (e: { stopPropagation(): void; object: THREE.Object3D }) => clicked(e.object.name), name: 'hit' }, h('boxGeometry', {}), h('meshBasicMaterial', {})),
        ),
        h('mesh', { position: () => [10, 0, 0], onPointerMissed: missed }, h('boxGeometry', {})),
      ],
      { frameloop: 'always', events: createPointerEvents },
    );
    store.getState().camera.updateMatrixWorld();
    store.getState().scene.updateMatrixWorld();
    const handlers = store.getState().events.handlers!;
    const event = (type: string) =>
      ({ type, offsetX: 50, offsetY: 50, pointerId: 1, target: canvas }) as unknown as PointerEvent;
    handlers.onPointerDown(event('pointerdown'));
    handlers.onClick(event('click'));
    expect(clicked).toHaveBeenCalledWith('hit');
    expect(groupClicked).toHaveBeenCalledTimes(1);
    // As in R3F, misses are dispatched before each handler that fires.
    expect(missed).toHaveBeenCalled();
  });
});

describe('roots', () => {
  it('calls onCreated, sizes the camera and releases on unmount', async () => {
    const onCreated = vi.fn();
    const { root, store, gl } = await mount(() => [h('mesh', {}, h('boxGeometry', {}))], { onCreated });
    expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ scene: store.getState().scene }));
    const camera = store.getState().camera as THREE.PerspectiveCamera;
    expect(camera.aspect).toBe(1);
    expect(store.getState().internal.active).toBe(true);
    const geometry = (store.getState().scene.children[0] as THREE.Mesh).geometry;
    const spy = vi.spyOn(geometry, 'dispose');
    root.unmount();
    roots.splice(roots.indexOf(root), 1);
    await flush();
    expect(store.getState().internal.active).toBe(false);
    expect(spy).toHaveBeenCalled();
    expect(gl.render).toBeDefined();
  });

  it('accepts a renderer factory and camera options', async () => {
    const { store } = await mount(() => [], { camera: { position: [0, 0, 10], fov: 40 }, orthographic: false });
    const camera = store.getState().camera as THREE.PerspectiveCamera;
    expect(camera.position.z).toBe(10);
    expect(camera.fov).toBe(40);
  });

  it('a store can be read without the runtime and with selectors', async () => {
    const { store } = await mount(() => []);
    const s: RootStore = store;
    expect(typeof s.getState().invalidate).toBe('function');
    const unsub = s.subscribe(() => {});
    unsub();
  });
});

describe('more instance semantics', () => {
  it('keeps handlers and refs across reconstruction', async () => {
    const [n, setN] = g.createSignal(1);
    const ref = { current: null as THREE.Mesh | null };
    const click = vi.fn();
    const { store, scene } = await mount(() => [h('mesh', { ref, onClick: click, args: () => [new THREE.BoxGeometry(n(), 1, 1)] })], {
      events: createPointerEvents,
    });
    const first = scene.children[0];
    expect(store.getState().internal.interaction).toContain(first);
    setN(2);
    await flush();
    const second = scene.children[0];
    expect(second).not.toBe(first);
    expect(ref.current).toBe(second);
    expect(store.getState().internal.interaction).toContain(second);
    expect(store.getState().internal.interaction).not.toContain(first);
  });

  it('re-renders value-mode list rows whose item changed', async () => {
    const [items, setItems] = g.createSignal([{ id: 'a', v: 1 }]);
    const { scene } = await mount(() => [list(items, (i) => i.id, (item: { v: number }) => h('group', { name: String(item.v) }), 0)]);
    expect(scene.children[0].name).toBe('1');
    setItems([{ id: 'a', v: 2 }]);
    await flush();
    expect(scene.children[0].name).toBe('2');
  });

  it('nests components, regions and lists in one parent in order', async () => {
    const [show, setShow] = g.createSignal(false);
    const [items, setItems] = g.createSignal(['x']);
    function Pair() {
      return [h('group', { name: 'p1' }), h('group', { name: 'p2' })];
    }
    const { scene } = await mount(() => [
      h('group', { name: 'a' }),
      h(Pair, {}),
      () => show() && h('group', { name: 'cond' }),
      list(items, (i) => i, (i: () => string) => h('group', { name: () => 'row-' + i() })),
      h('group', { name: 'z' }),
    ]);
    const names = (): string => scene.children.map((c) => c.name).join(",");
    expect(names()).toBe('a,p1,p2,row-x,z');
    setShow(true);
    setItems(['y', 'x']);
    await flush();
    expect(names()).toBe('a,p1,p2,cond,row-y,row-x,z');
  });
});

describe('Krate-style primitives', () => {
  class TextLoader extends THREE.Loader<string> {
    static resolvers: Array<() => void> = [];
    load(url: string, onLoad: (data: string) => void, _p?: unknown, onError?: (e: unknown) => void): void {
      TextLoader.resolvers.push(() => (url.includes('bad') ? onError?.({ message: 'nope' }) : onLoad(`text:${url}`)));
    }
  }

  it('createThree returns accessors that follow the root state', async () => {
    let state!: () => RootState;
    let width!: () => number;
    function Probe() {
      state = createThree();
      width = createThree((s) => s.size.width);
      return h('group', { name: () => `w${width()}` });
    }
    const { store, scene } = await mount(() => [h(Probe, {})]);
    expect(state().scene).toBe(store.getState().scene);
    expect(scene.children[0].name).toBe('w100');
    store.getState().setSize(300, 200);
    await flush();
    expect(width()).toBe(300);
    expect(scene.children[0].name).toBe('w300');
    expect(() => createThree()).toThrow(/within the Canvas/);
  });

  it('createFrame subscribes until unmount and returns a stop function', async () => {
    const calls: string[] = [];
    let stop!: () => void;
    function A() {
      createFrame(() => calls.push('a'));
      stop = createFrame(() => calls.push('stopped'));
      return null;
    }
    const [show, setShow] = g.createSignal(true);
    const { store } = await mount(() => [() => show() && h(A, {})]);
    advance(1, true, store.getState());
    expect(calls).toEqual(['a', 'stopped']);
    stop();
    stop();
    advance(2, true, store.getState());
    expect(calls).toEqual(['a', 'stopped', 'a']);
    setShow(false);
    await flush();
    expect(store.getState().internal.subscribers).toHaveLength(0);
  });

  it('createFrame follows an accessor priority', async () => {
    const [priority, setPriority] = g.createSignal(0);
    function A() {
      createFrame(() => {}, priority);
      return null;
    }
    const { store } = await mount(() => [h(A, {})]);
    expect(store.getState().internal.priority).toBe(0);
    setPriority(1);
    g.krateFlushSync();
    expect(store.getState().internal.subscribers).toHaveLength(1);
    expect(store.getState().internal.priority).toBe(1);
    setPriority(0);
    g.krateFlushSync();
    expect(store.getState().internal.priority).toBe(0);
  });

  it('createLoader suspends readers, reports progress and follows its input', async () => {
    const [url, setUrl] = g.createSignal('/one');
    let model!: ReturnType<typeof createLoader<string, typeof TextLoader>>;
    function Model() {
      model = createLoader(TextLoader, url);
      return h('group', { name: () => model() });
    }
    const { scene } = await mount(() => [h(Model, {})]);
    expect(scene.children).toHaveLength(0);
    expect(model.loading).toBe(true);
    expect(model.state).toBe('loading');
    expect(model.latest).toBeUndefined();
    TextLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(model.state).toBe('ready');
    expect(model.latest).toBe('text:/one');
    expect(scene.children.map((c) => c.name)).toEqual(['text:/one']);
    setUrl('/two');
    await flush();
    expect(model.loading).toBe(true);
    TextLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(scene.children.map((c) => c.name)).toEqual(['text:/two']);
    setUrl('/bad');
    await flush();
    TextLoader.resolvers.splice(0).forEach((r) => r());
    await flush();
    expect(model.state).toBe('error');
    expect(String(model.error)).toMatch(/Could not load \/bad: nope/);
    createLoader.clear(TextLoader, '/bad');
  });

  it('createGraph maps an object tree and follows an accessor', async () => {
    const a = new THREE.Group();
    a.add(Object.assign(new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial({ name: 'red' })), { name: 'box' }));
    const b = new THREE.Group();
    b.add(Object.assign(new THREE.Object3D(), { name: 'empty' }));
    const [object, setObject] = g.createSignal<THREE.Object3D>(a);
    let graph!: () => { nodes: Record<string, unknown>; materials: Record<string, unknown> };
    function Probe() {
      graph = createGraph(object);
      return null;
    }
    await mount(() => [h(Probe, {})]);
    expect(Object.keys(graph().nodes)).toEqual(['box']);
    expect(Object.keys(graph().materials)).toEqual(['red']);
    setObject(b);
    expect(Object.keys(graph().nodes)).toEqual(['empty']);
  });
});
