#!/usr/bin/env node
/**
 * Prints the size of every generated platform package binary (krate and
 * krate-shell) as a Markdown table, and appends it to the GitHub step summary
 * when GITHUB_STEP_SUMMARY is set.
 *
 *   node scripts/report-platform-sizes.mjs
 */
import { appendFileSync, existsSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { platforms } from './platform-package.mjs';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const mib = (n) => (n / (1024 * 1024)).toFixed(1);

const lines = ['### Platform package binaries', '', '| package | krate (MiB) | krate-shell (MiB) | total (MiB) |', '|---|---|---|---|'];
let found = 0;
for (const t of platforms) {
  const bin = join(root, 'dist-platform', t.pkg, 'bin');
  const sizes = ['krate', 'krate-shell'].map((n) => {
    const f = join(bin, n + t.ext);
    return existsSync(f) ? statSync(f).size : null;
  });
  if (sizes.every((s) => s === null)) continue;
  found++;
  const total = sizes.reduce((a, s) => a + (s ?? 0), 0);
  lines.push(`| @krate/${t.pkg} | ${sizes[0] === null ? '-' : mib(sizes[0])} | ${sizes[1] === null ? '-' : mib(sizes[1])} | ${mib(total)} |`);
}
if (found === 0) {
  console.error('report-platform-sizes: no dist-platform/ packages; run scripts/build-platform-packages.mjs first');
  process.exit(1);
}
const text = lines.join('\n') + '\n';
console.log(text);
if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, text + '\n');
