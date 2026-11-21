// Entity tracking: everything created through here is destroyed on dispose.

import type { FEngine, FEntity, FilamentModule } from './filament.js';

export class Entities {
  private live = new Set<FEntity>();
  /** Entities that carry a renderable or transform component. */
  private components = new Map<FEntity, { renderable: boolean; transform: boolean }>();

  constructor(
    private F: FilamentModule,
    private engine: FEngine
  ) {}

  create(): FEntity {
    const e = this.F.EntityManager.get().create();
    this.live.add(e);
    return e;
  }

  markRenderable(e: FEntity): void {
    const c = this.components.get(e) ?? { renderable: false, transform: false };
    c.renderable = true;
    this.components.set(e, c);
  }

  markTransform(e: FEntity): void {
    const c = this.components.get(e) ?? { renderable: false, transform: false };
    c.transform = true;
    this.components.set(e, c);
  }

  get size(): number {
    return this.live.size;
  }

  destroy(e: FEntity): void {
    if (!this.live.delete(e)) return;
    const c = this.components.get(e);
    this.components.delete(e);
    if (c?.renderable) this.engine.getRenderableManager().destroy(e);
    if (c?.transform) this.engine.getTransformManager().destroy(e);
    this.engine.destroyEntity(e);
    e.delete?.();
  }

  destroyAll(): void {
    for (const e of [...this.live]) this.destroy(e);
  }
}
