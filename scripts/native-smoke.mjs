#!/usr/bin/env node
// Packaging, install, launch and update smoke tests for Krate Native on a real
// operating system. CI runs one step at a time:
//
//   node scripts/native-smoke.mjs <linux|macos|windows> <step>
//
//   linux    prepare deb rpm appimage update
//   macos    prepare dmg update
//   windows  cert prepare nsis msix update
//
// "prepare" builds examples/native-tasks three times with `krate native build
// --release --package`: version 1.0.0 (A), 1.1.0 (B, with a delta update from
// A) and 1.1.0 again with a host that exits shortly after start (Bad), all
// with the updater public key of a freshly generated Ed25519 key pair. The
// other steps install A the way a user would, launch it and check that it
// serves its first page (the release executable's --browser mode prints the
// URL; a window is opened too when a display exists), then update A to B
// through the real updater package against a local HTTP feed (the update
// must be a delta update, and B confirms a healthy start), and finally update
// a fresh A to Bad and check that the second launch of Bad restores A.
//
// Environment: SMOKE_WORK (work directory, default $RUNNER_TEMP/native-smoke),
// KRATE_REQUIRE_E2E (a missing optional tool fails instead of skipping).
import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const work = path.resolve(process.env.SMOKE_WORK || path.join(process.env.RUNNER_TEMP || os.tmpdir(), 'native-smoke'));
const isWin = process.platform === 'win32';
const goos = { win32: 'windows', darwin: 'darwin', linux: 'linux' }[process.platform];
const goarch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
const exampleDir = path.join(repo, 'examples', 'native-tasks');
const cfgPath = path.join(exampleDir, 'krate.config.ts');
const productName = 'Native Tasks';
const appId = 'com.example.tasks';
const versionA = '1.0.0';
const versionB = '1.1.0';
const requireE2E = !!process.env.KRATE_REQUIRE_E2E;

const krateBin = path.join(work, 'bin', isWin ? 'krate.exe' : 'krate');
const keyFile = path.join(work, 'updater.key');
const pubFile = path.join(work, 'updater.pub');
const certEnvFile = path.join(work, 'windows-cert.json');

function log(...a) {
  console.log(...a);
}

function quote(a) {
  return /[\s"]/.test(a) ? JSON.stringify(a) : a;
}

function run(cmd, args, o = {}) {
  log(`+ ${cmd} ${args.map(quote).join(' ')}`);
  const r = spawnSync(cmd, args, {
    cwd: o.cwd,
    env: { ...process.env, ...o.env },
    encoding: 'utf8',
    stdio: o.capture ? ['ignore', 'pipe', 'inherit'] : 'inherit',
    maxBuffer: 64 << 20,
  });
  if (r.error) throw r.error;
  if (r.status !== 0 && !o.allowFail) throw new Error(`${cmd} exited with ${r.status}`);
  return r;
}

// runAsync is run for callers that must keep the event loop alive (the local
// update feed is served by this process).
function runAsync(cmd, args, o = {}) {
  log(`+ ${cmd} ${args.map(quote).join(' ')}`);
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { cwd: o.cwd, env: { ...process.env, ...o.env }, stdio: 'inherit' });
    child.on('error', reject);
    child.on('exit', (code) => (code === 0 ? resolve() : reject(new Error(`${cmd} exited with ${code}`))));
  });
}

function ps(script, o = {}) {
  return run('powershell.exe', ['-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-Command', script], o);
}

function fail(msg) {
  throw new Error(msg);
}

function skipOrFail(what) {
  if (requireE2E) fail(`${what} (KRATE_REQUIRE_E2E is set)`);
  log(`SKIP: ${what}`);
}

function which(name) {
  const dirs = (process.env.PATH || '').split(path.delimiter);
  const exts = isWin ? (process.env.PATHEXT || '.EXE;.CMD').split(';') : [''];
  for (const d of dirs) {
    for (const e of exts) {
      const p = path.join(d, name + e);
      if (fs.existsSync(p) && fs.statSync(p).isFile()) return p;
    }
  }
  return '';
}

function walk(dir) {
  const out = [];
  if (!fs.existsSync(dir)) return out;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else out.push(p);
  }
  return out;
}

