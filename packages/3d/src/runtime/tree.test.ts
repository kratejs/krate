import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFake, fakeFetch } from './__fixtures__/fake.js';
import { createEnvironment, setEnvironmentPresets } from './environment.js';
import { useGLTF } from './gltf.js';
import { useFrame } from './hooks.js';
import { setCurrentScene } from './context.js';
import { canvasStyle, mountCanvas, sceneOptions } from './mount.js';
import { Scene3D } from './scene.js';
import { materialParams } from './tree.js';

const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0));
const el = (type: string, props: Record<string, unknown> = {}, ...children: unknown[]) => ({ __k3: 1, type, props, children });

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', () => 1);
  vi.stubGlobal('cancelAnimationFrame', () => {});
});
afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
  setCurrentScene(undefined);
});

function make(children: unknown, extra: Record<string, unknown> = {}) {
  const fake = createFake();
  const canvas = document.createElement('canvas');
  document.body.appendChild(canvas);
  Object.defineProperty(canvas, 'clientWidth', { value: 100 });
  Object.defineProperty(canvas, 'clientHeight', { value: 100 });
  let scene!: Scene3D;
  const teardown = mountCanvas(canvas, {
    filament: fake.module,
    fetchBytes: fakeFetch,
    pixelRatio: 1,
    materialBase: 'https://m/',
    children,
    onCreated: (s) => (scene = s),
    ...extra,
  });
  return { fake, canvas, scene, teardown };
}

describe('fallback tree mount', () => {
  it('mounts meshes, groups and lights through the shared scene API', async () => {
    const mesh = (p: Record<string, unknown>) =>
      el('mesh', p, el('boxGeometry', { args: [2, 2, 2] }), el('meshStandardMaterial', { color: 'orange', roughness: 0.4 }));
    const { fake, scene } = make([
      el('group', { position: [0, 1, 0] }, mesh({ position: [2, 0, 0] })),
      el('directionalLight', { intensity: 1.5, position: [0, 5, 0] }),
      el('ambientLight', { intensity: 0.2 }),
    ]);
    await scene.ready;
    await settle();
    expect(fake.state.renderables).toHaveLength(1);
    expect(fake.state.renderables[0].rec.boundingBox[0][0]).toMatchObject({ halfExtent: [1, 1, 1] });
    scene.tick(1);
    // group (0,1,0) * mesh (2,0,0)
    expect(fake.state.transforms.at(-1)!.m.slice(12, 15)).toEqual([2, 1, 0]);
    expect(fake.state.instances[0].params.roughness).toBe(0.4);
    expect((fake.state.instances[0].params.baseColor as number[])[1]).toBeCloseTo(0.3763, 4);
    expect(fake.state.lights[0].rec.intensity[0]).toEqual([165000]);
    expect(fake.state.lights[0].rec.direction[0][0]).toEqual([-0, -1, -0]);
  });

  it('follows reactive (thunk) props with effects', async () => {
    const [x, setX] = (globalThis as unknown as { createSignal: <T>(v: T) => [() => T, (v: T) => void] }).createSignal(1);
    const [rough, setRough] = (globalThis as unknown as { createSignal: <T>(v: T) => [() => T, (v: T) => void] }).createSignal(0.2);
    const { fake, scene } = make(
      el('mesh', { position: () => [x(), 0, 0] }, el('sphereGeometry', {}), el('meshStandardMaterial', { roughness: () => rough() })),
    );
    await scene.ready;
    await settle();
    scene.tick(1);
    expect(fake.state.transforms.at(-1)!.m[12]).toBe(1);
    setX(5);
    setRough(0.9);
    await settle();
    scene.tick(2);
    expect(fake.state.transforms.at(-1)!.m[12]).toBe(5);
    expect(fake.state.instances[0].params.roughness).toBe(0.9);
  });

  it('wires pointer events to the picker', async () => {
    const click = vi.fn();
    const { fake, scene, canvas } = make(
      el('mesh', { onClick: click }, el('boxGeometry'), el('meshBasicMaterial', { color: 'red' })),
    );
    await scene.ready;
    await settle();
    const id = (fake.state.renderables[0].entity.getId as () => number)();
    canvas.dispatchEvent(new MouseEvent('click', { clientX: 1, clientY: 1 }));
    scene.tick(1);
    expect(click).not.toHaveBeenCalled();
    fake.state.picks[0].cb({ renderable: id, depth: 0, fragCoords: [] });
    expect(click).toHaveBeenCalledOnce();
  });

  it('calls components and unwraps vnodes, arrays and lists', async () => {
    const Box = () => el('mesh', {}, el('boxGeometry'), el('meshStandardMaterial'));
    const { fake, scene } = make([
      { __k3: 2, type: Box, props: {} },
      { __kv: 1, tag: 'mesh', props: {}, children: [el('planeGeometry'), el('meshStandardMaterial')] },
      { __ka: 1, items: () => [1, 2], render: () => el('mesh', {}, el('boxGeometry'), el('meshStandardMaterial')) },
      null,
      false,
      'text',
    ]);
    await scene.ready;
    await settle();
    expect(fake.state.renderables).toHaveLength(4);
  });

  it('maps spot light angles and falloff', async () => {
    const { fake, scene } = make(
      el('spotLight', { position: [0, 4, 0], target: [0, 0, 0], angle: 0.6, penumbra: 0.5, distance: 20, intensity: 2 }),
    );
    await scene.ready;
    const l = fake.state.lights[0];
    expect(l.rec.spotLightCone[0][0]).toBeCloseTo(0.3, 6);
    expect(l.rec.spotLightCone[0][1]).toBe(0.6);
    expect(l.rec.falloff[0]).toEqual([20]);
    expect(l.rec.direction[0][0]).toEqual([0, -1, 0]);
  });

  it('warns about unknown elements and meshes without geometry', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    make([el('torus'), el('mesh', {}, el('meshStandardMaterial'))]);
    expect(warn).toHaveBeenCalledTimes(2);
    warn.mockRestore();
  });

  it('teardown stops effects and disposes the scene', async () => {
    const { fake, scene, teardown } = make(el('mesh', {}, el('boxGeometry'), el('meshStandardMaterial')));
    await scene.ready;
    await settle();
    teardown();
    expect(scene.disposed).toBe(true);
    expect(fake.alive.size).toBe(0);
  });
});

