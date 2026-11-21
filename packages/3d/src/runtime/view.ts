// Canvas props to Filament View, ColorGrading and Camera.

import type { Antialias, BloomOptions, CameraProps, SsaoOptions, ToneMapping } from '../types.js';
import { DEFAULT_CAMERA, DEFAULT_EXPOSURE } from './calibration.js';
import type { FCamera, FColorGrading, FEngine, FView, FilamentModule } from './filament.js';

export interface ViewOptions {
  shadows?: boolean;
  bloom?: boolean | BloomOptions;
  ssao?: boolean | SsaoOptions;
  antialias?: Antialias;
  toneMapping?: ToneMapping;
}

export interface SceneCameraOptions extends CameraProps {
  aperture?: number;
  shutter?: number;
  iso?: number;
  /** Exposure compensation in EV stops. */
  exposure?: number;
}

export const DEFAULT_VIEW = { antialias: 'fxaa', toneMapping: 'aces' } as const;

export function bloomOptions(F: FilamentModule, bloom: ViewOptions['bloom']): Record<string, unknown> {
  if (!bloom) return { enabled: false };
  const o = bloom === true ? {} : bloom;
  return {
    enabled: true,
    strength: o.strength ?? 0.1,
    levels: o.levels ?? 6,
    threshold: o.threshold ?? true,
    blendMode: F.View$BloomOptions$BlendMode?.ADD ?? 0,
  };
}

export function ssaoOptions(F: FilamentModule, ssao: ViewOptions['ssao']): Record<string, unknown> {
  if (!ssao) return { enabled: false };
  const o = ssao === true ? {} : ssao;
  return {
    enabled: true,
    radius: o.radius ?? 0.3,
    intensity: o.intensity ?? 1,
    bias: o.bias ?? 0.0005,
    quality: F.View$QualityLevel?.HIGH ?? 2,
  };
}

/** Filament enum name for a tone mapper; AGX exists only in newer bindings, so it falls back to FILMIC. */
export function toneMapperValue(F: FilamentModule, tm: ToneMapping): number {
  const E = F.ColorGrading$ToneMapping;
  switch (tm) {
    case 'linear':
      return E.LINEAR;
    case 'filmic':
      return E.FILMIC;
    case 'agx':
      return E.AGX ?? E.FILMIC;
    default:
      return E.ACES;
  }
}

/**
 * Applies the options to the view. Shadows need no View call (Filament shadows
 * follow light and renderable flags); fog, SSR, VSM and stereo stay off because
 * the compiled materials exclude those variants. Returns the new color grading.
 */
export function applyView(
  F: FilamentModule,
  engine: FEngine,
  view: FView,
  opts: ViewOptions,
  previous?: FColorGrading
): FColorGrading {
  view.setBloomOptions(bloomOptions(F, opts.bloom));
  view.setAmbientOcclusionOptions(ssaoOptions(F, opts.ssao));
  const aa = opts.antialias ?? DEFAULT_VIEW.antialias;
  const A = F.View$AntiAliasing;
  view.setAntiAliasing(aa === 'fxaa' ? A.FXAA : A.NONE);
  view.setTemporalAntiAliasingOptions({ enabled: aa === 'taa' });
  view.setMultiSampleAntiAliasingOptions(aa === 'msaa' ? { enabled: true, sampleCount: 4 } : { enabled: false });
  const grading = F.ColorGrading.Builder()
    .toneMapping(toneMapperValue(F, opts.toneMapping ?? DEFAULT_VIEW.toneMapping))
    .build(engine);
  view.setColorGrading(grading);
  if (previous) engine.destroyColorGrading(previous);
  return grading;
}

export interface CameraState {
  position: number[];
  target: number[];
  fov: number;
  near: number;
  far: number;
}

export function cameraState(o: SceneCameraOptions = {}): CameraState {
  return {
    position: [...(o.position ?? DEFAULT_CAMERA.position)],
    target: [...(o.target ?? DEFAULT_CAMERA.target)],
    fov: o.fov ?? DEFAULT_CAMERA.fov,
    near: o.near ?? DEFAULT_CAMERA.near,
    far: o.far ?? DEFAULT_CAMERA.far,
  };
}

/** Exposure: default f/16, 1/125 s, ISO 100, with compensation applied as ISO stops. */
export function applyExposure(camera: FCamera, o: SceneCameraOptions = {}): void {
  const iso = (o.iso ?? DEFAULT_EXPOSURE.iso) * 2 ** (o.exposure ?? 0);
  camera.setExposure(o.aperture ?? DEFAULT_EXPOSURE.aperture, o.shutter ?? DEFAULT_EXPOSURE.shutter, iso);
}

export function applyProjection(F: FilamentModule, camera: FCamera, s: CameraState, width: number, height: number): void {
  camera.setProjectionFov(s.fov, width / Math.max(1, height), s.near, s.far, F.Camera$Fov.VERTICAL);
}

export function applyLookAt(camera: FCamera, s: CameraState): void {
  camera.lookAt(s.position, s.target, [0, 1, 0]);
}