function findPackage(label, re) {
  const hit = walk(path.join(work, label)).filter((p) => re.test(path.basename(p)));
  if (hit.length === 0) fail(`no package matching ${re} under ${path.join(work, label)}; found: ${walk(path.join(work, label, 'packages')).join(', ')}`);
  return hit[0];
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// ---------------------------------------------------------------- build

function ensureKrate() {
  if (fs.existsSync(krateBin)) return;
  fs.mkdirSync(path.dirname(krateBin), { recursive: true });
  run('go', ['build', '-o', krateBin, './cmd/krate'], { cwd: path.join(repo, 'packages', 'compiler') });
}

function ensureKeys() {
  if (fs.existsSync(keyFile) && fs.existsSync(pubFile)) return fs.readFileSync(pubFile, 'utf8').trim();
  ensureKrate();
  fs.rmSync(keyFile, { force: true });
  const r = run(krateBin, ['native', 'keygen', '--key-out', keyFile], { capture: true });
  const m = /^\s*([A-Za-z0-9+/]{43}=)\s*$/m.exec(r.stdout);
  if (!m) fail(`could not read the public key from krate native keygen:\n${r.stdout}`);
  fs.writeFileSync(pubFile, m[1]);
  return m[1];
}

function withConfig(version, fn) {
  const pub = ensureKeys();
  const orig = fs.readFileSync(cfgPath, 'utf8');
  const marker = "version: '1.0.0',";
  if (!orig.includes(marker)) fail(`${cfgPath} no longer contains ${marker}`);
  const bundle = goos === 'windows' ? "\n    bundle: { windows: { publisher: 'CN=Krate CI' } }," : '';
  const edited = orig.replace(
    marker,
    `version: '${version}',\n    updater: { url: 'https://updates.example.invalid/latest-{target}.json', publicKey: '${pub}' },${bundle}`,
  );
  fs.writeFileSync(cfgPath, edited);
  try {
    return fn();
  } finally {
    fs.writeFileSync(cfgPath, orig);
  }
}

// withBadHost adds a Go file to the example whose package init ends the
// process 1.5 s after start: after the shell's first-start protection ran, but
// long before it would confirm a healthy start.
function withBadHost(fn) {
  const file = path.join(exampleDir, 'src', 'lib', 'zz_smoke_bad.go');
  fs.writeFileSync(
    file,
    'package lib\n\nimport (\n\t"os"\n\t"time"\n)\n\nfunc init() {\n\tgo func() {\n\t\ttime.Sleep(1500 * time.Millisecond)\n\t\tos.Exit(3)\n\t}()\n}\n',
  );
  try {
    return fn();
  } finally {
    fs.rmSync(file, { force: true });
  }
}

// buildUpdateVersions builds B (with a delta update from A) and Bad.
function buildUpdateVersions(formats, extraArgs = [], env = {}) {
  buildPackages('B', versionB, formats, [...extraArgs, '--delta-from', path.join(work, 'A', 'packages'), '--delta-count', '3'], env);
  withBadHost(() => buildPackages('Bad', versionB, formats, extraArgs, env));
}

// buildPackages builds and packages the example as one version into
// <work>/<label>.
function buildPackages(label, version, formats, extraArgs = [], env = {}) {
  ensureKrate();
  const out = path.join(work, label);
  fs.rmSync(out, { recursive: true, force: true });
  withConfig(version, () =>
    run(
      krateBin,
      ['native', 'build', exampleDir, '--release', '--package', '--format', formats.join(','), '--out', out, '--notes', `smoke ${version}`, ...extraArgs],
      { env: { GOWORK: 'off', KRATE_UPDATER_PRIVATE_KEY_FILE: keyFile, ...env } },
    ),
  );
  log(`packages for ${label}:`);
  for (const p of walk(path.join(out, 'packages'))) log(`  ${path.relative(out, p)}`);
}

// ---------------------------------------------------------------- launch

// Children start in their own process group on Unix: an AppImage runtime runs
// the app as its own child, which a plain kill of the runtime would orphan.
function start(cmd, args, opts) {
  return spawn(cmd, args, { ...opts, detached: !isWin });
}

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

function exited(child) {
  return new Promise((r) => (child.exitCode !== null ? r() : child.once('exit', r)));
}

// httpGet fetches a URL, following redirects and keeping cookies (browser
// mode answers the first request with a redirect that sets the session
// cookie).
async function httpGet(url, hops = 4) {
  let cookie = '';
  let cur = url;
  for (let i = 0; i <= hops; i++) {
    const res = await new Promise((resolve, reject) => {
      http
        .get(cur, { headers: cookie ? { Cookie: cookie } : {} }, (r) => {
          let body = '';
          r.setEncoding('utf8');
          r.on('data', (c) => (body += c));
          r.on('end', () => resolve({ status: r.statusCode, body, headers: r.headers }));
        })
        .on('error', reject);
    });
    const set = res.headers['set-cookie'];
    if (set) cookie = [cookie, ...set.map((c) => c.split(';')[0])].filter(Boolean).join('; ');
    if (res.status >= 300 && res.status < 400 && res.headers.location) {
      cur = new URL(res.headers.location, cur).toString();
      continue;
    }
    return res;
  }
  throw new Error(`too many redirects from ${url}`);
}

// launchCheck starts an installed app and checks that it reports the
// version, serves its first page in browser mode and, when a display exists,
// keeps a window open.
async function launchCheck(bin, { version, env = {}, window = true, settle = 0 } = {}) {
  log(`== launch check: ${bin}`);
  const v = run(bin, ['--version'], { capture: true, env });
  if (!v.stdout.includes(version)) fail(`--version printed ${JSON.stringify(v.stdout)}, want ${version}`);

  const child = start(bin, ['--browser'], { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, ...env } });
  let out = '';
  let err = '';
  child.stdout.on('data', (c) => (out += c));
  child.stderr.on('data', (c) => (err += c));
  try {
    let url = '';
    for (let i = 0; i < 300 && !url; i++) {
      const m = /^url (http\S+)/m.exec(out);
      if (m) url = m[1];
      else if (child.exitCode !== null) fail(`the app exited with ${child.exitCode} before serving:\n${out}\n${err}`);
      else await sleep(100);
    }
    if (!url) fail(`the app printed no url within 30s:\n${out}\n${err}`);
    const res = await httpGet(url);
    if (res.status !== 200 || !res.body.includes(productName)) fail(`first page: HTTP ${res.status}, body does not mention ${productName}:\n${res.body.slice(0, 400)}`);
    log(`first page served at ${url.replace(/\?.*/, '')} (${res.body.length} bytes)`);
    if (settle) await sleep(settle);
  } finally {
    killTree(child);
    await exited(child);
  }

  const hasDisplay = goos !== 'linux' || process.env.DISPLAY || process.env.WAYLAND_DISPLAY;
  if (!window || !hasDisplay) {
    log('no display: skipping the window check');
    return;
  }
  // KRATE_SMOKE_EXEC_WRAPPER prefixes the window launch (a local sandbox hook;
  // CI leaves it unset).
  const wrap = (process.env.KRATE_SMOKE_EXEC_WRAPPER || '').split(' ').filter(Boolean);
  const win = start(wrap[0] || bin, [...wrap.slice(1), ...(wrap.length ? [bin] : [])], { stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, ...env } });
  let werr = '';
  win.stderr.on('data', (c) => (werr += c));
  await sleep(8000);
  const alive = win.exitCode === null;
  killTree(win);
  await exited(win);
  if (!alive) fail(`the window app exited early (code ${win.exitCode}):\n${werr}`);
  log('window app stayed up for 8s');
}

