#!/usr/bin/env node
/**
 * Works out the npm dist-tag and the GitHub pre-release flag for a version, and
 * writes them as key=value lines for $GITHUB_OUTPUT.
 *
 *   node scripts/release-tag.mjs 0.5.0-beta.3 >> "$GITHUB_OUTPUT"
 *
 * Rules:
 * - A pre-release is tagged with its first identifier (alpha, beta, rc). The
 *   identifier must start with a letter: npm rejects a tag that is a valid
 *   semver range, such as "1" for 1.0.0-1, and the old shell logic only found
 *   that out when the first package was published.
 * - A stable version is tagged latest. Unless it is lower than what npm already
 *   serves as latest (a patch for an older line), which would move latest
 *   backwards for every user; that one is tagged latest-<major>.<minor>.
 */

import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const VERSION = /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/;

function parse(version) {
  const m = VERSION.exec(version);
  if (!m) throw new Error(`"${version}" is not a semver version (expected x.y.z or x.y.z-alpha.N)`);
  return { core: [Number(m[1]), Number(m[2]), Number(m[3])], pre: m[4] };
}

/** Negative, zero or positive: a before, equal to, or after b. A pre-release sorts below its stable version. */
function compareCore(a, b) {
  for (let i = 0; i < 3; i++) if (a.core[i] !== b.core[i]) return a.core[i] - b.core[i];
  if (a.pre && !b.pre) return -1;
  if (!a.pre && b.pre) return 1;
  return 0;
}

/**
 * @param {string} version the version being released
 * @param {string | undefined} currentLatest what npm serves as latest now, when known
 * @returns {{ tag: string, prerelease: boolean }}
 */
export function distTag(version, currentLatest) {
  const v = parse(version);
  if (v.pre !== undefined) {
    const first = v.pre.split('.')[0];
    if (!/^[A-Za-z][A-Za-z0-9-]*$/.test(first) || first === 'latest') {
      throw new Error(
        `the pre-release identifier "${first}" of ${version} cannot be a dist-tag; start it with a letter and do not call it latest (for example 1.0.0-rc.1)`,
      );
    }
    return { tag: first, prerelease: true };
  }
  if (currentLatest) {
    const latest = parse(currentLatest);
    if (compareCore(v, latest) < 0) {
      return { tag: `latest-${v.core[0]}.${v.core[1]}`, prerelease: false };
    }
  }
  return { tag: 'latest', prerelease: false };
}

function npmLatest(name) {
  const res = spawnSync('npm', ['view', name, 'dist-tags.latest'], { encoding: 'utf8', shell: process.platform === 'win32' });
  const out = (res.stdout ?? '').trim();
  if (res.status === 0 && out) return out;
  if (/E404|404/.test(`${res.stderr}${res.stdout}`)) return undefined; // never published
  throw new Error(`could not read the latest dist-tag of ${name} from npm: ${(res.stderr || res.error?.message || 'unknown error').trim()}`);
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const version = (process.argv[2] ?? '').replace(/^v/, '');
  try {
    const stable = !version.includes('-');
    const { tag, prerelease } = distTag(version, stable ? npmLatest('@krate/core') : undefined);
    if (tag !== 'latest' && !prerelease) {
      console.error(`release-tag: ${version} is lower than the version npm serves as latest, so it is tagged ${tag} and latest stays where it is`);
    }
    console.log(`dist_tag=${tag}`);
    console.log(`prerelease=${prerelease}`);
  } catch (err) {
    console.error(`release-tag: ${err.message}`);
    process.exit(1);
  }
}
