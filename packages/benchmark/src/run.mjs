#!/usr/bin/env node
// Benchmark orchestrator.
//
//   node src/run.mjs build [--filter <id>] [--runs N]
//   node src/run.mjs api   [--filter <id>] [--duration ms] [--connections N]
//   node src/run.mjs all   [same flags]
//   node src/run.mjs report
import os from 'node:os';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { BUILD_FRAMEWORKS, API_FRAMEWORKS, npxCmd, exeName } from './lib/frameworks.mjs';
import {
  pkgRoot,
  exists,
  run,
  timed,
  spawnProcess,
  freePort,
  waitForUrl,
  dirSize,
  readJson,
  mkdirp,
  rmrf,
} from './lib/exec.mjs';
import { loadTest } from './lib/loadtest.mjs';
import { median, formatMs, writeResults, renderMarkdown } from './lib/report.mjs';

const RESULTS_DIR = join(pkgRoot, 'results');
const BIN_DIR = join(pkgRoot, '.krate-bench');

function log(...a) {
  process.stdout.write(a.join(' ') + '\n');
}
function warn(...a) {
  process.stderr.write('[warn] ' + a.join(' ') + '\n');
}

function detectPm() {
  const ua = process.env.npm_config_user_agent ?? '';
  if (ua.startsWith('pnpm')) return 'pnpm';
  if (ua.startsWith('yarn')) return 'yarn';
  if (ua.startsWith('bun')) return 'bun';
  return 'npm';
}

function parseArgs(argv) {
  const opts = { command: 'all', filter: null, runs: 3, durationMs: 5000, connections: 32, pm: detectPm() };
  const args = argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    switch (a) {
      case 'build':
      case 'api':
      case 'all':
      case 'report':
        opts.command = a;
        break;
      case '--filter':
        opts.filter = args[++i];
        break;
      case '--runs':
        opts.runs = parseInt(args[++i], 10) || 3;
        break;
      case '--duration':
        opts.durationMs = parseInt(args[++i], 10) || 5000;
        break;
      case '--connections':
        opts.connections = parseInt(args[++i], 10) || 32;
        break;
      case '--pm':
        opts.pm = args[++i];
        break;
      case '-h':
      case '--help':
        log(HELP);
        process.exit(0);
      default:
        warn(`unknown option: ${a}`);
        process.exit(1);
    }
  }
  return opts;
}

const HELP = `
Usage: node src/run.mjs <build|api|all|report> [options]

Commands:
  build    Time cold production builds across frameworks
  api      Measure API throughput across servers
  all      Run build + api (default)
  report   Print the latest results as Markdown

Options:
  --filter <id>     Only run one framework (e.g. krate, nextjs, vite)
  --runs N          Number of build timing runs (default 3)
  --duration ms     API load-test duration (default 5000)
  --connections N   API load-test concurrency (default 32)
  --pm <name>       Package manager for installs: npm|pnpm|yarn|bun (default: detected)

Dependencies are installed automatically the first time a fixture is run.
`;

// Build the krate CLI once (cached) so build timing excludes Go toolchain setup.
async function ensureKrateBin() {
  const bin = join(BIN_DIR, exeName('krate'));
  if (exists(bin)) return bin;
  await mkdirp(BIN_DIR);
  const compilerDir = join(pkgRoot, '..', 'compiler');
  log('building krate CLI...');
  const res = await run('go', ['build', '-o', bin, './cmd/krate'], { cwd: compilerDir });
  if (res.code !== 0) throw new Error('failed to build krate CLI:\n' + res.stderr);
  return bin;
}

async function collectMeta() {
  const go = await run('go', ['version']);
  return {
    date: new Date().toISOString(),
    node: process.version,
    go: go.stdout.trim().replace(/^go version go/, 'go'),
    os: `${os.type()} ${os.release()}`,
    arch: process.arch,
    cpus: os.cpus().length,
    model: os.cpus()[0]?.model ?? '',
  };
}

function fixtureDir(id) {
  return join(pkgRoot, 'fixtures', id);
}

async function maybeInstall(fw, opts) {
  if (!fw.install) return true;
  if (existsSync(join(fixtureDir(fw.fixture), 'node_modules'))) return true;
  log(`installing ${fw.name} (${opts.pm})...`);
  const pmCmd = opts.pm === 'npm' ? 'npm' : opts.pm;
  const res = await run(pmCmd, ['install'], { cwd: fixtureDir(fw.fixture) });
  if (res.code !== 0) {
    warn(`install failed for ${fw.name}:\n${res.stderr.slice(-500)}`);
    return false;
  }
  return true;
}

