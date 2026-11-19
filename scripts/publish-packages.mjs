#!/usr/bin/env node
/**
 * Publishes package directories to npm, skipping versions that are already
 * there so a failed release can be re-run.
 *
 *   node scripts/publish-packages.mjs <dist-tag> <dir>...
 *   node scripts/publish-packages.mjs --check-registered
 *
 * `pnpm publish` of a version that exists fails (E403), so a re-run after a
 * partial publish used to stop at the first package the earlier run had
 * published and never reach the rest. A skipped package is not re-checked: it
 * is whatever the earlier run uploaded.
 *
 * The release workflow publishes with npm trusted publishing (OIDC), which can
 * only publish a package that already exists. Before publishing anything,
 * every package must be on npm; a new one is published once by hand with
 * scripts/npm-trust.mjs (see RELEASE.md). --check-registered runs that check
 * alone for every package a release publishes, so the workflow fails before it
 * builds anything.
 */

import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { npmPackageNames } from './release-packages.mjs';

const shell = process.platform === 'win32';

const notFound = /E404|404 Not Found|is not in this registry/;

/** True when name@version is on the registry, false when it is not, an Error for anything else. */
export function viewVersion(name, version) {
  const res = spawnSync('npm', ['view', `${name}@${version}`, 'version'], { encoding: 'utf8', shell });
  if (res.status === 0) return res.stdout.trim() === version;
  if (notFound.test(`${res.stderr}${res.stdout}`)) return false;
  return new Error((res.stderr || res.error?.message || 'npm view failed').trim());
}

/** True when the package exists on the registry, false when it does not, an Error for anything else. */
export function viewPackage(name) {
  const res = spawnSync('npm', ['view', name, 'name'], { encoding: 'utf8', shell });
  if (res.status === 0) return res.stdout.trim() === name;
  if (notFound.test(`${res.stderr}${res.stdout}`)) return false;
  return new Error((res.stderr || res.error?.message || 'npm view failed').trim());
}

/** The packages of names that are not on npm yet, or an Error when the registry cannot tell. */
export function missingPackages(names, exists = viewPackage) {
  const missing = [];
  for (const name of names) {
    const found = exists(name);
    if (found instanceof Error) return new Error(`${name}: cannot tell whether it is on npm: ${found.message}`);
    if (!found) missing.push(name);
  }
  return missing;
}

/** What to do about packages that are not on npm yet. */
export function newPackageMessage(missing) {
  return [
    `${missing.join(', ')} ${missing.length === 1 ? 'is' : 'are'} not on npm yet.`,
    'The release workflow publishes with npm trusted publishing, which cannot create a package.',
    'Publish a placeholder and trust the workflow once, from a machine logged in to npm (see RELEASE.md, "Trusted publishing"):',
    `  node scripts/npm-trust.mjs ${missing.join(' ')}`,
    'then re-run the release job. Nothing was published.',
  ].join('\n');
}

function pnpmPublish(dir, tag) {
  const res = spawnSync('pnpm', ['publish', '--access', 'public', '--no-git-checks', '--tag', tag], { cwd: dir, stdio: 'inherit', shell });
  return res.status === 0;
}

/**
 * @param {{ dirs: string[], tag: string, read?: (dir: string) => {name: string, version: string},
 *           view?: typeof viewVersion, exists?: typeof viewPackage, publish?: typeof pnpmPublish,
 *           log?: (s: string) => void }} opts
 * @returns {{ published: string[], skipped: string[], failed?: string }}
 */
export function publishAll({ dirs, tag, read = readManifest, view = viewVersion, exists = viewPackage, publish = pnpmPublish, log = console.log }) {
  const result = { published: [], skipped: [] };
  const manifests = dirs.map((dir) => read(dir));
  const missing = missingPackages(manifests.map((m) => m.name), exists);
  if (missing instanceof Error) {
    result.failed = missing.message;
    return result;
  }
  if (missing.length > 0) {
    result.failed = newPackageMessage(missing);
    return result;
  }
  for (const [i, dir] of dirs.entries()) {
    const { name, version } = manifests[i];
    const id = `${name}@${version}`;
    const published = view(name, version);
    if (published instanceof Error) {
      result.failed = `${id}: cannot tell whether it is published: ${published.message}`;
      return result;
    }
    if (published) {
      log(`${id} is already on npm; skipping (left as the earlier run published it)`);
      result.skipped.push(id);
      continue;
    }
    log(`Publishing ${id} with tag ${tag}...`);
    if (!publish(dir, tag)) {
      result.failed = `${id}: pnpm publish failed`;
      return result;
    }
    result.published.push(id);
  }
  return result;
}

function readManifest(dir) {
  return JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'));
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  if (process.argv[2] === '--check-registered') {
    const names = npmPackageNames();
    const missing = missingPackages(names);
    if (missing instanceof Error) {
      console.error(`publish-packages: ${missing.message}`);
      process.exit(1);
    }
    if (missing.length > 0) {
      console.error(`publish-packages: ${newPackageMessage(missing)}`);
      process.exit(1);
    }
    console.log(`publish-packages: all ${names.length} packages are on npm`);
    process.exit(0);
  }
  const [tag, ...dirs] = process.argv.slice(2);
  if (!tag || dirs.length === 0) {
    console.error('usage: node scripts/publish-packages.mjs <dist-tag> <dir>...\n       node scripts/publish-packages.mjs --check-registered');
    process.exit(1);
  }
  const result = publishAll({ dirs, tag });
  console.log(`publish-packages: ${result.published.length} published, ${result.skipped.length} already on npm`);
  if (result.failed) {
    console.error(`publish-packages: ${result.failed}`);
    process.exit(1);
  }
}
