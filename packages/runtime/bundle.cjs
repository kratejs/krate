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
 * - krate-core.js + krate-f-*.js + krate-features.json - the same hydration
 *   runtime split into a core and optional feature bundles (SPA router,
 *   class helpers, server actions, error boundaries/class components). The
 *   compiler assembles only the features a site's hydration code references
 *   (see packages/compiler/internal/build/runtime_features.go); the full
 *   bundles above remain the fallback for dev mode and unknown code.
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
export * from './src/navigation';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/hook-args';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
export * from './src/use-id';
export * from './src/hints';
export * from './src/highlight-bind';
`;

const HYDRATE_ENTRY = `
export * from './src/signal';
export * from './src/effects';
export * from './src/dom';
export * from './src/list';
export * from './src/slots';
export * from './src/resource';
export * from './src/router';
export * from './src/navigation';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/hook-args';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
export * from './src/use-id';
export * from './src/hints';
export * from './src/highlight-bind';
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
export * from './src/navigation';
export * from './src/class-helpers';
export * from './src/context';
export * from './src/hook-args';
export * from './src/storage';
export * from './src/boundary';
export * from './src/class-component';
export * from './src/actions';
export * from './src/use-id';
export * from './src/hints';
export * from './src/highlight-bind';
`;

const SUSPENSE_ENTRY = `export * from './src/suspense';`;

// Modules the suspense bundle shares with the hydration bundle. They resolve
// to the window globals the hydration bundle installed, so there is a single
// signal graph.
const SHARED_GLOBALS = {
  signal: ['createSignal', 'createEffect', 'onCleanup', 'krateGetOwner', 'krateSetOwner', 'krateOnDispose', 'krateCurrentScope'],
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

// Core of the split runtime: everything the other pieces build on. refs,
// events and portal are exported too, because feature bundles import them
// from the shared window globals.
// hook-args holds krateParam, which the compiler emits into every custom hook
// that takes an argument.
const CORE_MODULES = ['signal', 'effects', 'dom', 'slots', 'resource', 'context', 'storage', 'hook-args', 'use-id', 'hints', 'refs', 'events', 'portal'];
const CORE_ENTRY = CORE_MODULES.map((m) => `export * from './src/${m}';`).join('\n');

// Optional features. A feature bundle imports core modules through window
// globals (one signal graph) and is appended after krate-core.js.
const FEATURES = {
  router: { file: 'krate-f-router.js', modules: ['router'] },
  'router-lite': { file: 'krate-f-router-lite.js', modules: ['router-lite'] },
  navigation: { file: 'krate-f-navigation.js', modules: ['navigation'] },
  helpers: { file: 'krate-f-helpers.js', modules: ['class-helpers'] },
  list: { file: 'krate-f-list.js', modules: ['list'] },
  actions: { file: 'krate-f-actions.js', modules: ['actions'] },
  classes: { file: 'krate-f-classes.js', modules: ['boundary', 'class-component'] },
  highlight: { file: 'krate-f-highlight.js', modules: ['highlight-bind'] },
};

async function exportNames(entry) {
  const result = await esbuild.build({
    stdin: { contents: entry, resolveDir: __dirname, sourcefile: 'krate-exports.ts' },
    bundle: true,
    write: false,
    format: 'esm',
    metafile: true,
    outfile: 'exports.js',
    logLevel: 'silent',
  });
  return Object.values(result.metafile.outputs)[0].exports.slice().sort();
}

// Resolves './signal'-style imports of core modules to window globals.
function coreGlobalsPlugin(coreExports) {
  return {
    name: 'krate-core-globals',
    setup(build) {
      build.onResolve({ filter: /^\.\/[a-z-]+(\.js)?$/ }, (args) => {
        const name = args.path.replace(/^\.\//, '').replace(/\.js$/, '');
        if (!coreExports[name]) return null;
        return { path: name, namespace: 'krate-core-global' };
      });
      build.onLoad({ filter: /.*/, namespace: 'krate-core-global' }, (args) => ({
        contents: coreExports[args.path].map((n) => `export const ${n} = /* @__PURE__ */ (() => window.${n})();`).join('\n'),
        loader: 'js',
      }));
    },
  };
}

// The navigation hooks import the router. The lite bundle has no SPA router,
// so they read router-lite there.
const liteRouterPlugin = {
  name: 'krate-lite-router',
  setup(build) {
    build.onResolve({ filter: /^\.\/router(\.js)?$/ }, () => ({ path: require('path').join(__dirname, 'src', 'router-lite.ts') }));
  },
};

// Names the router and router-lite share; the navigation feature reads them
// from whichever router the page loaded.
const ROUTER_GLOBALS = ['getNavigationState', 'subscribeNavigation', 'krateRouter'];

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

async function buildSplitRuntime() {
  const fs = require('fs');
  const path = require('path');
  const coreExports = {};
  for (const m of CORE_MODULES) coreExports[m] = await exportNames(`export * from './src/${m}';`);
  const featureGlobals = { ...coreExports, router: ROUTER_GLOBALS };
  await buildBundle('krate-core.js', CORE_ENTRY);
  // `full` lists every export of the full hydration bundle. The compiler's
  // tests compare it with the core and feature exports so a name the emitter
  // generates cannot go missing from the trimmed runtime.
  const manifest = {
    version: 1,
    core: { file: 'krate-core.js', exports: await exportNames(CORE_ENTRY) },
    full: await exportNames(HYDRATE_ENTRY),
    features: {},
  };
  for (const [name, f] of Object.entries(FEATURES)) {
    const entry = f.modules.map((m) => `export * from './src/${m}';`).join('\n');
    await buildBundle(f.file, entry, true, [coreGlobalsPlugin(f.modules.includes('navigation') ? featureGlobals : coreExports)]);
    manifest.features[name] = { file: f.file, exports: await exportNames(entry) };
  }
  fs.writeFileSync(path.join(__dirname, 'dist', 'krate-features.json'), JSON.stringify(manifest, null, 1) + '\n');
}

async function main() {
  const fs = require('fs');
  const path = require('path');
  fs.mkdirSync(path.join(__dirname, 'dist'), { recursive: true });
  await buildBundle('krate-runtime.js', RUNTIME_ENTRY);
  await buildBundle('krate-hydrate.js', HYDRATE_ENTRY);
  await buildBundle('krate-hydrate-lite.js', LITE_ENTRY, true, [liteRouterPlugin]);
  await buildBundle('krate-suspense.js', SUSPENSE_ENTRY, true, [sharedGlobalsPlugin]);
  await buildSplitRuntime();
  await buildBundle('krate-dev.js', DEV_ENTRY, false);
  console.log('Runtime bundled! (krate-runtime.js + krate-hydrate.js + krate-hydrate-lite.js + krate-suspense.js + krate-dev.js)');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
