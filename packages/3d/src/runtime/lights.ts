// Lights. Intensities here are already in Filament units (see calibration.ts).

import { colorToLinear } from './color.js';
import type { Entities } from './entities.js';
import type { FEngine, FEntity, FIndirectLight, FilamentModule } from './filament.js';

export type LightKind = 'directional' | 'point' | 'spot' | 'ambient';
export type LightColor = string | number | readonly number[];

export interface LightProps {
  /** Filament units: lux for directional, lumens for point and spot, IBL intensity for ambient. */
  intensity?: number;
  /** Linear RGB triple, or a CSS/hex color converted from sRGB. */
  color?: LightColor;
  position?: readonly number[];
  direction?: readonly number[];
  castShadows?: boolean;
  /** Point and spot falloff radius in world units. */
  falloff?: number;
  /** Spot cone angles in radians. */
  inner?: number;
  outer?: number;
  /** Use Filament's SUN type for a directional light. */
  sun?: boolean;
}

export function lightRgb(color: LightColor | undefined): number[] {
  if (color === undefined) return [1, 1, 1];
  return typeof color === 'string' || typeof color === 'number' ? colorToLinear(color) : [color[0], color[1], color[2]];
}

/** Builds a directional, point or spot light entity. `shadows` is the Canvas-level switch. */
export function buildLight(
  F: FilamentModule,
  engine: FEngine,
  entities: Entities,
  kind: Exclude<LightKind, 'ambient'>,
  props: LightProps,
  shadows: boolean
): FEntity {
  const T = F.LightManager$Type;
  const type = kind === 'directional' ? (props.sun ? T.SUN : T.DIRECTIONAL) : kind === 'point' ? T.POINT : T.SPOT;
  const entity = entities.create();
  const b = F.LightManager.Builder(type)
    .color(lightRgb(props.color))
    .intensity(props.intensity ?? 0)
    .castShadows(shadows && !!props.castShadows);
  if (props.direction && kind !== 'point') b.direction([...props.direction]);
  if (props.position && kind !== 'directional') b.position([...props.position]);
  if (kind !== 'directional' && props.falloff !== undefined) b.falloff(props.falloff);
  if (kind === 'spot') b.spotLightCone(props.inner ?? 0, props.outer ?? Math.PI / 4);
  b.build(engine, entity);
  return entity;
}

/** Live update of a built light (reactive props). */
export function updateLight(engine: FEngine, entity: FEntity, name: string, value: number | readonly number[]): void {
  const lm = engine.getLightManager();
  const inst = lm.getInstance(entity);
  if (name === 'intensity') lm.setIntensity(inst, value as number);
  else if (name === 'color') lm.setColor(inst, lightRgb(value as LightColor));
  else if (name === 'position') lm.setPosition(inst, [...(value as readonly number[])]);
  else if (name === 'direction') lm.setDirection(inst, [...(value as readonly number[])]);
}

/** Uniform ambient light as a one-band spherical-harmonics IndirectLight. */
export function buildAmbient(F: FilamentModule, engine: FEngine, props: LightProps): FIndirectLight {
  const [r, g, b] = lightRgb(props.color);
  // Band 0 of the irradiance SH for uniform radiance L is 2 * sqrt(pi) * L.
  const k = 2 * Math.sqrt(Math.PI);
  return F.IndirectLight.Builder()
    .irradianceSh(1, [r * k, g * k, b * k])
    .intensity(props.intensity ?? 0)
    .build(engine);
}
