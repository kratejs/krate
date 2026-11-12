#!/usr/bin/env node
// Native shell benchmark: the same small app built with Krate Native, Electron
// and Tauri v2. For each app the harness builds it, runs it N times and
// measures
//
//   cold start    from spawning the process to the first contentful paint of the
//                 page (paint time and time origin are read inside the page, the
//                 spawn time is taken here; both are wall clock on one machine)
//   idle memory   N seconds after the first paint, summed over the whole
//                 process tree (renderer, GPU and webview helpers included)
//   sizes         installers each tool produces, and the size of the app on disk
//
// Usage: node bench.mjs [--runs 5] [--idle 5] [--apps krate,electron,tauri]
//                       [--out results] [--skip-build] [--no-package]
//
// Environment: BENCH_WRAP (a command prefix for launching apps, used by the
// rootless WebKitGTK sysroot of the Krate developers), BENCH_ELECTRON_ARGS.
// Memory comes from packages/native/cmd/krate-memtree, which reads the same
// numbers as `krate native profile --memory`: PSS on Linux (smaps_rollup),
// phys_footprint on macOS, private bytes on Windows (plus the private working
// set, queried through PowerShell, which is what the table reports there).
import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..', '..');
const isWin = process.platform === 'win32';
const isMac = process.platform === 'darwin';
const goos = { win32: 'windows', darwin: 'darwin', linux: 'linux' }[process.platform];
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
const exeExt = isWin ? '.exe' : '';

// ------------------------------------------------------------------ options

function parseArgs(argv) {
  const o = { runs: 5, idle: 5, apps: ['krate', 'electron', 'tauri'], out: path.join(here, 'results'), skipBuild: false, package: true };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const val = () => {
      if (i + 1 >= argv.length) throw new Error(`${a} needs a value`);
      return argv[++i];
    };
    if (a === '--runs') o.runs = Number(val());
    else if (a === '--idle') o.idle = Number(val());
    else if (a === '--apps') o.apps = val().split(',').map((s) => s.trim()).filter(Boolean);
    else if (a === '--out') o.out = path.resolve(val());
    else if (a === '--skip-build') o.skipBuild = true;
    else if (a === '--no-package') o.package = false;
    else if (a === '-h' || a === '--help') {
      console.log(fs.readFileSync(fileURLToPath(import.meta.url), 'utf8').split('\n').slice(1, 23).map((l) => l.replace(/^\/\/ ?/, '')).join('\n'));
      process.exit(0);
    } else throw new Error(`unknown argument ${a}`);
  }
  if (!(o.runs >= 1) || !(o.idle >= 0)) throw new Error('--runs must be >= 1 and --idle >= 0');
  for (const a of o.apps) if (!['krate', 'electron', 'tauri'].includes(a)) throw new Error(`unknown app ${a}`);
  return o;
}

// ------------------------------------------------------------------ helpers

