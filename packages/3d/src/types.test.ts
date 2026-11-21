import { describe, expect, it } from 'vitest';
import type { CanvasProps, MeshStandardMaterialProps, SceneElements } from './types.js';

describe('types', () => {
  it('accepts the documented props', () => {
    const mat: MeshStandardMaterialProps = { color: 'orange', roughness: 0.4, metallic: 0 };
    const canvas: CanvasProps = { shadows: true, bloom: true, antialias: 'fxaa', toneMapping: 'aces' };
    const el: Pick<SceneElements, 'mesh'> = { mesh: { position: [0, 1, 0], onClick: () => {} } };
    expect([mat.color, canvas.shadows, el.mesh.position]).toEqual(['orange', true, [0, 1, 0]]);
  });
});
