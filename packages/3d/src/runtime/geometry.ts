// Procedural geometry (Three.js layout and defaults) and its Filament buffers.

import type { FBox, FEngine, FIndexBuffer, FVertexBuffer, FilamentModule } from './filament.js';

export type GeometryKind = 'box' | 'sphere' | 'plane';

export interface MeshData {
  positions: Float32Array;
  normals: Float32Array;
  uvs: Float32Array;
  indices: Uint16Array | Uint32Array;
}

class Builder {
  p: number[] = [];
  n: number[] = [];
  uv: number[] = [];
  i: number[] = [];
  done(): MeshData {
    const vertexCount = this.p.length / 3;
    return {
      positions: new Float32Array(this.p),
      normals: new Float32Array(this.n),
      uvs: new Float32Array(this.uv),
      indices: vertexCount > 65535 ? new Uint32Array(this.i) : new Uint16Array(this.i),
    };
  }
}

/** Three.js BoxGeometry: 24 vertices, 12 triangles per 1x1x1 segment grid. */
export function buildBox(width = 1, height = 1, depth = 1): MeshData {
  const b = new Builder();
  const face = (u: number, v: number, w: number, udir: number, vdir: number, fw: number, fh: number, fd: number): void => {
    const start = b.p.length / 3;
    const hw = fw / 2;
    const hh = fh / 2;
    for (let iy = 0; iy < 2; iy++) {
      const y = iy * fh - hh;
      for (let ix = 0; ix < 2; ix++) {
        const x = ix * fw - hw;
        const pos = [0, 0, 0];
        pos[u] = x * udir;
        pos[v] = y * vdir;
        pos[w] = fd / 2;
        const nrm = [0, 0, 0];
        nrm[w] = fd > 0 ? 1 : -1;
        b.p.push(pos[0], pos[1], pos[2]);
        b.n.push(nrm[0], nrm[1], nrm[2]);
        b.uv.push(ix, 1 - iy);
      }
    }
    // Grid 1x1: vertices a=0, b=2, c=3, d=1 of the 2x2 vertex layout.
    b.i.push(start, start + 2, start + 1, start + 2, start + 3, start + 1);
  };
  face(2, 1, 0, -1, -1, depth, height, width);
  face(2, 1, 0, 1, -1, depth, height, -width);
  face(0, 2, 1, 1, 1, width, depth, height);
  face(0, 2, 1, 1, -1, width, depth, -height);
  face(0, 1, 2, 1, -1, width, height, depth);
  face(0, 1, 2, -1, -1, width, height, -depth);
  return b.done();
}

/** Three.js SphereGeometry (full sphere). */
export function buildSphere(radius = 1, widthSegments = 32, heightSegments = 16): MeshData {
  const ws = Math.max(3, Math.floor(widthSegments));
  const hs = Math.max(2, Math.floor(heightSegments));
  const b = new Builder();
  for (let iy = 0; iy <= hs; iy++) {
    const v = iy / hs;
    const theta = v * Math.PI;
    for (let ix = 0; ix <= ws; ix++) {
      const u = ix / ws;
      const phi = u * Math.PI * 2;
      const x = -Math.cos(phi) * Math.sin(theta);
      const y = Math.cos(theta);
      const z = Math.sin(phi) * Math.sin(theta);
      b.p.push(radius * x, radius * y, radius * z);
      b.n.push(x, y, z);
      b.uv.push(u, 1 - v);
    }
  }
  const row = ws + 1;
  for (let iy = 0; iy < hs; iy++) {
    for (let ix = 0; ix < ws; ix++) {
      const a = iy * row + ix + 1;
      const bb = iy * row + ix;
      const c = (iy + 1) * row + ix;
      const d = (iy + 1) * row + ix + 1;
      if (iy !== 0) b.i.push(a, bb, d);
      if (iy !== hs - 1) b.i.push(bb, c, d);
    }
  }
  return b.done();
}

