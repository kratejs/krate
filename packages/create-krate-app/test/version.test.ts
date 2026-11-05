import { describe, expect, it } from 'vitest';
import { krateRange } from '../src/version.js';

describe('krateRange', () => {
  it('uses a caret range for a stable release', () => {
    expect(krateRange('0.5.2')).toBe('^0.5.2');
    expect(krateRange('1.0.0')).toBe('^1.0.0');
  });

  it('pins a prerelease exactly, so a channel cannot move under the project', () => {
    expect(krateRange('0.5.0-beta.1')).toBe('0.5.0-beta.1');
    expect(krateRange('1.2.3-rc.4+build.7')).toBe('1.2.3-rc.4');
  });

  it('falls back to latest for anything that is not a version', () => {
    expect(krateRange('')).toBe('latest');
    expect(krateRange('dev')).toBe('latest');
    expect(krateRange('1.2')).toBe('latest');
  });
});
