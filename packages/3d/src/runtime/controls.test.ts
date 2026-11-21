import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createFake, fakeFetch } from './__fixtures__/fake.js';
import { OrbitController, OrbitControls, clampPhi, orbitFromPosition, orbitToPosition, panOffset } from './controls.js';
import { setCurrentScene } from './context.js';
import { Scene3D } from './scene.js';

const rounded = (a: ArrayLike<number>, d = 5): number[] => Array.from(a, (v) => Number(v.toFixed(d)) + 0);

describe('orbit math', () => {
  it('round-trips position <-> spherical', () => {
    const target = [1, 2, 3];
    const pos = [4, 6, 9];
    const o = orbitFromPosition(pos, target);
    expect(o.radius).toBeCloseTo(Math.hypot(3, 4, 6), 6);
    expect(rounded(orbitToPosition(target, o))).toEqual(rounded(pos));
  });

  it('puts a camera on +Z at theta 0, phi 90 degrees', () => {
    const o = orbitFromPosition([0, 0, 5], [0, 0, 0]);
    expect(o.theta).toBeCloseTo(0, 6);
    expect(o.phi).toBeCloseTo(Math.PI / 2, 6);
    expect(rounded(orbitToPosition([0, 0, 0], { radius: 5, theta: Math.PI / 2, phi: Math.PI / 2 }))).toEqual(rounded([5, 0, 0]));
  });

  it('clamps phi away from the poles', () => {
    expect(clampPhi(-1)).toBeGreaterThan(0);
    expect(clampPhi(10)).toBeLessThan(Math.PI);
    expect(clampPhi(1)).toBe(1);
  });

  it('pans along the camera right and up axes', () => {
    // Camera on +Z looking at the origin: dragging right moves the target left, down moves it up.
    const o = { radius: 10, theta: 0, phi: Math.PI / 2 };
    const k = (2 * 10 * Math.tan((50 * Math.PI) / 360)) / 100;
    expect(rounded(panOffset(o, 50, 100, 10, 0))).toEqual(rounded([-10 * k, 0, 0]));
    expect(rounded(panOffset(o, 50, 100, 0, 10))).toEqual(rounded([0, 10 * k, 0]));
    // After a quarter turn the right axis is -Z.
    expect(rounded(panOffset({ ...o, theta: Math.PI / 2 }, 50, 100, 10, 0))).toEqual(rounded([0, 0, 10 * k]));
  });
});

let scene: Scene3D;
let canvas: HTMLCanvasElement;

beforeEach(async () => {
  vi.stubGlobal('requestAnimationFrame', () => 1);
  vi.stubGlobal('cancelAnimationFrame', () => {});
  const fake = createFake();
  canvas = document.createElement('canvas');
  document.body.appendChild(canvas);
  Object.defineProperty(canvas, 'clientHeight', { value: 100 });
  Object.defineProperty(canvas, 'clientWidth', { value: 200 });
  scene = new Scene3D(canvas, { filament: fake.module, fetchBytes: fakeFetch, camera: { position: [0, 0, 10], target: [0, 0, 0], fov: 60 } });
  await scene.ready;
});
afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