// ---------------------------------------------------------------- update

function serveDir(dir) {
  const server = http.createServer((req, res) => {
    const name = path.basename(decodeURIComponent(new URL(req.url, 'http://x').pathname));
    const p = path.join(dir, name);
    if (!name || !fs.existsSync(p) || !fs.statSync(p).isFile()) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { 'Content-Length': fs.statSync(p).size, 'Content-Type': name.endsWith('.json') ? 'application/json' : 'application/octet-stream' });
    fs.createReadStream(p).pipe(res);
  });
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve({ port: server.address().port, close: () => server.close() })));
}

// updateFlow runs the real updater against the signed feed of version B and
// replaces the installed version A.
async function updateFlow({ exe, feedDir, appImage, delta = '0' }) {
  log(`== update ${versionA} -> ${versionB} (delta ${delta}): ${exe}`);
  const srv = await serveDir(feedDir);
  try {
    const target = `${goos}-${goarch}`;
    const feed = `http://127.0.0.1:${srv.port}/latest-{target}.json`;
    const probe = await httpGet(`http://127.0.0.1:${srv.port}/latest-${target}.json`);
    if (probe.status !== 200) fail(`the feed latest-${target}.json is not in ${feedDir}`);
    log(`feed: ${probe.body}`);
    await runAsync('go', ['test', '-count=1', '-run', 'TestSmokeUpdateInstalledApp', '-v', './updater'], {
      cwd: path.join(repo, 'packages', 'native'),
      env: {
        KRATE_UPDATE_SMOKE_FEED: feed,
        KRATE_UPDATE_SMOKE_KEY: fs.readFileSync(pubFile, 'utf8').trim(),
        KRATE_UPDATE_SMOKE_APP_ID: appId,
        KRATE_UPDATE_SMOKE_VERSION: versionA,
        KRATE_UPDATE_SMOKE_EXPECT: versionB,
        KRATE_UPDATE_SMOKE_EXE: exe,
        KRATE_UPDATE_SMOKE_APPIMAGE: appImage || '',
        KRATE_UPDATE_SMOKE_DELTA: delta,
        KRATE_UPDATE_SMOKE_ROLLBACK: appImage ? '' : '1',
      },
    });
  } finally {
    srv.close();
  }
  const bin = appImage || exe;
  const v = run(bin, ['--version'], { capture: true, env: appImage ? { APPIMAGE_EXTRACT_AND_RUN: '1' } : {} });
  if (!v.stdout.includes(versionB)) fail(`after the update ${bin} reports ${JSON.stringify(v.stdout)}, want ${versionB}`);
  log(`updated: ${v.stdout.trim()}`);
}

