#!/usr/bin/env node
/**
 * Syncs the npm package versions to a single version before a release.
 *
 * Usage:
 *   node scripts/set-version.mjs <version>
 *
 * Updates packages/core, packages/runtime, and packages/components, and
 * aligns @krate/core's optionalDependencies (the platform packages) to the
 * same version. Also keeps @krate/components' peerDependency on
 * @krate/runtime in lockstep.
 */

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const version = process.argv[2];
if (!version) {
  console.error('usage: node scripts/set-version.mjs <version>');
  process.exit(1);
}

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const manifests = [
  join(root, 'packages', 'core', 'package.json'),
  join(root, 'packages', 'runtime', 'package.json'),
  join(root, 'packages', 'components', 'package.json'),
  join(root, 'packages', 'create-krate-app', 'package.json'),
  join(root, 'packages', 'create-krate-docs', 'package.json'),
];

for (const file of manifests) {
  const pkg = JSON.parse(readFileSync(file, 'utf-8'));
  pkg.version = version;
  if (pkg.name === '@krate/core') {
    for (const dep of Object.keys(pkg.optionalDependencies ?? {})) {
      if (dep.startsWith('@krate/core-')) pkg.optionalDependencies[dep] = version;
    }
  }
  if (pkg.name === '@krate/components') {
    if (pkg.peerDependencies?.['@krate/runtime']) {
      pkg.peerDependencies['@krate/runtime'] = `^${version}`;
    }
  }
  writeFileSync(file, JSON.stringify(pkg, null, 2) + '\n');
  console.log(`${pkg.name} -> ${version}`);
}

console.log('Done.');
