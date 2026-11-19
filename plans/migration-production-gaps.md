# Plan: production gaps in the migrated DigitalDesigns app (PR #45)

Status: PLAN ONLY. Nothing changes until the user says "implement" in this thread.
Then the work lands on `perf/output-quality` (PR #45).

## Sources

- Krate tree read at `17305682` (PR #45 head). All paths below are under `packages/compiler/internal/` unless they start with `packages/`.
- App copy: `~/wt/krate-mig-pr45`.
- Logs: `~/wt/krate-pr35-scratch/logs/{migrate,build}.txt`.
- Original Next app (read only): `~/Repos/DigitalDesigns/packages/web`.

## Summary of decisions

| Item | Decision | Manual step left |
|---|---|---|
| A1 global `Buffer` in an embedded API route | Auto-route to Node any route whose import graph has an unguarded Node-only global. KR7160 becomes an error only when `ssr.apiRuntime` is explicitly `"quickjs"`. | none |
| A2 KR7116 self-clash | Reproduce and instrument first (the realpath dedupe already exists, so there is a second cause). Canonicalise module identity at resolve time. Print project-relative paths. | none |
| A3 KR8310 hint | Per-cause hint (prefixed but unset / set under `NEXT_PUBLIC_` / unprefixed / not in the configured prefixes) | none |
| A4 (new) redirect and header source matching | One shared Next-compatible matcher. Pass the query string through. Support `statusCode`. Run redirects before middleware and on `/api`. | none |
| B1 env | New `envPrefix` config (default `["KRATE_PUBLIC_"]`). The migrator writes `["KRATE_PUBLIC_", "NEXT_PUBLIC_"]` and stops renaming. | none |
| B2 headers and CSP | The migrator copies next.config's helper code into `krate.config.ts`. Krate accepts Next's `[{key, value}]` header shape and Next sources. | none |
| B3 images | Same copy-through. Krate accepts `protocol` and `port` in remotePatterns. | none |
| B4 output and deploy | `output: 'standalone'` becomes `deploy.target: "node"`. New `outputFileTracingRoot` key, with a mirrored layout like Next standalone. Includes may leave the project but must stay inside the root. | none |
| B5 conditional redirects | Option (c): `has`/`missing` in Krate redirects, headers and rewrites. Middleware fallback only for regexes RE2 cannot express. | none for this app |
| B6 transpilePackages / turbopack | Verify resolution at migrate time and drop silently on success. `turbopack.root` folds into the tracing root. | only if a package fails to resolve |
| B7 package.json | Dependency and script updates become the default (`--no-update-package` to opt out). Remove `next`/`next-auth` when nothing imports them. Correct `@krate/*` versions. | one line: run your package manager's install |
| B8 outside CSS reference | Leave a stub `src/app/tokens.css` that `@import`s the moved file. | none (info line) |
| B9 instrumentation | First-class `src/instrumentation.ts` `register()`, awaited before the Node server listens. The migrator keeps the file and folds `NEXT_RUNTIME`. | none |
| B10 next-auth types | `@krate/auth` gets an augmentable `SessionUserExtensions` and a `DefaultSession` alias. The migrator rewrites the `.d.ts`. | none |

After this work, a re-run of `krate migrate` on the app should print no `!` items except "install dependencies". The goal is a test that asserts exactly that (see "End-to-end acceptance").

---

## A. Bugs

### A1. A global `Buffer` in an API route served by the embedded runtime returns 500

**Today**

- `build/build.go:5513-5603` `compileSingleRoute` chooses embedded QuickJS unless:
  - esbuild leaves an external import (`hasExternalImport`, `build.go:5586`), or
  - `provablyNodeOnly` (`build/nodeonly.go:53`) proves the route needs Node.
- Both checks look only at imports, never at globals.
- `scanRuntimeCompat` (`build/api_compat.go:83`) does scan the whole bundled graph. That is why the helper `src/server/discord-embed.ts:133` was also flagged.
- `jsruntime/compat_diag.go:24` emits the result as `SeverityWarning`, and the result never feeds into routing.
- The ledger (`packages/native/jsruntime/compat.json`) already marks the following as not-supported: `Buffer`, `global`, `__dirname`, `__filename`, `process.nextTick`, `process.cwd`, `process.argv`, `process.exit`, `process.platform`, `process.version(s)`, `process.hrtime`, `process.memoryUsage`, `process.uptime`, `process.on`, `process.stdout`, `process.stderr`. It also lists engine gaps such as parts of `Intl`.
- The migrator (`migrate/segmentconfig.go:50,99`) strips `export const runtime` and says "Krate API routes already run on Node". That is wrong: the default is embedded QuickJS.

**Options**

1. **Auto-route by global usage (pick).**
   - In `compileSingleRoute`, after the QuickJS bundle is built, run `ScanCompatFiles`.
   - If any file in the graph has an unguarded not-supported ref and the scan is `Complete`, call `finishNodeOnlyRoute`. That is the same path an external `node:` import takes.
   - Guarded refs (`typeof Buffer`, `globalThis.Buffer ??`) keep the route embedded, as they do today.
   - Node implements everything the ledger marks not-supported, so engine gaps such as `Intl` route to Node too. A route that would 500 on QuickJS works on Node.
   - `apibatch.go:96-113` (the batch path) and `plan.detected` must make the same decision. Factor it into one `routeNeedsNode(meta, scan)` helper.
   - The build prints one info line per route, `api/discord-embed.json -> Node (Buffer at src/server/discord-embed.ts:133)`, so the move is visible. The line is not a warning.
2. **The migrator sets `ssr.apiRuntime: "node"` globally** when any Next route lacks `runtime = 'edge'`.
   - Rejected as the default. It moves all 46 routes to Node and loses the embedded runtime's latency and memory profile for the other 45.
3. **Honour a per-route `export const runtime`** (`'nodejs'` routes to Node; `'edge'` keeps the route embedded and turns any not-supported ref into an error).
   - Add this as an explicit override on top of 1.
   - The migrator stops stripping `runtime = 'nodejs' | 'edge'` from API routes. It still strips it from pages, where it means nothing in Krate.

**KR7160 severity**

- With `ssr.apiRuntime` unset (auto), KR7160 cannot fire for a route that 1 moved to Node.
- A route that stays embedded only because of a guarded ref keeps the warning.
- With `ssr.apiRuntime: "quickjs"` set explicitly, or `export const runtime = 'edge'`, an unguarded not-supported ref is a **build error** (KR7160 as `SeverityError`). The user asked for embedded, and the route would fail at runtime.
- `embeddedRuntime()` treats `""` and `"quickjs"` the same today. Distinguish them in `api_compat.go:17`.

**Files**

- `build/build.go` (`compileSingleRoute`, `finishNodeOnlyRoute` call site)
- `build/apibatch.go`
- `build/api_compat.go`
- `build/nodeonly.go`
- `jsruntime/compat_diag.go` (severity by mode)
- `diag/codes_apiruntime.go` (explanation text)
- `migrate/segmentconfig.go` (stop stripping API `runtime`, fix the wording)
- `build/apimaxduration.go` style reader for `runtime`. A new file `build/apiruntimeexport.go` reads `export const runtime` statically.
- Docs: `packages/web/src/content/docs/reference/config.md` (`ssr.apiRuntime`), the API routes guide, and `guides/migrating-from-nextjs.md`.

**Tests** (`build/api_compat_test.go`, helpers `compatProject`, `readRouteMetas`, `b.CompileAPIRoutes`)

- A route that uses a global `Buffer` gets `meta.NodeOnly == true`, no KR7160, and the info line.
- A route importing a helper that uses a global `Buffer`: same result. This is the user's case.
- A guarded `typeof Buffer !== 'undefined'` stays embedded and keeps the warning.
- With `ssr.apiRuntime: "quickjs"`, the build fails with KR7160 at both locations.
- `export const runtime = 'edge'` with `Buffer` is an error. `export const runtime = 'nodejs'` routes to Node with no refs at all.
- An e2e test in `build/serve_api_test.go`: `GET` of the route returns 200 JSON. Next parity: the same route source returns 200 under Next 16 (golden fixture, see "Next parity fixtures").
- Migrator: `segmentconfig` keeps `runtime` in API routes. Update the existing assertions.

**Output change for apps that already migrate cleanly.** Only routes that warn KR7160 for a not-supported ref today, which are broken at runtime, move to Node. If such a route exists, `krate serve` now starts the Node API sidecar where it did not before. Routes with no unsupported refs are untouched.

### A2. KR7116 false positives: a file clashes with itself (111 warnings)

**Today**

- `bundler/private_names.go:531` reports a clash when `distinctFiles(stuck) > 1`.
- `distinctFiles` (line 557) already keys on `realPathOf` (EvalSymlinks), and `TestExportClashIgnoresOneFileUnderTwoPaths` (`build/rc5_buildgraph_test.go:103`) covers the symlink case.
- The build log came from a binary at `17305682` (`go version -m ~/wt/krate-pr34/krate-bin`), and the copy holds exactly one `api-keys.ts` and one `settings.ts`. Yet the warning still fires.
- **So the symlink spelling is not the (only) cause. There is a second path that realpath does not unify.**
- The message prints `filepath.Base` (lines 445, 534), which hides which paths they are.

**Plan**

1. **Reproduce and instrument first.** Add a `KRATE_DEBUG=exportclash` dump of the full `stuck` paths, owners and importers, and build the migrated app once. Candidate causes to confirm or rule out:
   - Two `Module` values for one file whose `Path`s differ in a way EvalSymlinks keeps, for example a non-absolute path from a tsconfig `paths` base (`tsconfig.base.json` at the monorepo root) that EvalSymlinks resolves against the process cwd and fails on, falling back to `p`.
   - A module reached through `selfReference` (`resolver/resolver.go:117`), which returns an unresolved path.
   - Relative imports inside a package reached via a symlinked dir (`bundler.go:1104-1110` never realpaths). This is likely the same as above.
   - `realPaths` memo poisoning across builder passes (`sync.Map` cleared only by `ResetResolveCache`).
2. **Fix at the source.**
   - `resolveImportForModuleUncached` (`bundler.go:374`) returns `realPathOf(abs)` for every non-`node_modules` project or workspace result, and always an absolute, cleaned path.
   - Then one file is one `Module`, and the clash check, the renames and the graph all agree.
   - Keep `distinctFiles` as a safeguard, and also collapse `ownerMods` by real path in `planExportRenames`, so a duplicate never produces a rename either.
3. **Messages.** `files` and `importers` print `filepath.Rel(b.root, mod.Path)`, falling back to the absolute path for files outside the root. The dedupe key at line 629 uses the same strings.

**Files:** `bundler/private_names.go`, `bundler/bundler.go` (resolve), possibly `resolver/resolver.go` (`selfReference`), plus a debug hook in `bundler/` behind an env var.

**Tests**

- A regression in `build/rc5_buildgraph_test.go` or `build/exportclash_test.go` that copies the shape found in step 1: a workspace package behind a pnpm-style symlink, a barrel `export * from './settings'`, and two pages that import it once by the package name and once by whichever second spelling step 1 finds. Assert no KR7116.
- A real clash (two distinct files) prints project-relative paths: `packages/a/settings.ts and packages/b/settings.ts both export ...`.
- The migrated-app build has 0 KR7116 (acceptance run).

**Output change:** none, except fewer and clearer warnings. If a duplicate `Module` really exists today, bundles may lose a duplicated copy of that module, which makes them smaller. Esbuild-built server bundles already dedupe by realpath, so nothing changes at runtime.

### A3. KR8310's hint is wrong when the name is already prefixed

**Today:** `bundler/envdiag.go:58` always says "name the variable with the KRATE_PUBLIC_ prefix". `build/irwarnings.go:97-116` (`releaseEnvWarnings`) holds `b`, so it can see `b.Env`, `environ.Current` and the shell.

**Plan**

- Add a structured `EnvName` and `EnvRoot` to the diagnostic instead of parsing the message.
- Rewrite the hint in `releaseEnvWarnings` using the configured prefixes (see B1). Hint texts:
  - (a) **Prefixed, unset**: `KRATE_PUBLIC_UMAMI_URL is not set when krate build runs. Define it in <envDir>/.env (Krate loads .env, .env.production, .env.local, .env.production.local from <envDir>) or export it in the build environment.`
  - (b) **Prefixed, unset, and a twin under another known public prefix is set** (for example `NEXT_PUBLIC_UMAMI_URL` is set): `NEXT_PUBLIC_UMAMI_URL is set but the code reads KRATE_PUBLIC_UMAMI_URL. Rename one of them, or add "NEXT_PUBLIC_" to envPrefix in krate.config.ts.`
  - (c) **Unprefixed**: the current hint, plus `(KRATE_PUBLIC_X is set: use that name)` when it exists.
  - (d) **A prefix not in `envPrefix`** (for example `NEXT_PUBLIC_X` read in code while `envPrefix` lacks it): `NEXT_PUBLIC_ is not a public prefix in this project; add it to envPrefix in krate.config.ts, or rename the variable to KRATE_PUBLIC_X.`
- An empty value counts as set, matching how inlining treats it.

**Files:** `bundler/envdiag.go`, `build/irwarnings.go`, `build/env_public.go` (helper), `diag/codes_render.go:53-59` (explanation).

**Tests:** `build/module_values_test.go` (`envProject`) gets one case per hint (a) to (d), asserting the exact hint text. `t.Setenv` covers the shell-set variant.

**Output change:** diagnostic text only.

### A4. Found while planning: redirect and header sources do not match like Next

The user didn't list these, but they affect this app and any migrated app.

- **Regex params never match.**
  - The migrated `/store/:slug((?!compare$)[^/]+)` to `/templates/:slug` is written into `krate.config.ts`, but `matchRedirectParamsMods` (`build/serve.go:2843`) splits it on `/`, so `/store/<slug>` never matches.
  - Old template links therefore get Krate's 404 instead of a 308.
  - Go's RE2 also has no lookahead, so the regex cannot be passed through as written.
- **Header sources.** `headerSourceMatches` (`build/serve_hardening.go:445`) knows exact paths, `prefix*` and `path.Match` only. `/:path*` becomes the prefix `/:path` and never matches.
- **Query string.** Redirects drop the request's query (`http.Redirect` with the rewritten destination, `serve_hardening.go:163`). Next appends it, for example `/store/plans?ref=x` goes to `/store?ref=x`.
- **Status.** Krate sends 301 or 302. Next sends 308 or 307, and also accepts `statusCode`.
- **Order and coverage.**
  - Krate runs middleware before config redirects (`serve.go:2462`, `gate.wrap(redirectRewriteHandler)`). Next runs headers, then redirects, then middleware.
  - Config redirects do not apply to `/api/*`, which is mounted directly (`serve.go:1782`). In Next they do.
  - For this app, the maintenance and coming-soon gate in `src/middleware.ts` would answer `/store/foo` before the redirect fires.

**Plan: one shared source matcher, `build/routesource.go`**

- It compiles Next's path-to-regexp subset once at load: literal segments, `:name`, `:name*`, `:name+`, `:name?`, `:name(regex)`, unnamed `(regex)`, and `{...}` optional groups if cheap.
- It compiles to RE2. Leading negative lookaheads of the form `(?!a$|b$)rest` inside a param are compiled to "rest matches AND ^(a|b)$ does not". That is the only lookahead form found in the app and the common Next idiom.
- Any other regex RE2 cannot express (lookbehind, backreferences, a lookahead not at the start) is a **config error at load** (new code, for example KR2310 "redirect source uses a regex feature Go cannot run"). It must never be a silently dead rule. The migrator handles such rules through the middleware fallback (B5).
- Krate's legacy forms keep working unchanged: `/x/*` with `splat`, and `path.Match` globs in headers.
- The `redirects`, `rewrites` and `headers` matchers all call it. `headerSourceMatches` keeps its old behaviour for sources without `:` or `(`.

**Other redirect changes**

- **Query passthrough:** the request's query is merged into the destination query (destination keys win), as Next's `prepareDestination` does.
- **`statusCode`** (301/302/303/307/308) is added to `config.Redirect`.
  - `permanent` keeps meaning 301/302 in Krate, so existing apps see no change.
  - The migrator always writes `statusCode: 308` or `307` for Next rules, so migrated apps match Next exactly.
- **Order:** move `serveConfigRedirect` to an outer layer that runs before the middleware gate and also covers `/api/*`.
  - Headers stay outermost, as they are today.
  - Rewrites stay after middleware, matching Next's `beforeFiles` placement.
- **Custom header precedence:** `customHeadersMiddleware` sets headers before the inner handler, so a handler that sets `Cache-Control` (static files, pages) overrides the user's rule.
  - Next applies `headers()` to public files and pages, so the user's rule wins. Exception: Next forces its own `Cache-Control` on `/_next/static`.
  - Wrap the writer so user rules are applied at `WriteHeader` time and win. Krate's own hashed-asset path (`/_krate/...`) keeps its immutable `Cache-Control`.

**Files**

- New `build/routesource.go`.
- `build/serve.go`: matcher, `rewriteDestination`, handler order.
- `build/serve_hardening.go`: `serveConfigRedirect`, `customHeadersMiddleware`, `headerSourceMatches`.
- `config/config.go`: `Redirect.StatusCode`.
- `config/validate.go`: known keys, and compile errors at load.
- `diag/` new code.
- Docs: `reference/config.md` (redirects, rewrites, headers source syntax, query, status, order).

**Tests**

- `build/routesource_test.go`: a table of source and path to params, including `/:path*`, `/:file(regex)`, `/store/:slug((?!compare$)[^/]+)` (matches `/store/foo`, rejects `/store/compare`), and the unsupported-regex error.
- `build/serve_hardening_test.go`: query passthrough, `statusCode`, redirects before middleware (a middleware that 503s everything, and the redirect still fires), and a redirect applying to `/api/x`.
- `build/serve_security_test.go`: open-redirect protections still hold through the new matcher.
- Next parity golden (see "Next parity fixtures"): every rule in the app's `next.config.ts` plus the cache rules, each request's status, `Location` and headers recorded from Next 16.

**Output changes for existing Krate apps.** These need a CHANGELOG "Changed" entry.

- Redirects now keep the request query.
- Redirects run before middleware and also apply to `/api/*`.
- User `headers` rules now win over Krate's per-handler `Cache-Control` on public files and pages.
- Sources containing `:` or `(` that silently never matched now match.

---

## B. Manual items from `krate migrate`

### B1. Public environment variables

**Today**

- Krate inlines only `KRATE_PUBLIC_*`. The prefix is hardcoded in `build/env_public.go:43-49` and `bundler/envdiag.go:12`.
- The migrator (`migrate/env.go:55-128`) renames source reads `NEXT_PUBLIC_X` to `KRATE_PUBLIC_X` and never writes values.
- `readPublicEnv` (`env.go:29`, called at `nextjs.go:277`) looks only in the app dir. It ignores the `envDir: "../.."` the migrator itself derived from `loadEnvConfig`, and it ignores `.env.example`.
- The values come from the root `.env` and ward at build time under their `NEXT_PUBLIC_` names. So after the rename nothing defines `KRATE_PUBLIC_*`, and Umami, Turnstile and the bgmodel URL are undefined in the browser.

**Options**

1. **Krate always treats `NEXT_PUBLIC_*` as public too** (an implicit alias).
   - Zero config.
   - But it changes inlining for every Krate app, including non-migrated ones whose shell happens to carry `NEXT_PUBLIC_` variables from another project. They are only inlined if code reads them, but it is still an implicit widening of what counts as public.
2. **The migrator keeps the `NEXT_PUBLIC_` names in code and Krate accepts a configurable prefix list (pick).**
   - New config key `envPrefix: string | string[]`, default `["KRATE_PUBLIC_"]`, the same idea as Vite's `envPrefix`.
   - The migrator writes `envPrefix: ["KRATE_PUBLIC_", "NEXT_PUBLIC_"]` and stops renaming. Source, `.env` files, ward and CI stay exactly as they were.
   - Validation: each prefix is non-empty and ends in `_`. `""` is rejected, because it would expose every variable.
   - It's explicit, existing apps don't change, and the migrated app needs no hand work.
3. **The migrator honours `envDir` and the monorepo root** when looking for definitions.
   - On its own this only improves the report text, since values still aren't written.
   - Do it as well, cheaply. With option 2 the item disappears, and the report gets an info line instead: `public env vars keep their NEXT_PUBLIC_ names (envPrefix); defined in ../../.env.example: 4 of 4`.

A **KRATE_PUBLIC_X falls back to NEXT_PUBLIC_X** alias is rejected. Two names for one value, with silent precedence, is confusing.

**Files**

- `config/config.go` (`EnvPrefix`), `config/validate.go` (known keys), the `tsconfig.go` parse, and the defineConfig types in `packages/core`.
- `build/env_public.go` (use the prefixes), `bundler/envdiag.go` (prefixes passed in via options), and `envFingerprint` (include the prefixes).
- `migrate/env.go` (no rename when writing `envPrefix`; destructuring needs no manual item any more, since the names are unchanged), `migrate/configmerge.go` (key lists at :53, :138), and `migrate/nextjs.go:277` (pass `EnvDir`, read `.env.example` names).
- Docs: `reference/config.md`, the environment guide, and `guides/migrating-from-nextjs.md`.

**Tests**

- `build/module_values_test.go`:
  - With `envPrefix` including `NEXT_PUBLIC_`, a client read of `process.env.NEXT_PUBLIC_X` and `import.meta.env.NEXT_PUBLIC_X` is inlined.
  - Without it, the read is not inlined and gets hint (d).
  - `""` in `envPrefix` is a config error.
- `migrate/` (new `env_test.go`):
  - `NEXT_PUBLIC_` reads are unchanged and `envPrefix` is written.
  - Definitions are found in an `envDir` outside the app and in `.env.example`.
- `build/next_migrate_dynamic_test.go`: migrate then build a fixture with the value only in a monorepo-root `.env`. The client bundle contains the value.

**Output change:** none for existing apps (the default prefix list is unchanged). Migrated apps keep `NEXT_PUBLIC_` names in source instead of renaming. This is a behaviour change of the migrator, recorded in the CHANGELOG.

### B2. `next.config.ts` headers and security headers

**Today**

- The migrator parses the AST only (`migrate/nextconfig.go:84`). `headers()` returns `[{ source: '/:path*', headers: securityHeaders }, ...buildCacheRules()]`, where:
  - `securityHeaders` is a top-level const computed by `buildSecurityHeaders({...process.env}, {dev})`;
  - `buildCacheRules()` is a spread of a local helper call.
- Both helpers are pure. `src/config/security-headers.ts` takes an env map. `cache-headers.ts` has no imports.
- `krate.config.ts` is bundled with esbuild and evaluated in QuickJS with the loaded env (shell plus `envDir` `.env`), at build, serve and dev. It can import local modules, and `defineConfig` accepts a function.

**Options**

1. **Evaluate `next.config.ts` at migrate time and write resolved values.**
   - Rejected. It bakes the migrating machine's env into source: `APP_URL`, S3 and asset origins, possibly secrets.
   - `APP_URL` on a dev box would put `localhost` into the production `form-action`.
   - It also needs `import.meta.dirname` and `@next/env` shims in QuickJS.
2. **Copy the helper code into `krate.config.ts` and run it there (pick).**
   - The migrator turns next.config.ts's preamble into krate.config.ts's preamble:
     - top-level imports of local modules;
     - `const` declarations the config object depends on;
     - the statements that mutate them (the `remotePatterns.push` in an `if`).
   - The rules for building that preamble:
     - Drop `import type` lines, and `import ... from 'next'` lines that are type-only.
     - Drop the `loadEnvConfig(...)` call and its `@next/env` import. The call is already mapped to `envDir` (`nextenvdir.go`).
     - Rewrite `import.meta.dirname` to a value jseval provides. Add `import.meta.dirname` and `import.meta.filename` support to `jseval.go:315`, next to `import.meta.url`.
     - Keep `node:path` (shimmed).
   - The config object becomes `defineConfig({ ...translated static keys, headers: [<translated rules>], images: { remotePatterns } })`. Computed expressions are copied verbatim, so `{ source: '/:path*', headers: securityHeaders }` and `...buildCacheRules()` survive as code.
   - Krate needs two changes to accept those values as Next wrote them:
     - `config.HeaderRule.Headers` also accepts Next's `[{ key, value }]` array (`config/security.go:23`, custom `UnmarshalJSON`). The order is kept, and the last value of a repeated key wins, as in Next.
     - Next source syntax in `headers`, from A4's shared matcher.
   - **Verification at migrate time.** After writing, the migrator evaluates the new `krate.config.ts` with `config.Load` (the real loader and env). If evaluation throws, for example because a helper imports `next/server` or a Node-only module the QuickJS shims lack, the migrator:
     - writes the rule commented out with the error;
     - emits a manual item that quotes the error and names the import that failed.
   - This app passes.
   - `dev`: next.config passes `{ dev: process.env.NODE_ENV !== 'production' }`. Krate's `ScriptEnv` sets `NODE_ENV` to development in dev and production otherwise (`environ/environ.go:60-68`), so the copied expression keeps its meaning.
3. **Generate middleware that sets the headers.**
   - Rejected for static headers. It costs a Node sidecar round trip per matched request, because this app's middleware runs on Node.
   - It also misses the static files the matcher excludes, such as `/_k/...` cache rules and public files.
   - Kept only for genuinely per-request values (a CSP nonce). The migrator does not need it here.

**The report-only CSP and its endpoint**

- `Content-Security-Policy-Report-Only` is just a header, so it passes through unchanged.
- `report-uri /api/csp-report` already exists as the migrated `src/api/csp-report.ts`. That route uses no Node globals, so it stays embedded.
- Krate's own `csp.enabled` meta-tag CSP stays off.
- A user `Content-Security-Policy` header replaces Krate's header CSP (`Header.Set`). There is no conflict, because the enforced CSP has no `script-src`, and the report-only one allows `'unsafe-inline'`, which Krate's inline hydration needs.
- `form-action` keeps Discord, Roblox and Stripe exactly as the helper builds them from env at serve time. That matters for section C's OAuth and checkout flows.

**Krate defaults that would differ from Next once the rules match**

- `Permissions-Policy` and the others are overridden by the user rule, so they are identical.
- HSTS: Krate adds `Strict-Transport-Security` when `seo.baseUrl` is https and `security.hsts` is unset. Next sends none, and in production the app's Caddy sets HSTS.
- Recommendation: leave Krate's default on. Caddy's header directive replaces an upstream value, and duplicate HSTS is harmless.
- The migrate report notes it as an info line, not a manual item: `Krate also sends HSTS for https baseUrl; set security.hsts: "off" to match Next exactly`.

**Semantics difference to document.** Next bakes `headers()` into `routes-manifest.json` at build. Krate evaluates `krate.config.ts` at build and again at serve start (`krate start`), but bakes the values for `deploy --target node`. For this app the build and runtime env are the same. The docs say which env each mode uses.

**Files**

- `migrate/nextconfig.go` (preamble extraction and emit), `migrate/nextconfig_rules.go` (header rules as code, drop the "computed" manual items), `migrate/configmerge.go` (krate.config.ts becomes a TS module with a preamble, not only JSON).
- `jseval/jseval.go` (`import.meta.dirname` and `filename`), `config/security.go` (array header shape).
- `build/serve_hardening.go` (matcher from A4).
- Docs: `reference/config.md` (headers shape), `guides/migrating-from-nextjs.md`.

**Tests**

- `migrate/nextconfig_gaps_test.go`: a next.config shaped like the app's (helper imports, a computed const, `headers()` returning a const plus a spread call, `loadEnvConfig`). It asserts:
  - the emitted krate.config.ts text, as a golden file;
  - that it evaluates with `config.Load` to the expected `HeaderRule`s for a given env.
- A helper importing `next/server`: commented rule plus a manual item quoting the error.
- `build/serve_hardening_test.go`: `[{key, value}]` headers apply, the last value wins, and user `Cache-Control` beats the static handler's.
- Next parity golden: for each request in the fixture (`/`, `/store`, a public file under `/_k/chunks/`, `/brand/x.svg`, `/preview/<hex16>.wasm`, `/api/x`), the full header set from Next 16 equals Krate's, minus an allowlist (`date`, `etag`, `x-powered-by`, `strict-transport-security`, `x-krate-*`, `vary` differences documented).

**Output change:** none for existing apps beyond A4. The migrator's krate.config.ts output for apps with only static next.config keys stays JSON-shaped and byte-identical. Assert this in a test.

### B3. Images: `remotePatterns` built from env

- Same copy-through as B2. `const remotePatterns = [...]; if (process.env.PUBLIC_ASSET_BASE_URL) { ... push(...) }` is copied, and `images: { remotePatterns }` references it.
- Krate's `ImagesConfig.RemotePattern` (`config/images.go:8`) gains optional `protocol`, `port` and `search`, enforced in `RemoteAllowed` as Next does. Today `migrate/images.go:74` drops them. With them, the copied objects mean the same thing, and an `http:` asset host is not silently allowed over https.
- **Files:** `config/images.go`, `build/image.go:476` (pass the scheme and port), `migrate/images.go`, `migrate/nextconfig.go`.
- **Tests:**
  - `config` unit tests for protocol and port matching.
  - The migrate golden from B2 includes this block.
  - A build test where `PUBLIC_ASSET_BASE_URL` is set in `.env`, and an `<Image src>` on that host builds with no KR8802, while a host off the list still fails with KR8802.
- **Output change:** none for existing configs. The new fields are optional, and absent means any, as today.

### B4. Output and deploy

**Today**

- `krate start` (= `serve`) is already a production server, like `next start`; it needs the installed `node_modules`.
- `krate deploy --target node` writes `dist/.krate/server/server.mjs` plus a minimal traced `node_modules`. That is the analogue of `.next/standalone`.
- `serverExternalPackages` is carried over already.
- `outputFileTracingIncludes` is carried over only when every glob stays in the project (`staticTraceIncludes`). `servertrace.go:231` rejects globs that leave the root.
- There is no tracing root, and route keys are flattened.
- The app's includes are `../render-service/dist/scramble-worker.mjs` and `../../node_modules/.pnpm/...` native binaries. At runtime the app looks for the worker at `<cwd>/../render-service/dist` (standalone, cwd = `packages/web`) or `<cwd>/packages/render-service/dist` (Docker, cwd = `/app`). So it relies on Next's mirrored standalone layout.

**Options for `output: 'standalone'`**

1. The migrator writes nothing and leaves a hint (today).
2. **The migrator writes `deploy: { target: "node" }` and points the `build` script at a build plus a node deploy (pick).** The exact command depends on whether `krate deploy` builds first, which I'll check during implementation. Prod then runs `node dist/.krate/server/<project-rel>/server.mjs`, as it runs `.next/standalone/packages/web/server.js` today.
3. Map to `krate start` from the worktree. That needs a full `pnpm install` in prod. It's simpler but not equivalent to standalone, so it isn't the default. The report says it is the alternative.

**`outputFileTracingRoot`.** New config key `outputFileTracingRoot` (a path, resolved against the project, which may point outside it). When set:

- `outputFileTracingIncludes` globs may leave the project as long as their real path stays inside the root. `servertrace.go:231` checks against the root instead of the project.
- **Mirrored layout (pick, matching Next standalone):**
  - The node target writes `dist/.krate/server/<rel(root, project)>/server.mjs`.
  - Included files land at `dist/.krate/server/<rel(root, file)>`.
  - Traced externals go under `dist/.krate/server/node_modules`, at the root level, where Node's resolution walks up to them.
  - The worker lookup `<cwd>/../render-service/dist` then works unchanged with cwd = `server/packages/web`.
- Alternative: keep `server.mjs` at the top and put outside files under `server/__root__/`. Rejected, because app code that computes paths relative to cwd, like this app's, breaks.
- Without the key, the output is unchanged.

**`outputFileTracingIncludes`**

- Route keys: keep per-route keys in config, but the node bundle is one process, so the union is copied (as today). Document that.
- The migrator translates the map verbatim when an `outputFileTracingRoot` is translated. The paths stay relative to the project, as in Next.
- `copyTraceIncludes` follows pnpm `.pnpm` symlinks (`checkRealInside` already allows the store).

**Migrator for `outputFileTracingRoot: path.resolve(import.meta.dirname, '../..')`.** Resolve it statically the way `staticEnvDir` does (`nextenvdir.go`) and write `"outputFileTracingRoot": "../.."`.

**Docker target in a monorepo.** `internal/deploy/deploy.go:425-545` uses the project's lockfile only. Out of scope for this app (prod uses the node bundle), but the report says so when a workspace root is detected. That is an info line, not a manual item.

**Files**

- `config/config.go` (`OutputFileTracingRoot`), `config/validate.go`.
- `build/serverdeploy.go` (layout, `serverEntry` cwd note), `build/servertrace.go` (root check, mirrored destinations).
- `migrate/nextconfig_rules.go` and `nextconfig_server.go` (translate root and includes; `output: 'standalone'` becomes `deploy.target`).
- `migrate/nextjs.go` (`build` script).
- Docs: the Self-hosting guide and `reference/config.md`.

**Tests**

- `build/serverless_targets_test.go`: a monorepo fixture with a sibling package file included via `../sibling/dist/x.mjs`. Assert it lands at `server/sibling/dist/x.mjs`, with `server.mjs` at `server/app/server.mjs`.
- A glob escaping the root is still an error.
- `migrate/nextconfig_gaps_test.go`: `outputFileTracingRoot` and the app's includes translate, and no manual item remains.
- Node-target smoke in `build/serve_server_e2e_test.go` style: start `server.mjs` with cwd = `server/app`, and an API route that does `fs.existsSync(path.resolve(process.cwd(), '../sibling/dist/x.mjs'))` returns true.

**Output change:** none unless `outputFileTracingRoot` is set.

### B5. Conditional redirects (`has`/`missing`)

The app's rules are `/store` with `has: [{type: 'query', key: 'vehicle' | 'category' | 'q'}]`, going to `/templates` with 308, and the query carried over.

**Option (a): Krate supports `has`/`missing` natively on `redirects`, `rewrites` and `headers`**

- Shape: `{ type: 'query' | 'header' | 'cookie' | 'host', key?: string, value?: string }`. `key` is required except for `host`.
- Semantics copied from Next's `matchHas`:
  - `value` is a regex, matched anchored as `^(?:value)$`.
  - With no `value`, the key only has to be present.
  - With no `value`, the item's value is captured under the key's name when the key is a valid identifier.
  - Named groups in `value` are captured.
  - Captures are usable in the destination as `:name`.
  - `missing` items must all fail to match.
- Values compile with A4's matcher, so the same RE2 limits apply and a config error is raised where they can't be met.
- Query passthrough comes from A4.
- Pros: a declarative, zero-cost Go check that runs before middleware like Next, so the maintenance gate does not swallow it. The migrator translates `has`/`missing` 1:1.
- Cons: more config surface and docs.

**Option (b): the migrator generates middleware code for conditional rules**

- Pros: no Krate change.
- Cons:
  - It edits user middleware text, which is fragile with the NextResponse shim.
  - It runs after Krate's redirect layer, and inside the app's maintenance and coming-soon gates unless it is inserted first.
  - Apps without middleware gain one, and with it the middleware runtime cost on every matched request.
  - The rules disappear from config, so they are not reviewable as data.

**Option (c): (a) by default, plus (b) only for rules (a) cannot express (pick)**

- The migrator writes `has`/`missing` into config.
- If a value regex fails A4's compile check (JS-only syntax), it inserts that one rule into the start of the middleware function in a marked block (`// krate migrate: next.config redirects[i]`), with a manual item only if no middleware file can be created safely.
- For this app all three rules are expressible, so the result is pure config and no manual item.

**Ordering with `src/middleware.ts`**

- After A4, config redirects run before middleware, matching Next. So `/store?vehicle=x` redirects even in maintenance mode, as in Next.
- The middleware matcher already includes `/store`. No change is needed there.
- `permanent: true` becomes `statusCode: 308`, so the status matches Next.

**Files:** `config/config.go` (`Has`, `Missing` on `Redirect`, `Rewrite`, `HeaderRule`), `config/validate.go`, `build/routesource.go` and `serve*.go` (evaluation and captures), `migrate/nextconfig_rules.go:50` (translate instead of refusing), and a new `migrate/middleware_inject.go` (the (c) fallback). Docs: `reference/config.md`.

**Tests**

- `build/routesource_test.go`:
  - query, header, cookie and host;
  - presence only;
  - a value regex;
  - a named capture used in the destination;
  - `missing`;
  - the key-as-capture rule;
  - a mixed `has` and `missing` rule.
- `build/serve_hardening_test.go`: the app's three rules. `/store?vehicle=x` returns 308 to `/templates?vehicle=x`. `/store` returns 200. `/store?other=1` returns 200.
- Next parity golden: the same requests recorded from Next 16.
- Migrator: the three rules become config with no manual items. A rule with a lookbehind value goes into middleware, and its golden text is asserted.

**Output change:** none for configs without `has`/`missing`.

### B6. `transpilePackages` and `turbopack`

**Do they matter in Krate?**

- No for `transpilePackages`. The resolver realpaths a workspace symlink outside `node_modules` (`resolver/resolver.go:134-137`), and `IsCompiledDependency` (line 660) treats its `.ts` as project source.
- All eight `@dd/*` packages export `src/*.ts` through `main` or `exports`.
- `turbopack.root` is the same monorepo root as the tracing root.

**Plan**

- **`transpilePackages`.** At migrate time, call `resolver.NodeModuleWith(projectRoot, name, ClientConditions(false))` for each name.
  - If every package resolves to a path outside `node_modules`, drop the key silently. The report gets an info line: `transpilePackages: 8 workspace packages resolve to source; Krate compiles them without config`.
  - If a package resolves into `node_modules` and ships TS: also fine.
  - If it does not resolve: keep a manual item naming the package and the reason, either "not installed (run pnpm install)" or "exports point at dist/ which does not exist".
  - That is the only case left manual, because the migrator cannot install or build sibling packages.
- **`turbopack`.**
  - `root`: drop when it equals the translated `outputFileTracingRoot` or `envDir` root (an info line).
  - `resolveAlias`: translate to tsconfig `paths` advice or a Krate alias if one exists. Otherwise a manual item.
  - `rules` (webpack loaders): stays manual. Loaders are arbitrary JS build steps, and the hint keeps pointing to plugins.
  - For this app only `root` is set, so there is no item.

**Files:** `migrate/nextconfig_rules.go:409-419`, plus a new helper in `migrate/` that calls `resolver`.

**Tests:** `migrate/nextconfig_gaps_test.go`. The existing `transpilePackages` assertion at `:222-231` changes:

- a workspace fixture with a resolvable `@acme/ui` gives no manual item and an info line;
- a missing package gives a manual item;
- `turbopack: { root }` gives no item;
- `turbopack: { rules }` gives a manual item.

**Output change:** the migrate report only.

### B7. `package.json`

**Today:** `--update-package` is opt-in.

- It adds `@krate/core`, `@krate/runtime`, `@krate/components`, `preact`, and `@krate/auth` when `next-auth` is listed.
- It rewrites `dev`/`build`/`serve`/`start` scripts that mention `next`.
- It removes nothing.
- `@krate/three` and `@krate/konva` copy the runtime's version spec (`reactthree.go:356-395`). But those packages version independently (0.1.0), so `^0.5.0-rc.4` would not resolve. That is a bug.

**Plan**

1. **Make the package update the default.**
   - `--no-update-package` opts out. Keep `--update-package` as an accepted no-op for scripts.
   - It's safe: `package.json` is in the backup, and `krate migrate rollback` restores it.
   - Without it, the migrated app cannot build (KR8009 for missing preact, and unresolved `@krate/three` and `@krate/konva`). A default that never builds is not a useful default.
2. **Add every dependency that the rewritten imports need**: `@krate/three`, `@krate/konva` and `@krate/auth` join the list.
3. **Correct versions.**
   - The compiler embeds a table of each published `@krate/*` package's version at release. Add a generator to `scripts/` and check it with `pnpm check:version`.
   - The migrator writes `^<that version>`.
   - For a dev build of the compiler (`vcs.modified`, or version `dev`), find the running `@krate/core`'s real path. If sibling `packages/<name>` exist (a Krate checkout), write `link:` specs to them, which is what the user did by hand. Otherwise write `latest` with an info line.
4. **Remove `next`, `next-auth` and `@next/env`** when a static scan of the project (and of workspace packages that depend on it via `workspace:*`) finds no remaining import of them.
   - In this app `@next/env` stays while `instrumentation-node.ts` imports it (see B9). The report then names the importer instead of a generic item.
   - `react` and `react-dom` stay: the Preact islands alias them, and other deps may peer-depend on them.
5. **Scripts.** `dev` becomes `krate dev`, and `build` becomes the command chosen in B4. `start` becomes `node dist/.krate/server/<rel>/server.mjs` when the deploy target is node, otherwise `krate start`.
   - `predev`/`prebuild` keep their app steps.
   - `typecheck: "next typegen && tsc --noEmit"` loses `next typegen &&`.
6. **Install remains manual. Justification:**
   - The migrator must not run network installs, choose a package manager's policy, or touch the lockfile. Regenerating a pnpm lockfile has dropped platform packages before.
   - The one remaining item is the exact command for the detected manager: `pnpm install` at the workspace root (from `pnpm-workspace.yaml`), `npm install`, and so on.

**Files:** `migrate/nextjs.go` (`packagePlan` at 944-1020, default flag), `cmd/krate/main.go:530` (flag), `migrate/reactthree.go`, `migrate/reactkonva.go`, `migrate/nextauth.go:159`, a new `migrate/krateversions_gen.go`, and the generator plus check in `scripts/`. Docs: `guides/migrating-from-nextjs.md`, and the CLI reference for the flag.

**Tests:** `migrate/imports_gaps_test.go`.

- Default runs update `package.json`, and `--no-update-package` does not.
- The `@krate/three` spec comes from the table, not from the runtime spec.
- A dev build with a fake checkout writes `link:`.
- `next` is removed only when unimported.
- The scripts mapping per deploy target.
- `cmd/krate/migratecmd_verify_test.go` for the flag.

**Output change:** the default migrate now edits `package.json`. That is a migrator behaviour change, noted in the CHANGELOG.

### B8. Shared CSS outside the project

**Today:** `migrate/outsiderefs.go` detects `packages/static-site/src/styles.css:3` importing `../../web/src/app/tokens.css` and only warns. Backup and rollback cover project files only.

**Options**

1. **Leave a stub `src/app/tokens.css` (pick).** The stub contains a comment and `@import '../migrated/app/tokens.css';`.
   - The sibling's import keeps working: Krate's CSS pipeline, which static-site also uses, inlines nested `@import`s.
   - There's one source of truth, it's project-internal and rollback-safe, and nothing outside the project is written.
   - The `src/app` "moved aside" logic already has a keep-in-place branch (`nextjs_gaps_a.go:329`). The stub is written after the move, and a dir holding only stubs is not treated as a route tree.
   - The report gets an info line: `kept src/app/tokens.css as a stub that imports the moved file, because packages/static-site/src/styles.css:3 references it`.
2. **Rewrite the sibling file.**
   - Cleanest end state, but it writes outside the directory the user migrated.
   - It needs workspace-root-relative entries in the manifest, preflight and `Rollback` (`nextjs.go:1135-1189`, `targetPath` at 1379).
   - Offer it as `--rewrite-workspace-refs` later; it isn't needed now.
3. **Keep a full copy at the old path.** Rejected: two copies drift.

The stub only works for CSS `@import`. For a JS or TS reference from outside, the stub is `export * from '../migrated/app/x'` plus `export { default } from ...` when the target has a default export. For any other file type (an image referenced by path), the original is kept as a copy and the manual item stays, with a hint that names the file.

**Files:** `migrate/outsiderefs.go` (emit stubs), `migrate/nextjs_gaps_a.go` (stub-aware move-aside), `migrate/nextjs.go` (manifest records stubs as written files).

**Tests:** a new `migrate/outsiderefs_test.go`. It uses a pnpm workspace fixture with a sibling CSS `@import`, a sibling TS import, and a sibling `url()` image. It asserts:

- the stubs' text;
- the info lines;
- that the image keeps its manual item;
- that rollback removes the stubs.

It also builds the sibling CSS through Krate's CSS pipeline and asserts that the tokens are inlined.

**Output change:** the migrator only.

### B9. Instrumentation

**Today:**

- The migrator lists `instrumentation*.ts` as manual (`migrate/nextjs_gaps_a.go:116-135`).
- Krate has no boot hook. Plugin serve hooks are per request and run in the Go host or a plugin subprocess, never in the app's Node process.
- The Node API sidecar (`build/serve.go:41`, `apiServerScriptContent`) loads middleware eagerly and routes lazily. With `KRATE_API_WORKERS > 1` it forks cluster workers.
- The node deploy bundle imports everything at boot and then calls `startNodeServer` (`serverdeploy.go:766`).

**The app's `register()` does three things:**

- loads the root `.env` (already covered by `envDir`);
- in production, revokes old preview sessions;
- in production, warms the preview pipeline.

The last two are fire-and-forget by design.

**Options**

1. **First-class `src/instrumentation.ts` (pick).**
   - Krate finds `instrumentation.{ts,js,mjs}` in `src/` or the project root, the same places as `middleware` (`findMiddlewareSource`, `serverdeploy.go:316`).
   - It bundles it for Node like the middleware.
   - **USER DECISION: `register()` runs exactly ONCE PER SERVER, not once per worker or process.**
    - `krate serve`/`start`/`dev`: it runs in the Node API sidecar's primary process, awaited before that process forks cluster workers (`KRATE_API_WORKERS > 1`) or listens.
    - Cluster workers and the SSR sidecar never run it.
    - The node deploy bundle runs it once before `startNodeServer`.
    - A sidecar restart by the supervisor is a new server start, so it runs again then. Dev hot reloads of API modules do not rerun it.
    - Tests must assert it ran exactly once: for example `register()` appends a line to a file, and with 2+ cluster workers plus SSR pages the file has exactly 1 line after startup and after several requests.
  - **Failure semantics, matching Next:**
     - A rejection is logged with the file and stack, and the process exits non-zero. The supervisor's backoff restart applies.
     - The test fixture checks Next 16's behaviour on a throwing `register()` and mirrors it. If Next logs and continues, Krate does the same.
     - Fire-and-forget code inside `register()` keeps its own semantics.
   - **Latency:** none per request. Startup waits for `register()`, as in Next.
   - **The embedded QuickJS runtime does not run it.** Next's edge `register()` branch has no Krate counterpart.
   - If the file exists, `sidecarNeedsNode` (`serve.go:3221`) counts it, so the sidecar starts even when every route is embedded. Otherwise the hook would silently never run. The build prints one info line saying so.
   - **The migrator:**
     - keeps both files in place, without moving them aside;
     - folds `process.env.NEXT_RUNTIME === 'nodejs'` to `true` and `=== 'edge'` to `false` in the instrumentation file only;
     - warns only if `onRequestError` is exported (no Krate equivalent yet, manual with that exact name);
     - leaves `loadEnvConfig` in `instrumentation-node.ts`. It works and is harmless, since it does not override variables that are already set. This keeps `@next/env` as a dependency, as B7 notes.
   - Optional: the migrator rewrites the call to a no-op with a comment that `envDir` covers it, then drops `@next/env`. I'd do that only when the path matches the translated `envDir`.
2. **A plugin "serve start" hook.**
   - Rejected. Plugins run in the Go host or a plugin subprocess, not in the app's Node process, so app modules (Redis clients, the preview cipher) would load in the wrong process.
   - Adding a Node-side plugin hook is equivalent to option 1 with more indirection.
3. **Generated middleware with a run-once guard.**
   - Rejected. It runs only when the first request reaches middleware, so warming starts late, which is exactly the slow first preview it should prevent.
   - It runs only in the middleware's process, not in each API cluster worker.
   - A rejected promise would surface as the first request's 503 (the middleware fails closed).
   - It also adds a check to every request.

**Files:**

- `build/serve.go` (sidecar script: import and await before `listen`; `sidecarNeedsNode`).
- `build/ssr.go` (SSR sidecar).
- `build/serverdeploy.go` (bundle and await before `startNodeServer`).
- A new `build/instrumentation.go` (discovery, bundling).
- `migrate/nextjs_gaps_a.go` (keep, fold `NEXT_RUNTIME`).
- Docs: a new "Instrumentation" section in the server guide, plus `guides/migrating-from-nextjs.md:302`.

**Tests**

- `build/serve_api_test.go`:
  - `register()` writes a marker to `globalThis`, and an API route returns it on the first request. That proves it ran before `listen`.
  - With two cluster workers and an SSR page, `register()` ran exactly once (the marker file has one line), and both workers still serve.
  - A `register()` that never resolves blocks readiness (`/readyz` is not ready).
  - A throwing `register()` behaves as the Next fixture shows.
  - An app with only embedded routes plus instrumentation starts the sidecar.
- `build/serverless_targets_test.go`: the node bundle awaits it before listening.
- `migrate/nextjs_gaps_a_test.go:110-155`: the files stay, `NEXT_RUNTIME` is folded, and there is no manual item. `onRequestError` gives a manual item.

**Output change:** none for apps without an instrumentation file.

### B10. next-auth type augmentation

**Today:**

- The `@krate/auth` `Session` (`packages/auth/src/types.ts:100-113`) is an interface with an inline `user` object type, including `role?: string | null` and an index signature.
- There is no `DefaultSession`.
- The migrator turns any `.d.ts` mentioning next-auth into a manual item (`migrate/nextauth.go:129-135`).
- A literal rewrite to `declare module '@krate/auth'` fails for two reasons:
  - `& Session['user']` is circular;
  - redeclaring `role: Role` clashes with `role?: string | null`.

**Plan**

- In `@krate/auth`, add `export interface SessionUserExtensions {}`.
  - `Session.user` becomes `Omit<BaseSessionUser, keyof SessionUserExtensions> & SessionUserExtensions`, where `BaseSessionUser` is today's shape.
  - That keeps existing apps' types unchanged, because the extensions are empty by default.
  - It also lets an augmentation override `role` with a narrower type.
- Export `type DefaultSession = { user?: BaseSessionUser; expires: string }`, matching next-auth's name.
- Do the same for `User` and the JWT token if `@krate/auth` has a JWT type. Otherwise keep that part manual by name.
- The migrator parses `declare module 'next-auth' { interface Session { user: { ...fields } & DefaultSession['user'] } }` and writes:

  ```ts
  import type { Role, Tier } from '@dd/shared';
  declare module '@krate/auth' {
    interface SessionUserExtensions { id: string; role: Role; tier: Tier; discordId: string; credits: number; banned: boolean; comingSoonAccess?: boolean }
  }
  export {};
  ```

  - It keeps the original type imports and drops `DefaultSession` imports.
  - It writes to the same file path, `src/types/next-auth.d.ts`, renamed to `src/types/krate-auth.d.ts`.
- Unrecognised shapes (`interface JWT` in `next-auth/jwt` without a Krate counterpart, or conditional types) get a manual item that quotes the declaration.
- The `authTypeExports` map (`nextauth.go:41-46`) gains `DefaultSession`.

**Files:** `packages/auth/src/types.ts` (plus the index exports), `migrate/nextauth.go`, `packages/web/src/content/docs/features/authentication.md` (a "Typing extra session fields" section).

**Tests**

- `packages/auth/test/*.test.ts` (Vitest) with a `tsd`-style type test compiled by `tsc --noEmit` in `pnpm typecheck`:
  - an augmentation narrows `role`;
  - with no augmentation, the types are unchanged.
- `migrate/nextauth_test.go:111,123`: the app's declaration is rewritten to the golden text.
- An unsupported shape keeps its manual item.
- An integration step: run `tsc --noEmit` on the migrated fixture (the app's `typecheck` script).

**Output change:** the types only. No runtime change. `pnpm typecheck` covers the package.

---

## Next parity fixtures

Use the same approach as the Tailwind golden: real Next output, recorded once and committed.

- `packages/compiler/internal/build/testdata/next-routing/`:
  - `app/` is a minimal Next 16 app whose `next.config.ts` holds:
    - the DigitalDesigns redirects, verbatim;
    - the security and cache helper headers, with a fixed env;
    - extra `has`/`missing` cases covering header, cookie, host, a named capture, and `missing`.
  - `src/app/api/node-buffer/route.ts` uses a global `Buffer` and imports a helper that also uses it.
  - `src/instrumentation.ts` sets a marker, and a throwing variant sits behind an env flag.
  - `cases.mjs` lists the requests: method, path, query, headers and cookies.
  - `gen.mjs` runs `next build && next start` and records each response's status, `Location` and headers into `golden.json`.
- A Go test builds the same app migrated by `krate migrate` and serves it in-process. It replays `cases.mjs` and compares against `golden.json`, with a documented allowlist of headers that legitimately differ.
- The golden is regenerated by hand (`node gen.mjs`) when the Next version moves, as with `tailwind-builtin`.

## End-to-end acceptance on the app copy

Run against a fresh copy, using the `round.sh` flow:

1. `krate migrate nextjs . --apply` prints exactly one manual item: install dependencies.
2. `krate build` has 0 KR7116, 0 KR7160, and 0 KR8310 for the four public vars.
3. All 46 static API routes return the same status as Next. `/discord-embed.json` returns 200.
4. A header diff against Next on :8448 for `/`, `/store`, `/templates`, `/_k/chunks/<file>`, `/brand/<svg>`, `/preview/<wasm>` and `/api/csp-report` (POST): identical except HSTS.
5. Redirects:
   - `/store?vehicle=x` returns 308 to `/templates?vehicle=x`;
   - `/store/some-slug` returns 308 to `/templates/some-slug`;
   - `/store/compare` returns 200;
   - `/store/plans?x=1` returns 308 to `/store?x=1`.
6. The client bundle contains the Umami, Turnstile and bgmodel values.
7. The node deploy bundle runs from `dist/.krate/server/packages/web` and finds `scramble-worker.mjs`.
8. Logs show `register()` ran in each API process (preview warm logs).
9. `pnpm typecheck` of the app passes with the rewritten auth types.
10. The page-load and interaction sweep is unchanged from `17305682`.

## Implementation order

Each step is independently testable; the CHANGELOG and docs are updated in each step.

1. A2: reproduce and instrument. It's quick, and it may turn up a resolver bug that affects other steps.
2. A3 and B1 (`envPrefix`). They share the env plumbing.
3. A4: the shared matcher, query passthrough, `statusCode` and handler order. Then B5 (`has`/`missing`).
4. B2 and B3: copy-through, the header array shape, and the image protocol and port.
5. A1: auto-routing by globals, `export const runtime`, and KR7160 severity.
6. B9: instrumentation.
7. B4: tracing root and node layout.
8. B6, B7, B8 and B10: migrator and package work.
9. The Next parity fixture, the end-to-end acceptance on the app copy, and a full CI run.

## Output changes for apps that already migrate cleanly

These are collected for the CHANGELOG "Changed" section:

- **Serve:**
  - redirects keep the request query string;
  - redirects run before middleware and also apply to `/api/*`;
  - user `headers` rules win over Krate's per-handler `Cache-Control` on public files and pages;
  - sources with `:` or `(` that silently never matched now match.
- **API runtime:** routes that used unguarded not-supported globals, which 500 today, now run on Node. That may start the Node sidecar where it did not run before.
- **Migrator:**
  - `NEXT_PUBLIC_` names are kept (`envPrefix`) instead of renamed;
  - the default run edits `package.json`;
  - `runtime` is kept in API routes;
  - instrumentation files are kept;
  - `krate.config.ts` can contain copied code when next.config was computed. Static configs stay byte-identical.
- **Unchanged:** the default `envPrefix`, `permanent` meaning 301/302, node deploy layout without `outputFileTracingRoot`, `Session` types without augmentation, and image patterns without `protocol`/`port`.
