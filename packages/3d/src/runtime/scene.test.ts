import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFake, fakeFetch } from './__fixtures__/fake.js';
import { __k3d } from './compiled.js';
import { Scene3D, type SceneOptions } from './scene.js';

let rafQueue: Array<(t: number) => void> = [];

beforeEach(() => {
  rafQueue = [];
  vi.stubGlobal('requestAnimationFrame', (cb: (t: number) => void) => rafQueue.push(cb));
  vi.stubGlobal('cancelAnimationFrame', () => {});
});
afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

function make(opts: SceneOptions = {}) {
  const fake = createFake();
  const canvas = document.createElement('canvas');
  document.body.appendChild(canvas);
  Object.defineProperty(canvas, 'clientWidth', { value: 300 });
  Object.defineProperty(canvas, 'clientHeight', { value: 150 });
  const scene = new Scene3D(canvas, { filament: fake.module, fetchBytes: fakeFetch, pixelRatio: 2, materialBase: 'https://m/', ...opts });
  return { fake, canvas, scene };
}

const IDENT = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
const opens = (log: string[]): number => log.filter((l) => l === 'open').length;

describe('Scene3D readiness', () => {
  it('queues calls made before the engine is ready and replays them in order', async () => {
    const { fake, scene } = make();
    const g = scene.geometry('box', [1, 1, 1]);
    const m = scene.material('standard', { baseColor: [1, 0, 0, 1] });
    const a = scene.staticMesh(g, m, IDENT);
    const b = scene.staticMesh(g, m, IDENT);
    scene.light('point', { intensity: 5 });
    expect(scene.isReady).toBe(false);
    expect(fake.state.renderables).toHaveLength(0);
    expect(a.id).toBe(0);
    await scene.ready;
    expect(scene.isReady).toBe(true);
    await settle();
    expect(fake.state.renderables).toHaveLength(2);
    expect(fake.state.lights).toHaveLength(1);
    expect(a.id).toBeGreaterThan(0);
    expect(b.id).toBeGreaterThan(a.id);
  });

  it('applies material params set before ready when the instance is created', async () => {
    const { fake, scene } = make();
    const m = scene.material('standard', { roughness: 0.4 });
    scene.setMaterialParam(m, 'roughness', 0.9);
    await scene.ready;
    await settle();
    expect(fake.state.instances[0].params.roughness).toBe(0.9);
  });

  it('keeps pos/rot/scale writes made before ready and flushes them on the first frame', async () => {
    const { fake, scene } = make({ slots: 1 });
    const m = scene.material('standard');
    const mesh = scene.dynamicMesh(0, scene.geometry('box'), m, IDENT, { pos: [0, 0, 0], quat: [0, 0, 0, 1], scale: [1, 1, 1] });
    scene.pos(0, 4, 5, 6);
    await scene.ready;
    await settle();
    scene.tick(16);
    expect(mesh.id).toBeGreaterThan(0);
    const last = fake.state.transforms.at(-1)!;
    expect(last.m.slice(12, 15)).toEqual([4, 5, 6]);
  });

  it('reports a Filament failure through onError and never rejects ready', async () => {
    const onError = vi.fn();
    const { scene } = make({ filament: () => Promise.reject(new Error('boom')), onError });
    await scene.ready;
    expect(onError).toHaveBeenCalledOnce();
    expect(scene.isReady).toBe(false);
  });
});

