import { describe, expect, it } from 'vitest';
import { colorToLinear, parseColor, srgbToLinear } from './color.js';

describe('color', () => {
  it('converts sRGB to linear', () => {
    expect(srgbToLinear(0)).toBe(0);
    expect(srgbToLinear(1)).toBeCloseTo(1, 6);
    expect(srgbToLinear(0.5)).toBeCloseTo(0.214, 3);
    expect(srgbToLinear(0.02)).toBeCloseTo(0.02 / 12.92, 6);
  });

  it('parses hex, numbers, rgb() and names', () => {
    expect(parseColor('#ff8000')).toEqual([255, 128, 0]);
    expect(parseColor('#f80')).toEqual([255, 136, 0]);
    expect(parseColor(0x4488FF)).toEqual([0x44, 0x88, 0xFF]);
    expect(parseColor('rgb(1, 2, 3)')).toEqual([1, 2, 3]);
    expect(parseColor('Orange')).toEqual([255, 165, 0]);
    expect(parseColor('nonsense')).toEqual([255, 255, 255]);
  });

  it('matches the DESIGN.md example values', () => {
    const o = colorToLinear('orange');
    expect(o[0]).toBeCloseTo(1, 4);
    expect(o[1]).toBeCloseTo(0.3763, 4);
    const b = colorToLinear('#4488ff');
    expect(b[0]).toBeCloseTo(0.0578, 4);
    expect(b[1]).toBeCloseTo(0.2462, 4);
  });
});