const log = (...a) => console.log(...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const quote = (a) => (/[\s"]/.test(a) ? JSON.stringify(a) : a);

function run(cmd, args, o = {}) {
  log(`+ ${cmd} ${args.map(quote).join(' ')}`);
  const r = spawnSync(cmd, args, {
    cwd: o.cwd,
    env: { ...process.env, ...o.env },
    encoding: 'utf8',
    stdio: o.capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
    maxBuffer: 64 << 20,
    shell: isWin && o.shell !== false && /\.(cmd|bat)$|^(npm|npx|pnpm)$/.test(cmd),
  });
  if (r.error) throw r.error;
  if (r.status !== 0 && !o.allowFail) throw new Error(`${cmd} exited with ${r.status}${o.capture ? `\n${r.stdout}\n${r.stderr}` : ''}`);
  return r;
}

function which(cmd) {
  const r = spawnSync(isWin ? 'where' : 'which', [cmd], { encoding: 'utf8' });
  return r.status === 0 ? r.stdout.split(/\r?\n/)[0].trim() : '';
}

function version(cmd, args) {
  const r = spawnSync(cmd, args, { encoding: 'utf8' });
  return r.status === 0 ? (r.stdout || r.stderr).split('\n')[0].trim() : '';
}

export function median(xs) {
  if (xs.length === 0) return null;
  const s = [...xs].sort((a, b) => a - b);
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

// dirSize sums the sizes of the files under p (symlinks count as themselves).
export function dirSize(p) {
  const st = fs.lstatSync(p);
  if (!st.isDirectory()) return st.size;
  let n = 0;
  for (const e of fs.readdirSync(p)) n += dirSize(path.join(p, e));
  return n;
}

// findFiles lists files under root whose lowercase name matches re, without
// descending into unpacked app directories, bundles or node_modules.
export function findFiles(root, re) {
  const out = [];
  if (!fs.existsSync(root)) return out;
  const walk = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) {
        if (/unpacked$|\.app$|^node_modules$|^\.deps$/.test(e.name)) continue;
        walk(p);
      } else if (re.test(e.name.toLowerCase())) out.push(p);
    }
  };
  walk(root);
  return out;
}

// formatOf names the installer format of a file.
export function formatOf(file) {
  const n = file.toLowerCase();
  if (n.endsWith('.deb')) return 'deb';
  if (n.endsWith('.appimage')) return 'AppImage';
  if (n.endsWith('.dmg')) return 'dmg';
  if (n.endsWith('.exe')) return 'NSIS';
  if (n.endsWith('.msix')) return 'MSIX';
  if (n.endsWith('.msi')) return 'MSI';
  return path.extname(n).slice(1);
}

const installerRe = /\.(deb|appimage|dmg|exe|msix|msi)$/;

function collectInstallers(roots) {
  const seen = new Set();
  const out = [];
  for (const r of roots) {
    for (const f of findFiles(r, installerRe)) {
      if (seen.has(f)) continue;
      seen.add(f);
      out.push({ format: formatOf(f), name: path.basename(f), bytes: fs.statSync(f).size });
    }
  }
  return out.sort((a, b) => a.format.localeCompare(b.format));
}

// ------------------------------------------------------------------ icons

function crc32(buf) {
  let c;
  let crc = 0xffffffff;
  for (let i = 0; i < buf.length; i++) {
    c = (crc ^ buf[i]) & 0xff;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    crc = (crc >>> 8) ^ c;
  }
  return (crc ^ 0xffffffff) >>> 0;
}

// writeIconPng writes a plain 1024x1024 PNG, the source `tauri icon` expands.
export function pngBytes(size = 1024) {
  const chunk = (type, data) => {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const td = Buffer.concat([Buffer.from(type, 'ascii'), data]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(td));
    return Buffer.concat([len, td, crc]);
  };
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // RGBA
  const row = Buffer.alloc(1 + size * 4);
  for (let x = 0; x < size; x++) {
    const o = 1 + x * 4;
    row[o] = 0x2a;
    row[o + 1] = 0x6f;
    row[o + 2] = 0xdb;
    row[o + 3] = 0xff;
  }
  const raw = Buffer.concat(Array.from({ length: size }, () => row));
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

// ------------------------------------------------------------------ memory

// parseEtime reads ps etime ([[dd-]hh:]mm:ss) as seconds.
export function parseEtime(s) {
  const m = /^(?:(\d+)-)?(?:(\d+):)?(\d+):(\d+)$/.exec(s.trim());
  if (!m) return NaN;
  return (Number(m[1] || 0) * 24 + Number(m[2] || 0)) * 3600 + Number(m[3]) * 60 + Number(m[4]);
}

// macOS: WebKit runs in XPC services that are not children of the app. On a
// runner nothing else uses WebKit, so the WebKit services started since the
// launch belong to the app.
function darwinWebKitPids(sinceSeconds) {
  const r = spawnSync('ps', ['-axo', 'pid=,etime=,comm='], { encoding: 'utf8' });
  const pids = [];
  for (const line of r.stdout.split('\n')) {
    const m = /^\s*(\d+)\s+(\S+)\s+(.*)$/.exec(line);
    if (!m) continue;
    if (!/com\.apple\.WebKit\.(WebContent|Networking|GPU)/.test(m[3])) continue;
    const age = parseEtime(m[2]);
    if (Number.isFinite(age) && age <= sinceSeconds + 2) pids.push(Number(m[1]));
  }
  return pids;
}

// windowsPrivateWorkingSet sums "Working Set - Private" over pids (bytes), or
// null when PowerShell cannot say.
function windowsPrivateWorkingSet(pids) {
  const script = `$ids=@(${pids.join(',')}); (Get-CimInstance Win32_PerfFormattedData_PerfProc_Process | Where-Object { $ids -contains $_.IDProcess } | Measure-Object -Property WorkingSetPrivate -Sum).Sum`;
  const r = spawnSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-Command', script], { encoding: 'utf8' });
  const n = Number((r.stdout || '').trim());
  return r.status === 0 && Number.isFinite(n) && n > 0 ? n : null;
}

function sampleMemory(tools, pid, sinceSeconds, kind) {
  const args = ['--pid', String(pid)];
  if (isMac && kind !== 'electron') {
    const extra = darwinWebKitPids(sinceSeconds);
    if (extra.length) args.push('--extra', extra.join(','));
  }
  const r = run(tools.memtree, args, { capture: true });
  const rep = JSON.parse(r.stdout);
  const sum = (k) => rep.procs.reduce((a, p) => a + (p[k] || 0), 0);
  const out = {
    source: rep.source,
    processes: rep.procs.length,
    pssBytes: rep.total.pss,
    rssBytes: sum('rss'),
    names: rep.procs.map((p) => p.name),
  };
  out.memoryBytes = out.pssBytes;
  if (isWin) {
    const pws = windowsPrivateWorkingSet(rep.procs.map((p) => p.pid));
    if (pws) {
      out.privateWorkingSetBytes = pws;
      out.memoryBytes = pws;
      out.source = 'private_working_set';
    }
  }
  return out;
}

// ------------------------------------------------------------------ running

function killTree(child) {
  if (child.exitCode !== null) return;
  if (isWin) {
    spawnSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' });
    return;
  }
  try {
    process.kill(-child.pid, 'SIGKILL');
  } catch {
    child.kill('SIGKILL');
  }
}

async function waitFor(fn, timeoutMs, stepMs = 20) {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    const v = fn();
    if (v) return v;
    await sleep(stepMs);
  }
  return null;
}

// runOnce launches the app and returns its cold start and idle memory.
async function runOnce(app, tools, opts, workDir, index) {
  const report = path.join(workDir, `report-${app.kind}-${index}.json`);
  fs.rmSync(report, { force: true });
  const wrap = (process.env.BENCH_WRAP || '').split(/\s+/).filter(Boolean);
  const cmd = wrap.length ? wrap[0] : app.exe;
  const args = wrap.length ? [...wrap.slice(1), app.exe, ...app.args] : app.args;
  const logFile = fs.openSync(path.join(workDir, `${app.kind}-${index}.log`), 'w');
  const t0 = Date.now();
  const child = spawn(cmd, args, {
    cwd: app.cwd,
    env: { ...process.env, ...app.env, BENCH_REPORT: report },
    stdio: ['ignore', logFile, logFile],
    detached: !isWin,
  });
  let spawnErr = null;
  child.once('error', (e) => (spawnErr = e));
  try {
    const raw = await waitFor(() => {
      if (spawnErr) throw spawnErr;
      if (child.exitCode !== null) throw new Error(`${app.kind} exited with ${child.exitCode} before it reported a first paint (see ${app.kind}-${index}.log)`);
      try {
        const s = fs.readFileSync(report, 'utf8').trim();
        return s.endsWith('}') ? s : null;
      } catch {
        return null;
      }
    }, 60000);
    if (!raw) throw new Error(`${app.kind} reported no first paint within 60 s`);
    const rec = JSON.parse(raw);
    const gotAt = Date.now();
    const result = { coldStartMs: Math.round((rec.paintAtMs - t0) * 10) / 10, firstPaintInPageMs: rec.fcp, reportDelayMs: Math.round(rec.reportedAtMs - rec.paintAtMs), userAgent: rec.userAgent };
    if (result.coldStartMs < 0) throw new Error(`negative cold start (${result.coldStartMs} ms): clock mismatch between page and harness`);
    await sleep(Math.max(0, opts.idle * 1000 - (gotAt - rec.paintAtMs)));
    if (child.exitCode !== null) throw new Error(`${app.kind} exited during the idle wait`);
    const mem = sampleMemory(tools, child.pid, (Date.now() - t0) / 1000, app.kind);
    return { ...result, ...mem };
  } finally {
    killTree(child);
    await sleep(500);
    fs.closeSync(logFile);
  }
}

function summarize(runs) {
  const col = (k) => runs.map((r) => r[k]).filter((v) => typeof v === 'number');
  const s = (k) => {
    const v = col(k);
    return v.length ? { median: median(v), min: Math.min(...v), max: Math.max(...v) } : null;
  };
  return { coldStartMs: s('coldStartMs'), memoryBytes: s('memoryBytes'), rssBytes: s('rssBytes'), processes: s('processes') };
}

// ------------------------------------------------------------------ builders

function buildTools(work) {
  const bin = path.join(work, 'bin');
  fs.mkdirSync(bin, { recursive: true });
  const t = { krate: path.join(bin, `krate${exeExt}`), memtree: path.join(bin, `krate-memtree${exeExt}`) };
  run('go', ['build', '-o', t.krate, './cmd/krate'], { cwd: path.join(repo, 'packages', 'compiler') });
  run('go', ['build', '-o', t.memtree, './cmd/krate-memtree'], { cwd: path.join(repo, 'packages', 'native') });
  return t;
}

function linkKrateDeps(dir) {
  const nm = path.join(dir, 'node_modules', '@krate');
  fs.mkdirSync(nm, { recursive: true });
  for (const p of ['core', 'runtime', 'app']) {
    const link = path.join(nm, p);
    fs.rmSync(link, { recursive: true, force: true });
    fs.symlinkSync(path.join(repo, 'packages', p), link, isWin ? 'junction' : 'dir');
  }
}

function buildKrate(tools, opts, work) {
  const dir = path.join(here, 'krate');
  const out = path.join(work, 'krate-out');
  const notes = [];
  if (!opts.skipBuild) {
    linkKrateDeps(dir);
    fs.rmSync(out, { recursive: true, force: true });
    const formats = { linux: ['deb'], darwin: ['dmg'], windows: ['nsis'] }[goos];
    if (goos === 'linux' && which('appimagetool')) formats.push('appimage');
    if (goos === 'windows' && !which('makensis')) {
      formats.pop();
      notes.push('makensis not found: no NSIS installer for Krate');
    }
    const args = ['native', 'build', dir, '--release', '--out', out];
    if (opts.package && formats.length) args.push('--package', '--format', formats.join(','));
    run(tools.krate, args, { env: { GOWORK: 'off' } });
  }
  const target = path.join(out, `${goos}-${goarch}`);
  const exe = path.join(target, `KrateBench${exeExt}`);
  if (!fs.existsSync(exe)) throw new Error(`krate build produced no ${exe}`);
  let payload = target;
  if (isMac) {
    const bundles = findDirs(path.join(out, 'packages'), /\.app$/);
    if (bundles.length) payload = bundles[0];
  }
  return {
    kind: 'krate',
    exe,
    args: [],
    cwd: target,
    env: {},
    payload,
    installers: collectInstallers([path.join(out, 'packages')]),
    notes,
    versions: { krate: gitSha() },
  };
}

function findDirs(root, re) {
  const out = [];
  if (!fs.existsSync(root)) return out;
  const walk = (d) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      if (!e.isDirectory()) continue;
      const p = path.join(d, e.name);
      if (re.test(e.name)) out.push(p);
      else walk(p);
    }
  };
  walk(root);
  return out;
}

