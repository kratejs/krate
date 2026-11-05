// Times every rule of a bundle against hostile strings, to find regexes that
// take super-linear time on one match attempt. A rule that never finishes is
// reported as a hang. usage:
// node hl_rules.mjs <bundle.json> <n> <threshold-ms> <out.json>
import fs from "node:fs";
import { Worker, isMainThread, parentPort, workerData } from "node:worker_threads";

const rep = (s, k) => s.repeat(Math.ceil(k / s.length)).slice(0, k);
const chars = ['"', "'", "`", "/", "*", "\\", "{", "}", "(", ")", "[", "]", "<", ">", "-", "#", "$", "@", ":", ";", ",", ".", "=", "!", "&", "|", "%", "+", "~", "^", "?", " ", "\t", "a", "A", "0", "1", "_", "\u00e9", "\n"];
const seqs = ["a ", "a,", "a.", "a-", "a_", "1.", "1,", "1e", "a:", " a", "a b", "a\n", "\\\n", "a'", 'a"', "*/", "/*", "{}", "()", "[]", "0x", "<a", "--", "..", "::", "$a", "#a", "@a", "a=", "a;", "ab"];
function makeProbes(n) {
  const probes = [];
  for (const c of chars) {
    probes.push(rep(c, n), "!" + rep(c, n));
    for (const suffix of ["!", "z", "A", "0", "_", "-", "\u00e9", "\n", " ", "$", "\\"]) probes.push(rep(c, n) + suffix);
  }
  for (const s of seqs) probes.push(rep(s, n), rep(s, n) + "!", rep(s, n) + "\n");
  return probes;
}
function timeRule(re, probes) {
  let worst = 0;
  let worstProbe = "";
  for (const p of probes) {
    for (const start of [0, 1]) {
      re.lastIndex = start;
      const t0 = process.hrtime.bigint();
      re.exec(p);
      const ms = Number(process.hrtime.bigint() - t0) / 1e6;
      if (ms > worst) {
        worst = ms;
        worstProbe = JSON.stringify(p.slice(0, 6)) + "x" + p.length + "@" + start;
      }
    }
  }
  return [worst, worstProbe];
}

if (!isMainThread) {
  const { bundlePath, n, from } = workerData;
  const bundle = JSON.parse(fs.readFileSync(bundlePath, "utf8"));
  const small = makeProbes(40);
  const mid = makeProbes(200);
  const big = makeProbes(n);
  let idx = 0;
  for (const t of bundle.tables) {
    t.s.forEach((rules, si) => {
      rules.forEach((r, ri) => {
        const me = idx++;
        if (me < from) return;
        const src = r[0];
        if (src === "") return;
        parentPort.postMessage({ start: me, table: t.n, state: si, rule: ri, pattern: src.slice(0, 200) });
        let re;
        try {
          re = new RegExp(src, "yu");
        } catch (e) {
          parentPort.postMessage({ me, error: String(e) });
          return;
        }
        // Short hostile strings first, so exponential backtracking is caught
        // before it stalls the longer runs.
        for (const [probes, kind, limit] of [[small, "exponential", 40], [mid, "exponential", 40], [big, "slow", Number(workerData.threshold)]]) {
          const [worst, probe] = timeRule(re, probes);
          if (worst > limit) {
            parentPort.postMessage({ me, ms: worst, probe, kind });
            return;
          }
        }
      });
    });
  }
  parentPort.postMessage({ done: true });
} else {
  const [bundlePath, nArg, thrArg, outPath] = process.argv.slice(2);
  const slow = [];
  let from = 0;
  while (true) {
    const finished = await new Promise((resolve) => {
      const w = new Worker(new URL(import.meta.url), { workerData: { bundlePath, n: Number(nArg), threshold: Number(thrArg), from } });
      let current = null;
      let timer = setTimeout(onHang, 8000);
      function onHang() {
        w.terminate();
        if (current) slow.push({ ...current, kind: "hang", ms: 8000 });
        from = current ? current.start + 1 : from + 1;
        resolve(false);
      }
      w.on("message", (m) => {
        clearTimeout(timer);
        timer = setTimeout(onHang, 8000);
        if (m.start !== undefined) current = m;
        else if (m.done) {
          clearTimeout(timer);
          w.terminate();
          resolve(true);
        } else if (m.error !== undefined || m.kind) slow.push({ ...current, ...m });
      });
      w.on("error", (e) => {
        clearTimeout(timer);
        slow.push({ ...current, error: String(e) });
        from = current ? current.start + 1 : from + 1;
        resolve(false);
      });
    });
    if (finished) break;
  }
  fs.writeFileSync(outPath, JSON.stringify({ slow }));
}
