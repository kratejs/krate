import assert from 'node:assert/strict';
import { test } from 'node:test';
import { distTag } from './release-tag.mjs';
import { missingPackages, publishAll } from './publish-packages.mjs';
import { bootstrapVersion, needsOTP, placeholderManifest, planTrust, trustsWorkflow } from './npm-trust.mjs';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { packageNotice, platformManifest, platforms } from './platform-package.mjs';
import { nativeTemplate, nativeTemplateKrateDep, npmPackageNames, pinNativeTemplate, releasePackages, trustedPublisher } from './release-packages.mjs';

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
    exists: () => true,
    publish: (d) => (calls.push(d), true),
    log: () => {},
  });
  assert.deepEqual(calls, ['b', 'c']);
  assert.deepEqual(r.skipped, ['@k/a@1.0.0']);
  assert.equal(r.failed, undefined);
});

test('publishAll stops at a failed publish and does not publish later packages', () => {
  const calls = [];
  const r = publishAll({ dirs: ['a', 'b', 'c'], tag: 'beta', read: (d) => manifests[d], view: () => false, exists: () => true, publish: (d) => (calls.push(d), d !== 'b'), log: () => {} });
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

test('publishAll publishes nothing while a package is not on npm, and says how to create it', () => {
  const calls = [];
  const r = publishAll({
    dirs: ['a', 'b', 'c'],
    tag: 'rc',
    read: (d) => manifests[d],
    view: () => false,
    exists: (name) => name !== '@k/c',
    publish: (d) => (calls.push(d), true),
    log: () => {},
  });
  assert.deepEqual(calls, []);
  assert.match(r.failed, /@k\/c is not on npm yet/);
  assert.match(r.failed, /node scripts\/npm-trust\.mjs @k\/c/);
});

test('missingPackages stops at a registry error instead of calling the package new', () => {
  const r = missingPackages(['@k/a', '@k/b'], (name) => (name === '@k/a' ? new Error('ETIMEDOUT') : false));
  assert.ok(r instanceof Error);
  assert.match(r.message, /@k\/a: cannot tell whether it is on npm: ETIMEDOUT/);
});

test('npmPackageNames lists the platform packages, then every released package, @krate/core last', () => {
  const names = npmPackageNames();
  assert.equal(names.length, 7 + releasePackages.length);
  assert.ok(names.slice(0, 7).every((n) => n.startsWith('@krate/core-')));
  assert.equal(names.at(-1), '@krate/core');
  assert.ok(names.includes('create-krate-app') && names.includes('@krate/app'));
});

test('planTrust creates missing packages, trusts the rest once, and leaves another publisher alone', () => {
  const ours = JSON.stringify([{ type: 'github', repository: trustedPublisher.repository, file: trustedPublisher.workflow }]);
  const other = JSON.stringify([{ type: 'github', repository: 'someone/else', file: 'publish.yml' }]);
  const plan = planTrust(['new', 'done', 'fresh', 'other', 'broken'], {
    exists: (n) => (n === 'broken' ? new Error('E500') : n !== 'new'),
    trustList: (n) => ({ done: ours, fresh: '[]', other }[n]),
  });
  assert.deepEqual(plan.slice(0, 3), [
    { name: 'new', bootstrap: true, trust: true },
    { name: 'done', bootstrap: false, trust: false },
    { name: 'fresh', bootstrap: false, trust: true },
  ]);
  assert.match(plan[3].error, /trusts another publisher/);
  assert.match(plan[4].error, /E500/);
  assert.ok(trustsWorkflow(ours) && !trustsWorkflow(other));
});

test('the bootstrap placeholder names the repository provenance checks against', () => {
  const m = placeholderManifest('@krate/new');
  assert.equal(m.version, bootstrapVersion);
  assert.match(m.repository.url, new RegExp(`github\\.com/${trustedPublisher.repository}\\.git$`));
});

test('needsOTP recognises npm asking for two-factor authentication', () => {
  assert.ok(needsOTP('npm error code EOTP\nnpm error This operation requires a one-time password.'));
  assert.ok(!needsOTP('npm error code E404\nnpm error 404 Not Found'));
});

test('every published package ships a NOTICE that credits Krate', () => {
  for (const dir of releasePackages) {
    const pkgDir = join('packages', dir);
    const pkg = JSON.parse(readFileSync(join(pkgDir, 'package.json'), 'utf8'));
    assert.ok(pkg.files?.includes('NOTICE'), `${pkg.name}: "files" must list NOTICE`);
    assert.ok(existsSync(join(pkgDir, 'NOTICE')), `${pkg.name}: missing ${pkgDir}/NOTICE`);
    assert.match(readFileSync(join(pkgDir, 'NOTICE'), 'utf8'), /https:\/\/github\.com\/kratejs\/krate/, `${pkg.name}: NOTICE must credit Krate`);
  }
  for (const target of platforms) {
    assert.ok(platformManifest(target, '0.0.0').files.includes('NOTICE'), `@krate/${target.pkg}: "files" must list NOTICE`);
  }
  assert.match(packageNotice('@krate/x'), /^@krate\/x\n[\s\S]*https:\/\/github\.com\/kratejs\/krate/);
});
