// Splits the Go tests of one CI job across shards by their recorded times.
//
//   pick   picks the recorded times every shard of a run restores: the same
//          cache entry for all of them, chosen through the REST API.
//   list   lists the top-level tests of every package (go test -list).
//   plan   prints the partition of a list into shards.
//   run    runs one shard: re-derives the whole partition, checks it covers
//          every listed test exactly once, then runs go test -json with an
//          anchored -run regex for each package of the shard.
//   merge  checks the results of all shards together: every listed test
//          reported a result exactly once. It also writes the times file the
//          next runs plan with.
//
// Every shard plans from the same inputs (the list it makes itself and the
// times restored from the cache), and the plan is deterministic, so the
// shards agree on it without talking to each other. A test with no recorded
// time (a new one) gets an estimate and is still assigned. Tests that share a
// fixture built once per test binary are kept on one shard (groups file), so
// the fixture is not built on several.
//
// The times file maps "<import path> <TestName>" to {s, serial}: the seconds
// the test took and whether it ran serially (never called t.Parallel). A
// serial test holds the whole package while it runs, so it is planned at its
// time multiplied by --serial-weight (the runner's cores), which keeps the
// shards' serial phases short and balanced.
//
// Every shard picks the times on its own (no job runs in front of the
// shards): the newest go-test-times-<kind>- cache this run may restore that
// was created before the run started. The run's start is the same for every
// shard of an attempt, so they pick the same entry, and a cache saved while
// the shards start cannot split them. Each shard records the key it used and
// a hash of its partition; merge fails unless all shards agree.
//
// Usage:
//   node go-test-shards.mjs pick  --kind Linux   (env: GH_TOKEN and the GITHUB_* run variables)
//   node go-test-shards.mjs list  --module DIR [--module DIR] --out list.json [-- go test flags]
//   node go-test-shards.mjs plan  --list list.json [--times t.json] [--groups g.json] --shards N
//                                 [--serial-weight W]
//   node go-test-shards.mjs run   --list list.json [--times t.json] [--groups g.json] --shards N --shard I
//                                 [--serial-weight W] --out DIR [--plan-out F] [--times-key K]
//                                 [--jobs P] [-- go test flags]
//   node go-test-shards.mjs merge --list list.json --results DIR [--results DIR] [--plans F...]
//                                 [--times-out t.json]
import { spawn, spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// An estimate for a test with no recorded time: the median of its package's
// recorded tests, or this when the package has none.
const DEFAULT_SECONDS = 1;

// PACKAGE_SECONDS is what a shard pays for each package it runs (a go test
// run of its own: the test binary's start, TestMain, its result).
const PACKAGE_SECONDS = 1.5;

// testName matches what go test -list prints for a test it can run. Benchmarks
// only run with -bench, so they are not listed.
const testName = /^(Test|Fuzz|Example)[^\s/]*$/;

export function parseArgs(argv) {
  const opts = { module: [], results: [], extra: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--") {
      opts.extra = argv.slice(i + 1);
      break;
    }
    if (!a.startsWith("--")) {
      throw new Error(`unexpected argument ${a}`);
    }
    const key = a.slice(2);
    const val = argv[++i];
    if (val === undefined) {
      throw new Error(`${a} needs a value`);
    }
    if (key === "module" || key === "results") {
      opts[key].push(val);
    } else {
      opts[key] = val;
    }
  }
  return opts;
}

// parseListOutput reads the go test -list -json events of one module and
// returns {importPath: [test, ...]}.
export function parseListOutput(text) {
  const pkgs = {};
  const failed = [];
  for (const line of text.split("\n")) {
    if (!line.trim().startsWith("{")) {
      continue;
    }
    let ev;
    try {
      ev = JSON.parse(line);
    } catch {
      continue;
    }
    if (!ev.Package) {
      continue;
    }
    if (ev.Action === "output" && !ev.Test) {
      const name = (ev.Output || "").trim();
      if (testName.test(name)) {
        (pkgs[ev.Package] ??= []).push(name);
      }
    } else if (ev.Action === "fail" && !ev.Test) {
      failed.push(ev.Package);
    }
  }
  // go test prints packages in the order their builds finish, so sort them:
  // every shard must write the same list byte for byte.
  const sorted = {};
  for (const p of Object.keys(pkgs).sort()) {
    sorted[p] = [...new Set(pkgs[p])].sort();
  }
  return { pkgs: sorted, failed };
}

