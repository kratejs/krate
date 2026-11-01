import { existsSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { spawn } from 'child_process';
import { createRequire } from 'module';

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const BIN_DIR = join(__dirname, '..', 'bin');

function binaryName(): string {
  return process.platform === 'win32' ? 'krate.exe' : 'krate';
}

function isMusl(): boolean {
  try {
    const report = (process as unknown as { report?: { getReport?: () => { header?: { glibcVersionRuntime?: string } } } }).report;
    const header = report?.getReport?.().header;
    return !!header && !header.glibcVersionRuntime;
  } catch {
    return false;
  }
}

function platformPackage(): string {
  const platform = process.platform;
  const arch = process.arch;
  if (platform === 'linux' && arch === 'x64' && isMusl()) {
    return '@krate/core-linux-x64-musl';
  }
  return `@krate/core-${platform}-${arch}`;
}

export function resolveBinary(): string {
  // Explicit override for tests and custom deployments.
  const override = process.env.KRATE_BINARY;
  if (override && existsSync(override)) return override;

  const pkgName = platformPackage();
  try {
    const pkgPath = dirname(require.resolve(`${pkgName}/package.json`));
    const binary = join(pkgPath, 'bin', binaryName());
    if (existsSync(binary)) return binary;
  } catch (e) {
    // Ignore, try local path
  }

  const local = join(BIN_DIR, binaryName());
  if (existsSync(local)) return local;

  const pathBin = binaryName();
  if (existsSync(pathBin)) return pathBin;

  return local;
}

export function execBinary(args: string[], cwd?: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const bin = resolveBinary();
    const proc = spawn(bin, args, {
      stdio: 'inherit',
      cwd: cwd || process.cwd(),
    });
    proc.on('close', (code) => {
      if (code === 0) resolve();
      else {
        const err = new Error(`krate exited with code ${code}`) as Error & { exitCode?: number };
        err.exitCode = code ?? 1;
        reject(err);
      }
    });
    proc.on('error', (err) => {
      reject(new Error(`Failed to run krate binary: ${err.message}`));
    });
  });
}
