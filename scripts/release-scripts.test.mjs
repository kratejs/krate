import assert from 'node:assert/strict';
import { test } from 'node:test';
import { distTag } from './release-tag.mjs';
import { publishAll } from './publish-packages.mjs';

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
