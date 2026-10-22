#!/usr/bin/env node
/**
 * Builds the client-side IIFE bundles (krate-runtime.js / krate-hydrate.js)
 * with esbuild: TypeScript compilation, tree-shaking, and minification in one
 * step. The bundles attach every exported API directly onto `window` so the
 * compiler's generated hydration scripts (and SPA router) can reference them
 * as globals.
 *
 * - krate-runtime.js - signals + DOM + JSX runtime + SPA router
 * - krate-hydrate.js - same minus the JSX runtime (used for SSG hydration)
 * - krate-hydrate-lite.js - hydration without the SPA router (`spa: false`)
 * - krate-suspense.js - Suspense, use(), lazy hydration chunks and async
 *   transitions. The compiler appends it to the runtime chunk only for sites
 *   whose hydration code uses them; it reads signals and slot helpers from the
 *   globals of the hydration bundle instead of bundling a second copy.
 *
 * esbuild minification here is complemented by the compiler, which re-minifies
 * the shared chunk with its embedded esbuild when writing dist/chunks/.
 */
const esbuild = require('esbuild');

const RUNTIME_ENTRY = `
export * from './src/signal';
export * from './src/effects';
export * from './src/dom';
export * from './src/list';
export * from './src/slots';
export * from './src/resource';
export * from './src/jsx-runtime';
export * from './src/router';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
`;

const HYDRATE_ENTRY = `
export * from './src/signal';
export * from './src/effects';
export * from './src/dom';
export * from './src/list';
export * from './src/slots';
export * from './src/resource';
export * from './src/router';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
`;

// Lite hydration bundle for sites with the SPA router disabled. It keeps the
// public navigation API as full-page navigation stubs.
const LITE_ENTRY = `
export * from './src/signal';
export * from './src/effects';
export * from './src/dom';
export * from './src/list';
export * from './src/slots';
export * from './src/resource';
export * from './src/router-lite';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
`;

const SUSPENSE_ENTRY = `export * from './src/suspense';`;

// Modules the suspense bundle shares with the hydration bundle. They resolve
// to the window globals the hydration bundle installed, so there is a single
// signal graph.
const SHARED_GLOBALS = {
  signal: ['createSignal', 'createEffect', 'onCleanup', 'krateGetOwner', 'krateSetOwner'],
};

const sharedGlobalsPlugin = {
  name: 'krate-shared-globals',
  setup(build) {
    build.onResolve({ filter: /^\.\/(signal)(\.js)?$/ }, (args) => ({
      path: args.path.replace(/^\.\//, '').replace(/\.js$/, ''),
      namespace: 'krate-global',
    }));
    build.onLoad({ filter: /.*/, namespace: 'krate-global' }, (args) => ({
      contents: SHARED_GLOBALS[args.path]
        .map((name) => `export const ${name} = (...a) => window.${name}(...a);`)
        .join('\n'),
      loader: 'js',
    }));
  },
};

// Dev-only overlay + toolbar. Loaded by `krate dev` only, never in production.
const DEV_ENTRY = `import './src/dev-overlay';`;

async function buildBundle(outFile, entry, minify = true, plugins = []) {
  const result = await esbuild.build({
    plugins,
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
  await buildBundle('krate-hydrate-lite.js', LITE_ENTRY);
  await buildBundle('krate-suspense.js', SUSPENSE_ENTRY, true, [sharedGlobalsPlugin]);
  await buildBundle('krate-dev.js', DEV_ENTRY, false);
  console.log('Runtime bundled! (krate-runtime.js + krate-hydrate.js + krate-hydrate-lite.js + krate-suspense.js + krate-dev.js)');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
