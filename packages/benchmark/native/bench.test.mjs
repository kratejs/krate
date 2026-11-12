import assert from 'node:assert/strict';
import test from 'node:test';
import { dirSize, formatOf, median, parseEtime, pngBytes, renderMarkdown } from './bench.mjs';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

test('median', () => {
  assert.equal(median([3, 1, 2]), 2);
  assert.equal(median([4, 1, 2, 3]), 2.5);
  assert.equal(median([]), null);
});

test('parseEtime', () => {
  assert.equal(parseEtime('00:07'), 7);
  assert.equal(parseEtime('01:02:03'), 3723);
  assert.equal(parseEtime('2-00:00:01'), 172801);
  assert.ok(Number.isNaN(parseEtime('x')));
});

test('formatOf', () => {
  assert.equal(formatOf('/x/App_1.0.0_amd64.deb'), 'deb');
  assert.equal(formatOf('/x/App-x86_64.AppImage'), 'AppImage');
  assert.equal(formatOf('/x/App_1.0.0.dmg'), 'dmg');
  assert.equal(formatOf('/x/App-setup.exe'), 'NSIS');
});

test('pngBytes is a PNG of the requested size', () => {
  const b = pngBytes(16);
  assert.deepEqual([...b.subarray(0, 8)], [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  assert.equal(b.readUInt32BE(16), 16);
  assert.equal(b.readUInt32BE(20), 16);
});

test('dirSize', () => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), 'bench-size-'));
  try {
    fs.mkdirSync(path.join(d, 'a'));
    fs.writeFileSync(path.join(d, 'a', 'x'), 'abc');
    fs.writeFileSync(path.join(d, 'y'), 'de');
    assert.equal(dirSize(d), 5);
  } finally {
    fs.rmSync(d, { recursive: true, force: true });
  }
});

test('renderMarkdown shows every app and failures', () => {
  const s = { coldStartMs: { median: 100, min: 90, max: 120 }, memoryBytes: { median: 52428800, min: 50000000, max: 55000000 }, processes: { median: 3 }, memorySource: 'smaps_rollup' };
  const md = renderMarkdown({
    meta: { os: 'Linux', arch: 'amd64', cpu: 'cpu x4', memBytes: 2 ** 33, runs: 5, idleSeconds: 5, display: '' },
    apps: {
      krate: { summary: s, installers: [{ format: 'deb', bytes: 5242880 }], onDiskBytes: 10485760, versions: { krate: 'abc' }, notes: [] },
      tauri: { error: 'no toolchain', summary: {}, installers: [], versions: {}, notes: [] },
    },
    notes: [],
  });
  assert.match(md, /\| krate \| 100 ms \(90 ms to 120 ms\) \| 50.0 MiB/);
  assert.match(md, /deb 5.0 MiB/);
  assert.match(md, /\| tauri \| failed: no toolchain/);
});