describe('meshes', () => {
  it('static meshes are placed once with the folded matrix and get no slot', async () => {
    const { fake, scene } = make();
    const g = scene.geometry('box');
    const m = scene.material('standard');
    const matrix = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 1, 0, 1];
    scene.staticMesh(g, m, matrix);
    await scene.ready;
    await settle();
    expect(fake.state.transforms).toHaveLength(1);
    expect(fake.state.transforms[0].m).toEqual(matrix);
    const r = fake.state.renderables[0];
    expect(r.rec.boundingBox[0][0]).toEqual({ center: [0, 0, 0], halfExtent: [0.5, 0.5, 0.5] });
    expect(r.entity.hasTransform).toBe(true);
    scene.tick(16);
    expect(fake.state.transforms).toHaveLength(1);
    expect(scene.transforms.instances).toHaveLength(0);
  });

  it('dynamic meshes register their transform slot and update only dirty slots', async () => {
    const { fake, scene } = make({ slots: 3 });
    const g = scene.geometry('sphere', [1, 32, 16]);
    const m = scene.material('standard');
    for (let s = 0; s < 3; s++) scene.dynamicMesh(s, g, m, IDENT, { pos: [s, 0, 0], quat: [0, 0, 0, 1], scale: [1, 1, 1] });
    await scene.ready;
    await settle();
    scene.tick(16);
    expect(fake.state.transforms).toHaveLength(3);
    expect(opens(fake.log)).toBe(1);

    fake.state.transforms.length = 0;
    scene.rot(1, 0, Math.PI, 0);
    scene.tick(32);
    expect(fake.state.transforms).toHaveLength(1);
    expect(fake.state.transforms[0].entity).toBe(scene.transforms.instances[1] && (scene.transforms.instances[1] as { entity: unknown }).entity);
    expect(opens(fake.log)).toBe(2);
    // Rotation of pi about Y flips the x axis.
    expect(fake.state.transforms[0].m[0]).toBeCloseTo(-1, 5);

    fake.state.transforms.length = 0;
    scene.tick(48);
    expect(fake.state.transforms).toHaveLength(0);
    expect(opens(fake.log)).toBe(2);
  });

  it('shares one geometry between identical requests', async () => {
    const { scene } = make();
    expect(scene.geometry('box', [1, 1, 1])).toBe(scene.geometry('box', [1, 1, 1]));
    expect(scene.geometry('box', [1, 1, 1])).not.toBe(scene.geometry('box', [2, 1, 1]));
  });

  it('instanced meshes share geometry and material and place each copy in one transaction', async () => {
    const { fake, scene } = make();
    const g = scene.geometry('box');
    const m = scene.material('standard');
    const matrices = new Float32Array(16 * 3);
    for (let i = 0; i < 3; i++) {
      matrices.set(IDENT, i * 16);
      matrices[i * 16 + 12] = i * 2;
    }
    const h = scene.instanced(g, m, 3, matrices);
    await scene.ready;
    await settle();
    expect(h.entities).toHaveLength(3);
    expect(fake.state.renderables).toHaveLength(3);
    const geos = fake.state.renderables.map((r) => r.rec.geometry[0]);
    expect(new Set(geos.map((a) => a[2])).size).toBe(1);
    expect(new Set(fake.state.renderables.map((r) => r.rec.material[0][1])).size).toBe(1);
    expect(fake.state.transforms.map((t) => t.m[12])).toEqual([0, 2, 4]);
    expect(opens(fake.log)).toBe(1);
  });

  it('shadow flags apply only when the Canvas enabled shadows', async () => {
    const off = make();
    off.scene.staticMesh(off.scene.geometry('box'), off.scene.material('standard'), IDENT, { castShadow: true, receiveShadow: true });
    await off.scene.ready;
    await settle();
    expect(off.fake.state.renderables[0].rec.castShadows[0]).toEqual([false]);

    const on = make({ view: { shadows: true } });
    on.scene.staticMesh(on.scene.geometry('box'), on.scene.material('standard'), IDENT, { castShadow: true });
    await on.scene.ready;
    await settle();
    expect(on.fake.state.renderables[0].rec.castShadows[0]).toEqual([true]);
    expect(on.fake.state.renderables[0].rec.receiveShadows[0]).toEqual([false]);
  });
});

describe('materials', () => {
  it('sets the DESIGN.md defaults and converts nothing it was given as linear', async () => {
    const { fake, scene } = make();
    scene.material('standard', { baseColor: [0.5, 0.25, 0.125, 1], roughness: 0.4 });
    await scene.ready;
    await settle();
    const p = fake.state.instances[0].params;
    expect(p.baseColor).toEqual([0.5, 0.25, 0.125, 1]);
    expect(p.roughness).toBe(0.4);
    expect(p.metallic).toBe(0);
    expect(p.emissive).toEqual([0, 0, 0]);
    expect(p.emissiveIntensity).toBe(1);
    expect(p.opacity).toBe(1);
    expect(p.normalScale).toBe(1);
    expect(p.useNormalMap).toBe(0);
    expect(p.baseColorMap).toBeDefined();
  });

  it('converts sRGB color strings to linear', async () => {
    const { fake, scene } = make();
    scene.material('standard', { baseColor: '#4488ff', emissive: 'orange' });
    await scene.ready;
    await settle();
    const p = fake.state.instances[0].params;
    expect((p.baseColor as number[])[0]).toBeCloseTo(0.0578, 4);
    expect((p.baseColor as number[])[3]).toBe(1);
    expect((p.emissive as number[])[1]).toBeCloseTo(0.3763, 4);
  });

  it('picks the transparent variant for transparent or opacity < 1', async () => {
    const { fake, scene } = make();
    scene.material('standard', {});
    scene.material('standard', { transparent: true });
    scene.material('physical', { opacity: 0.5 });
    scene.material('unlit', { opacity: 1 });
    await scene.ready;
    await settle();
    expect(fake.state.instances.map((i) => i.kind)).toEqual([
      'https://m/standard.filamat',
      'https://m/standard_transparent.filamat',
      'https://m/physical_transparent.filamat',
      'https://m/unlit.filamat',
    ]);
  });

  it('loads each .filamat once and drops parameters the material does not declare', async () => {
    const { fake, scene } = make();
    scene.material('unlit', { roughness: 0.2, baseColor: [1, 1, 1, 1] });
    scene.material('unlit', {});
    await scene.ready;
    await settle();
    expect(fake.state.materialBytes).toHaveLength(1);
    expect(fake.state.instances[0].params.roughness).toBeUndefined();
    expect(fake.state.instances[0].params.baseColor).toEqual([1, 1, 1, 1]);
  });

  it('writes reactive updates straight to the instance', async () => {
    const { fake, scene } = make();
    const m = scene.material('physical', {});
    await scene.ready;
    await settle();
    scene.setMaterialParam(m, 'clearcoat', 0.7);
    scene.setMaterialParam(m, 'baseColor', 'red');
    expect(fake.state.instances[0].params.clearcoat).toBe(0.7);
    expect(fake.state.instances[0].params.baseColor).toEqual([1, 0, 0, 1]);
  });
});

