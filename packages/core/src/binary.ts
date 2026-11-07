import { accessSync, chmodSync, constants, existsSync, statSync } from 'fs';
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

// A copy of this package under node_modules came from a registry, so only a
// source checkout (or a linked one, which resolves to its real path) can hold a
// binary built on purpose in bin/.
export function isInstalledPackage(dir: string): boolean {
  return dir.split(/[\\/]/).includes('node_modules');
}

export function resolveBinary(binDir: string = BIN_DIR, resolvePackage: (id: string) => string = require.resolve): string {
  // Explicit override for tests and custom deployments.
  const override = process.env.KRATE_BINARY;
  if (override && existsSync(override)) return override;

  const local = join(binDir, binaryName());

  // A binary built from source wins over a platform package that happens to be
  // installed from the registry, which would be an older compiler.
  if (!isInstalledPackage(binDir) && existsSync(local)) return local;

  const pkgName = platformPackage();
  try {
    const pkgPath = dirname(resolvePackage(`${pkgName}/package.json`));
    const binary = join(pkgPath, 'bin', binaryName());
    if (existsSync(binary)) return binary;
  } catch (e) {
    // Ignore, try local path
  }

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

/**
 * The dist-tag `krate init` asks npm for: the first pre-release identifier of
 * this package's version (0.5.0-beta.3 -> beta), as scripts/release-tag.mjs
 * publishes it, and latest for a stable version.
 */
export function scaffoldTag(packageVersion: string): string {
  const m = /^\d+\.\d+\.\d+-([A-Za-z][A-Za-z0-9-]*)/.exec(packageVersion);
  return m ? m[1] : 'latest';
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

/**
 * Restore the exec bit on an installed compiler. A tarball, mirror or Docker
 * layer can lose it, and that should not stop the CLI. Only binaries under
 * node_modules are touched: a KRATE_BINARY or a source build is the user's own.
 */
export function ensureExecutable(bin: string, platform: NodeJS.Platform = process.platform): void {
  if (platform === 'win32' || !isInstalledPackage(bin)) return;
  try {
    accessSync(bin, constants.X_OK);
    return;
  } catch {
    // Not executable, or missing, which spawn reports.
  }
  try {
    chmodSync(bin, statSync(bin).mode | 0o111);
  } catch {
    // Someone else owns the file. spawn fails next, with a hint.
  }
}

/** The error for a compiler that could not be started, with the way out. */
export function spawnFailure(err: NodeJS.ErrnoException, bin: string, pkg: string = platformPackage()): string {
  const message = `Failed to run krate binary: ${err.message}`;
  if (err.code === 'EACCES') {
    return `${message}\n  hint: the binary is not executable and krate could not fix it. Run \`chmod +x ${bin}\`, reinstall as the user who owns node_modules, or check that the volume is not mounted noexec.`;
  }
  if (err.code === 'ENOENT') {
    return `${message}\n  hint: ${pkg} is not installed. Reinstall without --no-optional or --omit=optional, or set KRATE_BINARY to a compiler you built.`;
  }
  return message;
}

export function execBinary(args: string[], cwd?: string, packageVersion?: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const bin = resolveBinary();
    ensureExecutable(bin);
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
      reject(new Error(spawnFailure(err, bin)));
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
