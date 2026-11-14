#!/usr/bin/env node
/**
 * Syncs the npm package versions to a single version before a release.
 *
 * Usage:
 *   node scripts/set-version.mjs <version>
 *
 * Updates packages/core, packages/runtime, packages/components,
 * packages/plugin, packages/ts-plugin, packages/base-docs-theme, packages/simple-docs-theme, packages/auth, and the
 * create-krate scaffolds,
 * and aligns @krate/core's
 * optionalDependencies (the platform packages) to the same version. Also keeps
 * @krate/components' peerDependency on @krate/runtime in lockstep.
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
  join(root, 'packages', 'plugin', 'package.json'),
  join(root, 'packages', 'ts-plugin', 'package.json'),
  join(root, 'packages', 'base-docs-theme', 'package.json'),
  join(root, 'packages', 'simple-docs-theme', 'package.json'),
  join(root, 'packages', 'auth', 'package.json'),
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

// packages/web is intentionally not bumped here. Its @krate pins are managed
// manually so the docs site can track a published release independently of the
// workspace version being prepared.

// Add this release to the supply-chain exclusions. Existing entries stay: the
// docs site pins an earlier release, which can still be younger than the policy.
const workspacePath = join(root, 'pnpm-workspace.yaml');
let workspace = readFileSync(workspacePath, 'utf-8');
const kratePackages = [
  '@krate/components',
  '@krate/core-darwin-arm64',
  '@krate/core-darwin-x64',
  '@krate/core-linux-arm64',
  '@krate/core-linux-x64',
  '@krate/core-linux-x64-musl',
  '@krate/core-win32-arm64',
  '@krate/core-win32-x64',
  '@krate/core',
  '@krate/plugin',
  '@krate/runtime',
];
const excluded = new Map();
const block = workspace.match(/minimumReleaseAgeExclude:\n(?:  - .*\n?)*/)?.[0] ?? '';
for (const [, name, versions] of block.matchAll(/^ {2}- '(@[^@]+)@(.*)'$/gm)) {
  excluded.set(name, versions.split(' || '));
}
const exclusions = kratePackages.map((name) => {
  const versions = excluded.get(name) ?? [];
  return `  - '${name}@${(versions.includes(version) ? versions : [...versions, version]).join(' || ')}'`;
}).join('\n');
workspace = workspace.replace(/minimumReleaseAgeExclude:\n(?:  - .*\n?)*/, `minimumReleaseAgeExclude:\n${exclusions}\n`);
writeFileSync(workspacePath, workspace);
console.log(`pnpm release exclusions -> ${version}`);

console.log('Done.');
