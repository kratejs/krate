// Shared process + filesystem helpers for the benchmark suite.
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, readdir, readFile, writeFile, rm, cp, stat } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import net from 'node:net';

const __dirname = dirname(fileURLToPath(import.meta.url));

/** Package root (packages/benchmark). */
export const pkgRoot = join(__dirname, '..', '..');

export const exists = existsSync;

// ─── Windows command shims ──────────────────────────────────────────────────
// On Windows, `npm`, `npx`, `pnpm`, etc. are `.cmd` shims that cannot be spawned
// directly (CreateProcess can't run .cmd). We detect them and route through
// `cmd.exe` via `shell: true` with a manually quoted command line; real `.exe`
// binaries (go, node) are spawned directly.
const WINDOWS_SHIMS = new Set(['npm', 'npx', 'pnpm', 'yarn', 'yarnpkg', 'bun']);

function isWindowsShim(cmd) {
  if (process.platform !== 'win32') return false;
  const lower = cmd.toLowerCase();
  if (lower.endsWith('.cmd') || lower.endsWith('.bat')) return true;
  return WINDOWS_SHIMS.has(cmd);
}

function quoteCmdArg(arg) {
  if (arg === '') return '""';
  if (/^[\w\-./\\:@%^+=,]+$/.test(arg)) return arg;
  return '"' + arg.replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/, '$1$1') + '"';
}

function spawnRaw(cmd, args, opts = {}) {
  if (isWindowsShim(cmd)) {
    const line = [cmd, ...args].map(quoteCmdArg).join(' ');
    return spawn(line, [], { ...opts, shell: true, env: childEnv(opts.env) });
  }
  return spawn(cmd, args, { ...opts, env: childEnv(opts.env) });
}

// `npm run` exports its own parsed config into the environment as
// `npm_config_*` variables; those leak into every npm/pnpm/yarn we spawn and
// poison project installs (e.g. EALLOWSCRIPTS). Drop them so child package
// managers read their real config from files instead.
function childEnv(extra) {
  const env = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (/^npm_config_/i.test(key)) continue;
    env[key] = value;
  }
  return extra ? { ...env, ...extra } : env;
}

export async function mkdirp(dir) {
  await mkdir(dir, { recursive: true });
}

export async function readJson(path) {
  return JSON.parse(await readFile(path, 'utf-8'));
}

export async function writeJson(path, data) {
  await mkdirp(dirname(path));
  await writeFile(path, JSON.stringify(data, null, 2) + '\n');
}

export async function writeText(path, content) {
  await mkdirp(dirname(path));
  await writeFile(path, content);
}

export async function rmrf(path) {
  await rm(path, { recursive: true, force: true });
}

/** Recursively copy a directory. */
export async function copyDir(src, dest) {
  await cp(src, dest, { recursive: true, force: true });
}

/** Total size (bytes) of all files under a directory, or 0 if missing. */
export async function dirSize(dir) {
  if (!existsSync(dir)) return 0;
  let total = 0;
  const stack = [dir];
  while (stack.length) {
    const cur = stack.pop();
    for (const entry of await readdir(cur, { withFileTypes: true })) {
      const p = join(cur, entry.name);
      if (entry.isDirectory()) stack.push(p);
      else total += (await stat(p)).size;
    }
  }
  return total;
}

/** Run a command, resolve with captured stdout/stderr and exit code. */
export function run(cmd, args = [], opts = {}) {
  return new Promise((resolve) => {
    const child = spawnRaw(cmd, args, opts);
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    child.on('error', (err) => resolve({ code: 1, stdout, stderr, error: err }));
    child.on('close', (code) => resolve({ code: code ?? 1, stdout, stderr }));
  });
}

/** Run a command, resolving with wall-clock duration in ms. */
export async function timed(cmd, args = [], opts = {}) {
  const start = performance.now();
  const res = await run(cmd, args, opts);
  return { ...res, ms: Math.round(performance.now() - start) };
}

/**
 * Spawn a long-running process. Resolves with a handle exposing a `stop()`
 * that kills the process.
 */
export function spawnProcess(cmd, args = [], opts = {}) {
  const child = spawnRaw(cmd, args, { stdio: 'pipe', ...opts });
  child.stdout.on('data', () => {});
  child.stderr.on('data', () => {});
  return {
    proc: child,
    stop() {
      return new Promise((resolve) => {
        let settled = false;
        const finish = () => {
          if (settled) return;
          settled = true;
          try { child.stdout.destroy(); } catch {}
          try { child.stderr.destroy(); } catch {}
          resolve();
        };
        if (child.exitCode !== null || child.signalCode !== null) return finish();
        child.once('exit', finish);
        if (process.platform === 'win32') {
          // Servers spawn helper processes that inherit our stdio pipes;
          // killing only the parent would leak the pipe handles and keep
          // this process alive, so take down the whole tree.
          const killer = spawn(
            'taskkill',
            ['/pid', String(child.pid), '/T', '/F'],
            { stdio: 'ignore' },
          );
          killer.on('error', () => child.kill());
        } else {
          child.kill('SIGTERM');
        }
        setTimeout(() => {
          try { child.kill('SIGKILL'); } catch {}
          finish();
        }, 5000).unref();
      });
    },
  };
}

/** Grab a currently-free TCP port on localhost. */
export async function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const port = srv.address().port;
      srv.close(() => resolve(port));
    });
  });
}

/** Poll a URL until it responds, or the deadline passes. */
export async function waitForUrl(url, { timeoutMs = 30000, intervalMs = 200 } = {}) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url);
      await res.arrayBuffer();
      if (res.ok || res.status < 500) return true;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  return false;
}
