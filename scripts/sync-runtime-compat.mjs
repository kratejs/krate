#!/usr/bin/env node
/**
 * Generates the embedded API runtime compatibility reference page from the
 * ledger the compiler embeds (packages/compiler/internal/jsruntime/compat.json).
 *
 * Usage:
 *   node scripts/sync-runtime-compat.mjs           # write the page
 *   node scripts/sync-runtime-compat.mjs --check   # exit 1 if the page is stale
 *
 * scripts/sync-krate-docs.mjs --check also fails when the page is stale.
 */

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const ledgerPath = path.join(root, 'packages/compiler/internal/jsruntime/compat.json');
export const pagePath = path.join(root, 'packages/web/src/content/docs/reference/api-runtime-compat.md');

const labels = {
  supported: 'Supported',
  partial: 'Partial',
  'not-supported': 'Not supported',
  'by-design': 'By design',
};

function cell(value) {
  return String(value || '').replaceAll('|', '\\|').replaceAll('\n', ' ');
}

function code(api) {
  return '`' + api + '`';
}

/** Renders the reference page for a parsed ledger. */
export function renderRuntimeCompat(ledger) {
  const lines = [
    '---',
    'title: API Runtime Compatibility',
    'order: 6',
    'description: Which Web APIs and Node globals the embedded QuickJS runtime for API routes and middleware supports, generated from the compiler ledger.',
    '---',
    '',
    '<!-- Generated from packages/compiler/internal/jsruntime/compat.json by',
    '     scripts/sync-runtime-compat.mjs. Do not edit by hand; edit the ledger and',
    '     run pnpm sync:docs. -->',
    '',
    '# API Runtime Compatibility',
    '',
    'API routes and middleware run in Krate\'s embedded QuickJS runtime unless',
    '`ssr.apiRuntime` (or `ssr.middlewareRuntime`) selects `"node"`, `"bun"` or',
    '`"deno"`. This page lists what that runtime provides. It is generated from the',
    'ledger the compiler embeds, and every supported row names the Go tests that',
    'verify it. Tests named `corpus:<case>` are cases of the differential corpus',
    '(`packages/compiler/internal/build/testdata/api_runtime_corpus`), which runs',
    'each route under QuickJS and under Node and compares the responses; a row that',
    'cites a case with a recorded difference from Node is at most partial.',
    '',
    'Each feature group below is installed the first time code reads one of its',
    'globals, so a route pays only for the APIs it uses. Pooled API and middleware',
    'workers install the groups the build found in their sources when they start.',
    '',
    `Coverage: ${ledger.denominator}`,
    '',
    '## Status key',
    '',
    '- **Supported** - implemented and covered by the listed tests.',
    '- **Partial** - implemented with the limits given in the notes.',
    '- **Not supported** - undefined, or a stub that throws `NotSupportedError`.',
    '- **By design** - handled another way on purpose.',
    '',
    '## Build-time warnings',
    '',
    'When an API route or middleware is built for the embedded runtime, Krate scans',
    'the modules it bundles and reports each not-supported API it uses as',
    '[`KR7160`](/docs/reference/diagnostics/#kr7160), with the file, line and',
    'column and the hint from this table. The build still succeeds. Only global',
    'references count: imports and locals of the same name, `typeof X` checks and',
    'code guarded by them are not reported, and a dependency is reported once per',
    'package and API. Calling the API at run time throws a `NotSupportedError`',
    'whose message carries the same code. Instance methods (such as',
    '`ReadableStream.prototype.tee`) cannot be seen by the scan and only fail at',
    'run time. Routes that import a `node:` builtin are served by Node and are not',
    'scanned.',
    '',
  ];
  const features = Object.keys(ledger.features);
  const counts = {};
  for (const row of ledger.rows) counts[row.status] = (counts[row.status] || 0) + 1;
  lines.push(
    '## Summary',
    '',
    '| Status | APIs |',
    '|---|---|',
    ...Object.keys(labels).map((s) => `| ${labels[s]} | ${counts[s] || 0} |`),
    '',
  );
  for (const name of features) {
    const rows = ledger.rows.filter((r) => r.feature === name);
    if (rows.length === 0) continue;
    const f = ledger.features[name];
    lines.push(`## ${f.title}`, '', `${f.description} Feature group: \`${name}\`.`, '');
    lines.push('| API | Status | Notes | Instead | Tests |', '|---|---|---|---|---|');
    for (const r of rows) {
      const tests = (r.tests || []).map(code).join(', ');
      lines.push(
        `| ${code(r.api)} | ${labels[r.status] || r.status} | ${cell(r.notes)} | ${cell(r.hint)} | ${cell(tests)} |`,
      );
    }
    lines.push('');
  }
  lines.push(
    '## See also',
    '',
    '- [API Routes](/docs/features/api-routes/)',
    '- [Middleware](/docs/features/middleware/)',
    '- [Diagnostic codes](/docs/reference/diagnostics/)',
    '',
  );
  return lines.join('\n');
}

/** Returns true when the page on disk matches the ledger. */
export function runtimeCompatInSync() {
  const want = renderRuntimeCompat(JSON.parse(fs.readFileSync(ledgerPath, 'utf8')));
  const got = fs.existsSync(pagePath) ? fs.readFileSync(pagePath, 'utf8').replaceAll('\r\n', '\n') : '';
  return got === want;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.argv.includes('--check')) {
    if (!runtimeCompatInSync()) {
      console.error('runtime-compat: generated docs are stale; run pnpm sync:docs');
      process.exit(1);
    }
    console.log('runtime-compat: docs are in sync');
  } else {
    const ledger = JSON.parse(fs.readFileSync(ledgerPath, 'utf8'));
    fs.writeFileSync(pagePath, renderRuntimeCompat(ledger));
    console.log(`runtime-compat: wrote ${path.relative(root, pagePath)}`);
  }
}
