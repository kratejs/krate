import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { DreiOrbitControls, forwardProps, lightformerColor, lightformerTarget, setEnvProps } from './drei.js';

describe('@krate/three/drei engine helpers', () => {
  it('zooms a fixed step per wheel event, as drei (three-stdlib) does', () => {
    const controls = new DreiOrbitControls(new THREE.PerspectiveCamera());
    expect(controls._getZoomScale()).toBeCloseTo(0.95);
    controls.zoomSpeed = 2;
    expect(controls._getZoomScale()).toBeCloseTo(0.9025);
  });

  it('does not capture the pointer on press, so a click reaches the canvas', () => {
    const el = document.createElement('div');
    document.body.appendChild(el);
    const captured: number[] = [];
    el.setPointerCapture = (id: number) => void captured.push(id);
    const controls = new DreiOrbitControls(new THREE.PerspectiveCamera(), el);
    el.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 7, button: 0, pointerType: 'mouse', clientX: 1, clientY: 1 }));
    expect(captured).toEqual([]);
    expect(Object.prototype.hasOwnProperty.call(el, 'setPointerCapture')).toBe(true);
    controls.dispose();
    el.remove();
  });

  it('forwards props as thunks, events and refs as they are', () => {
    const onClick = () => {};
    const ref = { current: null };
    const props = { get position() { return [1, 2, 3]; }, onClick, ref, makeDefault: true, children: [] };
    const out = forwardProps(props, ['makeDefault']);
    expect(Object.keys(out).sort()).toEqual(['onClick', 'position', 'ref']);
    expect(out.onClick).toBe(onClick);
    expect(out.ref).toBe(ref);
    expect((out.position as () => unknown)()).toEqual([1, 2, 3]);
  });

  it('sets and restores the scene environment', () => {
    const scene = new THREE.Scene();
    const texture = new THREE.Texture();
    const restore = setEnvProps(false, undefined, scene, texture, { environmentIntensity: 2 });
    expect(scene.environment).toBe(texture);
    expect(scene.background).toBeNull();
    expect(scene.environmentIntensity).toBe(2);
    restore();
    expect(scene.environment).toBeNull();
    expect(scene.environmentIntensity).toBe(1);
  });

  it('gives a Lightformer its color times intensity and faces it at the origin', () => {
    const mesh = new THREE.Mesh(new THREE.PlaneGeometry(), new THREE.MeshBasicMaterial());
    mesh.position.set(0, 0, 5);
    mesh.rotation.x = 1;
    lightformerColor(mesh, 'white', 2.5);
    expect((mesh.material as THREE.MeshBasicMaterial).color.r).toBeCloseTo(2.5);
    lightformerTarget(mesh, [0, 0, 0], false);
    // Its +z axis points at the target (three's lookAt for non-cameras).
    const facing = new THREE.Vector3(0, 0, 1).applyQuaternion(mesh.quaternion);
    expect(facing.z).toBeCloseTo(-1);
    mesh.rotation.x = 1;
    lightformerTarget(mesh, [0, 0, 0], true);
    expect(mesh.rotation.x).toBe(1);
  });
});
