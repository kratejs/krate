import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { commandSpec, quoteWindowsArg } from '../src/spawn.js';
import { printVersion, versionMismatchWarning } from '../src/binary.js';

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