function gitSha() {
  return version('git', ['-C', repo, 'rev-parse', '--short', 'HEAD']);
}

function pkgVersion(dir, name) {
  try {
    return JSON.parse(fs.readFileSync(path.join(dir, 'node_modules', name, 'package.json'), 'utf8')).version;
  } catch {
    return '';
  }
}

function buildElectron(opts) {
  const dir = path.join(here, 'electron');
  const notes = [];
  const out = path.join(dir, 'dist');
  if (!opts.skipBuild) {
    run('npm', ['ci', '--ignore-scripts', '--no-audit', '--no-fund'], { cwd: dir });
    run(process.execPath, [path.join(dir, 'node_modules', 'electron', 'install.js')], { cwd: dir });
    fs.rmSync(out, { recursive: true, force: true });
    const platformFlag = { linux: '--linux', darwin: '--mac', windows: '--win' }[goos];
    const targets = opts.package ? { linux: ['deb', 'AppImage'], darwin: ['dmg'], windows: ['nsis'] }[goos] : ['dir'];
    const r = run('npx', ['--no-install', 'electron-builder', platformFlag, ...targets, '--publish', 'never'], {
      cwd: dir,
      env: { CSC_IDENTITY_AUTO_DISCOVERY: 'false' },
      allowFail: true,
    });
    if (r.status !== 0) {
      notes.push('electron-builder failed for the installer targets; the unpacked app was built without them');
      run('npx', ['--no-install', 'electron-builder', platformFlag, '--dir', '--publish', 'never'], { cwd: dir, env: { CSC_IDENTITY_AUTO_DISCOVERY: 'false' } });
    }
  }
  let payload;
  let exe;
  if (isMac) {
    payload = findDirs(out, /^ElectronBench\.app$/)[0];
    exe = payload && path.join(payload, 'Contents', 'MacOS', 'ElectronBench');
  } else if (isWin) {
    payload = path.join(out, 'win-unpacked');
    exe = path.join(payload, 'ElectronBench.exe');
  } else {
    payload = path.join(out, 'linux-unpacked');
    exe = path.join(payload, 'electron-bench');
  }
  if (!exe || !fs.existsSync(exe)) throw new Error(`electron build produced no ${exe}`);
  // Chromium's sandbox needs a setuid helper or user namespaces, which CI
  // images often restrict; the sandbox is off on Linux for the measurement.
  const args = [...(goos === 'linux' ? ['--no-sandbox'] : []), ...(process.env.BENCH_ELECTRON_ARGS || '').split(/\s+/).filter(Boolean)];
  return {
    kind: 'electron',
    exe,
    args,
    cwd: payload,
    env: {},
    payload,
    installers: collectInstallers([out]),
    notes,
    versions: { electron: pkgVersion(dir, 'electron'), 'electron-builder': pkgVersion(dir, 'electron-builder') },
  };
}

