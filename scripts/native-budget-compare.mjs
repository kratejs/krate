#!/usr/bin/env node
// Compares the memory a Krate Native app uses on this OS (the "memory" section
// of dist-native/report.json from `krate native profile --memory`) with the
// linux-amd64 limits in packages/native/budgets.json, and prints a table. It is
// informational: macOS and Windows have no budgets ("planned") until real
// numbers exist, so this never fails the job. The numbers go to the step
// summary and to the log so they can be turned into budgets later.
//
//   node scripts/native-budget-compare.mjs <report.json>... [--budgets packages/native/budgets.json]
//
// A report path may be written as label=path to name it in the table.
import fs from 'node:fs';
import path from 'node:path';

const args = process.argv.slice(2);
let budgetsPath = 'packages/native/budgets.json';
const inputs = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--budgets') budgetsPath = args[++i];
  else inputs.push(args[i]);
}
if (inputs.length === 0) {
  console.error('usage: native-budget-compare.mjs <report.json>... [--budgets file]');
  process.exit(2);
}

const budgets = JSON.parse(fs.readFileSync(budgetsPath, 'utf8'));
const limits = budgets.memory?.['linux-amd64'] ?? {};

function mib(n) {
  return n === undefined || n === null ? '-' : (n / (1 << 20)).toFixed(1);
}

function flatten(sample) {
  const out = {};
  const put = (prefix, part) => {
    if (!part) return;
    for (const [role, v] of Object.entries(part.roles ?? {})) {
      out[`${prefix}.${role}.pss`] = v.pss;
      out[`${prefix}.${role}.privateDirty`] = v.privateDirty;
    }
    if (part.total) {
      out[`${prefix}.total.pss`] = part.total.pss;
      out[`${prefix}.total.privateDirty`] = part.total.privateDirty;
    }
  };
  put('app', sample.app);
  put('floor', sample.floor);
  put('appMinusFloor', sample.appMinusFloor);
  return out;
}

const lines = [];
for (const input of inputs) {
  const [label, file] = input.includes('=') ? input.split(/=(.*)/s) : [path.basename(path.dirname(input)) || input, input];
  let report;
  try {
    report = JSON.parse(fs.readFileSync(file, 'utf8')).memory;
  } catch (e) {
    lines.push(`### ${label}\n\nNo readable report at \`${file}\` (${e.message}).\n`);
    continue;
  }
  if (!report?.samples?.length) {
    lines.push(`### ${label}\n\nThe report has no memory samples.\n`);
    continue;
  }
  lines.push(`### ${label} (${report.os}; ${report.source})\n`);
  for (const s of report.samples) {
    const flat = flatten(s);
    lines.push(`Sample \`${s.name}\`: app minus blank-page floor, against the linux-amd64 limit (MiB). Not enforced.\n`);
    lines.push('| Metric | This OS | linux-amd64 measured | linux-amd64 limit | Over limit |');
    lines.push('|---|---:|---:|---:|:---:|');
    const keys = Object.keys(flat)
      .filter((k) => k.startsWith('appMinusFloor.'))
      .sort();
    for (const k of keys) {
      const lim = limits[k];
      const over = lim && flat[k] > lim.max ? 'yes' : '';
      lines.push(`| ${k} | ${mib(flat[k])} | ${mib(lim?.measured)} | ${mib(lim?.max)} | ${over} |`);
    }
    const tot = flat['app.total.pss'];
    const floor = flat['floor.total.pss'];
    lines.push(`\nAbsolute total PSS: app ${mib(tot)} MiB, floor ${mib(floor)} MiB (linux-amd64 app ${mib(limits['app.total.pss']?.measured)} MiB).\n`);
  }
  for (const n of report.notes ?? []) lines.push(`Note: ${n}\n`);
}
const text = lines.join('\n');
console.log(text);
if (process.env.GITHUB_STEP_SUMMARY) fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Native memory budget comparison\n\n${text}\n`);
