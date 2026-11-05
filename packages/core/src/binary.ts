import { existsSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { spawn, spawnSync } from 'child_process';
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

  // A binary built from source into this package's bin/ is a deliberate local
  // choice; it wins over a platform package that happens to be installed from
  // the registry (published tarballs ship only the JS launcher in bin/).
  const localFirst = join(BIN_DIR, binaryName());
  if (existsSync(localFirst)) return localFirst;

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

/**
 * Compare the output of `krate version` ("krate v0.1.0") with the npm package
 * version. Returns a warning when they differ, or null when they match or the
 * binary is an unversioned local build ("krate vdev").
 */
export function versionMismatchWarning(binaryOutput: string, packageVersion: string): string | null {
  const m = /v?(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)/.exec(binaryOutput);
  if (!m) return null;
  if (m[1] === packageVersion) return null;
  return (
    `warning: the krate binary is v${m[1]} but @krate/core is v${packageVersion}. ` +
    'Reinstall the matching platform package, or point KRATE_BINARY at the right binary.'
  );
}

let versionChecked = false;

function warnOnVersionMismatch(bin: string, packageVersion: string): void {
  if (versionChecked) return;
  versionChecked = true;
  try {
    const res = spawnSync(bin, ['version'], { encoding: 'utf-8', timeout: 5000 });
    if (res.status !== 0 || typeof res.stdout !== 'string') return;
    const warning = versionMismatchWarning(res.stdout, packageVersion);
    if (warning) console.error(warning);
  } catch {
    // The real invocation below reports an unusable binary.
  }
}

export function execBinary(args: string[], cwd?: string, packageVersion?: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const bin = resolveBinary();
    if (packageVersion) warnOnVersionMismatch(bin, packageVersion);
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

/**
 * `krate version` reports the native binary, the code that actually runs. The
 * npm wrapper's version can differ from it, so printing that instead would hide
 * a stale platform package. Exec failures surface as a rejection.
 */
export async function printVersion(
  args: string[],
  packageVersion: string,
  run: typeof execBinary = execBinary,
): Promise<void> {
  try {
    await run(args, undefined, packageVersion);
  } catch (err) {
    throw new Error(
      `cannot read the krate binary version (@krate/core is v${packageVersion}): ${(err as Error).message}`,
    );
  }
}
