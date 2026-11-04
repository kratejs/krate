import { describe, expect, it } from 'vitest';
import { commandSpec } from '../src/spawn.js';

describe('install command construction', () => {
  it('runs `<pm> install` through a shell on Windows', () => {
    for (const pm of ['npm', 'pnpm', 'yarn', 'bun']) {
      expect(commandSpec(pm, ['install'], 'win32')).toEqual({ command: pm, args: ['install'], shell: true });
    }
  });

  it('does not use a shell elsewhere', () => {
    expect(commandSpec('npm', ['install'], 'darwin').shell).toBe(false);
  });
});
