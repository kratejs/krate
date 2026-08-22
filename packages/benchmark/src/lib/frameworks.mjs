// Framework registry: what to build, how to install, and how to start each
// API server for the throughput benchmark.
import { join } from 'node:path';
import { run, spawnProcess, writeText } from './exec.mjs';

export function npxCmd() {
  return process.platform === 'win32' ? 'npx.cmd' : 'npx';
}

export function exeName(base) {
  return process.platform === 'win32' ? `${base}.exe` : base;
}

/**
 * Build-time frameworks. Each entry maps a committed fixture under fixtures/
 * to a production build command. `install` controls whether `npm install` must
 * run first.
 */
export const BUILD_FRAMEWORKS = [
  {
    id: 'krate',
    name: 'Krate',
    fixture: 'krate',
    outDir: 'dist',
    install: true,
    versionPackage: null,
    build: (ctx) => ({ cmd: ctx.krateBin, args: ['build'], cwd: ctx.workDir }),
  },
  {
    id: 'nextjs',
    name: 'Next.js',
    fixture: 'next',
    outDir: '.next',
    install: true,
    versionPackage: 'next',
    build: () => ({ cmd: npxCmd(), args: ['next', 'build'], cwd: null }),
  },
  {
    id: 'vite',
    name: 'Vite + React',
    fixture: 'vite',
    outDir: 'dist',
    install: true,
    versionPackage: 'vite',
    build: () => ({ cmd: npxCmd(), args: ['vite', 'build'], cwd: null }),
  },
  {
    id: 'astro',
    name: 'Astro',
    fixture: 'astro',
    outDir: 'dist',
    install: true,
    versionPackage: 'astro',
    build: () => ({ cmd: npxCmd(), args: ['astro', 'build'], cwd: null }),
  },
  {
    id: 'sveltekit',
    name: 'SvelteKit',
    fixture: 'sveltekit',
    outDir: 'build',
    install: true,
    versionPackage: '@sveltejs/kit',
    prebuild: () => ({ cmd: npxCmd(), args: ['svelte-kit', 'sync'], cwd: null }),
    build: () => ({ cmd: npxCmd(), args: ['vite', 'build'], cwd: null }),
  },
  {
    id: 'remix',
    name: 'Remix',
    fixture: 'remix',
    outDir: 'build',
    install: true,
    versionPackage: '@remix-run/dev',
    build: () => ({ cmd: npxCmd(), args: ['remix', 'vite:build'], cwd: null }),
  },
];

/**
 * API-throughput targets. Each entry prepares a server (build step) and starts
 * it on a given port. All servers answer `GET /api/json` with the same JSON
 * payload so the load test is apples-to-apples.
 */
export const API_FRAMEWORKS = [
  {
    id: 'krate-go',
    name: 'Krate — Go API route',
    fixture: 'krate-go',
    install: true,
    async prepare(ctx) {
      const res = await run(ctx.krateBin, ['build'], { cwd: ctx.workDir });
      if (res.code !== 0) throw new Error(res.stderr || 'krate build failed');
      ctx.bin = join(ctx.workDir, '.krate', exeName('goapi-server'));
    },
    async start(ctx, port) {
      return spawnProcess(ctx.bin, [], {
        cwd: ctx.workDir,
        env: { ...process.env, KRATE_GOAPI_PORT: String(port) },
      });
    },
    path: '/api/json',
  },
  {
    id: 'krate-ts',
    name: 'Krate — TS API route',
    fixture: 'krate',
    install: true,
    async prepare(ctx) {
      const res = await run(ctx.krateBin, ['build'], { cwd: ctx.workDir });
      if (res.code !== 0) throw new Error(res.stderr || 'krate build failed');
    },
    async start(ctx, port) {
      const cfgPath = join(ctx.workDir, '.krate', 'bench-serve-config.ts');
      await writeText(
        cfgPath,
        [
          'export default {',
          "  entry: 'src/pages/index.tsx',",
          "  outDir: 'dist',",
          "  pagesDir: 'src/pages',",
          "  publicDir: 'public',",
          '  minify: true,',
          `  devServer: { port: ${port}, open: false },`,
          '};',
          '',
        ].join('\n'),
      );
      return spawnProcess(ctx.krateBin, ['serve', '--config', cfgPath, '.'], {
        cwd: ctx.workDir,
      });
    },
    path: '/api/json',
  },
  {
    id: 'nextjs',
    name: 'Next.js — route handler',
    fixture: 'next',
    install: true,
    async prepare(ctx) {
      const res = await run(npxCmd(), ['next', 'build'], { cwd: ctx.workDir });
      if (res.code !== 0) throw new Error(res.stderr || 'next build failed');
    },
    async start(ctx, port) {
      return spawnProcess(npxCmd(), ['next', 'start', '-p', String(port)], {
        cwd: ctx.workDir,
      });
    },
    path: '/api/json',
  },
  {
    id: 'node-http',
    name: 'Node http (baseline)',
    fixture: 'node-http',
    install: false,
    async prepare() {},
    async start(ctx, port) {
      return spawnProcess('node', ['server.mjs'], {
        cwd: ctx.workDir,
        env: { ...process.env, PORT: String(port) },
      });
    },
    path: '/api/json',
  },
  {
    id: 'go-nethttp',
    name: 'Go net/http (baseline)',
    fixture: 'go-nethttp',
    install: false,
    async prepare(ctx) {
      const out = exeName('server');
      const res = await run('go', ['build', '-o', out, '.'], { cwd: ctx.workDir });
      if (res.code !== 0) throw new Error(res.stderr || 'go build failed');
      ctx.bin = join(ctx.workDir, out);
    },
    async start(ctx, port) {
      return spawnProcess(ctx.bin, [], {
        cwd: ctx.workDir,
        env: { ...process.env, PORT: String(port) },
      });
    },
    path: '/api/json',
  },
];