function buildTauri(opts, work) {
  const dir = path.join(here, 'tauri');
  const rust = path.join(dir, 'src-tauri');
  const notes = [];
  const targetDir = process.env.CARGO_TARGET_DIR || path.join(rust, 'target');
  const release = path.join(targetDir, 'release');
  if (!opts.skipBuild) {
    run('npm', ['ci', '--ignore-scripts', '--no-audit', '--no-fund'], { cwd: dir });
    const png = path.join(dir, 'app-icon.png');
    fs.writeFileSync(png, pngBytes());
    run('npx', ['--no-install', 'tauri', 'icon', png], { cwd: dir });
    const bundle = { linux: 'deb,appimage', darwin: 'dmg', windows: 'nsis' }[goos];
    let ok = false;
    if (opts.package) {
      ok = run('npx', ['--no-install', 'tauri', 'build', '--bundles', bundle], { cwd: dir, allowFail: true }).status === 0;
      if (!ok) notes.push(`tauri build --bundles ${bundle} failed; the binary was built without installers`);
    }
    if (!ok) run('npx', ['--no-install', 'tauri', 'build', '--no-bundle'], { cwd: dir });
  }
  let exe = path.join(release, `bench-native-tauri${exeExt}`);
  let payload = exe;
  if (isMac) {
    const app = findDirs(path.join(release, 'bundle'), /\.app$/)[0];
    if (app) {
      payload = app;
      const macos = path.join(app, 'Contents', 'MacOS');
      exe = path.join(macos, fs.readdirSync(macos)[0]);
    }
  }
  if (!fs.existsSync(exe)) throw new Error(`tauri build produced no ${exe}`);
  const cargoLock = fs.readFileSync(path.join(rust, 'Cargo.lock'), 'utf8');
  const crate = (n) => new RegExp(`name = "${n}"\\nversion = "([^"]+)"`).exec(cargoLock)?.[1] || '';
  return {
    kind: 'tauri',
    exe,
    args: [],
    cwd: path.dirname(exe),
    env: {},
    payload,
    installers: collectInstallers([path.join(release, 'bundle')]),
    notes,
    versions: { tauri: crate('tauri'), '@tauri-apps/cli': pkgVersion(dir, '@tauri-apps/cli'), wry: crate('wry'), rustc: version('rustc', ['--version']) },
  };
}

