import { defineConfig, type Plugin } from 'vitest/config';
import { transform } from 'esbuild';
import { fileURLToPath } from 'node:url';

const runtimeSrc = fileURLToPath(new URL('../runtime/src', import.meta.url));

// The package tsconfig uses `jsx: preserve` (Krate's compiler owns the
// transform), so Vitest's esbuild leaves JSX in place. This pre-plugin forces
// the automatic JSX transform against the workspace runtime.
const krateTsx: Plugin = {
  name: 'krate-tsx',
  enforce: 'pre',
  async transform(code, id) {
    if (!id.endsWith('.tsx')) return null;
    const result = await transform(code, {
      loader: 'tsx',
      jsx: 'automatic',
      jsxImportSource: '@krate/runtime',
      sourcemap: true,
    });
    return { code: result.code, map: result.map };
  },
};

export default defineConfig({
  plugins: [krateTsx],
  resolve: {
    alias: [
      { find: '@krate/runtime/jsx-dev-runtime', replacement: runtimeSrc + '/jsx-dev-runtime.ts' },
      { find: '@krate/runtime/jsx-runtime', replacement: runtimeSrc + '/jsx-runtime.ts' },
      { find: '@krate/runtime', replacement: runtimeSrc + '/index.ts' },
    ],
  },
  test: {
    environment: 'happy-dom',
    include: ['test/**/*.test.tsx'],
  },
});
