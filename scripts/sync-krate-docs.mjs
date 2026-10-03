#!/usr/bin/env node
/**
 * Syncs Krate's framework docs from the docs site source
 * (packages/web/src/content/docs) into the compiler, where they are embedded
 * into the binary via //go:embed and served by the MCP search_docs tool and
 * the krate://docs/{slug} resource.
 *
 * The destination directory (packages/compiler/internal/kratedocs/docs) is
 * gitignored (except its committed .gitkeep) and regenerated on every build —
 * do not edit it by hand. Edit the source under packages/web/src/content/docs
 * instead.
 *
 * Usage:
 *   node scripts/sync-krate-docs.mjs
 *   node scripts/sync-krate-docs.mjs --check   # exit 1 if out of sync
 */

import { createHash } from 'node:crypto';
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const src = join(root, 'packages', 'web', 'src', 'content', 'docs');
const dest = join(root, 'packages', 'compiler', 'internal', 'kratedocs', 'docs');
const gitkeep = '.gitkeep';
const check = process.argv.includes('--check');

if (!existsSync(src)) {
  console.error(`sync-krate-docs: source not found: ${src}`);
  process.exit(1);
}

/** Recursively list markdown files relative to `dir` (slash-separated). */
function listMarkdown(dir, base = dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      out.push(...listMarkdown(full, base));
    } else if (name.endsWith('.md') || name.endsWith('.mdx')) {
      out.push(relative(base, full).split('\\').join('/'));
    }
  }
  return out.sort();
}

/** Hash every markdown file's contents, keyed by relative path. */
function hashMarkdown(dir) {
  const map = new Map();
  for (const rel of listMarkdown(dir)) {
    const data = readFileSync(join(dir, rel));
    map.set(rel, createHash('sha256').update(data).digest('hex'));
  }
  return map;
}

if (check) {
  const want = listMarkdown(src);
  const got = existsSync(dest) ? listMarkdown(dest) : [];

  // Compare the file set AND the content of every file, so edits that keep the
  // same filenames (content drift) are caught, not just adds/removes/renames.
  const added = want.filter((f) => !got.includes(f));
  const removed = got.filter((f) => !want.includes(f));
  const wantHash = hashMarkdown(src);
  const gotHash = existsSync(dest) ? hashMarkdown(dest) : new Map();
  const changed = want.filter(
    (f) => gotHash.has(f) && wantHash.get(f) !== gotHash.get(f),
  );

  if (added.length || removed.length || changed.length) {
    const detail = [];
    if (added.length) detail.push(`added: ${added.join(', ')}`);
    if (removed.length) detail.push(`removed: ${removed.join(', ')}`);
    if (changed.length) detail.push(`modified: ${changed.join(', ')}`);
    console.error(
      'sync-krate-docs: compiler docs are out of date ' +
        `(${detail.join('; ')}). Run \`node scripts/sync-krate-docs.mjs\`.`,
    );
    process.exit(1);
  }
  console.log(`sync-krate-docs: up to date (${want.length} files).`);
  process.exit(0);
}

// Clear synced content but preserve the committed .gitkeep so the //go:embed
// pattern stays valid in a fresh checkout before this script has run.
if (existsSync(dest)) {
  for (const name of readdirSync(dest)) {
    if (name === gitkeep) continue;
    rmSync(join(dest, name), { recursive: true, force: true });
  }
} else {
  mkdirSync(dest, { recursive: true });
}
cpSync(src, dest, { recursive: true });
console.log(
  `sync-krate-docs: copied ${listMarkdown(src).length} files -> ${relative(root, dest)}`,
);
