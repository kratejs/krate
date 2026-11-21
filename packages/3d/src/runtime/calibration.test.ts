import { describe, expect, it } from 'vitest';
import {
  AMBIENT_IBL_PER_UNIT,
  DEFAULT_EXPOSURE,
  DIRECTIONAL_LUX_PER_UNIT,
  POINT_LUMENS_PER_UNIT,
  lightIntensity,
} from './calibration.js';

describe('light calibration', () => {
  it('maps Three.js intensities to the DESIGN.md constants', () => {
    expect(DIRECTIONAL_LUX_PER_UNIT).toBe(110000);
    expect(AMBIENT_IBL_PER_UNIT).toBe(30000);
    expect(POINT_LUMENS_PER_UNIT).toBeCloseTo(4 * Math.PI * 1000, 9);
    expect(lightIntensity('directional', 1.5)).toBe(165000);
    expect(lightIntensity('ambient', 0.5)).toBe(15000);
    expect(lightIntensity('point', 2)).toBeCloseTo(8000 * Math.PI, 6);
    expect(lightIntensity('spot', 2)).toBe(lightIntensity('point', 2));
  });

  it('default exposure is f/16, 1/125 s, ISO 100 (EV100 about 15)', () => {
    const { aperture, shutter, iso } = DEFAULT_EXPOSURE;
    expect(Math.log2((aperture * aperture) / shutter)).toBeCloseTo(14.97, 2);
    expect(iso).toBe(100);
  });
});
