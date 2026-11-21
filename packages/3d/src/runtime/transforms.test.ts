import { describe, expect, it } from 'vitest';
import { composeTRS, eulerToQuat, mulMat4, STRIDE, TransformBuffer } from './transforms.js';
import type { FTransformManager } from './filament.js';

const rounded = (a: ArrayLike<number>, d = 5): number[] => Array.from(a, (v) => Number(v.toFixed(d)) + 0);

function fakeTcm() {
  const log: string[] = [];
  const set: Array<{ id: number; m: number[] }> = [];
  const tcm = {
    openLocalTransformTransaction: () => log.push('open'),
    commitLocalTransformTransaction: () => log.push('commit'),
    setTransform: (i: { id: number }, m: ArrayLike<number>) => set.push({ id: i.id, m: Array.from(m) }),
  } as unknown as FTransformManager;
  return { tcm, log, set };
}

describe('eulerToQuat', () => {
  it('is identity for zero rotation', () => {
    const q = new Float32Array(4);
    eulerToQuat(q, 0, 0, 0, 0);
    expect(rounded(q)).toEqual(rounded([0, 0, 0, 1]));
  });

  it('matches known single-axis quaternions', () => {
    const q = new Float32Array(4);
    eulerToQuat(q, 0, Math.PI / 2, 0, 0);
    expect(rounded(q)).toEqual(rounded([Math.SQRT1_2, 0, 0, Math.SQRT1_2]));
    eulerToQuat(q, 0, 0, Math.PI, 0);
    expect(rounded(q)).toEqual(rounded([0, 1, 0, 0]));
    eulerToQuat(q, 0, 0, 0, Math.PI / 2);
    expect(rounded(q)).toEqual(rounded([0, 0, Math.SQRT1_2, Math.SQRT1_2]));
  });

  it('is the product Qx * Qy * Qz (Three.js XYZ order)', () => {
    const mul = (a: number[], b: number[]): number[] => [
      a[3] * b[0] + a[0] * b[3] + a[1] * b[2] - a[2] * b[1],
      a[3] * b[1] - a[0] * b[2] + a[1] * b[3] + a[2] * b[0],
      a[3] * b[2] + a[0] * b[1] - a[1] * b[0] + a[2] * b[3],
      a[3] * b[3] - a[0] * b[0] - a[1] * b[1] - a[2] * b[2],
    ];
    const axis = (i: number, angle: number): number[] => {
      const q = [0, 0, 0, Math.cos(angle / 2)];
      q[i] = Math.sin(angle / 2);
      return q;
    };
    const q = new Float32Array(4);
    eulerToQuat(q, 0, 0.3, 0.5, 0.7);
    expect(rounded(q)).toEqual(rounded(mul(mul(axis(0, 0.3), axis(1, 0.5)), axis(2, 0.7))));
    expect(q[0]).toBeCloseTo(0.2199, 3);
  });

  it('writes at the given offset', () => {
    const q = new Float32Array(8);
    eulerToQuat(q, 4, 0, 0, 0);
    expect(Array.from(q)).toEqual([0, 0, 0, 0, 0, 0, 0, 1]);
  });
});

describe('composeTRS and mulMat4', () => {
  it('composes translation, rotation and scale', () => {
    const data = new Float32Array([1, 2, 3, 0, 0, Math.SQRT1_2, Math.SQRT1_2, 2, 2, 2]);
    const m = new Float32Array(16);
    composeTRS(m, data, 0);
    // 90 degrees about Z scaled by 2: x axis to y axis.
    expect(rounded(m)).toEqual(rounded([0, 2, 0, 0, -2, 0, 0, 0, 0, 0, 2, 0, 1, 2, 3, 1]));
  });

  it('multiplies parent * local in column-major order', () => {
    const parent = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 10, 0, 0, 1]);
    const local = Float32Array.from([2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 1, 0, 0, 1]);
    const out = new Float32Array(16);
    mulMat4(out, parent, 0, local);
    // The local translation (1,0,0) is scaled by nothing in the parent and offset by 10.
    expect(rounded(out)).toEqual(rounded([2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 11, 0, 0, 1]));
    // Reversed order scales the parent translation instead.
    mulMat4(out, local, 0, parent);
    expect(rounded(out)).toEqual(rounded([2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 21, 0, 0, 1]));
  });
});

describe('TransformBuffer', () => {
  it('starts every slot at identity with a clean mask', () => {
    const t = new TransformBuffer(40);
    expect(t.data).toHaveLength(40 * STRIDE);
    expect(t.mask).toHaveLength(2);
    expect(Array.from(t.mask)).toEqual([0, 0]);
    expect(rounded(t.data.subarray(STRIDE * 39, STRIDE * 40))).toEqual(rounded([0, 0, 0, 0, 0, 0, 1, 1, 1, 1]));
  });

  it('flushes only dirty slots in one transaction and clears the mask', () => {
    const t = new TransformBuffer(70);
    const { tcm, log, set } = fakeTcm();
    for (const s of [0, 31, 32, 69]) t.instances[s] = { id: s } as never;
    t.pos(31, 1, 2, 3);
    t.scale(69, 2, 2, 2);
    t.rot(0, 0, 0, 0);
    expect(t.isDirty(31)).toBe(true);
    expect(t.isDirty(32)).toBe(false);
    expect(t.flush(tcm)).toBe(3);
    expect(log).toEqual(['open', 'commit']);
    expect(set.map((s) => s.id).sort((a, b) => a - b)).toEqual([0, 31, 69]);
    expect(Array.from(t.mask)).toEqual([0, 0, 0]);
    // Nothing dirty: no transaction at all.
    log.length = 0;
    expect(t.flush(tcm)).toBe(0);
    expect(log).toEqual([]);
  });

  it('composes parent * T * R * S', () => {
    const t = new TransformBuffer(1);
    const { tcm, set } = fakeTcm();
    t.instances[0] = { id: 0 } as never;
    // Parent translates by (0, 1, 0); the slot is at (2, 0, 0).
    t.setParent(0, [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 1]);
    t.pos(0, 2, 0, 0);
    t.flush(tcm);
    expect(rounded(set[0].m)).toEqual(rounded([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 1, 0, 1]));
  });

  it('skips slots without an entity yet, so the caller can re-mark them', () => {
    const t = new TransformBuffer(2);
    const { tcm, log } = fakeTcm();
    t.pos(1, 1, 1, 1);
    expect(t.flush(tcm)).toBe(0);
    expect(log).toEqual([]);
    expect(t.isDirty(1)).toBe(false);
  });

  it('grows without losing data', () => {
    const t = new TransformBuffer(1);
    t.pos(0, 5, 6, 7);
    t.ensure(100);
    expect(Array.from(t.data.subarray(0, 3))).toEqual([5, 6, 7]);
    expect(t.isDirty(0)).toBe(true);
    expect(rounded(t.data.subarray(STRIDE * 99, STRIDE * 99 + 10))).toEqual(rounded([0, 0, 0, 0, 0, 0, 1, 1, 1, 1]));
  });
});
