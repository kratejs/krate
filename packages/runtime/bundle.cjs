#!/usr/bin/env node
/**
 * Builds the client-side IIFE bundles (krate-runtime.js / krate-hydrate.js)
 * with esbuild: TypeScript compilation, tree-shaking, and minification in one
 * step. The bundles attach every exported API directly onto `window` so the
 * compiler's generated hydration scripts (and SPA router) can reference them
 * as globals.
 *
 * - krate-runtime.js — signals + DOM + JSX runtime + SPA router
 * - krate-hydrate.js  — same minus the JSX runtime (used for SSG hydration)
 *
 * esbuild minification here is complemented by the compiler, which re-minifies
 * the shared chunk with its embedded esbuild when writing dist/chunks/.
 */
const esbuild = require('esbuild');

const RUNTIME_ENTRY = `
export * from './src/signal';
export * from './src/dom';
export * from './src/resource';
export * from './src/jsx-runtime';
export * from './src/reconcile';
export * from './src/router';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/storage';
`;

const HYDRATE_ENTRY = `
export * from './src/signal';
export * from './src/dom';
export * from './src/resource';
export * from './src/reconcile';
export * from './src/router';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/storage';
`;

// Dev-only overlay + toolbar. Loaded by `krate dev` only, never in production.
const DEV_ENTRY = `import './src/dev-overlay';`;

async function buildBundle(outFile, entry, minify = true) {
  const result = await esbuild.build({
    stdin: { contents: entry, resolveDir: __dirname, sourcefile: 'krate-entry.ts' },
    bundle: true,
    write: false,
    format: 'iife',
    globalName: '__krate_exports',
    minify,
    treeShaking: true,
    // Matches the .browserslistrc baseline (ES2020); the compiler emits ES2020
    // for hydration chunks and workers too.
    target: 'es2020',
    charset: 'utf8',
    logLevel: 'silent',
    footer: {
      js: 'for (const k in __krate_exports) { window[k] = __krate_exports[k]; }',
    },
  });
  const fs = require('fs');
  fs.writeFileSync(require('path').join(__dirname, 'dist', outFile), result.outputFiles[0].text);
  console.log(`Built ${outFile} (${(result.outputFiles[0].text.length / 1024).toFixed(1)} KB)`);
}

async function main() {
  const fs = require('fs');
  const path = require('path');
  fs.mkdirSync(path.join(__dirname, 'dist'), { recursive: true });
  await buildBundle('krate-runtime.js', RUNTIME_ENTRY);
  await buildBundle('krate-hydrate.js', HYDRATE_ENTRY);
  await buildBundle('krate-dev.js', DEV_ENTRY, false);
  console.log('Runtime bundled! (krate-runtime.js + krate-hydrate.js + krate-dev.js)');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
