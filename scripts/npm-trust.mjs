#!/usr/bin/env node
/**
 * Makes npm trust the release workflow to publish every Krate package
 * (trusted publishing), creating any package that is not on npm yet.
 *
 *   node scripts/npm-trust.mjs [--dry-run] [name...]
 *
 * Run it from a machine logged in to npm (`npm login`) with an account that
 * can publish the packages and has 2FA on. It needs npm 11.15.0 or later for
 * `npm trust`. With no names it covers every package a release publishes.
 *
 * Trusted publishing cannot create a package, so a package that is not on npm
 * gets a placeholder first: version 0.0.0-bootstrap.0, deprecated, with no
 * code. npm makes a package's first version `latest` whatever the tag, so the
 * placeholder stays `latest` until the first stable release moves it; the
 * deprecation message says to install a released version.
 *
 * Packages that already trust the workflow are left alone, so the script can
 * be re-run, and is the step to run whenever a release adds a package.
 */

import { spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { viewPackage } from './publish-packages.mjs';
import { repo } from './platform-package.mjs';
import { npmPackageNames, trustedPublisher } from './release-packages.mjs';

const shell = process.platform === 'win32';
export const bootstrapVersion = '0.0.0-bootstrap.0';
const minNpm = [11, 15, 0];

/** Whether `npm trust list` output names this repository's release workflow. */
export function trustsWorkflow(listOutput) {
  return listOutput.includes(trustedPublisher.repository) && listOutput.includes(trustedPublisher.workflow);
}

/**
 * What to do for each package: create a placeholder when it is not on npm,
 * then trust the workflow unless it already does. A package that trusts
 * another publisher is reported, not changed (npm allows one configuration).
 *
 * @param {string[]} names
 * @param {{ exists: (name: string) => boolean | Error, trustList: (name: string) => string | Error }} probe
 */
export function planTrust(names, { exists, trustList }) {
  return names.map((name) => {
    const found = exists(name);
    if (found instanceof Error) return { name, error: `cannot tell whether it is on npm: ${found.message}` };
    if (!found) return { name, bootstrap: true, trust: true };
    const list = trustList(name);
    if (list instanceof Error) return { name, error: `npm trust list failed: ${list.message}` };
    if (trustsWorkflow(list)) return { name, bootstrap: false, trust: false };
    if (/github|gitlab|circleci/i.test(list)) return { name, error: `trusts another publisher; revoke it with npm trust list/revoke, then re-run:\n${list.trim()}` };
    return { name, bootstrap: false, trust: true };
  });
}

/** The placeholder package.json for name. */
export function placeholderManifest(name) {
  return {
    name,
    version: bootstrapVersion,
    description: 'Placeholder that lets the Krate release workflow publish this package with npm trusted publishing. Install a released version.',
    license: 'MIT',
    repository: { type: 'git', url: `git+${repo.url}` },
    homepage: repo.homepage,
  };
}

function run(args, opts = {}) {
  return spawnSync('npm', args, { encoding: 'utf8', shell, ...opts });
}

function trustList(name) {
  const res = run(['trust', 'list', name, '--json']);
  if (res.status === 0) return res.stdout;
  return new Error((res.stderr || res.stdout || res.error?.message || 'failed').trim());
}

function npmVersionOK() {
  const res = run(['--version']);
  const v = (res.stdout || '').trim().split('.').map(Number);
  for (let i = 0; i < 3; i++) {
    if ((v[i] ?? 0) !== minNpm[i]) return { ok: (v[i] ?? 0) > minNpm[i], version: res.stdout.trim() };
  }
  return { ok: true, version: res.stdout.trim() };
}

function bootstrap(name) {
  const dir = mkdtempSync(join(tmpdir(), 'krate-npm-bootstrap-'));
  try {
    writeFileSync(join(dir, 'package.json'), `${JSON.stringify(placeholderManifest(name), null, 2)}\n`);
    writeFileSync(join(dir, 'README.md'), `# ${name}\n\nPlaceholder. Install a released version of ${name}; see ${repo.homepage}.\n`);
    if (run(['publish', '--access', 'public', '--tag', 'bootstrap'], { cwd: dir, stdio: 'inherit' }).status !== 0) return false;
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  return run(['deprecate', `${name}@${bootstrapVersion}`, 'Placeholder for trusted publishing; install a released version.'], { stdio: 'inherit' }).status === 0;
}

function trust(name) {
  const args = ['trust', 'github', name, '--repo', trustedPublisher.repository, '--file', trustedPublisher.workflow, '--allow-publish', '--yes'];
  return run(args, { stdio: 'inherit' }).status === 0;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const args = process.argv.slice(2);
  const dryRun = args.includes('--dry-run');
  const names = args.filter((a) => !a.startsWith('--'));
  const all = npmPackageNames();
  for (const n of names) {
    if (!all.includes(n)) {
      console.error(`npm-trust: ${n} is not a package the release publishes (${all.join(', ')})`);
      process.exit(1);
    }
  }
  const npm = npmVersionOK();
  if (!npm.ok) {
    console.error(`npm-trust: npm trust needs npm ${minNpm.join('.')} or later (found ${npm.version || 'none'}): npm install -g npm@latest`);
    process.exit(1);
  }
  // Even a dry run reads each package's trust configuration, which needs a login.
  if (run(['whoami']).status !== 0) {
    console.error('npm-trust: not logged in to npm: run npm login first');
    process.exit(1);
  }
  const plan = planTrust(names.length > 0 ? names : all, { exists: viewPackage, trustList });
  let failed = 0;
  let calls = 0;
  for (const step of plan) {
    if (step.error) {
      console.error(`npm-trust: ${step.name}: ${step.error}`);
      failed++;
      continue;
    }
    if (!step.bootstrap && !step.trust) {
      console.log(`${step.name}: already trusts ${trustedPublisher.repository} ${trustedPublisher.workflow}`);
      continue;
    }
    if (step.bootstrap) {
      console.log(`${step.name}: not on npm; ${dryRun ? 'would publish' : 'publishing'} placeholder ${bootstrapVersion}`);
      if (!dryRun && !bootstrap(step.name)) {
        console.error(`npm-trust: ${step.name}: placeholder publish failed`);
        failed++;
        continue;
      }
    }
    console.log(`${step.name}: ${dryRun ? 'would trust' : 'trusting'} ${trustedPublisher.repository} ${trustedPublisher.workflow}`);
    if (!dryRun) {
      // npm asks to space trust calls out to stay under its rate limit.
      if (calls++ > 0) spawnSync(process.execPath, ['-e', 'setTimeout(() => {}, 2000)']);
      if (!trust(step.name)) {
        console.error(`npm-trust: ${step.name}: npm trust github failed`);
        failed++;
      }
    }
  }
  if (failed > 0) {
    console.error(`npm-trust: ${failed} package(s) failed; fix them and re-run (packages already done are skipped)`);
    process.exit(1);
  }
  console.log(`npm-trust: ${plan.length} package(s) ${dryRun ? 'checked' : 'trust the release workflow'}`);
}
