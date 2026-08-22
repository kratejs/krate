// Renders benchmark results as Markdown (and keeps the raw JSON alongside).
import { mkdirp, writeJson } from './exec.mjs';

export function formatBytes(n) {
  if (n == null) return '—';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

export function formatMs(n) {
  if (n == null) return '—';
  if (n < 1000) return `${Math.round(n)} ms`;
  return `${(n / 1000).toFixed(2)} s`;
}

export function median(values) {
  if (!values.length) return 0;
  const s = [...values].sort((a, b) => a - b);
  const mid = Math.floor(s.length / 2);
  return s.length % 2 ? s[mid] : (s[mid - 1] + s[mid]) / 2;
}

export function renderMarkdown(results) {
  const lines = [];
  lines.push('# Krate Benchmarks');
  lines.push('');
  lines.push(
    `_Generated ${results.meta.date} · Node ${results.meta.node} · Go ${results.meta.go} · ` +
      `${results.meta.os} ${results.meta.arch} · ${results.meta.cpus} CPU(s)_`,
  );
  lines.push('');

  if (results.build?.length) {
    lines.push('## Build time (cold, lower is better)');
    lines.push('');
    lines.push('A single page with a reactive counter and a 100-item list.');
    lines.push('');
    lines.push('| Framework | Version | Median | Best | Output size |');
    lines.push('|-----------|---------|-------:|-----:|------------:|');
    for (const row of results.build) {
      lines.push(
        `| ${row.name} | ${row.version ?? '—'} | ${formatMs(row.medianMs)} | ${formatMs(row.bestMs)} | ${formatBytes(row.outputBytes)} |`,
      );
    }
    lines.push('');
  }

  if (results.api?.length) {
    lines.push('## API throughput (higher is better)');
    lines.push('');
    lines.push(
      '`GET /api/json` → `{"message":"hello","id":42}` · ' +
        `${results.apiMeta?.connections ?? 32} connections · ${Math.round((results.apiMeta?.durationMs ?? 5000) / 1000)}s`,
    );
    lines.push('');
    lines.push('| Framework | Requests/s | p50 | p99 | Errors |');
    lines.push('|-----------|-----------:|----:|----:|-------:|');
    for (const row of results.api) {
      lines.push(
        `| ${row.name} | ${row.rps.toLocaleString()} | ${row.p50} ms | ${row.p99} ms | ${row.errors} |`,
      );
    }
    lines.push('');
  }

  lines.push('## Methodology');
  lines.push('');
  lines.push(
    '- **Build**: dependencies are installed once, then a cold production build is run ' +
      `${results.buildMeta?.runs ?? 1} time(s); the median is reported. Output size is the ` +
      'total size of the emitted directory.',
  );
  lines.push(
    '- **API**: each server is built, warmed up, then load-tested with a fixed ' +
      'concurrency of keep-alive connections for a fixed window; requests per second and ' +
      'latency percentiles are measured.',
  );
  lines.push('- Hardware and toolchain versions are captured in the raw JSON output.');
  lines.push('');
  lines.push('Run it yourself: `pnpm --filter @krate/benchmark bench` (see `packages/benchmark/README.md`).');
  lines.push('');

  return lines.join('\n');
}

export async function writeResults(results, outDir) {
  await mkdirp(outDir);
  await writeJson(`${outDir}/latest.json`, results);
  const md = renderMarkdown(results);
  const { writeFile } = await import('node:fs/promises');
  await writeFile(`${outDir}/latest.md`, md);
  return { markdown: md, json: results };
}
