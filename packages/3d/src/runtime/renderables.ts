// Mesh renderables. Filament 1.53.4's JS bindings have Builder.instances(count)
// but no InstanceBuffer and no per-instance transform API, so real GPU
// instancing is not available: an instanced mesh is `count` renderables
// sharing one vertex/index buffer and one material instance.

import type { Entities } from './entities.js';
import type { FEngine, FEntity, FMaterialInstance, FilamentModule, Mat4 } from './filament.js';
import type { GpuGeometry } from './geometry.js';

export interface MeshFlags {
  castShadow?: boolean;
  receiveShadow?: boolean;
}

/** Builds one triangle-mesh renderable with a transform component; the caller adds it to the scene. */
export function buildMesh(
  F: FilamentModule,
  engine: FEngine,
  entities: Entities,
  geo: GpuGeometry,
  mi: FMaterialInstance,
  flags: MeshFlags = {},
  shadows = false
): FEntity {
  const entity = entities.create();
  F.RenderableManager.Builder(1)
    .boundingBox(geo.box)
    .geometry(0, F.RenderableManager$PrimitiveType.TRIANGLES, geo.vb, geo.ib)
    .material(0, mi)
    .castShadows(shadows && !!flags.castShadow)
    .receiveShadows(shadows && !!flags.receiveShadow)
    .build(engine, entity);
  entities.markRenderable(entity);
  const tcm = engine.getTransformManager();
  tcm.create(entity);
  entities.markTransform(entity);
  return entity;
}

/** `count` renderables sharing geometry and material, placed by a packed array of column-major matrices. */
export function buildInstanced(
  F: FilamentModule,
  engine: FEngine,
  entities: Entities,
  geo: GpuGeometry,
  mi: FMaterialInstance,
  count: number,
  matrices: Float32Array,
  flags: MeshFlags = {},
  shadows = false
): FEntity[] {
  const tcm = engine.getTransformManager();
  const out: FEntity[] = [];
  tcm.openLocalTransformTransaction();
  for (let i = 0; i < count; i++) {
    const e = buildMesh(F, engine, entities, geo, mi, flags, shadows);
    tcm.setTransform(tcm.getInstance(e), matrices.subarray(i * 16, i * 16 + 16) as Mat4);
    out.push(e);
  }
  tcm.commitLocalTransformTransaction();
  return out;
}
