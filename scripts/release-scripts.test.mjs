import assert from 'node:assert/strict';
import { test } from 'node:test';
import { distTag } from './release-tag.mjs';
import { publishAll } from './publish-packages.mjs';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { nativeTemplate, nativeTemplateKrateDep, pinNativeTemplate, releasePackages } from './release-packages.mjs';

test('distTag rejects an identifier npm cannot use as a tag, before anything is published', () => {
  assert.throws(() => distTag('1.0.0-1'), /cannot be a dist-tag/);
  assert.throws(() => distTag('1.0.0-0.3'), /cannot be a dist-tag/);
  assert.throws(() => distTag('1.0.0-latest.1'), /cannot be a dist-tag/);
  assert.throws(() => distTag('not-a-version'), /not a semver version/);
  assert.throws(() => distTag(''), /not a semver version/);
});

test('distTag keeps latest from moving backwards for a patch on an older line', () => {
  assert.deepEqual(distTag('0.4.1', '0.5.0'), { tag: 'latest-0.4', prerelease: false });
  assert.deepEqual(distTag('0.9.9', '1.0.0'), { tag: 'latest-0.9', prerelease: false });
  assert.deepEqual(distTag('1.2.3', '1.10.0'), { tag: 'latest-1.2', prerelease: false });
});

const manifests = { a: { name: '@k/a', version: '1.0.0' }, b: { name: '@k/b', version: '1.0.0' }, c: { name: '@k/c', version: '1.0.0' } };

test('publishAll skips versions an earlier run already published and finishes the rest', () => {
  const calls = [];
  const r = publishAll({
    dirs: ['a', 'b', 'c'],
    tag: 'latest',
    read: (d) => manifests[d],
    view: (name) => name === '@k/a',
    publish: (d) => (calls.push(d), true),
    log: () => {},
  });
  assert.deepEqual(calls, ['b', 'c']);
  assert.deepEqual(r.skipped, ['@k/a@1.0.0']);
  assert.equal(r.failed, undefined);
});

test('publishAll stops at a failed publish and does not publish later packages', () => {
  const calls = [];
  const r = publishAll({ dirs: ['a', 'b', 'c'], tag: 'beta', read: (d) => manifests[d], view: () => false, publish: (d) => (calls.push(d), d !== 'b'), log: () => {} });
  assert.deepEqual(calls, ['a', 'b']);
  assert.match(r.failed, /@k\/b@1\.0\.0: pnpm publish failed/);
});

test('every package that is not private is released, and none that is', () => {
  const root = join(import.meta.dirname, '..');
  const public_ = readdirSync(join(root, 'packages')).filter((dir) => {
    const file = join(root, 'packages', dir, 'package.json');
    return existsSync(file) && !JSON.parse(readFileSync(file, 'utf8')).private;
  });
  assert.deepEqual([...releasePackages].sort(), public_.sort());
  assert.equal(releasePackages.at(-1), 'core');
});

test('pinNativeTemplate pins every @krate dependency of the native template and nothing else', () => {
  const root = join(import.meta.dirname, '..');
  const text = readFileSync(join(root, nativeTemplate), 'utf8');
  const pinned = pinNativeTemplate(text, '1.2.3-rc.4');
  const deps = [...pinned.matchAll(nativeTemplateKrateDep)].map((m) => [m[2], m[3]]);
  assert.deepEqual(deps.map(([name]) => name).sort(), ['@krate/app', '@krate/core', '@krate/runtime']);
  for (const [, range] of deps) assert.equal(range, '1.2.3-rc.4');
  assert.match(pinned, /"version": "0\.1\.0"/, 'the app keeps its own version');
  assert.match(pinned, /"typescript": "\^5/, 'other dependencies are left alone');
});
