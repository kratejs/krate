import { describe, expect, it } from 'vitest';
import { createFake } from './__fixtures__/fake.js';
import { applyExposure, applyView, bloomOptions, cameraState, ssaoOptions, toneMapperValue, type ViewOptions } from './view.js';

function setup() {
  const fake = createFake();
  const engine = fake.module.Engine.create(document.createElement('canvas'));
  const view = engine.createView();
  const apply = (o: ViewOptions) => applyView(fake.module, engine, view, o);
  return { fake, engine, view, apply, v: fake.state.view };
}

describe('view options', () => {
  it('defaults to FXAA, ACES and no bloom or SSAO', () => {
    const { fake, apply, v } = setup();
    const g = apply({}) as unknown as { rec: Record<string, unknown[][]> };
    expect(v.aa).toEqual([fake.module.View$AntiAliasing.FXAA]);
    expect(v.taa).toEqual([{ enabled: false }]);
    expect(v.msaa).toEqual([{ enabled: false }]);
    expect(v.bloom).toEqual([{ enabled: false }]);
    expect(v.ao).toEqual([{ enabled: false }]);
    expect(g.rec.toneMapping[0]).toEqual([fake.module.ColorGrading$ToneMapping.ACES]);
  });

  it.each([
    ['taa', 0, true, false],
    ['fxaa', 1, false, false],
    ['msaa', 0, false, true],
    ['none', 0, false, false],
  ] as const)('antialias=%s', (aa, filament, taa, msaa) => {
    const { apply, v } = setup();
    apply({ antialias: aa });
    expect(v.aa).toEqual([filament]);
    expect((v.taa[0] as { enabled: boolean }).enabled).toBe(taa);
    expect((v.msaa[0] as { enabled: boolean }).enabled).toBe(msaa);
  });

  it('msaa uses 4 samples', () => {
    const { apply, v } = setup();
    apply({ antialias: 'msaa' });
    expect(v.msaa).toEqual([{ enabled: true, sampleCount: 4 }]);
  });

  it.each([
    ['aces', 'ACES'],
    ['filmic', 'FILMIC'],
    ['linear', 'LINEAR'],
    ['agx', 'FILMIC'],
  ] as const)('toneMapping=%s', (tm, expected) => {
    const { fake } = setup();
    expect(toneMapperValue(fake.module, tm)).toBe(fake.module.ColorGrading$ToneMapping[expected]);
  });

  it('uses a real AGX mapper when the bindings have one', () => {
    const { fake } = setup();
    (fake.module.ColorGrading$ToneMapping as Record<string, number>).AGX = 99;
    expect(toneMapperValue(fake.module, 'agx')).toBe(99);
  });

  it('bloom true enables defaults; an object overrides them', () => {
    const { fake } = setup();
    expect(bloomOptions(fake.module, true)).toMatchObject({ enabled: true, strength: 0.1, levels: 6, threshold: true });
    expect(bloomOptions(fake.module, { strength: 0.5, levels: 4, threshold: false })).toMatchObject({
      enabled: true,
      strength: 0.5,
      levels: 4,
      threshold: false,
    });
    expect(bloomOptions(fake.module, false)).toEqual({ enabled: false });
  });

  it('ssao true enables defaults; an object overrides them', () => {
    const { fake } = setup();
    expect(ssaoOptions(fake.module, true)).toMatchObject({ enabled: true, radius: 0.3, intensity: 1 });
    expect(ssaoOptions(fake.module, { radius: 1, intensity: 2, bias: 0.01 })).toMatchObject({ radius: 1, intensity: 2, bias: 0.01 });
  });

  it('never enables fog, screen-space reflections, VSM or stereo (filtered out of the compiled materials)', () => {
    const { apply, view } = setup();
    apply({ shadows: true, bloom: true, ssao: true, antialias: 'taa' });
    const methods = Object.keys(view).filter((k) => /fog|reflection|vsm|stereo/i.test(k));
    expect(methods).toEqual([]);
  });

  it('replaces and destroys the previous color grading', () => {
    const { fake, engine, view } = setup();
    const first = applyView(fake.module, engine, view, {});
    expect(fake.alive.has(first as never)).toBe(true);
    applyView(fake.module, engine, view, { toneMapping: 'filmic' }, first);
    expect(fake.alive.has(first as never)).toBe(false);
  });
});

describe('camera', () => {
  it('defaults match Three.js: fov 75, near 0.1, far 1000', () => {
    expect(cameraState()).toMatchObject({ fov: 75, near: 0.1, far: 1000 });
    expect(cameraState({ fov: 50, position: [1, 2, 3] })).toMatchObject({ fov: 50, position: [1, 2, 3] });
  });

  it('exposure defaults to f/16, 1/125 s, ISO 100', () => {
    const calls: number[][] = [];
    const cam = { setExposure: (...a: number[]) => calls.push(a) };
    applyExposure(cam as never);
    applyExposure(cam as never, { aperture: 8, exposure: -1 });
    expect(calls).toEqual([
      [16, 1 / 125, 100],
      [8, 1 / 125, 50],
    ]);
  });
});
