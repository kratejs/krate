// HTTP load testing.
//
// Prefers `autocannon` when it is installed (add it with `pnpm add -D autocannon`
// in this package) because it can saturate fast servers that a single-process
// Node client cannot. Otherwise falls back to a dependency-free keep-alive
// client that is accurate enough for relative comparisons.
import http from 'node:http';
import https from 'node:https';

const MAX_SAMPLES = 500_000;

/**
 * Load-test a URL. Returns { requests, errors, durationMs, rps, latency }.
 */
export async function loadTest(url, { durationMs = 5000, connections = 32, warmupMs = 1000 } = {}) {
  let autocannon;
  try {
    ({ default: autocannon } = await import('autocannon'));
  } catch {
    autocannon = null;
  }

  if (autocannon) return loadTestAutocannon(autocannon, url, { durationMs, connections, warmupMs });
  return loadTestBuiltin(url, { durationMs, connections, warmupMs });
}

async function loadTestAutocannon(autocannon, url, { durationMs, connections, warmupMs }) {
  const result = await autocannon({
    url,
    connections,
    duration: Math.max(1, durationMs / 1000),
    warmup: warmupMs > 0,
  });
  return {
    requests: result.requests.total,
    errors: (result.errors ?? 0) + (result.non2xx ?? 0),
    durationMs,
    rps: Math.round(result.requests.average),
    latency: {
      p50: round(result.latency.p50),
      p90: round(result.latency.p90),
      p99: round(result.latency.p99),
      max: round(result.latency.max),
    },
  };
}

function round(n) {
  return n == null ? 0 : Math.round(n * 100) / 100;
}

async function loadTestBuiltin(url, { durationMs = 5000, connections = 32, warmupMs = 1000 } = {}) {
  const parsed = new URL(url);
  const mod = parsed.protocol === 'https:' ? https : http;
  const agent = new mod.Agent({ keepAlive: true, maxSockets: connections });

  // Warm up (drops cold-start / JIT noise from the sample window).
  const warmup = { running: true, ok: 0, err: 0, latencies: [], samples: 0 };
  const warmWorkers = Array.from({ length: connections }, () =>
    worker(parsed, mod, agent, warmup),
  );
  await new Promise((r) => setTimeout(r, warmupMs));
  warmup.running = false;
  await Promise.all(warmWorkers);

  const state = { running: true, ok: 0, err: 0, latencies: [], samples: 0 };
  const start = performance.now();
  const workers = Array.from({ length: connections }, () =>
    worker(parsed, mod, agent, state),
  );
  await new Promise((r) => setTimeout(r, durationMs));
  state.running = false;
  await Promise.all(workers);
  const elapsedMs = performance.now() - start;

  agent.destroy();

  const total = state.ok + state.err;
  const rps = total / (elapsedMs / 1000);
  const sorted = state.latencies.sort((a, b) => a - b);

  return {
    requests: total,
    errors: state.err,
    durationMs: Math.round(elapsedMs),
    rps: Math.round(rps),
    latency: {
      p50: percentile(sorted, 0.5),
      p90: percentile(sorted, 0.9),
      p99: percentile(sorted, 0.99),
      max: sorted.length ? sorted[sorted.length - 1] : 0,
    },
  };
}

async function worker(parsed, mod, agent, state) {
  while (state.running) {
    await oneRequest(parsed, mod, agent, state);
  }
}

function oneRequest(parsed, mod, agent, state) {
  return new Promise((resolve) => {
    const t0 = performance.now();
    const req = mod.request(
      {
        hostname: parsed.hostname,
        port: parsed.port,
        path: parsed.pathname + parsed.search,
        method: 'GET',
        agent,
      },
      (res) => {
        res.on('data', () => {});
        res.on('end', () => {
          if (state.samples < MAX_SAMPLES) {
            state.latencies.push(performance.now() - t0);
            state.samples++;
          }
          if (res.statusCode === 200) state.ok++;
          else state.err++;
          resolve();
        });
      },
    );
    req.on('error', () => {
      state.err++;
      resolve();
    });
    req.end();
  });
}

function percentile(sorted, q) {
  if (!sorted.length) return 0;
  const i = Math.min(sorted.length - 1, Math.floor(q * sorted.length));
  return Math.round(sorted[i] * 100) / 100;
}
