import { describe, expect, it } from 'vitest';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { commandSpec, quoteWindowsArg } from '../src/spawn.js';
import { ensureExecutable, isInstalledPackage, printVersion, resolveBinary, spawnFailure, versionMismatchWarning } from '../src/binary.js';

describe('commandSpec', () => {
  it('spawns directly without a shell on POSIX', () => {
    expect(commandSpec('npx', ['--yes', 'create-krate-app@latest', 'my app'], 'linux')).toEqual({
      command: 'npx',
      args: ['--yes', 'create-krate-app@latest', 'my app'],
      shell: false,
    });
  });

  it('uses a shell with the bare command name on Windows (no npx.cmd EINVAL)', () => {
    const spec = commandSpec('npx', ['--yes', 'create-krate-app@latest', 'C:\\Users\\Jo Doe\\site'], 'win32');
    expect(spec.command).toBe('npx');
    expect(spec.shell).toBe(true);
    expect(spec.args).toEqual(['--yes', 'create-krate-app@latest', '"C:\\Users\\Jo Doe\\site"']);
  });

  it('runs package managers through the shell on Windows so npm.cmd is found', () => {
    expect(commandSpec('pnpm', ['install'], 'win32')).toEqual({ command: 'pnpm', args: ['install'], shell: true });
  });
});

describe('quoteWindowsArg', () => {
  it('leaves simple arguments alone and quotes ones with spaces or shell metacharacters', () => {
    expect(quoteWindowsArg('--use-pnpm')).toBe('--use-pnpm');
    expect(quoteWindowsArg('my app')).toBe('"my app"');
    expect(quoteWindowsArg('a&b')).toBe('"a&b"');
    expect(quoteWindowsArg('')).toBe('""');
  });

  it('doubles a trailing backslash so it cannot escape the closing quote', () => {
    expect(quoteWindowsArg('C:\\my dir\\')).toBe('"C:\\my dir\\\\"');
  });

  it('rejects arguments that cannot be quoted safely for cmd.exe', () => {
    expect(() => quoteWindowsArg('50%PATH%')).toThrow(/cmd\.exe/);
    expect(() => quoteWindowsArg('say "hi"')).toThrow(/cmd\.exe/);
  });
});

describe('versionMismatchWarning', () => {
  it('is silent when binary and package agree or the binary is a dev build', () => {
    expect(versionMismatchWarning('krate v0.2.0\n', '0.2.0')).toBeNull();
    expect(versionMismatchWarning('krate vdev\n', '0.2.0')).toBeNull();
  });

  it('warns with both versions when they differ', () => {
    const w = versionMismatchWarning('krate v0.1.0\n', '0.2.0');
    expect(w).toContain('v0.1.0');
    expect(w).toContain('v0.2.0');
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
  it('asks the native binary and passes the package version for the mismatch check', async () => {
    const calls: unknown[][] = [];
    await printVersion(['version'], '0.5.0', async (...args) => {
      calls.push(args);
    });
    expect(calls).toEqual([[['version'], undefined, '0.5.0']]);
  });

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

  it('prefers a binary built into bin/ in a source checkout', () => {
    const t = layout(join('packages', 'core'));
    expect(resolveBinary(t.binDir, () => t.platformManifest)).toBe(t.local);
    rmSync(t.root, { recursive: true, force: true });
  });

  it('falls back to bin/ when no platform package is installed', () => {
    const t = layout(join('node_modules', '@krate', 'core'));
    const missing = () => {
      throw new Error('not found');
    };
    expect(resolveBinary(t.binDir, missing)).toBe(t.local);
    rmSync(t.root, { recursive: true, force: true });
  });

  it('recognizes installed packages on POSIX and Windows paths', () => {
    expect(isInstalledPackage('/app/node_modules/@krate/core/bin')).toBe(true);
    expect(isInstalledPackage('C:\\app\\node_modules\\@krate\\core\\bin')).toBe(true);
    expect(isInstalledPackage('/home/me/krate/packages/core/bin')).toBe(false);
  });
});

describe('published files', () => {
  it('ships the launcher from bin/ and no compiler binary', () => {
    const pkg = JSON.parse(readFileSync(join(__dirname, '..', 'package.json'), 'utf-8'));
    expect(pkg.files).toContain('bin/krate.js');
    expect(pkg.files).not.toContain('bin');
  });
});

describe('ensureExecutable', () => {
  const posix = process.platform !== 'win32';
  const compiler = (root: string, sub: string) => {
    const dir = join(root, sub);
    mkdirSync(dir, { recursive: true });
    const bin = join(dir, 'krate');
    writeFileSync(bin, '#!/bin/sh\n');
    chmodSync(bin, 0o644);
    return bin;
  };

  it.runIf(posix)('restores the exec bit on an installed compiler', () => {
    const root = mkdtempSync(join(tmpdir(), 'krate-exec-'));
    const bin = compiler(root, 'node_modules/@krate/core-linux-x64/bin');
    ensureExecutable(bin);
    expect(statSync(bin).mode & 0o777).toBe(0o755);
    rmSync(root, { recursive: true, force: true });
  });

  it.runIf(posix)('leaves a binary outside node_modules alone', () => {
    const root = mkdtempSync(join(tmpdir(), 'krate-exec-'));
    const bin = compiler(root, 'custom/bin');
    ensureExecutable(bin);
    expect(statSync(bin).mode & 0o777).toBe(0o644);
    rmSync(root, { recursive: true, force: true });
  });

  it('does nothing on Windows or for a missing file', () => {
    expect(() => ensureExecutable('C:\\node_modules\\krate.exe', 'win32')).not.toThrow();
    expect(() => ensureExecutable('/nope/node_modules/krate', 'linux')).not.toThrow();
  });
});

describe('spawnFailure', () => {
  const failure = (code: string) => Object.assign(new Error(`spawn x ${code}`), { code });

  it('names the fix for a binary that cannot be executed', () => {
    const text = spawnFailure(failure('EACCES'), '/app/node_modules/x/bin/krate');
    expect(text).toContain('spawn x EACCES');
    expect(text).toContain('chmod +x /app/node_modules/x/bin/krate');
  });

  it('names the platform package when the binary is missing', () => {
    expect(spawnFailure(failure('ENOENT'), '/x', '@krate/core-linux-x64')).toContain('@krate/core-linux-x64 is not installed');
  });

  it('passes other errors through', () => {
    expect(spawnFailure(failure('EBUSY'), '/x')).toBe('Failed to run krate binary: spawn x EBUSY');
  });
});