// krateProfileMemory cross-checks the Krate number with the tool the docs
// recommend. Best effort: it needs a display, so a failure is a note only.
function krateProfileMemory(tools, idle, work) {
  const dir = path.join(here, 'krate');
  const out = path.join(work, 'krate-profile');
  const wrap = (process.env.BENCH_WRAP || '').split(/\s+/).filter(Boolean);
  const r = run(wrap[0] || tools.krate, [...wrap.slice(1), ...(wrap.length ? [tools.krate] : []), 'native', 'profile', dir, '--memory', '--idle', `${idle}s`, '--out', out], {
    capture: true,
    allowFail: true,
    env: { GOWORK: 'off' },
  });
  try {
    const rep = JSON.parse(fs.readFileSync(path.join(out, 'report.json'), 'utf8'));
    const s = (rep.memory.samples || []).find((x) => x.name === 'afterIdle') || rep.memory.samples?.[0];
    return { totalPssBytes: s.app.total.pss, floorPssBytes: s.floor?.total?.pss ?? null };
  } catch (e) {
    return { error: `${r.status === 0 ? 'no report' : 'krate native profile failed'}: ${e.message}` };
  }
}

// ------------------------------------------------------------------ output

const mb = (b) => (b == null ? 'n/a' : `${(b / 1048576).toFixed(1)} MiB`);
const ms = (v) => (v == null ? 'n/a' : `${Math.round(v)} ms`);