async function installedVersion(pkgName, dir) {
  const p = join(dir, 'node_modules', pkgName, 'package.json');
  if (!existsSync(p)) return null;
  try {
    return (await readJson(p)).version ?? null;
  } catch {
    return null;
  }
}

async function runBuild(opts, krateBin) {
  const rows = [];
  for (const fw of BUILD_FRAMEWORKS) {
    if (opts.filter && fw.id !== opts.filter) continue;
    if (!(await maybeInstall(fw, opts))) continue;

    const dir = fixtureDir(fw.fixture);
    const ctx = { workDir: dir, krateBin };

    if (fw.prebuild) {
      const s = fw.prebuild(ctx);
      await run(s.cmd, s.args, { cwd: s.cwd ?? dir });
    }

    const times = [];
    for (let i = 0; i < opts.runs; i++) {
      const s = fw.build(ctx);
      const t = await timed(s.cmd, s.args, { cwd: s.cwd ?? dir });
      if (t.code !== 0) {
        warn(`${fw.name} build failed:\n${t.stderr.slice(-600)}`);
        times.length = 0;
        break;
      }
      times.push(t.ms);
    }
    if (!times.length) continue;

    const version = fw.versionPackage ? await installedVersion(fw.versionPackage, dir) : 'source';
    const outputBytes = await dirSize(join(dir, fw.outDir));
    rows.push({
      id: fw.id,
      name: fw.name,
      version,
      medianMs: Math.round(median(times)),
      bestMs: Math.min(...times),
      outputBytes,
      runs: times,
    });
    log(`  ${fw.name}: median ${formatMs(median(times))}, best ${formatMs(Math.min(...times))}, output ${outputBytes} B`);
  }
  return rows;
}

async function runApi(opts, krateBin) {
  const rows = [];
  for (const fw of API_FRAMEWORKS) {
    if (opts.filter && fw.id !== opts.filter) continue;
    if (!(await maybeInstall(fw, opts))) continue;

    const dir = fixtureDir(fw.fixture);
    const ctx = { workDir: dir, krateBin };

    try {
      await fw.prepare(ctx);
    } catch (err) {
      warn(`${fw.name} prepare failed: ${err.message}`);
      continue;
    }

    const port = await freePort();
    const handle = await fw.start(ctx, port);
    const url = `http://127.0.0.1:${port}${fw.path}`;
    const ready = await waitForUrl(url);
    if (!ready) {
      warn(`${fw.name} did not become ready; skipping`);
      await handle.stop();
      continue;
    }

    const result = await loadTest(url, {
      durationMs: opts.durationMs,
      connections: opts.connections,
    });
    await handle.stop();

    rows.push({
      id: fw.id,
      name: fw.name,
      rps: result.rps,
      p50: result.latency.p50,
      p99: result.latency.p99,
      errors: result.errors,
      requests: result.requests,
      durationMs: result.durationMs,
    });
    log(`  ${fw.name}: ${result.rps.toLocaleString()} req/s, p50 ${result.latency.p50}ms, p99 ${result.latency.p99}ms`);
  }
  return rows;
}

async function main() {
  const opts = parseArgs(process.argv);

  if (opts.command === 'report') {
    try {
      const results = await readJson(join(RESULTS_DIR, 'latest.json'));
      process.stdout.write(renderMarkdown(results));
    } catch {
      warn('no results yet — run `node src/run.mjs all` first');
      process.exit(1);
    }
    return;
  }

  const meta = await collectMeta();
  const krateBin = await ensureKrateBin();

  const results = {
    meta,
    build: null,
    api: null,
    buildMeta: { runs: opts.runs },
    apiMeta: { connections: opts.connections, durationMs: opts.durationMs },
  };

  if (opts.command === 'build' || opts.command === 'all') {
    log('\n— build time —');
    results.build = await runBuild(opts, krateBin);
  }
  if (opts.command === 'api' || opts.command === 'all') {
    log('\n— api throughput —');
    results.api = await runApi(opts, krateBin);
  }

  const out = await writeResults(results, RESULTS_DIR);
  log('\n' + out.markdown);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
