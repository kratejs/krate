import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFake, fakeFetch } from './__fixtures__/fake.js';
import { __k3d } from './compiled.js';
import { aimLight } from './aim.js';
import { colorToLinear } from './color.js';
import { Scene3D } from './scene.js';

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', () => 0);
  vi.stubGlobal('cancelAnimationFrame', () => {});
});
afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0));

function make(opts = {}) {
  const fake = createFake();
  const canvas = document.createElement('canvas');
  document.body.appendChild(canvas);
  const scene = new Scene3D(canvas, { filament: fake.module, fetchBytes: fakeFetch, materialBase: 'https://m/', slots: 2, ...opts });
  return { fake, scene };
}

describe('compiled entry points', () => {
  it('__k3d.effect runs the function and reruns it when a signal changes', async () => {
    const [n, setN] = (globalThis as unknown as { createSignal(v: number): [() => number, (v: number) => void] }).createSignal(1);
    const seen: number[] = [];
    __k3d.effect(() => {
      seen.push(n());
    });
    setN(2);
    await settle();
    expect(seen).toEqual([1, 2]);
  });

  it('pos, rot and scale accept an array and a uniform scale number', () => {
    const { scene } = make();
    scene.pos(0, [1, 2, 3]);
    scene.scale(0, 2);
    scene.scale(1, [1, 2, 3]);
    scene.rot(1, [0, Math.PI, 0]);
    const d = scene.transforms.data;
    expect(Array.from(d.slice(0, 3))).toEqual([1, 2, 3]);
    expect(Array.from(d.slice(7, 10))).toEqual([2, 2, 2]);
    expect(Array.from(d.slice(17, 20))).toEqual([1, 2, 3]);
    expect(d[14]).toBeCloseTo(1, 6);
  });

  it('a light intensity changed before the engine is ready is used when it is built', async () => {
    const { fake, scene } = make();
    const h = scene.light('point', { intensity: 1 });
    scene.setLight(h, 'intensity', 777);
    await scene.ready;
    expect(fake.state.lights[0].rec.intensity[0]).toEqual([777]);
  });

  it('builds a custom material from its compiled file and applies uniforms', async () => {
    const { fake, scene } = make();
    const m = scene.material('custom', { url: '/assets/3d/abc.filamat', params: { speed: 2, tint: [1, 0, 0] }, transparent: true });
    await scene.ready;
    await settle();
    expect(m.kind).toBe('custom');
    expect(fake.state.materialBytes).toHaveLength(1);
    expect(fake.state.instances[0].kind).toBe('/assets/3d/abc.filamat');
    expect(fake.state.instances[0].params).toEqual({ speed: 2, tint: [1, 0, 0] });
    scene.setMaterialParam(m, 'speed', 5);
    scene.setMaterialParam(m, 'dir', [0, 1, 0, 1]);
    expect(fake.state.instances[0].params.speed).toBe(5);
    expect(fake.state.instances[0].params.dir).toEqual([0, 1, 0, 1]);
  });

  it('a mesh can use a custom material', async () => {
    const { fake, scene } = make();
    const m = scene.material('custom', { url: '/x.filamat' });
    scene.staticMesh(scene.geometry('box'), m, [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
    await scene.ready;
    await settle();
    expect(fake.state.renderables).toHaveLength(1);
  });
});

describe('aimLight', () => {
  it('aims a directional light from its position at the origin', async () => {
    const { fake, scene } = make();
    const h = scene.light('directional', { intensity: 1 });
    await scene.ready;
    aimLight(scene, h, 'directional', [0, 4, 0]);
    expect(fake.state.lights[0].entity.position).toEqual([0, 4, 0]);
    expect(fake.state.lights[0].entity.direction).toEqual([0, -1, 0]);
  });

  it('applies the parent matrix and an explicit target to a spot light', async () => {
    const { fake, scene } = make();
    const h = scene.light('spot', {});
    await scene.ready;
    const parent = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 0, 0, 1];
    aimLight(scene, h, 'spot', [0, 2, 0], [0, 0, 0], parent);
    expect(fake.state.lights[0].entity.position).toEqual([10, 2, 0]);
    expect(fake.state.lights[0].entity.direction).toEqual([0, -1, 0]);
    aimLight(scene, h, 'point', [1, 1, 1], null, null);
    expect(fake.state.lights[0].entity.position).toEqual([1, 1, 1]);
  });
});

describe('named colors', () => {
  it('knows the full CSS list', () => {
    expect(colorToLinear('rebeccapurple')[2]).toBeCloseTo(0.3185, 3);
    expect(colorToLinear('cornflowerblue')[2]).toBeCloseTo(0.8468732, 5);
    expect(colorToLinear('DarkSlateGray')[0]).toBeCloseTo(0.0284, 3);
  });
});
