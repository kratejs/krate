import assert from 'node:assert/strict';
import { test } from 'node:test';
import { platforms } from './platform-package.mjs';
import { isMusl, platformFor, quoteCmdArg, spawnPlan } from './smoke-helpers.mjs';

test('quoteCmdArg quotes paths with spaces and doubles a trailing backslash', () => {
  assert.equal(quoteCmdArg('--no-audit'), '--no-audit');
  assert.equal(quoteCmdArg('C:\\Users\\a b\\x.tgz'), '"C:\\Users\\a b\\x.tgz"');
  assert.equal(quoteCmdArg('C:\\a b\\'), '"C:\\a b\\\\"');
  assert.equal(quoteCmdArg(''), '""');
  assert.throws(() => quoteCmdArg('50%PATH%'), /cmd\.exe/);
});

test('spawnPlan puts package managers behind a quoting shell on Windows', () => {
  const plan = spawnPlan('pnpm', ['pack', '--pack-destination', 'C:\\Users\\Jo Doe\\t'], 'win32');
  assert.deepEqual(plan, { command: 'pnpm', args: ['pack', '--pack-destination', '"C:\\Users\\Jo Doe\\t"'], shell: true });
});

test('spawnPlan starts a Windows .exe directly, with its path untouched', () => {
  const exe = 'C:\\Users\\Jo Doe\\app\\node_modules\\@krate\\core-win32-x64\\bin\\krate.exe';
  assert.deepEqual(spawnPlan(exe, ['version'], 'win32'), { command: exe, args: ['version'], shell: false });
});

test('spawnPlan never uses a shell off Windows', () => {
  assert.deepEqual(spawnPlan('npm', ['view', 'a b'], 'linux'), { command: 'npm', args: ['view', 'a b'], shell: false });
  assert.deepEqual(spawnPlan('npm', ['view'], 'darwin'), { command: 'npm', args: ['view'], shell: false });
});

test('platformFor picks the package for each CI machine', () => {
  const pkg = (opts) => platformFor(platforms, opts)?.pkg;
  assert.equal(pkg({ platform: 'darwin', arch: 'arm64', musl: false }), 'core-darwin-arm64');
  assert.equal(pkg({ platform: 'win32', arch: 'x64', musl: false }), 'core-win32-x64');
  assert.equal(pkg({ platform: 'linux', arch: 'x64', musl: false }), 'core-linux-x64');
  assert.equal(pkg({ platform: 'linux', arch: 'x64', musl: true }), 'core-linux-x64-musl');
  // musl only has an x64 package, as in @krate/core.
  assert.equal(pkg({ platform: 'linux', arch: 'arm64', musl: true }), 'core-linux-arm64');
  assert.equal(pkg({ platform: 'freebsd', arch: 'x64', musl: false }), undefined);
});

test('isMusl reads the glibc field of the process report', () => {
  assert.equal(isMusl({ getReport: () => ({ header: { glibcVersionRuntime: '2.39' } }) }, 'linux'), false);
  assert.equal(isMusl({ getReport: () => ({ header: {} }) }, 'linux'), true);
  assert.equal(isMusl(null, 'linux'), false);
});

test('isMusl is false off Linux, whose reports have no glibc field', () => {
  assert.equal(isMusl({ getReport: () => ({ header: {} }) }, 'darwin'), false);
  assert.equal(isMusl({ getReport: () => ({ header: {} }) }, 'win32'), false);
});