function cmdList(opts) {
  if (opts.module.length === 0 || !opts.out) {
    throw new Error("list needs --module and --out");
  }
  const list = { modules: {} };
  for (const dir of opts.module) {
    const args = ["test", "-list", ".", "-json", ...opts.extra, "./..."];
    const res = spawnSync("go", args, { cwd: dir, encoding: "utf8", maxBuffer: 1 << 30 });
    const { pkgs, failed } = parseListOutput(res.stdout || "");
    if (res.status !== 0 || failed.length > 0) {
      process.stderr.write(res.stderr || "");
      throw new Error(`go test -list failed in ${dir}${failed.length ? ": " + failed.join(", ") : ""}`);
    }
    list.modules[dir] = pkgs;
  }
  fs.writeFileSync(opts.out, JSON.stringify(list, null, 1) + "\n");
  const n = Object.values(list.modules).reduce((s, m) => s + Object.values(m).reduce((t, x) => t + x.length, 0), 0);
  console.log(`listed ${n} tests in ${Object.values(list.modules).reduce((s, m) => s + Object.keys(m).length, 0)} packages`);
}

// restorableRefs are the refs whose caches a run can restore: a pull request
// reads its own ref and its base branch, a push (or schedule, or manual run)
// its own branch and the default branch.
export function restorableRefs({ event, ref, baseRef, defaultBranch }) {
  const refs = new Set();
  if (ref) {
    refs.add(ref);
  }
  if (event === "pull_request" || event === "pull_request_target") {
    if (baseRef) {
      refs.add(`refs/heads/${baseRef}`);
    }
  } else if (defaultBranch) {
    refs.add(`refs/heads/${defaultBranch}`);
  }
  return refs;
}

// pickTimesEntry returns the cache entry (of the REST API's actions_caches)
// to restore: the newest one with the prefix, on a ref the run can restore,
// created before the run started. Ties on the creation time go to the key
// that sorts last, so every shard picks the same entry. undefined when none
// qualifies.
export function pickTimesEntry(caches, { prefix, runStartedAt, refs }) {
  const start = Date.parse(runStartedAt);
  if (Number.isNaN(start)) {
    throw new Error(`bad run start ${runStartedAt}`);
  }
  const ok = caches.filter((c) => c.key?.startsWith(prefix) && refs.has(c.ref) && Date.parse(c.created_at) < start);
  ok.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || (a.key < b.key ? 1 : a.key > b.key ? -1 : 0));
  return ok[0];
}