// Environment of the launch that must confirm a healthy start at once.
const confirmEnv = { KRATE_UPDATER_HEALTH_GRACE_MS: '0' };

// keptPrevious lists what an unconfirmed update keeps next to the install.
function keptPrevious(exe) {
  const dir = path.dirname(exe);
  const hits = [];
  for (const d of [dir, ...[1, 2, 3, 4].map((n) => Array.from({ length: n }).reduce((acc) => path.dirname(acc), dir))]) {
    if (!fs.existsSync(d)) continue;
    for (const n of fs.readdirSync(d)) if (/^\.krate-prev$|\.app\.prev$/.test(n)) hits.push(path.join(d, n));
  }
  return hits;
}

// deltaFlow updates the installed A to B with the delta update, starts B (which
// confirms a healthy start) and checks that the kept copy of A is gone.
async function deltaFlow({ exe, feedDir }) {
  await updateFlow({ exe, feedDir, delta: '1' });
  if (keptPrevious(exe).length === 0) fail('the previous version was not kept for rollback after the update');
  await launchCheck(exe, { version: versionB, window: false, env: confirmEnv, settle: 800 });
  let left = keptPrevious(exe);
  for (let i = 0; i < 20 && left.length; i++) {
    await sleep(250);
    left = keptPrevious(exe);
  }
  if (left.length) fail(`the previous version is still kept after B confirmed a healthy start: ${left.join(', ')}`);
  log('B confirmed a healthy start; the previous version was deleted');
}

