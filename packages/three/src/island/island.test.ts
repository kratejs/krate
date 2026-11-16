import { createElement as h, render, type VNode } from 'preact';
import { useState } from 'preact/hooks';
import * as THREE from 'three';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createRoot, createSceneRoot, type Instance, type ReconcilerRoot, type RootStore } from '../engine/index.js';
import { context, createPortal, useFrame, useThree } from './index.js';

// The island build renders a scene with Preact through the engine's host:
// Preact creates SceneNodes (the scene namespace), and the island applies each
// element's props once Preact has diffed it.

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
afterEach(() => {
  for (const r of roots.splice(0)) r.unmount();
});

async function mount(vnode: VNode<any>) {
  const canvas = document.createElement('canvas');
  const root = createRoot(canvas);
  roots.push(root);
  await root.configure({ gl: fakeGL(canvas) as any, size: { width: 100, height: 100, top: 0, left: 0 }, frameloop: 'never' });
  let node: unknown;
  const store = root.render({
    __k3mount: (scene: Instance, s: RootStore) => {
      node = createSceneRoot(scene, s);
      render(h(context.Provider, { value: s }, vnode), node as Element);
      return () => render(null, node as Element);
    },
  });
  return { store, scene: store.getState().scene };
}

const flush = () => new Promise<void>((r) => setTimeout(r, 0));

describe('@krate/three in a Preact island', () => {
  it('builds objects from Preact elements, attaches children and passes the object to refs', async () => {
    let mesh: THREE.Mesh | null = null;
    const { scene } = await mount(
      h('group', { name: 'g', position: [1, 2, 3] }, h('mesh', { name: 'box', ref: (m: THREE.Mesh) => (mesh = m) }, h('boxGeometry', { args: [2, 2, 2] }), h('meshBasicMaterial', { color: 'red' }))),
    );
    const group = scene.getObjectByName('g')!;
    expect(group).toBeInstanceOf(THREE.Group);
    expect(group.position.toArray()).toEqual([1, 2, 3]);
    const box = scene.getObjectByName('box') as THREE.Mesh;
    expect(box.parent).toBe(group);
    expect(mesh).toBe(box);
    expect((box.geometry as THREE.BoxGeometry).parameters.width).toBe(2);
    expect((box.material as THREE.MeshBasicMaterial).color.getHexString()).toBe('ff0000');
  });

  it('updates props in place, adds and removes objects, and rebuilds on new args', async () => {
    let set!: (n: number) => void;
    const refs: unknown[] = [];
    function Scene() {
      const [n, setN] = useState(1);
      set = setN;
      return h(
        'group',
        { name: 'root' },
        h('mesh', { name: 'm', scale: n, ref: (o: unknown) => refs.push(o) }, h('sphereGeometry', { args: [n, 8, 8] })),
        n > 1 ? h('mesh', { name: 'extra' }) : null,
      );
    }
    const { scene } = await mount(h(Scene, {}));
    const m = scene.getObjectByName('m') as THREE.Mesh;
    const geometry = m.geometry;
    expect(scene.getObjectByName('extra')).toBeUndefined();
    set(2);
    await flush();
    expect(scene.getObjectByName('m')).toBe(m);
    expect(m.scale.x).toBe(2);
    expect(m.geometry).not.toBe(geometry);
    expect((m.geometry as THREE.SphereGeometry).parameters.radius).toBe(2);
    expect(scene.getObjectByName('extra')).toBeInstanceOf(THREE.Mesh);
    set(1);
    await flush();
    expect(scene.getObjectByName('extra')).toBeUndefined();
    expect(refs.filter(Boolean).every((o) => o === m)).toBe(true);
  });

  it('registers pointer handlers for raycasting', async () => {
    const { store, scene } = await mount(h('mesh', { name: 'hit', onClick: () => {} }));
    const hit = scene.getObjectByName('hit')!;
    expect(store.getState().internal.interaction).toContain(hit);
  });

  it('runs useThree and useFrame as hooks', async () => {
    const seen: number[] = [];
    const frames = vi.fn();
    function Probe() {
      const width = useThree((s) => s.size.width);
      useFrame(frames);
      seen.push(width);
      return h('group', { name: 'w' + width });
    }
    const { store, scene } = await mount(h(Probe, {}));
    expect(scene.getObjectByName('w100')).toBeDefined();
    store.getState().setSize(300, 100);
    await flush();
    expect(scene.getObjectByName('w300')).toBeDefined();
    expect(seen).toContain(300);
    store.getState().advance(1000);
    expect(frames).toHaveBeenCalled();
  });

  it('renders portals into another object', async () => {
    const target = new THREE.Scene();
    await mount(h('group', null, createPortal(h('mesh', { name: 'inside' }), target)));
    expect(target.getObjectByName('inside')).toBeInstanceOf(THREE.Mesh);
  });
});
