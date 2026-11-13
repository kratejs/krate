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
