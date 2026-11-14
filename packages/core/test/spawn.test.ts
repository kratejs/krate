import { describe, expect, it } from 'vitest';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { quoteWindowsArg } from '../src/spawn.js';
import { printVersion, resolveBinary } from '../src/binary.js';

describe('quoteWindowsArg', () => {

  it('doubles a trailing backslash so it cannot escape the closing quote', () => {
    expect(quoteWindowsArg('C:\\my dir\\')).toBe('"C:\\my dir\\\\"');
  });

  it('rejects arguments that cannot be quoted safely for cmd.exe', () => {
    expect(() => quoteWindowsArg('50%PATH%')).toThrow(/cmd\.exe/);
    expect(() => quoteWindowsArg('say "hi"')).toThrow(/cmd\.exe/);
  });
});

describe('scaffolder copies', () => {
  it('keeps the three spawn helper copies identical', () => {
    const base = join(__dirname, '..', '..');
    const core = readFileSync(join(base, 'core', 'src', 'spawn.ts'), 'utf-8');
    for (const pkg of ['create-krate-app', 'create-krate-docs']) {
      expect(readFileSync(join(base, pkg, 'src', 'spawn.ts'), 'utf-8')).toBe(core);
    }
  });
});

describe('printVersion', () => {

  it('fails loudly instead of printing the wrapper version when the binary cannot run', async () => {
    const run = async () => {
      throw new Error('Failed to run krate binary: ENOENT');
    };
    await expect(printVersion(['version'], '0.5.0', run)).rejects.toThrow(
      'cannot read the krate binary version (@krate/core is v0.5.0): Failed to run krate binary: ENOENT',
    );
  });
});

describe('resolveBinary', () => {
  const name = process.platform === 'win32' ? 'krate.exe' : 'krate';

  function layout(coreDir: string) {
    const root = mkdtempSync(join(tmpdir(), 'krate-bin-'));
    const binDir = join(root, coreDir, 'bin');
    const platformDir = join(root, 'platform');
    mkdirSync(binDir, { recursive: true });
    mkdirSync(join(platformDir, 'bin'), { recursive: true });
    const local = join(binDir, name);
    const platform = join(platformDir, 'bin', name);
    writeFileSync(local, '');
    writeFileSync(platform, '');
    return { root, binDir, local, platform, platformManifest: join(platformDir, 'package.json') };
  }

  it('prefers the platform package over a file in bin/ for an installed package', () => {
    const t = layout(join('node_modules', '@krate', 'core'));
    expect(resolveBinary(t.binDir, () => t.platformManifest)).toBe(t.platform);
    rmSync(t.root, { recursive: true, force: true });
  });
});

describe('published files', () => {
  it('ships the launcher from bin/ and no compiler binary', () => {
    const pkg = JSON.parse(readFileSync(join(__dirname, '..', 'package.json'), 'utf-8'));
    expect(pkg.files).toContain('bin/krate.js');
    expect(pkg.files).not.toContain('bin');
  });
});