// rollbackFlow updates a fresh install of A to Bad, whose host exits before it
// can confirm a healthy start: the next launch must restore A.
async function rollbackFlow({ exe, feedDir }) {
  log('== rollback flow');
  await updateFlow({ exe, feedDir, delta: '0' });
  // First start of Bad: it dies on its own (exit 3) before confirming.
  const first = spawnSync(exe, ['--browser'], { encoding: 'utf8', timeout: 60000, env: process.env });
  log(`first start of ${versionB}: exit ${first.status}`);
  if (first.status === 0 || first.status === null) fail(`the bad build should have exited with an error, got ${first.status} ${first.error || ''}\n${first.stdout}\n${first.stderr}`);
  // The next start restores A. NO_RELAUNCH keeps A from starting detached.
  const second = spawnSync(exe, ['--browser'], { encoding: 'utf8', timeout: 60000, env: { ...process.env, KRATE_UPDATER_ROLLBACK_NO_RELAUNCH: '1' } });
  log(`second start: exit ${second.status}\n${second.stderr}`);
  if (second.status !== 0 || !/rolled back/.test(second.stderr)) fail(`the second start should roll back and exit 0:\n${second.stdout}\n${second.stderr}`);
  const v = run(exe, ['--version'], { capture: true });
  if (!v.stdout.includes(versionA)) fail(`after the rollback ${exe} reports ${JSON.stringify(v.stdout)}, want ${versionA}`);
  await runAsync('go', ['test', '-count=1', '-run', 'TestSmokeRollbackState', '-v', './updater'], {
    cwd: path.join(repo, 'packages', 'native'),
    env: {
      KRATE_UPDATE_SMOKE_APP_ID: appId,
      KRATE_UPDATE_SMOKE_ROLLED_BACK_FROM: versionB,
      KRATE_UPDATE_SMOKE_VERSION: versionA,
    },
  });
  await launchCheck(exe, { version: versionA, window: false });
  log('rolled back to ' + versionA);
}

// ---------------------------------------------------------------- linux

async function linux(step) {
  const sudo = process.env.KRATE_SMOKE_NO_SUDO ? null : 'sudo';
  const appImageTool = () => which('appimagetool');
  switch (step) {
    case 'prepare': {
      const formats = ['tar.gz', 'deb', 'rpm'];
      if (appImageTool()) formats.push('appimage');
      else skipOrFail('appimagetool is not on PATH; the AppImage is not built');
      buildPackages('A', versionA, formats);
      buildUpdateVersions(['tar.gz']);
      return;
    }
    case 'deb': {
      const deb = findPackage('A', /\.deb$/);
      run('dpkg-deb', ['--info', deb]);
      run('dpkg-deb', ['--contents', deb]);
      let bin;
      if (sudo) {
        const r = run(sudo, ['dpkg', '-i', deb], { allowFail: true });
        if (r.status !== 0) run(sudo, ['apt-get', 'install', '-f', '-y']);
        run('dpkg', ['-s', 'native-tasks']);
        run('dpkg', ['-L', 'native-tasks']);
        bin = '/usr/bin/native-tasks';
        for (const f of [bin, `/usr/share/applications/${appId}.desktop`]) if (!fs.existsSync(f)) fail(`${f} was not installed`);
      } else {
        const root = path.join(work, 'deb-root');
        fs.rmSync(root, { recursive: true, force: true });
        run('dpkg-deb', ['-x', deb, root]);
        bin = path.join(root, 'usr', 'bin', 'native-tasks');
        if (!fs.existsSync(bin)) fail(`${bin} is not in the package`);
      }
      await launchCheck(bin, { version: versionA });
      if (sudo) {
        run(sudo, ['dpkg', '-r', 'native-tasks']);
        if (fs.existsSync('/usr/bin/native-tasks')) fail('/usr/bin/native-tasks is still there after dpkg -r');
        log('uninstalled');
      }
      return;
    }
    case 'rpm': {
      const rpm = findPackage('A', /\.rpm$/);
      if (!which('rpm')) return skipOrFail('rpm is not installed');
      run('rpm', ['-qpi', rpm]);
      const list = run('rpm', ['-qpl', rpm], { capture: true }).stdout;
      for (const want of ['/usr/bin/native-tasks', `/usr/share/applications/${appId}.desktop`]) {
        if (!list.includes(want)) fail(`the rpm does not contain ${want}:\n${list}`);
      }
      const req = run('rpm', ['-qp', '--requires', rpm], { capture: true }).stdout;
      if (!/webkit2gtk4\.1/.test(req) || !/gtk3/.test(req)) fail(`unexpected rpm requirements:\n${req}`);
      return;
    }
    case 'appimage': {
      if (!appImageTool()) return skipOrFail('appimagetool is not on PATH');
      const img = findPackage('A', /\.AppImage$/);
      fs.chmodSync(img, 0o755);
      await launchCheck(img, { version: versionA, env: { APPIMAGE_EXTRACT_AND_RUN: '1' } });
      return;
    }
    case 'update': {
      const tgz = findPackage('A', /\.tar\.gz$/);
      const dir = path.join(work, 'install-linux');
      fs.rmSync(dir, { recursive: true, force: true });
      fs.mkdirSync(dir, { recursive: true });
      run('tar', ['-xzf', tgz, '-C', dir]);
      const exe = walk(dir).find((p) => path.basename(p) === 'native-tasks' || path.basename(p) === productName);
      if (!exe) fail(`no executable in ${tgz}: ${walk(dir).join(', ')}`);
      await launchCheck(exe, { version: versionA, window: false });
      await deltaFlow({ exe, feedDir: path.join(work, 'B', 'packages') });
      const dir2 = path.join(work, 'install-linux-rb');
      fs.rmSync(dir2, { recursive: true, force: true });
      fs.mkdirSync(dir2, { recursive: true });
      run('tar', ['-xzf', tgz, '-C', dir2]);
      const exe2 = walk(dir2).find((p) => path.basename(p) === path.basename(exe));
      await rollbackFlow({ exe: exe2, feedDir: path.join(work, 'Bad', 'packages') });
      return;
    }
    default:
      fail(`unknown linux step ${step}`);
  }
}

