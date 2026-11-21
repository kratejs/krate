// Transform buffer: one Float32Array of [px,py,pz,qx,qy,qz,qw,sx,sy,sz] per
// dynamic slot plus a dirty bitmask. flush() composes parent * T * R * S for
// the dirty slots only, without allocating.

import type { FInstance, FTransformManager } from './filament.js';

export const STRIDE = 10;

/** Euler XYZ (radians, Three.js order) to a unit quaternion written at out[o..o+3]. */
export function eulerToQuat(out: Float32Array, o: number, x: number, y: number, z: number): void {
  const c1 = Math.cos(x / 2);
  const c2 = Math.cos(y / 2);
  const c3 = Math.cos(z / 2);
  const s1 = Math.sin(x / 2);
  const s2 = Math.sin(y / 2);
  const s3 = Math.sin(z / 2);
  out[o] = s1 * c2 * c3 + c1 * s2 * s3;
  out[o + 1] = c1 * s2 * c3 - s1 * c2 * s3;
  out[o + 2] = c1 * c2 * s3 + s1 * s2 * c3;
  out[o + 3] = c1 * c2 * c3 - s1 * s2 * s3;
}

/** Column-major T * R * S from a slot at data[o..o+9] into m. */
export function composeTRS(m: Float32Array, data: Float32Array, o: number): void {
  const x = data[o + 3];
  const y = data[o + 4];
  const z = data[o + 5];
  const w = data[o + 6];
  const sx = data[o + 7];
  const sy = data[o + 8];
  const sz = data[o + 9];
  const x2 = x + x;
  const y2 = y + y;
  const z2 = z + z;
  const xx = x * x2;
  const xy = x * y2;
  const xz = x * z2;
  const yy = y * y2;
  const yz = y * z2;
  const zz = z * z2;
  const wx = w * x2;
  const wy = w * y2;
  const wz = w * z2;
  m[0] = (1 - (yy + zz)) * sx;
  m[1] = (xy + wz) * sx;
  m[2] = (xz - wy) * sx;
  m[3] = 0;
  m[4] = (xy - wz) * sy;
  m[5] = (1 - (xx + zz)) * sy;
  m[6] = (yz + wx) * sy;
  m[7] = 0;
  m[8] = (xz + wy) * sz;
  m[9] = (yz - wx) * sz;
  m[10] = (1 - (xx + yy)) * sz;
  m[11] = 0;
  m[12] = data[o];
  m[13] = data[o + 1];
  m[14] = data[o + 2];
  m[15] = 1;
}

/** out = a(at offset ao) * b, column-major. out must not alias a or b. */
export function mulMat4(out: Float32Array, a: Float32Array, ao: number, b: Float32Array): void {
  for (let c = 0; c < 4; c++) {
    const b0 = b[c * 4];
    const b1 = b[c * 4 + 1];
    const b2 = b[c * 4 + 2];
    const b3 = b[c * 4 + 3];
    for (let r = 0; r < 4; r++) {
      out[c * 4 + r] = a[ao + r] * b0 + a[ao + 4 + r] * b1 + a[ao + 8 + r] * b2 + a[ao + 12 + r] * b3;
    }
  }
}

const IDENTITY = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];

export class TransformBuffer {
  data: Float32Array;
  mask: Uint32Array;
  parents: Float32Array;
  instances: (FInstance | undefined)[] = [];
  private local = new Float32Array(16);
  private world = new Float32Array(16);
  private slots = 0;

  constructor(slots = 0) {
    this.data = new Float32Array(0);
    this.mask = new Uint32Array(0);
    this.parents = new Float32Array(0);
    this.ensure(slots);
  }

  get capacity(): number {
    return this.slots;
  }

  /** Grows the buffers to hold at least n slots; new slots are identity. */
  ensure(n: number): void {
    if (n <= this.slots) return;
    const data = new Float32Array(n * STRIDE);
    data.set(this.data);
    const parents = new Float32Array(n * 16);
    parents.set(this.parents);
    for (let s = this.slots; s < n; s++) {
      data[s * STRIDE + 6] = 1;
      data[s * STRIDE + 7] = data[s * STRIDE + 8] = data[s * STRIDE + 9] = 1;
      parents.set(IDENTITY, s * 16);
    }
    const mask = new Uint32Array(Math.ceil(n / 32));
    mask.set(this.mask);
    this.data = data;
    this.parents = parents;
    this.mask = mask;
    this.slots = n;
  }

  markDirty(slot: number): void {
    this.mask[slot >> 5] |= 1 << (slot & 31);
  }

  isDirty(slot: number): boolean {
    return (this.mask[slot >> 5] & (1 << (slot & 31))) !== 0;
  }

  pos(slot: number, x: number, y: number, z: number): void {
    const o = slot * STRIDE;
    this.data[o] = x;
    this.data[o + 1] = y;
    this.data[o + 2] = z;
    this.markDirty(slot);
  }

  rot(slot: number, x: number, y: number, z: number): void {
    eulerToQuat(this.data, slot * STRIDE + 3, x, y, z);
    this.markDirty(slot);
  }

  scale(slot: number, x: number, y: number, z: number): void {
    const o = slot * STRIDE + 7;
    this.data[o] = x;
    this.data[o + 1] = y;
    this.data[o + 2] = z;
    this.markDirty(slot);
  }

  setParent(slot: number, matrix: ArrayLike<number>): void {
    this.parents.set(matrix, slot * 16);
    this.markDirty(slot);
  }

  /** Writes every dirty slot's world matrix to the TransformManager in one transaction. */
  flush(tcm: FTransformManager): number {
    const { mask, data, parents, instances, local, world } = this;
    let n = 0;
    let open = false;
    for (let w = 0; w < mask.length; w++) {
      let bits = mask[w];
      if (bits === 0) continue;
      mask[w] = 0;
      while (bits !== 0) {
        const low = bits & -bits;
        bits ^= low;
        const slot = w * 32 + (31 - Math.clz32(low));
        const inst = instances[slot];
        if (!inst) {
          // Entity not built yet; it re-marks the slot when it appears.
          continue;
        }
        if (!open) {
          tcm.openLocalTransformTransaction();
          open = true;
        }
        composeTRS(local, data, slot * STRIDE);
        mulMat4(world, parents, slot * 16, local);
        tcm.setTransform(inst, world);
        n++;
      }
    }
    if (open) tcm.commitLocalTransformTransaction();
    return n;
  }
}
