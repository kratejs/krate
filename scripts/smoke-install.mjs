#!/usr/bin/env node
/**
 * Installs the packages a release would publish and runs them, the way a user
 * would. Unit tests and manifest checks all passed for two releases whose
 * install failed with EACCES, so this checks the outcome instead of a cause:
 * tarballs made by the publishing pnpm, installed by npm in a clean folder, and
 * a scaffolded project built by the installed `krate`.
 *
 * The version must not be on the registry yet, as at release time. npm would
 * otherwise install the published platform packages next to these tarballs, so
 * the result would say nothing about this release.
 *
 * Needs built JS packages and dist-platform/ for this machine:
 *   node scripts/set-version.mjs 0.0.0-smoke.0
 *   KRATE_ONLY_PLATFORM=core-linux-x64 node scripts/build-platform-packages.mjs
 *   node scripts/smoke-install.mjs
 *
 * On Alpine it picks core-linux-x64-musl, as @krate/core does at run time.
 * CI runs it on Linux, macOS and Windows, and in node:alpine.
 */

import { spawnSync } from 'node:child_process';
import { constants, accessSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { platforms } from './platform-package.mjs';
import { platformFor, spawnPlan } from './smoke-helpers.mjs';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const isWindows = process.platform === 'win32';
const packageDirs = ['plugin', 'runtime', 'components', 'ts-plugin', 'base-docs-theme', 'core'];
const templates = [
  ['create-krate-app/templates/app', 'dist/index.html'],
  ['create-krate-docs/templates/docs', 'dist/index.html'],
];

class SmokeError extends Error {}

function fail(message) {
  throw new SmokeError(message);
}

function run(command, args, cwd) {
  const plan = spawnPlan(command, args);
  const res = spawnSync(plan.command, plan.args, { cwd, encoding: 'utf8', shell: plan.shell });
  if (res.status !== 0) {
    const why = res.error ? `\n${res.error.message}` : '';
    fail(`${command} ${args.join(' ')} failed in ${cwd}${why}\n${res.stdout}\n${res.stderr}`);
  }
  return res.stdout;
}

function pack(dir, dest) {
  const before = new Set(readdirSync(dest));
  run('pnpm', ['pack', '--pack-destination', dest], dir);
  const made = readdirSync(dest).find((f) => !before.has(f));
  if (!made) fail(`pnpm pack wrote no tarball for ${dir}`);
  return join(dest, made);
}

function main() {
  const target = platformFor(platforms);
  if (!target) fail(`no platform package for ${process.platform}-${process.arch}`);
  const platformDir = join(root, 'dist-platform', target.pkg);
  if (!existsSync(join(platformDir, 'bin', `krate${target.ext}`))) {
    fail(`${platformDir} has no binary; run KRATE_ONLY_PLATFORM=${target.pkg} node scripts/build-platform-packages.mjs first`);
  }

  const version = JSON.parse(readFileSync(join(platformDir, 'package.json'), 'utf8')).version;
  const other = platforms.find((t) => t.pkg !== target.pkg);
  const lookup = spawnPlan('npm', ['view', `@krate/${other.pkg}@${version}`, 'version']);
  const published = spawnSync(lookup.command, lookup.args, { encoding: 'utf8', shell: lookup.shell });
  if (published.status === 0 && published.stdout.trim() !== '') {
    fail(`@krate/${other.pkg}@${version} is already on npm, so it would be installed instead of this build; run scripts/set-version.mjs with an unpublished version`);
  }
  if (published.status !== 0 && !/E404|404/.test(`${published.stderr}${published.stdout}`)) {
    console.warn(`smoke-install: could not ask npm whether ${version} is published (${(published.stderr || published.error?.message || '').trim().split('\n')[0]}); continuing`);
  }

  const work = mkdtempSync(join(tmpdir(), 'krate-smoke-'));
  try {
    const tarballDir = join(work, 'tarballs');
    mkdirSync(tarballDir);
    const tarballs = [pack(platformDir, tarballDir)];
    for (const dir of packageDirs) tarballs.push(pack(join(root, 'packages', dir), tarballDir));

    for (const [template, output] of templates) {
      const app = join(work, template.split('/').pop());
      cpSync(join(root, 'packages', template), app, { recursive: true });
      renameSync(join(app, 'gitignore'), join(app, '.gitignore'));

      // @krate/* are not on the registry at this version yet, so they come from
      // the tarballs. Everything else installs as it would for a user.
      const manifestPath = join(app, 'package.json');
      const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
      manifest.name = 'krate-smoke';
      for (const field of ['dependencies', 'devDependencies']) {
        for (const name of Object.keys(manifest[field] ?? {})) {
          if (name.startsWith('@krate/')) delete manifest[field][name];
        }
      }
      writeFileSync(manifestPath, JSON.stringify(manifest, null, 2));
      run('npm', ['install', '--no-audit', '--no-fund', ...tarballs], app);

      // Run the binary directly first: the wrapper repairs a lost exec bit, which
      // would hide a package published without it.
      const binary = join(app, 'node_modules', '@krate', target.pkg, 'bin', `krate${target.ext}`);
      if (!isWindows) {
        try {
          accessSync(binary, constants.X_OK);
        } catch {
          fail(`${binary} was installed without the executable bit`);
        }
      }
      const direct = run(binary, ['version'], app);
      if (!direct.includes(version)) fail(`the binary reports ${direct.trim()}, expected ${version}`);

      const viaNpx = run('npx', ['--no', 'krate', 'version'], app);
      if (!viaNpx.includes(version)) fail(`npx krate version printed ${viaNpx.trim()}, expected ${version}`);
      run('npx', ['--no', 'krate', 'build'], app);
      if (!existsSync(join(app, output))) fail(`${template}: krate build wrote no ${output}`);
      console.log(`smoke-install: ${template} ok (${target.pkg})`);
    }
  } finally {
    // Windows can hold a just-run executable for a moment; a leftover temp
    // folder must not turn a passing run into a failure.
    try {
      rmSync(work, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
    } catch (err) {
      console.warn(`smoke-install: could not remove ${work}: ${err.message}`);
    }
  }
}

try {
  main();
} catch (err) {
  if (!(err instanceof SmokeError)) throw err;
  console.error(`smoke-install: ${err.message}`);
  process.exit(1);
}
