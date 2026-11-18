#!/usr/bin/env node
/**
 * Syncs the npm package versions to a single version before a release.
 *
 * Usage:
 *   node scripts/set-version.mjs <version>
 *
 * Sets the version of every package in scripts/release-packages.mjs, aligns
 * @krate/core's optionalDependencies (the platform packages) and every
 * peerDependency on a @krate package (`^<version>`) with it, and pins the
 * @krate dependencies of the `krate native init` template to it. In the
 * repository all of these stay at 0.1.0; a release sets them here.
 */

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { krateVersionsFile, renderKrateVersions } from './gen-krate-versions.mjs';
import { nativeTemplate, pinNativeTemplate, releasePackages } from './release-packages.mjs';

const version = process.argv[2];
if (!version) {
  console.error('usage: node scripts/set-version.mjs <version>');
  process.exit(1);
}

const root = dirname(dirname(fileURLToPath(import.meta.url)));

for (const dir of releasePackages) {
  const file = join(root, 'packages', dir, 'package.json');
  const pkg = JSON.parse(readFileSync(file, 'utf-8'));
  pkg.version = version;
  if (pkg.name === '@krate/core') {
    for (const dep of Object.keys(pkg.optionalDependencies ?? {})) {
      if (dep.startsWith('@krate/core-')) pkg.optionalDependencies[dep] = version;
    }
  }
  for (const dep of Object.keys(pkg.peerDependencies ?? {})) {
    if (dep.startsWith('@krate/')) pkg.peerDependencies[dep] = `^${version}`;
  }
  writeFileSync(file, JSON.stringify(pkg, null, 2) + '\n');
  console.log(`${pkg.name} -> ${version}`);
}

const templatePath = join(root, nativeTemplate);
const template = readFileSync(templatePath, 'utf-8');
writeFileSync(templatePath, pinNativeTemplate(template, version));
console.log(`krate native init template -> ${version}`);

writeFileSync(join(root, krateVersionsFile), renderKrateVersions(root));
console.log('krate migrate version table ->', version);

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
