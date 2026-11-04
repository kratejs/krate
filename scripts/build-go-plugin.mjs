#!/usr/bin/env node
// Cross-compiles a Krate Go plugin into bin/ for the platforms listed in its
// index.js descriptor (binaries map). Usage:
//
//   node scripts/build-go-plugin.mjs <plugin-dir>                # all platforms
//   node scripts/build-go-plugin.mjs <plugin-dir> windows-amd64  # one platform
//
// Output binaries land in <plugin-dir>/bin/ with the names declared in the
// plugin's index.js so the descriptor stays the single source of truth.

import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL, fileURLToPath } from 'node:url';

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const pluginArg = process.argv[2] ? path.resolve(process.argv[2]) : path.join(root, 'examples', 'plugins', 'krate-plugin-demo-go');
const only = process.argv[3];

if (!existsSync(path.join(pluginArg, 'index.js')) || !existsSync(path.join(pluginArg, 'go.mod'))) {
  console.error(`Not a Go plugin directory: ${pluginArg}`);
  process.exit(1);
}

const descriptor = (await import(pathToFileURL(path.join(pluginArg, 'index.js')).href)).default();
const binaries = descriptor?.binaries ?? {};
const targets = only ? [only] : Object.keys(binaries);

if (!targets.length) {
  console.error(`No binaries map in ${path.join(pluginArg, 'index.js')}`);
  process.exit(1);
}

await mkdir(path.join(pluginArg, 'bin'), { recursive: true });

for (const target of targets) {
  const rel = binaries[target];
  if (!rel) {
    console.error(`Unknown platform ${target}`);
    process.exit(1);
  }
  const [goos, goarch] = target.split('-');
  const out = path.join(pluginArg, rel);
  console.log(`building ${target} -> ${path.relative(pluginArg, out)}`);
  // GOOS=windows yields a .exe suffix automatically; keep the rel path as-is.
  execFileSync('go', ['build', '-o', out, '.'], {
    cwd: pluginArg,
    // GOWORK=off: the plugin is a standalone module; a go.work in a parent
    // directory (created by builds of the examples project) would hijack it.
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0', GOWORK: 'off' },
    stdio: 'inherit',
  });
}