// Tests of go-test-shards.mjs: node --test .github/scripts/go-test-shards.test.mjs
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { checkCoverage, checkPlans, listDiff, checkResults, collectResults, invocations, parseListOutput, partition, pickTimesEntry, planHash, restorableRefs } from "./go-test-shards.mjs";

const list = {
  modules: {
    "packages/compiler": {
      "example.com/a": ["TestA1", "TestA2", "TestA3", "TestShared1", "TestShared2"],
      "example.com/b": ["TestB1", "TestNew"],
    },
    "packages/native": { "example.com/n": ["TestN1", "FuzzN"] },
  },
};
const times = {
  "example.com/a TestA1": { s: 30, serial: false },
  "example.com/a TestA2": { s: 5, serial: true },
  "example.com/a TestA3": 12,
  "example.com/a TestShared1": { s: 20, serial: false },
  "example.com/a TestShared2": { s: 1, serial: false },
  "example.com/b TestB1": { s: 8, serial: false },
  "example.com/n TestN1": { s: 3, serial: false },
  "example.com/n FuzzN": { s: 0.5, serial: false },
};
const groups = { "example.com/a": [["TestShared1", "TestShared2"]] };

test("every test lands in exactly one shard, new ones included", () => {
  for (const n of [1, 2, 3, 5, 12]) {
    const shards = partition(list, times, groups, n, 4);
    checkCoverage(list, shards);
    assert.equal(shards.length, n);
    assert.equal(shards.flatMap((s) => s.tests).length, 9);
  }
});

test("the plan is deterministic and keeps groups together", () => {
  const a = JSON.stringify(partition(list, times, groups, 3, 4));
  const b = JSON.stringify(partition(structuredClone(list), { ...times }, groups, 3, 4));
  assert.equal(a, b);
  const shards = JSON.parse(a);
  const home = (name) => shards.find((s) => s.tests.some((t) => t.test === name)).index;
  assert.equal(home("TestShared1"), home("TestShared2"));
});

test("longest first: the 30s test and the 20s serial test do not share a shard", () => {
  // TestA2 is 5s serial, so it weighs 20s with a serial weight of 4.
  const shards = partition(list, times, {}, 2, 4);
  const where = (name) => shards.find((s) => s.tests.some((t) => t.test === name)).index;
  assert.notEqual(where("TestA1"), where("TestA2"));
});

test("a broken partition fails the coverage check", () => {
  const shards = partition(list, times, groups, 3, 4);
  const dropped = structuredClone(shards);
  dropped[1].tests = dropped[1].tests.slice(1);
  assert.throws(() => checkCoverage(list, dropped), /is in no shard/);
  const doubled = structuredClone(shards);
  doubled[0].tests.push(doubled[2].tests[0]);
  assert.throws(() => checkCoverage(list, doubled), /is in shards/);
  const extra = structuredClone(shards);
  extra[0].tests.push({ module: "packages/compiler", pkg: "example.com/a", test: "TestGone", key: "example.com/a TestGone" });
  assert.throws(() => checkCoverage(list, extra), /not listed/);
});

test("the -run regex is anchored, per package, and escapes names", () => {
  const shards = partition(list, times, groups, 1);
  const invs = invocations(shards[0]);
  assert.deepEqual(invs.map((i) => i.pkg).sort(), ["example.com/a", "example.com/b", "example.com/n"]);
  const b = invs.find((i) => i.pkg === "example.com/b");
  assert.equal(b.run, "^(TestB1|TestNew)$");
  assert.equal(b.module, "packages/compiler");
  const odd = invocations({ tests: [{ module: "m", pkg: "p", test: "Test$x", key: "p Test$x" }] });
  assert.equal(odd[0].run, "^(Test\\$x)$");
});