describe('props mapping', () => {
  it('maps Three.js material props to uber-material parameters', () => {
    expect(
      materialParams({ color: '#ffffff', metalness: 1, map: 'a.png', metalnessMap: 'm.png', emissive: 'black', unknown: 1, opacity: 0.5 }),
    ).toEqual({
      baseColor: [1, 1, 1, 1],
      metallic: 1,
      baseColorMap: 'a.png',
      metallicMap: 'm.png',
      emissive: [0, 0, 0],
      opacity: 0.5,
    });
  });

  it('builds scene options from Canvas props', () => {
    const o = sceneOptions({ shadows: true, bloom: { strength: 1 }, antialias: 'taa', toneMapping: 'agx', camera: { fov: 40 }, exposure: 1 });
    expect(o.view).toMatchObject({ shadows: true, bloom: { strength: 1 }, antialias: 'taa', toneMapping: 'agx' });
    expect(o.camera).toMatchObject({ fov: 40, exposure: 1 });
  });

  it('merges canvas style', () => {
    expect(canvasStyle(undefined)).toBe('display:block;width:100%;height:100%;');
    expect(canvasStyle({ borderRadius: 4, color: 'red' })).toBe('display:block;width:100%;height:100%;border-radius:4;color:red');
    expect(canvasStyle('height:50%')).toContain('height:50%');
  });
});

describe('hooks', () => {
  it('useFrame runs on the current scene and throws without one', async () => {
    const { scene } = make(null);
    await scene.ready;
    const cb = vi.fn();
    useFrame(cb);
    scene.tick(1);
    scene.tick(1001);
    expect(cb).toHaveBeenCalledTimes(2);
    expect(cb.mock.calls[1][0]).toBeCloseTo(1, 6);
    scene.dispose();
    expect(() => useFrame(cb)).toThrow(/inside a <Canvas>/);
  });

  it('useGLTF loads an asset into the scene and exposes reactive state', async () => {
    const { fake, scene } = make(null);
    const gltf = useGLTF('/models/a.glb');
    expect(gltf.status).toBe('loading');
    await scene.ready;
    await settle();
    expect(gltf.status).toBe('ready');
    expect(gltf.asset).not.toBeNull();
    expect(gltf.error).toBeNull();
    scene.dispose();
    expect(fake.alive.size).toBe(0);
  });

  it('useGLTF reports fetch errors', async () => {
    const { scene } = make(null, { fetchBytes: () => Promise.reject(new Error('404')) });
    const gltf = useGLTF('/missing.glb');
    await scene.ready;
    await settle();
    expect(gltf.status).toBe('error');
    expect((gltf.error as Error).message).toBe('404');
  });

  it('useGLTF without a scene is an error state, not a throw', () => {
    const gltf = useGLTF('/a.glb');
    expect(gltf.status).toBe('error');
  });

  it('Environment loads an IBL and optional skybox; presets need a URL', async () => {
    const { fake, scene } = make(null);
    await scene.ready;
    createEnvironment(scene, { files: '/env/studio_ibl.ktx', background: true, intensity: 1234 });
    await settle();
    const sceneObj = [...fake.alive].find((o) => o.kind === 'scene')!;
    expect((sceneObj.ibl as { kind: string }).kind).toBe('ibl');
    expect([...fake.alive].some((o) => o.kind === 'skybox')).toBe(true);
    expect(() => createEnvironment(scene, { preset: 'city' })).toThrow(/setEnvironmentPresets/);
    setEnvironmentPresets({ city: '/env/city_ibl.ktx' });
    expect(() => createEnvironment(scene, { preset: 'city' })).not.toThrow();
    expect(() => createEnvironment(scene, { files: '/a.ktx2' })).toThrow(/KTX1/);
    expect(() => createEnvironment(scene, {})).toThrow(/files/);
  });
});