describe('lights', () => {
  it('builds directional, point and spot lights with calibrated intensity', async () => {
    const { fake, scene } = make({ view: { shadows: true } });
    scene.light('directional', { intensity: __k3d.intensity('directional', 1.5), direction: [0, -1, 0], castShadows: true });
    scene.light('point', { intensity: __k3d.intensity('point', 1), position: [1, 2, 3], falloff: 10 });
    scene.light('spot', { intensity: 1000, position: [0, 5, 0], direction: [0, -1, 0], inner: 0.2, outer: 0.5 });
    await scene.ready;
    const [d, p, s] = fake.state.lights;
    expect(d.type).toBe(fake.module.LightManager$Type.DIRECTIONAL);
    expect(d.rec.intensity[0]).toEqual([165000]);
    expect(d.rec.castShadows[0]).toEqual([true]);
    expect(d.rec.color[0]).toEqual([[1, 1, 1]]);
    expect(p.type).toBe(fake.module.LightManager$Type.POINT);
    expect(p.rec.intensity[0][0]).toBeCloseTo(4 * Math.PI * 1000, 6);
    expect(p.rec.falloff[0]).toEqual([10]);
    expect(s.rec.spotLightCone[0]).toEqual([0.2, 0.5]);
  });

  it('ignores shadows when the Canvas did not enable them, and uses SUN on request', async () => {
    const { fake, scene } = make();
    scene.light('directional', { intensity: 1, castShadows: true, sun: true });
    await scene.ready;
    expect(fake.state.lights[0].type).toBe(fake.module.LightManager$Type.SUN);
    expect(fake.state.lights[0].rec.castShadows[0]).toEqual([false]);
  });

  it('ambient light becomes the IBL unless an environment replaces it', async () => {
    const { fake, scene } = make();
    scene.light('ambient', { intensity: __k3d.intensity('ambient', 0.5), color: [1, 1, 1] });
    await scene.ready;
    const ambient = [...fake.alive].find((o) => o.kind === 'ibl')!;
    expect((ambient.rec as Record<string, unknown[][]>).intensity[0]).toEqual([15000]);
    const sceneObj = [...fake.alive].find((o) => o.kind === 'scene')!;
    expect(sceneObj.ibl).toBe(ambient);
    const env = fake.engine.createIblFromKtx1(new Uint8Array());
    scene.setEnvironment(env as never);
    expect(sceneObj.ibl).toBe(env);
  });

  it('updates a built light reactively', async () => {
    const { fake, scene } = make();
    const h = scene.light('point', { intensity: 1 });
    await scene.ready;
    scene.setLight(h, 'intensity', 42);
    scene.setLight(h, 'position', [1, 2, 3]);
    expect(fake.state.lights[0].entity.intensity).toBe(42);
    expect(fake.state.lights[0].entity.position).toEqual([1, 2, 3]);
  });
});