export function renderMarkdown(res) {
  const m = res.meta;
  const lines = [];
  lines.push(`### Native shell benchmark: ${m.os} ${m.arch}`);
  lines.push('');
  lines.push(`${m.cpu}, ${(m.memBytes / 2 ** 30).toFixed(1)} GB RAM. ${m.runs} runs per app, memory read ${m.idleSeconds} s after the first contentful paint. ${m.display}`);
  lines.push('');
  const src = new Set(Object.values(res.apps).map((a) => a.summary.memorySource).filter(Boolean));
  lines.push(`| App | Cold start (median, range) | Idle memory (median, range; ${[...src].join('/') || 'n/a'}) | Processes | Installer | On disk |`);
  lines.push('|---|---|---|---|---|---|');
  for (const [name, a] of Object.entries(res.apps)) {
    if (a.error) {
      lines.push(`| ${name} | failed: ${String(a.error).split('\n')[0]} | | | | |`);
      continue;
    }
    const s = a.summary;
    const inst = a.installers.map((i) => `${i.format} ${mb(i.bytes)}`).join(', ') || 'n/a';
    lines.push(
      `| ${name} | ${ms(s.coldStartMs?.median)} (${ms(s.coldStartMs?.min)} to ${ms(s.coldStartMs?.max)}) | ${mb(s.memoryBytes?.median)} (${mb(s.memoryBytes?.min)} to ${mb(s.memoryBytes?.max)}) | ${s.processes?.median ?? 'n/a'} | ${inst} | ${mb(a.onDiskBytes)} |`,
    );
  }
  lines.push('');
  lines.push('Versions: ' + Object.entries(res.apps).map(([n, a]) => `${n}: ${Object.entries(a.versions || {}).filter(([, v]) => v).map(([k, v]) => `${k} ${v}`).join(', ')}`).join('; '));
  for (const [n, a] of Object.entries(res.apps)) for (const note of a.notes || []) lines.push(`- ${n}: ${note}`);
  for (const note of res.notes || []) lines.push(`- ${note}`);
  lines.push('');
  return lines.join('\n');
}

