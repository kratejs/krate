import * as THREE from 'three';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CompiledScene, type Builder } from './compiled.js';
import { act, createPointerEvents, createRoot, type ReconcilerRoot } from './index.js';

const h = (tag: unknown, props: Record<string, unknown> | null, ...children: unknown[]): unknown =>
  (globalThis as unknown as { h: (...a: unknown[]) => unknown }).h(tag, props, ...children);
const g = globalThis as unknown as {
  createSignal<T>(v: T): [() => T, (v: T) => void];
  krateFlushSync(): void;
};

function fakeGL(canvas: HTMLCanvasElement, extra: Record<string, unknown> = {}) {
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
    ...extra,
  };
}

const roots: Array<ReconcilerRoot<HTMLCanvasElement>> = [];

async function mount(build: (t: Builder) => void, glExtra: Record<string, unknown> = {}, top = true) {
  const canvas = document.createElement('canvas');
  const gl = fakeGL(canvas, glExtra);
  const root = createRoot(canvas);
  roots.push(root);
  await root.configure({ gl: gl as any, size: { width: 100, height: 100, top: 0, left: 0 }, frameloop: 'never', events: createPointerEvents });
  const store = root.render(() => [h(CompiledScene, { top, build })]);
  await act(() => {});
  return { store, gl, scene: store.getState().scene };
}

afterEach(async () => {
  for (const r of roots.splice(0)) r.unmount();
  await act(() => {});
});

