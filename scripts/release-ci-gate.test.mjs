import assert from 'node:assert/strict';
import { test } from 'node:test';
import { decide } from './release-ci-gate.mjs';

const sha = 'abc123';
const run = (over) => ({ head_sha: sha, event: 'push', status: 'completed', conclusion: 'success', ...over });

test('a successful full run passes', () => {
  assert.equal(decide([run({})], sha).action, 'pass');
});

test('a success wins over an earlier failure of the same commit', () => {
  assert.equal(decide([run({ conclusion: 'failure' }), run({ event: 'workflow_dispatch' })], sha).action, 'pass');
});

test('a run in progress is waited for, even next to a failed one', () => {
  assert.equal(decide([run({ conclusion: 'failure' }), run({ status: 'in_progress', conclusion: null })], sha).action, 'wait');
  assert.equal(decide([run({ status: 'queued', conclusion: null })], sha).action, 'wait');
});

test('only failed full runs fail the gate', () => {
  assert.equal(decide([run({ conclusion: 'failure' })], sha).action, 'fail');
  assert.equal(decide([run({ conclusion: 'timed_out' })], sha).action, 'fail');
});

test('pull request runs never count', () => {
  assert.equal(decide([run({ event: 'pull_request' })], sha).action, 'dispatch');
});

test('runs of another commit never count', () => {
  assert.equal(decide([run({ head_sha: 'other' })], sha).action, 'dispatch');
});

test('cancelled runs say nothing, so CI is dispatched', () => {
  assert.equal(decide([run({ conclusion: 'cancelled' })], sha).action, 'dispatch');
  assert.equal(decide([], sha).action, 'dispatch');
});

test('schedule runs count as full runs', () => {
  assert.equal(decide([run({ event: 'schedule' })], sha).action, 'pass');
});
