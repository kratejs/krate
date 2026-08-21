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

export function resolveBinary(): string {
  const pkgName = `@krate/core-${process.platform}-${process.arch}`;
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
      else reject(new Error(`krate exited with code ${code}`));
    });
    proc.on('error', (err) => {
      reject(new Error(`Failed to run krate binary: ${err.message}`));
    });
  });
}