describe('compiled scenes', () => {
  it('shares geometry and material and mounts under the scene', async () => {
    const { scene } = await mount((t) => {
      const geo = t.geo('box', [1, 2, 3]);
      const mat = t.mat('meshStandardMaterial', { color: 'orange', roughness: 0.4 });
      expect(t.geo('box', [1, 2, 3])).toBe(geo);
      expect(t.mat('meshStandardMaterial', { color: 'orange', roughness: 0.4 })).toBe(mat);
      t.mesh(t.root, geo, mat, true, undefined, { name: 'a' });
      t.mesh(t.root, geo, mat, true, undefined, { name: 'b' });
    });
    const a = scene.getObjectByName('a') as THREE.Mesh;
    const b = scene.getObjectByName('b') as THREE.Mesh;
    expect(a.geometry).toBe(b.geometry);
    expect(a.material).toBe(b.material);
    expect((a.geometry as THREE.BoxGeometry).parameters.depth).toBe(3);
    expect((a.material as THREE.MeshStandardMaterial).color.getHexString()).toBe('ffa500');
    expect(a.parent!.parent).toBe(scene);
  });

  it('installs static matrices and stops three from recomputing them', async () => {
    const m = [2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 5, 6, 7, 1];
    const { scene } = await mount((t) => {
      const geo = t.geo('box');
      const mat = t.mat('meshBasicMaterial');
      const group = t.group(t.root, m, undefined, { name: 'g', position: [5, 6, 7], scale: [2, 2, 2] });
      t.mesh(group, geo, mat, true, [2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 5, 6, 7, 1], { name: 'child' });
    });
    const group = scene.getObjectByName('g')!;
    const child = scene.getObjectByName('child')!;
    expect(group.matrixAutoUpdate).toBe(false);
    expect(group.matrixWorldAutoUpdate).toBe(false);
    expect(group.position.toArray()).toEqual([5, 6, 7]);
    scene.updateMatrixWorld();
    expect(group.matrix.elements).toEqual(m);
    expect(child.matrixWorld.elements[12]).toBe(5);
    expect(child.matrixWorld.elements[0]).toBe(2);
    // updateMatrix would overwrite it: it is not called.
    group.position.set(0, 0, 0);
    scene.updateMatrixWorld();
    expect(group.matrix.elements[12]).toBe(5);
  });

  it('keeps matrixWorld automatic when something above can move', async () => {
    const { scene } = await mount(
      (t) => {
        t.mesh(t.root, t.geo('box'), t.mat('meshBasicMaterial'), [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1], undefined, { name: 'm' });
      },
      {},
      false,
    );
    const m = scene.getObjectByName('m')!;
    expect(m.matrixAutoUpdate).toBe(false);
    expect(m.matrixWorldAutoUpdate).toBe(true);
  });

  it('builds an InstancedMesh from a Float32Array and disposes it', async () => {
    const { scene } = await mount((t) => {
      const mats = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 0, 0, 1]);
      t.instances(t.root, t.geo('box'), t.mat('meshBasicMaterial'), mats, { castShadow: true });
    });
    const inst = scene.children[0].children[0] as THREE.InstancedMesh;
    expect(inst.isInstancedMesh).toBe(true);
    expect(inst.count).toBe(2);
    expect(inst.castShadow).toBe(true);
    const m = new THREE.Matrix4();
    inst.getMatrixAt(1, m);
    expect(m.elements[12]).toBe(2);
  });

  it('writes reactive transforms directly and invalidates', async () => {
    const [angle, setAngle] = g.createSignal(0);
    const { scene, store } = await mount((t) => {
      const o = t.group(t.root, null, null, { name: 'o' });
      t.fx(() => o.rotation.set(0, angle(), 0));
    });
    const o = scene.getObjectByName('o')!;
    expect(o.rotation.y).toBe(0);
    const invalidate = vi.spyOn(store.getState(), 'invalidate');
    setAngle(1.25);
    g.krateFlushSync();
    expect(o.rotation.y).toBe(1.25);
    expect(o.matrixAutoUpdate).toBe(true);
    expect(invalidate).toHaveBeenCalled();
  });

  it('writes instance matrices in place and uploads only the dirty range', async () => {
    const [a, setA] = g.createSignal(0);
    let before: Float32Array | undefined;
    let inst: THREE.InstancedMesh | undefined;
    const { scene } = await mount((t) => {
      const w = t.dynamicInstances(t.root, t.geo('box'), t.mat('meshBasicMaterial'), 4, null);
      inst = w.mesh;
      before = inst.instanceMatrix.array as Float32Array;
      t.fx(() => w.setv(2, 1, 2, 3, 0, a(), 0, 1, 1, 1));
      for (const i of [0, 1, 3]) w.setv(i, i, 0, 0, 0, 0, 0, 1, 1, 1);
    });
    const mesh = scene.children[0].children[0] as THREE.InstancedMesh;
    expect(mesh).toBe(inst);
    expect(mesh.frustumCulled).toBe(false);
    // The same array is written (no reallocation), with the rotation about y.
    expect(mesh.instanceMatrix.array).toBe(before);
    const m = new THREE.Matrix4();
    setA(0.5);
    g.krateFlushSync();
    mesh.getMatrixAt(2, m);
    expect(m.elements[0]).toBeCloseTo(Math.cos(0.5));
    expect(m.elements[12]).toBe(1);
    // The render hook flags only the written range.
    const attr = mesh.instanceMatrix;
    const version = attr.version;
    mesh.onBeforeRender({} as never, {} as never, {} as never, {} as never, {} as never, {} as never);
    expect(attr.version).toBeGreaterThan(version);
    const range = (attr as any).updateRanges?.[0] ?? (attr as any).updateRange;
    expect(range.count).toBe(16 * 4);
    mesh.onBeforeRender({} as never, {} as never, {} as never, {} as never, {} as never, {} as never);
    setA(0.75);
    g.krateFlushSync();
    mesh.onBeforeRender({} as never, {} as never, {} as never, {} as never, {} as never, {} as never);
    const narrow = (attr as any).updateRanges?.[0] ?? (attr as any).updateRange;
    expect(narrow.count).toBe(16);
    expect(narrow.start ?? narrow.offset).toBe(32);
  });

  it('composes instance matrices against the parent matrix', async () => {
    let w: ReturnType<Builder['dynamicInstances']> | undefined;
    await mount((t) => {
      w = t.dynamicInstances(t.root, t.geo('box'), t.mat('meshBasicMaterial'), 1, [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 0, 0, 1]);
      w.set(0, [1, 0, 0], null, 2);
    });
    const m = new THREE.Matrix4();
    w!.mesh.getMatrixAt(0, m);
    expect(m.elements[12]).toBe(11);
    expect(m.elements[0]).toBe(2);
  });

  it('routes pointer events to meshes with handlers and removes them on unmount', async () => {
    const onClick = vi.fn();
    const { scene, store } = await mount((t) => {
      t.mesh(t.root, t.geo('box'), t.mat('meshBasicMaterial'), true, undefined, { name: 'btn' }, { onClick });
    });
    const mesh = scene.getObjectByName('btn')!;
    const state = store.getState();
    expect(state.internal.interaction).toContain(mesh);
    expect((mesh as any).__r3f.eventCount).toBe(1);
    expect((mesh as any).__r3f.handlers.onClick).toBe(onClick);
    roots.splice(0)[0].unmount();
    await act(() => {});
    expect(state.internal.interaction).not.toContain(mesh);
  });

  it('warms the shaders up with compileAsync, or compile when it is missing', async () => {
    const compileAsync = vi.fn(() => Promise.resolve());
    await mount((t) => void t.mesh(t.root, t.geo('box'), t.mat('meshBasicMaterial'), true), { compileAsync });
    await Promise.resolve();
    expect(compileAsync).toHaveBeenCalledTimes(1);

    const compile = vi.fn();
    await mount((t) => void t.mesh(t.root, t.geo('box'), t.mat('meshBasicMaterial'), true), { compile });
    await Promise.resolve();
    expect(compile).toHaveBeenCalledTimes(1);
  });
});