// ------------------------------------------------------------------ main

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  const work = path.join(opts.out, 'work');
  fs.mkdirSync(work, { recursive: true });
  const tools = buildTools(work);
  const res = {
    meta: {
      os: `${os.type()} ${os.release()}`,
      platform: goos,
      arch: goarch,
      cpu: `${os.cpus()[0]?.model || 'unknown cpu'} x${os.cpus().length}`,
      memBytes: os.totalmem(),
      runs: opts.runs,
      idleSeconds: opts.idle,
      node: process.version,
      go: version('go', ['version']),
      display: goos === 'linux' ? `Display ${process.env.DISPLAY || 'unset'} (Xvfb: software rendering).` : 'Hosted runner session.',
      date: new Date().toISOString(),
    },
    apps: {},
    notes: [],
  };
  const builders = {
    krate: () => buildKrate(tools, opts, work),
    electron: () => buildElectron(opts),
    tauri: () => buildTauri(opts, work),
  };
  for (const name of opts.apps) {
    log(`\n=== ${name} ===`);
    try {
      const app = builders[name]();
      const runs = [];
      for (let i = 0; i < opts.runs; i++) {
        const r = await runOnce(app, tools, opts, work, i);
        log(`  run ${i + 1}: cold start ${r.coldStartMs} ms, memory ${mb(r.memoryBytes)} (${r.processes} processes)`);
        runs.push(r);
      }
      const summary = summarize(runs);
      summary.memorySource = runs[0].source;
      res.apps[name] = {
        versions: app.versions,
        notes: app.notes,
        installers: app.installers,
        onDiskBytes: dirSize(app.payload),
        payload: path.relative(opts.out, app.payload),
        summary,
        runs,
      };
    } catch (e) {
      log(`  failed: ${e.message}`);
      res.apps[name] = { error: e.message, installers: [], summary: {}, versions: {}, notes: [] };
    }
  }
  if (opts.apps.includes('krate') && res.apps.krate && !res.apps.krate.error) {
    res.apps.krate.profileMemory = krateProfileMemory(tools, opts.idle, work);
    const pm = res.apps.krate.profileMemory;
    if (pm.totalPssBytes) res.notes.push(`krate native profile --memory (same app, shell dev mode): ${mb(pm.totalPssBytes)} total, floor (blank page) ${mb(pm.floorPssBytes)}.`);
    else res.notes.push(`krate native profile --memory did not run: ${pm.error}`);
  }
  const base = path.join(opts.out, `native-bench-${goos}-${goarch}`);
  fs.writeFileSync(`${base}.json`, JSON.stringify(res, null, 2) + '\n');
  const md = renderMarkdown(res);
  fs.writeFileSync(`${base}.md`, md);
  log('\n' + md);
  log(`wrote ${base}.json and ${base}.md`);
  if (Object.values(res.apps).some((a) => a.error)) process.exitCode = 1;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((e) => {
    console.error(e);
    process.exit(1);
  });
}
