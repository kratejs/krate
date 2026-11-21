// Position and direction of a light whose position or target is reactive.
// Mirrors what the compiler computes at build time for constant values.

import type { LightHandle, Scene3D } from './scene.js';

type Vec3 = readonly number[];

function transformPoint(m: ArrayLike<number>, p: Vec3): number[] {
  const [x, y, z] = p;
  return [m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14]];
}

function direction(from: number[], to: number[]): number[] {
  const d = [to[0] - from[0], to[1] - from[1], to[2] - from[2]];
  const l = Math.hypot(d[0], d[1], d[2]) || 1;
  return [d[0] / l, d[1] / l, d[2] / l];
}

/**
 * Applies a light's world position and, for directional and spot lights, the
 * direction toward `target`. A directional light without a target aims at the
 * world origin; a spot light without one aims at the parent's origin. `parent`
 * is the static parent matrix of the enclosing groups, if any.
 */
export function aimLight(
  scene: Scene3D,
  light: LightHandle,
  kind: 'directional' | 'point' | 'spot',
  position: Vec3,
  target?: Vec3 | null,
  parent?: ArrayLike<number> | null
): void {
  const p = parent ? transformPoint(parent, position) : [position[0], position[1], position[2]];
  scene.setLight(light, 'position', p);
  if (kind === 'point') return;
  let t: number[];
  if (target) t = parent ? transformPoint(parent, target) : [target[0], target[1], target[2]];
  else t = kind === 'spot' && parent ? transformPoint(parent, [0, 0, 0]) : [0, 0, 0];
  scene.setLight(light, 'direction', direction(p, t));
}
