#!/usr/bin/env node
/**
 * Publishes package directories to npm, skipping versions that are already
 * there so a failed release can be re-run.
 *
 *   node scripts/publish-packages.mjs <dist-tag> <dir>...
 *
 * `pnpm publish` of a version that exists fails (E403), so a re-run after a
 * partial publish used to stop at the first package the earlier run had
 * published and never reach the rest. A skipped package is not re-checked: it
 * is whatever the earlier run uploaded.
 */

import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const shell = process.platform === 'win32';

/** True when name@version is on the registry, false when it is not, an Error for anything else. */
export function viewVersion(name, version) {
  const res = spawnSync('npm', ['view', `${name}@${version}`, 'version'], { encoding: 'utf8', shell });
  if (res.status === 0) return res.stdout.trim() === version;
  if (/E404|404 Not Found|is not in this registry/.test(`${res.stderr}${res.stdout}`)) return false;
  return new Error((res.stderr || res.error?.message || 'npm view failed').trim());
}

function pnpmPublish(dir, tag) {
  const res = spawnSync('pnpm', ['publish', '--access', 'public', '--no-git-checks', '--tag', tag], { cwd: dir, stdio: 'inherit', shell });
  return res.status === 0;
}

/**
 * @param {{ dirs: string[], tag: string, read?: (dir: string) => {name: string, version: string},
 *           view?: typeof viewVersion, publish?: typeof pnpmPublish, log?: (s: string) => void }} opts
 * @returns {{ published: string[], skipped: string[], failed?: string }}
 */
export function publishAll({ dirs, tag, read = readManifest, view = viewVersion, publish = pnpmPublish, log = console.log }) {
  const result = { published: [], skipped: [] };
  for (const dir of dirs) {
    const { name, version } = read(dir);
    const id = `${name}@${version}`;
    const exists = view(name, version);
    if (exists instanceof Error) {
      result.failed = `${id}: cannot tell whether it is published: ${exists.message}`;
      return result;
    }
    if (exists) {
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
  const [tag, ...dirs] = process.argv.slice(2);
  if (!tag || dirs.length === 0) {
    console.error('usage: node scripts/publish-packages.mjs <dist-tag> <dir>...');
    process.exit(1);
  }
  const result = publishAll({ dirs, tag });
  console.log(`publish-packages: ${result.published.length} published, ${result.skipped.length} already on npm`);
  if (result.failed) {
    console.error(`publish-packages: ${result.failed}`);
    process.exit(1);
  }
}