/** Three.js PlaneGeometry: in the XY plane facing +Z. */
export function buildPlane(width = 1, height = 1, widthSegments = 1, heightSegments = 1): MeshData {
  const gx = Math.max(1, Math.floor(widthSegments));
  const gy = Math.max(1, Math.floor(heightSegments));
  const b = new Builder();
  for (let iy = 0; iy <= gy; iy++) {
    const y = (iy * height) / gy - height / 2;
    for (let ix = 0; ix <= gx; ix++) {
      b.p.push((ix * width) / gx - width / 2, -y, 0);
      b.n.push(0, 0, 1);
      b.uv.push(ix / gx, 1 - iy / gy);
    }
  }
  const row = gx + 1;
  for (let iy = 0; iy < gy; iy++) {
    for (let ix = 0; ix < gx; ix++) {
      const a = ix + row * iy;
      const bb = ix + row * (iy + 1);
      const c = ix + 1 + row * (iy + 1);
      const d = ix + 1 + row * iy;
      b.i.push(a, bb, d, bb, c, d);
    }
  }
  return b.done();
}

export function buildGeometry(kind: GeometryKind, args: readonly number[] = []): MeshData {
  switch (kind) {
    case 'box':
      return buildBox(args[0], args[1], args[2]);
    case 'sphere':
      return buildSphere(args[0], args[1], args[2]);
    case 'plane':
      return buildPlane(args[0], args[1], args[2], args[3]);
    default:
      throw new Error(`@krate/3d: unknown geometry kind "${String(kind)}"`);
  }
}

export function boundsOf(positions: Float32Array): FBox {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  for (let i = 0; i < positions.length; i += 3) {
    for (let k = 0; k < 3; k++) {
      const v = positions[i + k];
      if (v < min[k]) min[k] = v;
      if (v > max[k]) max[k] = v;
    }
  }
  return {
    center: [0, 1, 2].map((k) => (min[k] + max[k]) / 2),
    halfExtent: [0, 1, 2].map((k) => (max[k] - min[k]) / 2),
  };
}

export interface GpuGeometry {
  vb: FVertexBuffer;
  ib: FIndexBuffer;
  box: FBox;
  indexCount: number;
}

/** Uploads positions, tangent frames (from SurfaceOrientation) and uv0. */
export function uploadGeometry(F: FilamentModule, engine: FEngine, data: MeshData): GpuGeometry {
  const count = data.positions.length / 3;
  const tri = data.indices.length / 3;
  // filament.js wraps normals/uvs/positions/triangles* without returning the
  // builder (and keeps the copied buffers on the JS object), so the calls are
  // not chained and build() runs on the same object.
  const sob = new F.SurfaceOrientation$Builder();
  sob.vertexCount(count);
  sob.normals(data.normals, 0);
  sob.uvs(data.uvs, 0);
  sob.positions(data.positions, 0);
  sob.triangleCount(tri);
  if (data.indices instanceof Uint32Array) sob.triangles32(data.indices);
  else sob.triangles16(data.indices);
  const orientation = sob.build();
  const A = F.VertexAttribute;
  const T = F.VertexBuffer$AttributeType;
  const vb = F.VertexBuffer.Builder()
    .vertexCount(count)
    .bufferCount(3)
    .attribute(A.POSITION, 0, T.FLOAT3, 0, 0)
    .attribute(A.TANGENTS, 1, T.SHORT4, 0, 0)
    .normalized(A.TANGENTS)
    .attribute(A.UV0, 2, T.FLOAT2, 0, 0)
    .build(engine);
  vb.setBufferAt(engine, 0, data.positions);
  vb.setBufferAt(engine, 1, orientation.getQuats(count));
  vb.setBufferAt(engine, 2, data.uvs);
  orientation.delete();
  const ib = F.IndexBuffer.Builder()
    .indexCount(data.indices.length)
    .bufferType(data.indices instanceof Uint32Array ? F.IndexBuffer$IndexType.UINT : F.IndexBuffer$IndexType.USHORT)
    .build(engine);
  ib.setBuffer(engine, data.indices);
  return { vb, ib, box: boundsOf(data.positions), indexCount: data.indices.length };
}
