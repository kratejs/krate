// Orbit, zoom and pan camera controls on the scene camera's lookAt. No
// three.js dependency; the damping model follows three's OrbitControls.

import type { OrbitControlsProps, Vec3 } from '../types.js';
import { getCurrentScene } from './context.js';
import type { Scene3D } from './scene.js';

export interface Orbit {
  radius: number;
  /** Azimuth around +Y, from +Z. */
  theta: number;
  /** Polar angle from +Y. */
  phi: number;
}

const EPS = 1e-6;

export function orbitFromPosition(position: readonly number[], target: readonly number[]): Orbit {
  const x = position[0] - target[0];
  const y = position[1] - target[1];
  const z = position[2] - target[2];
  const radius = Math.hypot(x, y, z) || EPS;
  return { radius, theta: Math.atan2(x, z), phi: Math.acos(Math.min(1, Math.max(-1, y / radius))) };
}

export function orbitToPosition(target: readonly number[], o: Orbit): Vec3 {
  const s = Math.sin(o.phi) * o.radius;
  return [target[0] + s * Math.sin(o.theta), target[1] + Math.cos(o.phi) * o.radius, target[2] + s * Math.cos(o.theta)];
}

export function clampPhi(phi: number): number {
  return Math.min(Math.PI - EPS, Math.max(EPS, phi));
}

/** World-space pan offset for a screen drag of (dx, dy) pixels. */
export function panOffset(o: Orbit, fovDeg: number, viewportHeight: number, dx: number, dy: number): Vec3 {
  const k = (2 * o.radius * Math.tan((fovDeg * Math.PI) / 360)) / Math.max(1, viewportHeight);
  // Camera right = (cos theta, 0, -sin theta); camera up in the orbit frame.
  const right: Vec3 = [Math.cos(o.theta), 0, -Math.sin(o.theta)];
  const up: Vec3 = [
    -Math.cos(o.phi) * Math.sin(o.theta),
    Math.sin(o.phi),
    -Math.cos(o.phi) * Math.cos(o.theta),
  ];
  return [
    (-dx * right[0] + dy * up[0]) * k,
    (-dx * right[1] + dy * up[1]) * k,
    (-dx * right[2] + dy * up[2]) * k,
  ];
}

export class OrbitController {
  orbit: Orbit;
  target: number[];
  dTheta = 0;
  dPhi = 0;
  damping = 0.05;
  private dirty = true;
  private drag: { x: number; y: number; pan: boolean } | null = null;
  private off: Array<() => void> = [];

  constructor(
    private scene: Scene3D,
    private props: () => OrbitControlsProps = () => ({})
  ) {
    const cam = scene.camState;
    this.target = [...(props().target ?? cam.target)];
    this.orbit = orbitFromPosition(cam.position, this.target);
    const el = scene.canvas;
    const on = <K extends keyof HTMLElementEventMap>(t: K, fn: (e: HTMLElementEventMap[K]) => void): void => {
      el.addEventListener(t, fn as EventListener);
      this.off.push(() => el.removeEventListener(t, fn as EventListener));
    };
    on('pointerdown', (e) => {
      this.drag = { x: e.clientX, y: e.clientY, pan: e.button === 2 || e.shiftKey };
      el.setPointerCapture?.(e.pointerId);
    });
    on('pointermove', (e) => {
      if (!this.drag) return;
      this.rotateOrPan(e.clientX - this.drag.x, e.clientY - this.drag.y, this.drag.pan);
      this.drag.x = e.clientX;
      this.drag.y = e.clientY;
    });
    on('pointerup', () => (this.drag = null));
    on('wheel', (e) => {
      if (this.props().enableZoom === false) return;
      e.preventDefault();
      this.zoom(e.deltaY);
    });
    on('contextmenu', (e) => e.preventDefault());
    this.off.push(scene.onFrame(() => this.update()));
  }

  rotateOrPan(dx: number, dy: number, pan: boolean): void {
    const p = this.props();
    const h = Math.max(1, this.scene.canvas.clientHeight);
    if (pan) {
      if (p.enablePan === false) return;
      const d = panOffset(this.orbit, this.scene.camState.fov, h, dx, dy);
      for (let i = 0; i < 3; i++) this.target[i] += d[i];
      this.dirty = true;
    } else if (p.enableRotate !== false) {
      this.dTheta -= (2 * Math.PI * dx) / h;
      this.dPhi -= (2 * Math.PI * dy) / h;
    }
  }

  zoom(deltaY: number): void {
    const p = this.props();
    const scale = deltaY > 0 ? 1 / 0.95 : 0.95;
    this.dirty = true;
    this.orbit.radius = Math.min(p.maxDistance ?? Infinity, Math.max(p.minDistance ?? 0, this.orbit.radius * scale));
  }

  /** Advances damping and auto-rotation, then pushes the pose to the camera. */
  update(): void {
    const p = this.props();
    const o = this.orbit;
    if (p.target) {
      this.target = [...p.target];
      this.dirty = true;
    }
    const idle = !this.dirty && !p.autoRotate && Math.abs(this.dTheta) < 1e-5 && Math.abs(this.dPhi) < 1e-5;
    if (idle) return;
    this.dirty = false;
    if (p.autoRotate) this.dTheta -= ((2 * Math.PI) / 60 / 60) * (p.autoRotateSpeed ?? 2) / this.damping;
    o.theta += this.dTheta * this.damping;
    o.phi = clampPhi(o.phi + this.dPhi * this.damping);
    this.dTheta *= 1 - this.damping;
    this.dPhi *= 1 - this.damping;
    this.scene.setCamera({ position: orbitToPosition(this.target, o), target: [...this.target] });
  }

  dispose(): void {
    for (const f of this.off.splice(0)) f();
  }
}

/** Attaches orbit controls to the current scene. Usable as `<OrbitControls />` inside a Canvas. */
export function OrbitControls(props: OrbitControlsProps = {}): null {
  const scene = getCurrentScene();
  if (!scene) throw new Error('@krate/3d: <OrbitControls> must be used inside a <Canvas>');
  const controller = new OrbitController(scene, () => props);
  scene.track(() => controller.dispose());
  return null;
}