// ---------------------------------------------------------------- macos

async function macos(step) {
  const installDir = path.join(work, 'install-macos');
  const appName = `${productName}.app`;
  const appExe = (dir) => path.join(dir, appName, 'Contents', 'MacOS', productName);
  switch (step) {
    case 'prepare':
      // --sign without an identity signs ad hoc (codesign --sign -).
      buildPackages('A', versionA, ['app', 'zip', 'dmg'], ['--sign']);
      buildUpdateVersions(['zip'], ['--sign']);
      return;
    case 'dmg': {
      const dmg = findPackage('A', /\.dmg$/);
      const mnt = path.join(work, 'mnt');
      fs.rmSync(mnt, { recursive: true, force: true });
      fs.mkdirSync(mnt, { recursive: true });
      run('hdiutil', ['verify', dmg]);
      run('hdiutil', ['attach', '-nobrowse', '-readonly', '-mountpoint', mnt, dmg]);
      try {
        const apps = fs.readdirSync(mnt).filter((n) => n.endsWith('.app'));
        if (apps.length !== 1) fail(`the dmg holds ${JSON.stringify(fs.readdirSync(mnt))}, want one .app`);
        fs.rmSync(installDir, { recursive: true, force: true });
        fs.mkdirSync(installDir, { recursive: true });
        run('ditto', [path.join(mnt, apps[0]), path.join(installDir, apps[0])]);
      } finally {
        run('hdiutil', ['detach', mnt], { allowFail: true });
      }
      const app = path.join(installDir, appName);
      run('codesign', ['--verify', '--deep', '--strict', '--verbose=2', app]);
      run('codesign', ['-dv', '--verbose=2', app], { allowFail: true });
      run('plutil', ['-p', path.join(app, 'Contents', 'Info.plist')]);
      await launchCheck(appExe(installDir), { version: versionA });
      return;
    }
    case 'update': {
      if (!fs.existsSync(appExe(installDir))) fail(`run the dmg step first: ${appExe(installDir)} is missing`);
      await deltaFlow({ exe: appExe(installDir), feedDir: path.join(work, 'B', 'packages') });
      run('codesign', ['--verify', '--deep', '--strict', path.join(installDir, appName)], { allowFail: true });
      const dir2 = path.join(work, 'install-macos-rb');
      fs.rmSync(dir2, { recursive: true, force: true });
      fs.mkdirSync(dir2, { recursive: true });
      run('ditto', ['-x', '-k', findPackage('A', /\.zip$/), dir2]);
      await rollbackFlow({ exe: appExe(dir2), feedDir: path.join(work, 'Bad', 'packages') });
      return;
    }
    default:
      fail(`unknown macos step ${step}`);
  }
}

// ---------------------------------------------------------------- windows

