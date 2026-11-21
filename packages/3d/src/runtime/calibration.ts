// Three.js to Filament light and exposure calibration. The Go copy of these
// constants lives in packages/compiler/internal/scene3d; keep both in sync.

/** Default camera: f/16, 1/125 s, ISO 100 (EV100 about 15, sunlight near middle grey). */
export const DEFAULT_EXPOSURE = { aperture: 16, shutter: 1 / 125, iso: 100 } as const;

/** Filament directional light illuminance (lux) per Three.js intensity unit. */
export const DIRECTIONAL_LUX_PER_UNIT = 110000;
/** Luminous power (lumens) per Three.js point/spot intensity unit: 4 * pi * 1000. */
export const POINT_LUMENS_PER_UNIT = 4 * Math.PI * 1000;
/** IBL intensity per Three.js ambient intensity unit (when there is no Environment). */
export const AMBIENT_IBL_PER_UNIT = 30000;

export type LightKind = 'directional' | 'point' | 'spot' | 'ambient';

/** Converts a Three.js-scale intensity into Filament units for the given light kind. */
export function lightIntensity(kind: LightKind, threeIntensity: number): number {
  switch (kind) {
    case 'directional':
      return threeIntensity * DIRECTIONAL_LUX_PER_UNIT;
    case 'ambient':
      return threeIntensity * AMBIENT_IBL_PER_UNIT;
    default:
      return threeIntensity * POINT_LUMENS_PER_UNIT;
  }
}

/** Camera defaults, matching Three.js PerspectiveCamera. */
export const DEFAULT_CAMERA = { fov: 75, near: 0.1, far: 1000, position: [0, 0, 5], target: [0, 0, 0] } as const;