describe('frame loop and camera', () => {
  it('renders every frame, even when no transform is dirty', async () => {
    const { fake, scene } = make();
    await scene.ready;
    scene.tick(16);
    scene.tick(32);
    expect(fake.state.renders).toBe(2);
    expect(opens(fake.log)).toBe(0);
  });

  it('runs frame callbacks with delta seconds before transforms flush', async () => {
    const { scene } = make({ slots: 1 });
    const m = scene.material('standard');
    scene.dynamicMesh(0, scene.geometry('box'), m, IDENT);
    await scene.ready;
    await settle();
    scene.tick(1);
    const seen: number[] = [];
    scene.onFrame((dt) => {
      seen.push(dt);
      scene.pos(0, 9, 0, 0);
    });
    scene.tick(501);
    scene.tick(1001);
    expect(seen).toEqual([0.5, 0.5]);
  });

  it('sizes the drawing buffer from CSS size and pixel ratio and updates the projection', async () => {
    const { fake, canvas, scene } = make();
    await scene.ready;
    expect(canvas.width).toBe(600);
    expect(canvas.height).toBe(300);
    expect(fake.state.view.viewport).toEqual([[0, 0, 600, 300]]);
    expect(fake.state.camera.fov.slice(0, 4)).toEqual([75, 2, 0.1, 1000]);
    expect(fake.state.camera.exposure).toEqual([16, 1 / 125, 100]);
    expect(fake.state.view.blend).toEqual([fake.module.View$BlendMode.TRANSLUCENT]);
  });

  it('applies exposure compensation as ISO stops', async () => {
    const { fake, scene } = make({ camera: { exposure: 1 } });
    await scene.ready;
    expect(fake.state.camera.exposure).toEqual([16, 1 / 125, 200]);
  });

  it('pushes camera changes to lookAt on the next frame', async () => {
    const { fake, scene } = make();
    await scene.ready;
    scene.setCamera({ position: [1, 2, 3], target: [0, 1, 0] });
    scene.tick(16);
    expect(fake.state.camera.lookAt).toEqual([[1, 2, 3], [0, 1, 0], [0, 1, 0]]);
  });

  it('stops and disposes when the canvas is removed from the document', async () => {
    const { fake, canvas, scene } = make();
    await scene.ready;
    expect(rafQueue).toHaveLength(1);
    canvas.remove();
    rafQueue.shift()!(16);
    expect(scene.disposed).toBe(true);
    expect(fake.state.renders).toBe(0);
    expect(rafQueue).toHaveLength(1);
  });
});

describe('dispose', () => {
  it('destroys every object it created', async () => {
    const { fake, scene } = make({ slots: 1, view: { shadows: true, bloom: true } });
    const g = scene.geometry('box');
    const g2 = scene.geometry('plane');
    const m = scene.material('standard', { baseColor: 'red', map: 'tex.png' } as never);
    const m2 = scene.material('physical', { opacity: 0.5 });
    scene.staticMesh(g, m, IDENT);
    scene.dynamicMesh(0, g2, m2, IDENT);
    scene.instanced(g, m2, 2, new Float32Array(32));
    scene.light('directional', { intensity: 1 });
    scene.light('ambient', { intensity: 1 });
    scene.light('ambient', { intensity: 2 });
    await scene.ready;
    await settle();
    scene.setEnvironment(fake.engine.createIblFromKtx1(new Uint8Array()) as never, fake.engine.createSkyFromKtx1(new Uint8Array()) as never);
    scene.tick(16);
    expect(fake.alive.size).toBeGreaterThan(10);
    scene.dispose();
    expect([...fake.alive].map((o) => o.kind)).toEqual([]);
    expect(fake.log.at(-1)).toBe('engineDestroy');
    // Idempotent, and later calls are ignored.
    scene.dispose();
    scene.staticMesh(g, m, IDENT);
    scene.tick(32);
  });

  it('disposing before the engine is ready creates nothing', async () => {
    const { fake, scene } = make();
    scene.staticMesh(scene.geometry('box'), scene.material('standard'), IDENT);
    scene.dispose();
    await scene.ready;
    expect(fake.alive.size).toBe(0);
    expect(fake.state.renderables).toHaveLength(0);
  });
});

describe('__k3d', () => {
  it('scene() returns a Scene3D', async () => {
    const fake = createFake();
    const canvas = document.createElement('canvas');
    document.body.appendChild(canvas);
    const scene = __k3d.scene(canvas, { filament: fake.module, fetchBytes: fakeFetch });
    expect(scene).toBeInstanceOf(Scene3D);
    await scene.ready;
    scene.dispose();
    expect(fake.alive.size).toBe(0);
  });

  it('exposes the calibration and color helpers', () => {
    expect(__k3d.intensity('directional', 1)).toBe(110000);
    expect(__k3d.color('#ffffff')).toEqual([1, 1, 1]);
  });
});