function signtoolDir() {
  if (which('signtool')) return '';
  const roots = ['C:\\Program Files (x86)\\Windows Kits\\10\\bin', 'C:\\Program Files\\Windows Kits\\10\\bin'];
  const found = [];
  for (const r of roots) {
    if (!fs.existsSync(r)) continue;
    for (const v of fs.readdirSync(r)) {
      const d = path.join(r, v, 'x64');
      if (fs.existsSync(path.join(d, 'signtool.exe'))) found.push(d);
    }
  }
  found.sort();
  return found.length ? found[found.length - 1] : '';
}

function windowsEnv() {
  const env = {};
  const cert = JSON.parse(fs.readFileSync(certEnvFile, 'utf8'));
  env.KRATE_WINDOWS_PFX = cert.pfx;
  env.KRATE_WINDOWS_PFX_PASSWORD = cert.password;
  const d = signtoolDir();
  if (d) env.PATH = `${d}${path.delimiter}${process.env.PATH}`;
  return env;
}

async function windows(step) {
  const installDir = path.join(work, 'install-windows');
  switch (step) {
    case 'cert': {
      fs.mkdirSync(work, { recursive: true });
      const pfx = path.join(work, 'ci.pfx');
      const cer = path.join(work, 'ci.cer');
      const password = 'krate-ci-' + Math.random().toString(36).slice(2);
      ps(
        [
          "$ErrorActionPreference = 'Stop'",
          '$cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=Krate CI" -CertStoreLocation Cert:\\CurrentUser\\My -KeyExportPolicy Exportable -KeySpec Signature -NotAfter (Get-Date).AddDays(3)',
          `$pw = ConvertTo-SecureString -String '${password}' -AsPlainText -Force`,
          `Export-PfxCertificate -Cert $cert -FilePath '${pfx}' -Password $pw | Out-Null`,
          `Export-Certificate -Cert $cert -FilePath '${cer}' | Out-Null`,
          // Trust it so signtool verify, the MSIX install and Authenticode checks accept it.
          `Import-Certificate -FilePath '${cer}' -CertStoreLocation Cert:\\LocalMachine\\Root | Out-Null`,
          `Import-Certificate -FilePath '${cer}' -CertStoreLocation Cert:\\LocalMachine\\TrustedPeople | Out-Null`,
          'Write-Output $cert.Thumbprint',
        ].join('; '),
      );
      fs.writeFileSync(certEnvFile, JSON.stringify({ pfx, password }));
      if (!signtoolDir() && !which('signtool')) skipOrFail('signtool was not found in the Windows SDK');
      return;
    }
    case 'prepare': {
      if (!which('makensis')) skipOrFail('makensis is not on PATH (choco install nsis)');
      const env = windowsEnv();
      const formats = ['zip', 'nsis', 'msix'];
      buildPackages('A', versionA, formats, ['--sign'], env);
      buildUpdateVersions(['zip'], ['--sign'], env);
      return;
    }
    case 'nsis': {
      const setup = findPackage('A', /-setup\.exe$/);
      verifySignature(setup);
      fs.rmSync(installDir, { recursive: true, force: true });
      // /D must be the last argument and must not be quoted.
      run(setup, ['/S', `/D=${installDir}`]);
      const exe = path.join(installDir, `${productName}.exe`);
      for (let i = 0; i < 100 && !fs.existsSync(exe); i++) await sleep(200);
      for (const f of [exe, path.join(installDir, 'Uninstall.exe')]) if (!fs.existsSync(f)) fail(`the installer did not create ${f}`);
      verifySignature(exe);
      ps(`if (-not (Test-Path 'HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${appId}')) { throw 'no uninstall registry entry' }`);
      await launchCheck(exe, { version: versionA });
      // _?= keeps the uninstaller in place so -Wait works; remove it after.
      run(path.join(installDir, 'Uninstall.exe'), ['/S', `_?=${installDir}`]);
      ps(`if (Test-Path 'HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${appId}') { throw 'the uninstall registry entry is still there' }`);
      if (fs.existsSync(exe)) fail(`${exe} is still there after the uninstaller ran`);
      fs.rmSync(installDir, { recursive: true, force: true });
      log('installed, launched and uninstalled');
      return;
    }
    case 'msix': {
      const msix = findPackage('A', /\.msix$/);
      const sdk = signtoolDir() || path.dirname(which('makeappx') || '');
      const makeappx = which('makeappx') || (sdk && fs.existsSync(path.join(sdk, 'makeappx.exe')) ? path.join(sdk, 'makeappx.exe') : '');
      const unpacked = path.join(work, 'msix-unpacked');
      fs.rmSync(unpacked, { recursive: true, force: true });
      if (makeappx) {
        // makeappx validates the manifest and the block map our packager wrote.
        run(makeappx, ['unpack', '/p', msix, '/d', unpacked, '/l']);
      } else {
        skipOrFail('makeappx was not found in the Windows SDK');
      }
      verifySignature(msix);
      ps(`Add-AppxPackage -Path '${msix}'`);
      try {
        const info = ps(
          "$p = Get-AppxPackage | Where-Object { $_.Name -like '*Tasks*' -or $_.Name -like '*example*' } | Select-Object -First 1; if (-not $p) { throw 'the MSIX is not installed' }; Write-Output ($p.PackageFamilyName + '|' + $p.InstallLocation)",
          { capture: true },
        ).stdout.trim().split('\n').pop();
        const [pfn, loc] = info.trim().split('|');
        const manifest = fs.readFileSync(path.join(loc, 'AppxManifest.xml'), 'utf8');
        const id = /<Application\s+Id="([^"]+)"/.exec(manifest)?.[1];
        if (!id) fail('no Application Id in the installed manifest');
        ps(`Start-Process 'shell:AppsFolder\\${pfn}!${id}'`);
        let running = false;
        for (let i = 0; i < 60 && !running; i++) {
          await sleep(500);
          running = spawnSync('tasklist', ['/FI', `IMAGENAME eq ${productName}.exe`], { encoding: 'utf8' }).stdout.includes(productName);
        }
        spawnSync('taskkill', ['/IM', `${productName}.exe`, '/F'], { stdio: 'ignore' });
        if (!running) fail('the MSIX app did not start');
        log('MSIX app started');
      } finally {
        ps("Get-AppxPackage | Where-Object { $_.Name -like '*Tasks*' -or $_.Name -like '*example*' } | Remove-AppxPackage", { allowFail: true });
      }
      return;
    }
    case 'update': {
      const zip = findPackage('A', /-windows-.*\.zip$|\.zip$/);
      fs.rmSync(installDir, { recursive: true, force: true });
      ps(`Expand-Archive -Path '${zip}' -DestinationPath '${installDir}' -Force`);
      const exe = walk(installDir).find((p) => path.basename(p) === `${productName}.exe`);
      if (!exe) fail(`no ${productName}.exe in ${zip}`);
      await launchCheck(exe, { version: versionA, window: false });
      await deltaFlow({ exe, feedDir: path.join(work, 'B', 'packages') });
      const dir2 = `${installDir}-rb`;
      fs.rmSync(dir2, { recursive: true, force: true });
      ps(`Expand-Archive -Path '${zip}' -DestinationPath '${dir2}' -Force`);
      const exe2 = walk(dir2).find((p) => path.basename(p) === `${productName}.exe`);
      await rollbackFlow({ exe: exe2, feedDir: path.join(work, 'Bad', 'packages') });
      return;
    }
    default:
      fail(`unknown windows step ${step}`);
  }
}

function verifySignature(file) {
  ps(
    `$s = Get-AuthenticodeSignature -FilePath '${file}'; Write-Output ($s.Status.ToString() + ' ' + $s.SignerCertificate.Subject); if ($s.Status -ne 'Valid') { throw ('not validly signed: ' + $s.Status) }`,
  );
}

// ---------------------------------------------------------------- main

const [osName, step] = process.argv.slice(2);
const table = { linux, macos, windows };
if (!table[osName] || !step) {
  console.error('usage: node scripts/native-smoke.mjs <linux|macos|windows> <step>');
  process.exit(2);
}
if (osName !== goos && !(osName === 'macos' && goos === 'darwin')) {
  console.error(`this is ${goos}, not ${osName}`);
  process.exit(2);
}
fs.mkdirSync(work, { recursive: true });
try {
  await table[osName](step);
  log(`OK: ${osName} ${step}`);
} catch (e) {
  console.error(`FAIL: ${osName} ${step}: ${e.message}`);
  process.exit(1);
}