test("go test -list -json output is parsed per package", () => {
  const ev = (pkg, out) => JSON.stringify({ Action: "output", Package: pkg, Output: out });
  const text = [
    ev("example.com/a", "TestA1\n"),
    ev("example.com/a", "BenchmarkA\n"),
    ev("example.com/a", "ExampleA\n"),
    ev("example.com/a", "ok  \texample.com/a\t0.01s\n"),
    JSON.stringify({ Action: "pass", Package: "example.com/a" }),
    ev("example.com/b", "FuzzB\n"),
    "not json",
  ].join("\n");
  const { pkgs, failed } = parseListOutput(text);
  assert.deepEqual(pkgs, { "example.com/a": ["ExampleA", "TestA1"], "example.com/b": ["FuzzB"] });
  assert.deepEqual(failed, []);
});

test("merge fails when a listed test reported no result or two", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "shards-"));
  try {
    const line = (pkg, t, action) => JSON.stringify({ Action: action, Package: pkg, Test: t, Elapsed: 1 });
    const small = { modules: { m: { "example.com/a": ["TestA1", "TestA2"] } } };
    fs.writeFileSync(path.join(dir, "one.json"), [line("example.com/a", "TestA1", "pass"), line("example.com/a", "TestA1/sub", "pass")].join("\n"));
    assert.throws(() => checkResults(small, collectResults([dir])), /TestA2 reported no result/);
    fs.writeFileSync(path.join(dir, "two.json"), [line("example.com/a", "TestA2", "skip"), line("example.com/a", "TestA1", "pass")].join("\n"));
    assert.throws(() => checkResults(small, collectResults([dir])), /TestA1 reported 2 results/);
    fs.writeFileSync(path.join(dir, "two.json"), line("example.com/a", "TestA2", "fail"));
    assert.equal(checkResults(small, collectResults([dir])), 2);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a test's time runs from its start to its result and records whether it was parallel", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "shards-"));
  try {
    const ev = (action, t, time, extra = {}) => JSON.stringify({ Time: time, Action: action, Package: "p", Test: t, ...extra });
    fs.writeFileSync(
      path.join(dir, "p.json"),
      [
        ev("run", "TestPar", "2026-01-01T00:00:00Z"),
        ev("pause", "TestPar", "2026-01-01T00:00:00Z"),
        ev("run", "TestSer", "2026-01-01T00:00:00Z"),
        ev("pass", "TestSer", "2026-01-01T00:00:02Z", { Elapsed: 2 }),
        ev("cont", "TestPar", "2026-01-01T00:00:02Z"),
        // Parallel subtests finish after the test function: Elapsed is 0.
        ev("pass", "TestPar", "2026-01-01T00:00:12Z", { Elapsed: 0 }),
      ].join("\n"),
    );
    const r = collectResults([dir]);
    assert.deepEqual(
      [r.get("p TestPar")[0].elapsed, r.get("p TestPar")[0].parallel, r.get("p TestSer")[0].elapsed, r.get("p TestSer")[0].parallel],
      [10, true, 2, false],
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

const caches = [
  { key: "go-test-times-Linux-aaa-1", ref: "refs/heads/dev", created_at: "2026-11-20T10:00:00Z" },
  { key: "go-test-times-Linux-bbb-1", ref: "refs/heads/dev", created_at: "2026-11-20T12:00:00Z" },
  // Saved while this run's shards were starting.
  { key: "go-test-times-Linux-ccc-1", ref: "refs/heads/dev", created_at: "2026-11-20T12:30:05Z" },
  { key: "go-test-times-Linux-ddd-1", ref: "refs/heads/feature", created_at: "2026-11-20T12:20:00Z" },
  { key: "go-test-times-macOS-eee-1", ref: "refs/heads/dev", created_at: "2026-11-20T12:10:00Z" },
  { key: "go-test-times-Linux-fff-1", ref: "refs/pull/7/merge", created_at: "2026-11-20T11:00:00Z" },
];
const runStartedAt = "2026-11-20T12:30:00Z";

test("a pull request restores the newest times of its base or its own ref from before the run", () => {
  const refs = restorableRefs({ event: "pull_request", ref: "refs/pull/7/merge", baseRef: "dev", defaultBranch: "main" });
  assert.deepEqual([...refs].sort(), ["refs/heads/dev", "refs/pull/7/merge"]);
  // ccc was created after the run started; ddd is on another branch.
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-Linux-", runStartedAt, refs }).key, "go-test-times-Linux-bbb-1");
});

test("an entry created after the run started or on a ref the run cannot read is never picked", () => {
  const refs = restorableRefs({ event: "pull_request", ref: "refs/pull/9/merge", baseRef: "main", defaultBranch: "main" });
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-Linux-", runStartedAt, refs }), undefined);
  const later = restorableRefs({ event: "pull_request", ref: "refs/pull/7/merge", baseRef: "dev" });
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-Linux-", runStartedAt: "2026-11-20T10:30:00Z", refs: later }).key, "go-test-times-Linux-aaa-1");
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-Linux-", runStartedAt: "2026-11-20T09:00:00Z", refs: later }), undefined);
});

test("a push restores its own branch or the default branch", () => {
  const refs = restorableRefs({ event: "push", ref: "refs/heads/feature", defaultBranch: "dev" });
  assert.deepEqual([...refs].sort(), ["refs/heads/dev", "refs/heads/feature"]);
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-Linux-", runStartedAt, refs }).key, "go-test-times-Linux-ddd-1");
  assert.equal(pickTimesEntry(caches, { prefix: "go-test-times-macOS-", runStartedAt, refs }).key, "go-test-times-macOS-eee-1");
});

test("entries created at the same instant are picked alike in any order", () => {
  const tie = [
    { key: "go-test-times-Linux-x-1", ref: "refs/heads/dev", created_at: "2026-11-20T12:00:00Z" },
    { key: "go-test-times-Linux-y-1", ref: "refs/heads/dev", created_at: "2026-11-20T12:00:00Z" },
  ];
  const refs = new Set(["refs/heads/dev"]);
  const a = pickTimesEntry([...tie], { prefix: "go-test-times-Linux-", runStartedAt, refs }).key;
  const b = pickTimesEntry([...tie].reverse(), { prefix: "go-test-times-Linux-", runStartedAt, refs }).key;
  assert.equal(a, b);
});

test("shards that planned from different times or partitions fail the check", () => {
  const h = planHash(partition(list, times, groups, 2, 4));
  const ok = [0, 1].map((shard) => ({ shard, shards: 2, timesKey: "k", hash: h }));
  checkPlans(ok);
  assert.throws(() => checkPlans([ok[0], { ...ok[1], timesKey: "other" }]), /did not plan alike/);
  assert.notEqual(planHash(partition(list, {}, groups, 2, 4)), h);
  assert.throws(() => checkPlans([ok[0], { ...ok[1], hash: "x" }]), /did not plan alike/);
  assert.throws(() => checkPlans([ok[0]]), /one plan from each of 2 shards/);
});

test("the list is the same whatever order go test prints the packages in", () => {
  const ev = (pkg, out) => JSON.stringify({ Action: "output", Package: pkg, Output: out + "\n" });
  const a = [ev("m/b", "TestB2"), ev("m/b", "TestB1"), ev("m/a", "TestA")].join("\n");
  const b = [ev("m/a", "TestA"), ev("m/b", "TestB1"), ev("m/b", "TestB2")].join("\n");
  assert.equal(JSON.stringify(parseListOutput(a).pkgs), JSON.stringify(parseListOutput(b).pkgs));
  assert.deepEqual(Object.keys(parseListOutput(a).pkgs), ["m/a", "m/b"]);
});

test("the list check names every test one listing has and the other lacks", () => {
  const source = { "m/a": ["TestA", "TestB"], "m/b": ["FuzzB"] };
  assert.deepEqual(listDiff(source, structuredClone(source)), []);
  const compiled = { "m/a": ["TestA", "ExampleA"], "m/b": ["FuzzB"], "m/c": ["TestC"] };
  assert.deepEqual(listDiff(source, compiled), [
    "go test -list has m/a ExampleA; the source listing does not",
    "go test -list has m/c TestC; the source listing does not",
    "the source listing has m/a TestB; go test -list does not",
  ]);
});