async function githubGET(apiPath) {
  const api = process.env.GITHUB_API_URL || "https://api.github.com";
  const res = await fetch(api + apiPath, {
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${process.env.GH_TOKEN}`,
      "X-GitHub-Api-Version": "2022-11-28",
    },
  });
  if (!res.ok) {
    throw new Error(`GET ${apiPath}: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

async function cmdPick(opts) {
  if (!opts.kind) {
    throw new Error("pick needs --kind");
  }
  const env = process.env;
  const repo = env.GITHUB_REPOSITORY;
  const prefix = `go-test-times-${opts.kind}-`;
  const run = await githubGET(`/repos/${repo}/actions/runs/${env.GITHUB_RUN_ID}`);
  let defaultBranch = "";
  if (env.GITHUB_EVENT_PATH && fs.existsSync(env.GITHUB_EVENT_PATH)) {
    defaultBranch = JSON.parse(fs.readFileSync(env.GITHUB_EVENT_PATH, "utf8")).repository?.default_branch ?? "";
  }
  const refs = restorableRefs({ event: env.GITHUB_EVENT_NAME, ref: env.GITHUB_REF, baseRef: env.GITHUB_BASE_REF, defaultBranch });
  const list = await githubGET(`/repos/${repo}/actions/caches?key=${encodeURIComponent(prefix)}&sort=created_at&direction=desc&per_page=100`);
  const entry = pickTimesEntry(list.actions_caches ?? [], { prefix, runStartedAt: run.run_started_at, refs });
  const key = entry?.key ?? "";
  console.log(
    key
      ? `times: ${key} (${entry.ref}, created ${entry.created_at}; run started ${run.run_started_at})`
      : `times: none recorded before ${run.run_started_at} on ${[...refs].join(", ")}; every test gets the default estimate`,
  );
  if (env.GITHUB_OUTPUT) {
    fs.appendFileSync(env.GITHUB_OUTPUT, `key=${key}\n`);
  }
}

function readJSON(file, fallback) {
  if (!file) {
    return fallback;
  }
  if (!fs.existsSync(file)) {
    console.log(`${file} does not exist; using defaults`);
    return fallback;
  }
  return JSON.parse(fs.readFileSync(file, "utf8"));
}

// listedTests flattens a list into [{module, pkg, test, key}] in a stable
// order.
export function listedTests(list) {
  const out = [];
  for (const mod of Object.keys(list.modules).sort()) {
    const pkgs = list.modules[mod];
    for (const pkg of Object.keys(pkgs).sort()) {
      for (const test of pkgs[pkg]) {
        out.push({ module: mod, pkg, test, key: `${pkg} ${test}` });
      }
    }
  }
  return out;
}

function median(xs) {
  const s = [...xs].sort((a, b) => a - b);
  return s.length ? s[Math.floor(s.length / 2)] : undefined;
}

// recorded returns the planning weight of a times entry (a number of seconds,
// or {s, serial}), or undefined when it has none.
function recorded(v, serialWeight) {
  if (typeof v === "number") {
    return v >= 0 ? v : undefined;
  }
  if (v && typeof v.s === "number" && v.s >= 0) {
    return v.serial ? v.s * serialWeight : v.s;
  }
  return undefined;
}

// estimates returns the seconds to plan each listed test with.
export function estimates(tests, times, serialWeight = 1) {
  const byPkg = {};
  for (const t of tests) {
    const v = recorded(times[t.key], serialWeight);
    if (v !== undefined) {
      (byPkg[t.pkg] ??= []).push(v);
    }
  }
  const est = {};
  for (const t of tests) {
    est[t.key] = recorded(times[t.key], serialWeight) ?? median(byPkg[t.pkg] ?? []) ?? DEFAULT_SECONDS;
  }
  return est;
}

// partition assigns the listed tests to n shards: units (a test, or a group of
// tests sharing a fixture) longest first, each to the shard with the least
// planned time so far (the lowest index on a tie). Same inputs, same plan.
export function partition(list, times, groups, n, serialWeight = 1) {
  if (!Number.isInteger(n) || n < 1) {
    throw new Error(`bad shard count ${n}`);
  }
  const tests = listedTests(list);
  const est = estimates(tests, times, serialWeight);
  const byKey = new Map(tests.map((t) => [t.key, t]));
  const grouped = new Set();
  const units = [];
  for (const [pkg, sets] of Object.entries(groups ?? {})) {
    for (const names of Array.isArray(sets) ? sets : []) {
      if (!Array.isArray(names)) {
        continue;
      }
      const members = names.map((name) => byKey.get(`${pkg} ${name}`)).filter((t) => t && !grouped.has(t.key));
      if (members.length === 0) {
        continue;
      }
      members.forEach((t) => grouped.add(t.key));
      // The members wait for one fixture together, so each records about
      // the fixture's time: the group weighs as its longest member.
      units.push({ tests: members, seconds: Math.max(...members.map((t) => est[t.key])) });
    }
  }
  for (const t of tests) {
    if (!grouped.has(t.key)) {
      units.push({ tests: [t], seconds: est[t.key] });
    }
  }
  for (const t of tests) {
    t.seconds = est[t.key];
  }
  units.sort((a, b) => b.seconds - a.seconds || (a.tests[0].key < b.tests[0].key ? -1 : a.tests[0].key > b.tests[0].key ? 1 : 0));
  const shards = Array.from({ length: n }, (_, i) => ({ index: i, seconds: 0, tests: [], pkgs: new Set() }));
  for (const u of units) {
    const pkg = u.tests[0].pkg;
    const cost = (s) => s.seconds + u.seconds + (s.pkgs.has(pkg) ? 0 : PACKAGE_SECONDS);
    let best = shards[0];
    for (const s of shards) {
      if (cost(s) < cost(best)) {
        best = s;
      }
    }
    best.seconds = cost(best);
    best.tests.push(...u.tests);
    best.pkgs.add(pkg);
  }
  for (const s of shards) {
    s.tests.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
    delete s.pkgs;
  }
  return shards;
}

// checkCoverage fails unless every listed test is in exactly one shard and
// the shards hold nothing else.
export function checkCoverage(list, shards) {
  const want = new Set(listedTests(list).map((t) => t.key));
  const seen = new Map();
  const problems = [];
  for (const s of shards) {
    for (const t of s.tests) {
      if (!want.has(t.key)) {
        problems.push(`shard ${s.index} has ${t.key}, which is not listed`);
      }
      if (seen.has(t.key)) {
        problems.push(`${t.key} is in shards ${seen.get(t.key)} and ${s.index}`);
      }
      seen.set(t.key, s.index);
    }
  }
  for (const k of want) {
    if (!seen.has(k)) {
      problems.push(`${k} is in no shard`);
    }
  }
  if (problems.length > 0) {
    throw new Error(`the shards do not cover the listed tests exactly once:\n  ${problems.join("\n  ")}`);
  }
}

function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// invocations groups the tests of a shard by package: one go test run each,
// with a -run regex anchored to exactly those top-level tests. The regex
// does not reach subtests: go test splits -run at "/", and the first element
// alone selects the top-level test with all of its subtests.
export function invocations(shard) {
  const byPkg = new Map();
  for (const t of shard.tests) {
    const k = `${t.module}\0${t.pkg}`;
    if (!byPkg.has(k)) {
      byPkg.set(k, { module: t.module, pkg: t.pkg, tests: [] });
    }
    const p = byPkg.get(k);
    p.tests.push(t.test);
    p.seconds = (p.seconds ?? 0) + (t.seconds ?? 0);
  }
  // The longest packages first, so a long one does not start last.
  const invs = [...byPkg.values()].sort((a, b) => b.seconds - a.seconds || (a.pkg < b.pkg ? -1 : a.pkg > b.pkg ? 1 : 0));
  return invs.map((p) => ({ ...p, run: `^(${p.tests.map(escapeRegex).join("|")})$` }));
}

function loadPlanInputs(opts) {
  const list = readJSON(opts.list, null);
  if (!list) {
    throw new Error("--list is required");
  }
  const times = readJSON(opts.times, {});
  const groups = readJSON(opts.groups, {}).groups ?? {};
  const n = Number(opts.shards);
  const shards = partition(list, times, groups, n, Number(opts["serial-weight"] ?? 1));
  checkCoverage(list, shards);
  return { list, times, shards };
}

// planHash identifies a partition: which tests each shard runs.
export function planHash(shards) {
  const h = crypto.createHash("sha256");
  for (const s of shards) {
    h.update(`${s.index}:${s.tests.map((t) => t.key).join(",")}\n`);
  }
  return h.digest("hex").slice(0, 16);
}

// checkPlans fails unless every shard planned from the same times and got
// the same partition.
export function checkPlans(plans) {
  const keys = new Set(plans.map((p) => p.timesKey));
  const hashes = new Set(plans.map((p) => p.hash));
  if (keys.size > 1 || hashes.size > 1) {
    const lines = plans.map((p) => `shard ${p.shard}: times ${p.timesKey || "(none)"}, plan ${p.hash}`);
    throw new Error(`the shards did not plan alike, so their parts may overlap or leave tests out:\n  ${lines.join("\n  ")}`);
  }
  const shards = new Set(plans.map((p) => p.shard));
  const n = plans[0]?.shards ?? 0;
  if (shards.size !== plans.length || plans.length !== n) {
    throw new Error(`expected one plan from each of ${n} shards, got shards ${[...shards].join(", ")}`);
  }
}

function describe(shards) {
  for (const s of shards) {
    const pkgs = new Set(s.tests.map((t) => t.pkg));
    console.log(`shard ${s.index}: ${s.tests.length} tests in ${pkgs.size} packages, planned ${s.seconds.toFixed(1)}s`);
  }
}

function cmdPlan(opts) {
  const { shards } = loadPlanInputs(opts);
  describe(shards);
  if (opts.verbose) {
    for (const s of shards) {
      for (const inv of invocations(s)) {
        console.log(`  ${s.index} ${inv.pkg} -run ${inv.run.length > 120 ? inv.run.slice(0, 117) + "..." : inv.run}`);
      }
    }
  }
}

// readEvents parses a go test -json stream, skipping lines that are not JSON
// (a build failure prints plain text).
function readEvents(text) {
  const out = [];
  for (const line of text.split("\n")) {
    if (line.startsWith("{")) {
      try {
        out.push(JSON.parse(line));
      } catch {
        // not an event
      }
    }
  }
  return out;
}

// runOne runs go test for one package and writes its -json output to file.
// It prints what go test without -v prints: the output of failing tests and
// one line per package.
function runOne(inv, extra, file) {
  return new Promise((resolve) => {
    const args = ["test", "-json", ...extra, "-run", inv.run, inv.pkg];
    const child = spawn("go", args, { cwd: inv.module, stdio: ["ignore", "pipe", "pipe"] });
    const out = fs.createWriteStream(file);
    const pending = new Map();
    let buf = "";
    let stderr = "";
    const flush = (key, show) => {
      const lines = pending.get(key);
      pending.delete(key);
      if (show && lines) {
        process.stdout.write(lines.join(""));
      }
    };
    const handle = (line) => {
      if (!line.startsWith("{")) {
        process.stdout.write(line + "\n");
        return;
      }
      let ev;
      try {
        ev = JSON.parse(line);
      } catch {
        return;
      }
      const key = ev.Test ?? "";
      if (ev.Action === "output") {
        if (!pending.has(key)) {
          pending.set(key, []);
        }
        pending.get(key).push(ev.Output);
      } else if (ev.Action === "pass" || ev.Action === "skip") {
        flush(key, false);
        if (!ev.Test) {
          console.log(`ok   ${inv.pkg} (${inv.tests.length} tests, ${ev.Elapsed ?? 0}s)`);
        }
      } else if (ev.Action === "fail") {
        flush(key, true);
        if (!ev.Test) {
          console.log(`FAIL ${inv.pkg}`);
        }
      }
    };
    child.stdout.on("data", (d) => {
      out.write(d);
      buf += d.toString();
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        handle(buf.slice(0, i));
        buf = buf.slice(i + 1);
      }
    });
    child.stderr.on("data", (d) => {
      stderr += d.toString();
    });
    child.on("close", (code) => {
      if (buf) {
        handle(buf);
      }
      for (const k of [...pending.keys()]) {
        flush(k, code !== 0);
      }
      if (stderr) {
        process.stderr.write(stderr);
      }
      out.end(() => resolve(code ?? 1));
    });
  });
}

function resultFileName(inv) {
  return `${inv.module}__${inv.pkg}`.replace(/[^A-Za-z0-9._-]+/g, "_") + ".json";
}

async function cmdRun(opts) {
  const { shards } = loadPlanInputs(opts);
  const index = Number(opts.shard);
  if (!Number.isInteger(index) || index < 0 || index >= shards.length) {
    throw new Error(`bad --shard ${opts.shard} for ${shards.length} shards`);
  }
  if (!opts.out) {
    throw new Error("run needs --out");
  }
  describe(shards);
  const hash = planHash(shards);
  console.log(`plan ${hash} from times ${opts["times-key"] || "(none)"}`);
  if (opts["plan-out"]) {
    fs.writeFileSync(opts["plan-out"], JSON.stringify({ shard: index, shards: shards.length, timesKey: opts["times-key"] ?? "", hash }) + "\n");
  }
  fs.mkdirSync(opts.out, { recursive: true });
  const invs = invocations(shards[index]);
  const jobs = Math.max(1, Number(opts.jobs ?? 2));
  let next = 0;
  let failed = 0;
  const worker = async () => {
    while (next < invs.length) {
      const inv = invs[next++];
      const code = await runOne(inv, opts.extra, path.join(opts.out, resultFileName(inv)));
      if (code !== 0) {
        failed++;
      }
    }
  };
  await Promise.all(Array.from({ length: Math.min(jobs, invs.length) }, worker));
  if (failed > 0) {
    console.log(`${failed} of ${invs.length} go test runs failed`);
    process.exitCode = 1;
  }
}

// collectResults reads the -json results of every shard and returns the
// top-level results per test key.
export function collectResults(dirs) {
  const results = new Map();
  for (const dir of dirs) {
    if (!fs.existsSync(dir)) {
      continue;
    }
    for (const f of fs.readdirSync(dir).sort()) {
      if (!f.endsWith(".json")) {
        continue;
      }
      // A test that called t.Parallel pauses until the serial tests are done.
      const paused = new Set();
      // When a test runs. Elapsed leaves out parallel subtests that finish
      // after the test function returns, so the time is taken from the
      // events: from the run (or the last continue) to the result.
      const started = new Map();
      for (const ev of readEvents(fs.readFileSync(path.join(dir, f), "utf8"))) {
        const top = ev.Test && !ev.Test.includes("/");
        if (top && ev.Action === "pause") {
          paused.add(`${ev.Package} ${ev.Test}`);
        }
        if (top && (ev.Action === "run" || ev.Action === "cont") && ev.Time) {
          started.set(`${ev.Package} ${ev.Test}`, Date.parse(ev.Time));
        }
        if (!ev.Test || ev.Test.includes("/") || !["pass", "fail", "skip"].includes(ev.Action)) {
          continue;
        }
        const key = `${ev.Package} ${ev.Test}`;
        if (!results.has(key)) {
          results.set(key, []);
        }
        let elapsed = ev.Elapsed ?? 0;
        if (started.has(key) && ev.Time) {
          elapsed = Math.max(elapsed, (Date.parse(ev.Time) - started.get(key)) / 1000);
        }
        results.get(key).push({ action: ev.Action, elapsed, parallel: paused.has(key), file: path.join(dir, f) });
      }
    }
  }
  return results;
}

// checkResults fails unless every listed test reported exactly one result.
export function checkResults(list, results) {
  const problems = [];
  const want = listedTests(list);
  for (const t of want) {
    const r = results.get(t.key) ?? [];
    if (r.length === 0) {
      problems.push(`${t.key} reported no result`);
    } else if (r.length > 1) {
      problems.push(`${t.key} reported ${r.length} results (${r.map((x) => x.file).join(", ")})`);
    }
  }
  const listed = new Set(want.map((t) => t.key));
  for (const k of results.keys()) {
    if (!listed.has(k)) {
      problems.push(`${k} ran but is not listed`);
    }
  }
  if (problems.length > 0) {
    throw new Error(`the shards did not run every listed test exactly once:\n  ${problems.join("\n  ")}`);
  }
  return want.length;
}

function cmdMerge(opts) {
  const list = readJSON(opts.list, null);
  if (!list) {
    throw new Error("--list is required");
  }
  if (opts.plans) {
    const plans = opts.plans.split(",").map((f) => JSON.parse(fs.readFileSync(f, "utf8")));
    checkPlans(plans);
    console.log(`all ${plans.length} shards planned ${plans[0].hash} from times ${plans[0].timesKey || "(none)"}`);
  }
  const results = collectResults(opts.results);
  const n = checkResults(list, results);
  const counts = { pass: 0, fail: 0, skip: 0 };
  const times = {};
  for (const [k, r] of results) {
    counts[r[0].action]++;
    times[k] = { s: Math.round(r[0].elapsed * 100) / 100, serial: !r[0].parallel };
  }
  console.log(`all ${n} listed tests reported a result: ${counts.pass} passed, ${counts.fail} failed, ${counts.skip} skipped`);
  if (opts["times-out"]) {
    const sorted = Object.fromEntries(Object.entries(times).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
    fs.writeFileSync(opts["times-out"], JSON.stringify(sorted, null, 1) + "\n");
  }
  const slow = Object.entries(times).sort((a, b) => b[1].s - a[1].s).slice(0, 15);
  const lines = [`### Go tests: ${n} listed, ${counts.pass} passed, ${counts.fail} failed, ${counts.skip} skipped`, "", "| Slowest tests | s | serial |", "| --- | --- | --- |"];
  for (const [k, v] of slow) {
    lines.push(`| \`${k}\` | ${v.s} | ${v.serial ? "yes" : ""} |`);
  }
  if (process.env.GITHUB_STEP_SUMMARY) {
    fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, lines.join("\n") + "\n\n");
  }
}

async function main() {
  const [cmd, ...rest] = process.argv.slice(2);
  const opts = parseArgs(rest);
  switch (cmd) {
    case "pick":
      return cmdPick(opts);
    case "list":
      return cmdList(opts);
    case "plan":
      return cmdPlan(opts);
    case "run":
      return cmdRun(opts);
    case "merge":
      return cmdMerge(opts);
    default:
      throw new Error(`unknown command ${cmd ?? ""} (pick, list, plan, run, merge)`);
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((err) => {
    console.error(`go-test-shards: ${err.message}`);
    process.exit(1);
  });
}