describe('OrbitController', () => {
  it('starts from the scene camera and writes the pose on the first update', () => {
    const c = new OrbitController(scene);
    c.update();
    expect(rounded(scene.camState.position)).toEqual(rounded([0, 0, 10]));
    c.dispose();
  });

  it('rotates with damping and settles', () => {
    const c = new OrbitController(scene);
    c.update();
    c.rotateOrPan(25, 0, false);
    const before = c.orbit.theta;
    c.update();
    expect(c.orbit.theta).toBeLessThan(before);
    for (let i = 0; i < 400; i++) c.update();
    const settled = c.orbit.theta;
    c.update();
    expect(c.orbit.theta).toBeCloseTo(settled, 4);
    // A quarter of the viewport height, total: dx/height * 2pi = 1.5708 rad.
    expect(settled).toBeCloseTo(-Math.PI / 2, 1);
    c.dispose();
  });

  it('zooms within min and max distance', () => {
    const c = new OrbitController(scene, () => ({ minDistance: 8, maxDistance: 11 }));
    c.zoom(-1);
    expect(c.orbit.radius).toBeCloseTo(9.5, 6);
    for (let i = 0; i < 20; i++) c.zoom(-1);
    expect(c.orbit.radius).toBe(8);
    for (let i = 0; i < 40; i++) c.zoom(1);
    expect(c.orbit.radius).toBe(11);
    c.dispose();
  });

  it('honours enableRotate, enablePan and enableZoom', () => {
    const c = new OrbitController(scene, () => ({ enableRotate: false, enablePan: false, enableZoom: false }));
    c.rotateOrPan(50, 50, false);
    c.rotateOrPan(50, 50, true);
    expect([c.dTheta, c.dPhi, ...c.target]).toEqual([0, 0, 0, 0, 0]);
    const wheel = new WheelEvent('wheel', { deltaY: -100, cancelable: true });
    canvas.dispatchEvent(wheel);
    expect(c.orbit.radius).toBe(10);
    c.dispose();
  });

  it('drives rotation from pointer events and zoom from the wheel', () => {
    const c = new OrbitController(scene);
    canvas.dispatchEvent(new MouseEvent('pointerdown', { clientX: 0, clientY: 0, button: 0 }));
    canvas.dispatchEvent(new MouseEvent('pointermove', { clientX: 10, clientY: 0 }));
    expect(c.dTheta).toBeCloseTo(-(2 * Math.PI * 10) / 100, 6);
    canvas.dispatchEvent(new MouseEvent('pointerup'));
    canvas.dispatchEvent(new MouseEvent('pointermove', { clientX: 99, clientY: 0 }));
    expect(c.dTheta).toBeCloseTo(-(2 * Math.PI * 10) / 100, 6);
    const wheel = new WheelEvent('wheel', { deltaY: 100, cancelable: true });
    canvas.dispatchEvent(wheel);
    expect(wheel.defaultPrevented).toBe(true);
    expect(c.orbit.radius).toBeCloseTo(10 / 0.95, 6);
    c.dispose();
  });

  it('pans with a shift-drag', () => {
    const c = new OrbitController(scene);
    canvas.dispatchEvent(new MouseEvent('pointerdown', { clientX: 0, clientY: 0, shiftKey: true }));
    canvas.dispatchEvent(new MouseEvent('pointermove', { clientX: 20, clientY: 0 }));
    expect(c.target[0]).toBeLessThan(0);
    c.dispose();
  });

  it('auto-rotates when asked', () => {
    const c = new OrbitController(scene, () => ({ autoRotate: true }));
    const t0 = c.orbit.theta;
    for (let i = 0; i < 60; i++) c.update();
    expect(c.orbit.theta).not.toBeCloseTo(t0, 3);
    c.dispose();
  });

  it('stops listening after dispose', () => {
    const c = new OrbitController(scene);
    c.dispose();
    canvas.dispatchEvent(new MouseEvent('pointerdown', { clientX: 0, clientY: 0 }));
    canvas.dispatchEvent(new MouseEvent('pointermove', { clientX: 50, clientY: 0 }));
    expect(c.dTheta).toBe(0);
  });
});

describe('<OrbitControls>', () => {
  it('attaches to the current scene and detaches when the scene is disposed', () => {
    setCurrentScene(scene);
    expect(OrbitControls({})).toBeNull();
    scene.tick(1);
    scene.dispose();
    canvas.dispatchEvent(new WheelEvent('wheel', { deltaY: 1, cancelable: true }));
  });

  it('throws outside a Canvas', () => {
    scene.dispose();
    setCurrentScene(undefined);
    expect(() => OrbitControls({})).toThrow(/inside a <Canvas>/);
  });
});
