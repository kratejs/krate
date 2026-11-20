#!/usr/bin/env node
/**
 * Gates a release on a green full CI run of the tagged commit, instead of the
 * release workflow re-running the compiler, native, browser and JS suites
 * itself.
 *
 * A full run is a CI run of this exact commit from a push (dev, main), the
 * nightly schedule or a manual dispatch; pull request runs are the fast set
 * and run on a merge commit, so they never count. The gate:
 *   - passes as soon as a full run of the commit has succeeded;
 *   - waits while one is queued or in progress;
 *   - fails when every finished full run of the commit failed (fix it, or
 *     re-run the failed jobs and re-run this release);
 *   - dispatches CI on the tag when the commit has no full run (or only
 *     cancelled ones), then waits for that run.
 *
 * Env: GITHUB_TOKEN (actions: write to dispatch), GITHUB_REPOSITORY,
 * GITHUB_SHA, GITHUB_REF_NAME, optional GITHUB_API_URL, GITHUB_SERVER_URL,
 * KRATE_CI_GATE_TIMEOUT_MIN (default 150), KRATE_CI_GATE_POLL_SEC (default 30).
 */
import { fileURLToPath } from 'node:url';

export const WORKFLOW = 'ci.yml';
const fullEvents = new Set(['push', 'schedule', 'workflow_dispatch']);

// decide looks at the CI runs of one commit and says what the gate does next:
// { action: 'pass' | 'wait' | 'fail' | 'dispatch', runs }.
export function decide(runs, sha) {
  const full = runs.filter((r) => r.head_sha === sha && fullEvents.has(r.event));
  const passed = full.filter((r) => r.status === 'completed' && r.conclusion === 'success');
  if (passed.length) return { action: 'pass', runs: passed };
  const pending = full.filter((r) => r.status !== 'completed');
  if (pending.length) return { action: 'wait', runs: pending };
  // A cancelled run says nothing about the code; a skipped or neutral one
  // did not run the suite either.
  const failed = full.filter((r) => r.conclusion === 'failure' || r.conclusion === 'timed_out' || r.conclusion === 'startup_failure');
  if (failed.length) return { action: 'fail', runs: failed };
  return { action: 'dispatch', runs: [] };
}

async function main() {
  const token = process.env.GITHUB_TOKEN;
  const repo = process.env.GITHUB_REPOSITORY;
  const sha = process.env.GITHUB_SHA;
  const ref = process.env.GITHUB_REF_NAME;
  const api = process.env.GITHUB_API_URL || 'https://api.github.com';
  const server = process.env.GITHUB_SERVER_URL || 'https://github.com';
  const timeoutMin = Number(process.env.KRATE_CI_GATE_TIMEOUT_MIN || 150);
  const pollSec = Number(process.env.KRATE_CI_GATE_POLL_SEC || 30);
  if (!token || !repo || !sha || !ref) {
    console.error('release-ci-gate: GITHUB_TOKEN, GITHUB_REPOSITORY, GITHUB_SHA and GITHUB_REF_NAME are required');
    process.exit(2);
  }
  const headers = {
    Authorization: `Bearer ${token}`,
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
  };
  const listRuns = async () => {
    const url = `${api}/repos/${repo}/actions/workflows/${WORKFLOW}/runs?head_sha=${sha}&per_page=100`;
    const res = await fetch(url, { headers });
    if (!res.ok) throw new Error(`listing CI runs: HTTP ${res.status} ${await res.text()}`);
    return (await res.json()).workflow_runs;
  };
  const describe = (r) => `${r.event} run on ${r.head_branch}: ${r.status}${r.conclusion ? `/${r.conclusion}` : ''} ${r.html_url}`;

  const deadline = Date.now() + timeoutMin * 60_000;
  let dispatched = false;
  let last = '';
  for (;;) {
    const { action, runs } = decide(await listRuns(), sha);
    const line = `${action}: ${runs.map(describe).join('; ') || 'no full CI run of this commit'}`;
    if (line !== last) console.log(line);
    last = line;
    if (action === 'pass') return;
    if (action === 'fail') {
      console.log(`::error title=CI failed for ${sha.slice(0, 7)}::The full CI run of this commit failed, so it is not released. Fix it on dev, or re-run the failed jobs, then re-run this release.`);
      process.exit(1);
    }
    if (action === 'dispatch') {
      if (dispatched) {
        // The dispatched run has not been listed yet; keep polling.
      } else {
        const res = await fetch(`${api}/repos/${repo}/actions/workflows/${WORKFLOW}/dispatches`, {
          method: 'POST',
          headers,
          body: JSON.stringify({ ref }),
        });
        if (!res.ok) throw new Error(`dispatching CI on ${ref}: HTTP ${res.status} ${await res.text()}`);
        dispatched = true;
        console.log(`No full CI run of ${sha} yet; dispatched CI on ${ref} (${server}/${repo}/actions/workflows/${WORKFLOW}).`);
      }
    }
    if (Date.now() > deadline) {
      console.log(`::error title=CI did not finish::No full CI run of ${sha} finished within ${timeoutMin} minutes.`);
      process.exit(1);
    }
    await new Promise((r) => setTimeout(r, pollSec * 1000));
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((err) => {
    console.error(`release-ci-gate: ${err.message}`);
    process.exit(1);
  });
}
