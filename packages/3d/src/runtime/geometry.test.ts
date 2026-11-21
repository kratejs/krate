import { describe, expect, it } from 'vitest';
import { boundsOf, buildBox, buildGeometry, buildPlane, buildSphere, type MeshData } from './geometry.js';

function expectValid(d: MeshData): void {
  const n = d.positions.length / 3;
  expect(d.normals).toHaveLength(n * 3);
  expect(d.uvs).toHaveLength(n * 2);
  expect(d.indices.length % 3).toBe(0);
  for (const i of d.indices) expect(i).toBeLessThan(n);
  for (let i = 0; i < n; i++) {
    expect(Math.hypot(d.normals[i * 3], d.normals[i * 3 + 1], d.normals[i * 3 + 2])).toBeCloseTo(1, 5);
  }
}

describe('procedural geometry', () => {
  it('box defaults to 1x1x1 with 24 vertices and 12 triangles', () => {
    const d = buildBox();
    expect(d.positions.length / 3).toBe(24);
    expect(d.indices.length / 3).toBe(12);
    expectValid(d);
    expect(boundsOf(d.positions)).toEqual({ center: [0, 0, 0], halfExtent: [0.5, 0.5, 0.5] });
  });

  it('box honours width, height and depth', () => {
    const b = boundsOf(buildBox(2, 4, 6).positions);
    expect(b.halfExtent).toEqual([1, 2, 3]);
  });

  it('box faces wind counter-clockwise outward', () => {
    const d = buildBox();
    for (let t = 0; t < d.indices.length; t += 3) {
      const [a, b, c] = [d.indices[t], d.indices[t + 1], d.indices[t + 2]].map((i) => Array.from(d.positions.subarray(i * 3, i * 3 + 3)));
      const e1 = b.map((v, i) => v - a[i]);
      const e2 = c.map((v, i) => v - a[i]);
      const n = [e1[1] * e2[2] - e1[2] * e2[1], e1[2] * e2[0] - e1[0] * e2[2], e1[0] * e2[1] - e1[1] * e2[0]];
      const vn = Array.from(d.normals.subarray(d.indices[t] * 3, d.indices[t] * 3 + 3));
      expect(n[0] * vn[0] + n[1] * vn[1] + n[2] * vn[2]).toBeGreaterThan(0);
    }
  });

  it('sphere defaults to radius 1, 32x16 segments', () => {
    const d = buildSphere();
    expect(d.positions.length / 3).toBe(33 * 17);
    // Pole rows contribute one triangle per segment instead of two.
    expect(d.indices.length / 3).toBe(32 * 16 * 2 - 64);
    expectValid(d);
    for (let i = 0; i < d.positions.length; i += 3) {
      expect(Math.hypot(d.positions[i], d.positions[i + 1], d.positions[i + 2])).toBeCloseTo(1, 5);
    }
  });

  it('sphere scales with radius and clamps segment counts like Three.js', () => {
    const d = buildSphere(3, 1, 1);
    expect(d.positions.length / 3).toBe(4 * 3);
    expect(boundsOf(d.positions).halfExtent[1]).toBeCloseTo(3, 5);
  });

  it('plane faces +Z in the XY plane', () => {
    const d = buildPlane(2, 4, 2, 2);
    expect(d.positions.length / 3).toBe(9);
    expect(d.indices.length / 3).toBe(8);
    expectValid(d);
    expect(boundsOf(d.positions)).toEqual({ center: [0, 0, 0], halfExtent: [1, 2, 0] });
    expect(Array.from(d.normals.subarray(0, 3))).toEqual([0, 0, 1]);
  });

  it('plane defaults to 1x1', () => {
    expect(boundsOf(buildPlane().positions).halfExtent).toEqual([0.5, 0.5, 0]);
  });

  it('uses 32-bit indices only when 16 bits cannot address the vertices', () => {
    expect(buildSphere().indices).toBeInstanceOf(Uint16Array);
    expect(buildPlane(1, 1, 300, 300).indices).toBeInstanceOf(Uint32Array);
  });

  it('dispatches by kind and rejects unknown kinds', () => {
    expect(buildGeometry('box', [1, 2, 3]).positions).toHaveLength(72);
    expect(() => buildGeometry('torus' as never)).toThrow(/unknown geometry/);
  });
});
