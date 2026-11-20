# Releasing Krate

This is the release contract for the compiler and the publishable JS packages.
It covers versioning, dist-tags, deprecation, rollback, and the checks that must
pass before a tag is pushed.

## Versioning

Krate uses semantic versioning for the compiler and all publishable packages:

- **major**: a change that breaks a documented API, config key, diagnostic
  contract, or output shape.
- **minor**: new features and opt-in behavior. Existing projects must keep
  building.
- **patch**: fixes, documentation, and internal changes with no intended
  behavior change.

Pre-releases use `x.y.z-alpha.N`, `x.y.z-beta.N`, or `x.y.z-rc.N`. The
dist-tag is the first prerelease identifier (`alpha`, `beta`, or `rc`). The compatibility matrix in
`packages/web/src/data/compat-matrix.json` is the source of truth for what a
release claims; do not publish a release that leaves a roadmap row `planned`.

## Dist-tags

- `alpha`, `beta`, and `rc` are prerelease dist-tags; the release workflow
  derives the tag from the version.
- `latest` is the stable channel.
- `latest` moves only after the full release verification below passes.
- Publish platform packages first (the core wrapper's optional dependencies must
  exist), then `@krate/runtime`, `@krate/components`, `@krate/plugin`,
  `@krate/base-docs-theme`, `create-krate-app`, `create-krate-docs`, and
  `@krate/core`.

The workflow derives the tag with `scripts/release-tag.mjs` before it builds
anything. A pre-release identifier must start with a letter (`1.0.0-1` is
rejected: npm cannot use `1` as a tag). A stable version lower than the version
npm serves as `latest` (a patch for an older line) is published as
`latest-<major>.<minor>` so `latest` never moves backwards.

If a release fails part way, fix the cause and re-run the failed jobs for the same tag.
`scripts/publish-packages.mjs` skips every package version already on npm and
publishes the rest. A skipped package is whatever the earlier run uploaded; if
that upload was wrong, publish a new version instead.

## Trusted publishing

The release workflow publishes with npm trusted publishing: the job's GitHub
OIDC token (`permissions: id-token: write`) is exchanged for a short-lived
npm token, so no long-lived token has to be rotated, and every version gets a
provenance statement. On npmjs.com, each package trusts the GitHub Actions
workflow `release.yml` of `kratejs/krate` (`trustedPublisher` in
`scripts/release-packages.mjs`). npm stops accepting direct publishes with a
granular access token in January 2027; until a package trusts the workflow,
`pnpm publish` falls back to the `NPM_TOKEN` secret.

Trusted publishing cannot create a package. When a release adds one, publish
it once by hand before tagging, from a machine logged in to npm with an
account that can publish `@krate` packages and has 2FA on (npm 11.15.0 or
later):

```sh
npm login
node scripts/npm-trust.mjs --dry-run      # what it would do
node scripts/npm-trust.mjs <new-package>  # or no names: every package
```

npm asks for two-factor authentication on the first call: the script gives
that call the terminal, so open the URL it prints (or enter a code), and npm
lets the following calls through for a few minutes. `--otp <code>` passes an
authenticator code instead. `npm trust` does not accept an access token that
bypasses 2FA; if your npm login is one, `npm logout` and `npm login` again.

For a package that is not on npm, the script publishes a deprecated placeholder
(`0.0.0-bootstrap.0`, no code), then runs `npm trust github` for every
package that does not trust the workflow yet. Packages already set up are
skipped, so it can be re-run. npm makes a package's first version `latest`, so
the placeholder stays `latest` until the first stable release moves it.

The workflow's first step after the dist-tag,
`node scripts/publish-packages.mjs --check-registered`, fails before any build
when a package is not on npm yet and prints the command above, and
`publish-packages.mjs` itself refuses to publish anything while a package in
its list is missing, so a release never stops half published.
`check-release` fails when the workflow loses `id-token: write` or the check,
or a package's `repository.url` stops naming `kratejs/krate` (provenance is
rejected otherwise).

Scaffolded projects (`create-krate-app`, `create-krate-docs`) pin every `@krate/*`
dependency to the scaffolder's own version: `^x.y.z` for a stable release, the
exact version for a prerelease. The templates carry a `__KRATE_VERSION__`
placeholder that `scripts/check-version.mjs` requires.

`node scripts/set-version.mjs <version>` updates every publishable manifest,
the platform optional dependencies, and the runtime peer range. Run
`node scripts/check-version.mjs` before tagging; both CI and the release
workflow run it.

## Native module tags

`krate native build --release` of an app without Go code generates a Go module that
requires `github.com/kratejs/krate/packages/native` at the compiler's own version
(`v<version>`). Go resolves a module in a subdirectory by the tag
`packages/native/v<version>`, so every release tags that path next to `v<version>`.
`internal/build/nativemod/go.mod.txt` and `go.sum.txt` are copies of
`packages/native/go.mod` and `go.sum` (a test fails when they drift; `cp` them).

## Deprecation policy

- Announce a deprecation in the docs and release notes at least one minor
  release before removal.
- Keep the old behavior working for at least one minor release, with a warning
  or diagnostic when available.
- Never remove a config key, runtime export, or supported pattern in a patch.
- A diagnostic code is stable. If behavior changes, keep the code and update its
  explanation rather than reusing the code for a different problem.

## Migration backup and rollback

The Next.js migrator (`krate migrate nextjs`) is an early migration command and
implements this policy. It remains conservative: it reports manual work rather
than rewriting patterns it cannot prove safe. The policy is:

- Take a user-visible backup (or require a clean git worktree) before writing.
- Write changes atomically and support `--dry-run`.
- Leave every original file recoverable until the migration completes.
- A failed migration restores the backup; a successful migration can be undone
  with `krate migrate rollback <backup-dir>` or git.

For a release, rollback is:

1. Restore the previous published dist-tag (npm `dist-tag add`) or pin the
   previous package version.
2. Reinstall the previous compiler binary or use the previous platform package.
3. Rebuild from the same source. Builds are deterministic, so a clean rebuild
   with the prior compiler reproduces the prior output.
4. If source changes were made, restore the project from git or the user's
   backup. Krate does not write backups during a normal build.

## Release verification

The release workflow does not re-run the test suites. Its `ci-gate` job
requires a passed full CI run (a push to `dev` or `main`, the nightly run or a
manual dispatch) of the exact tagged commit. It waits while that run is in
progress, dispatches CI on the tag when the commit has none, and fails the
release when the run failed: fix it on `dev`, or re-run the failed jobs, then
re-run the release. Tag a commit whose `dev` or `main` CI is green and the gate
passes at once. The release itself only runs what depends on the version: the
seven platform packages (in parallel), `check:version`, `check:release`, the
docs site build and the install smoke test of the tarballs it publishes.

Run from a clean checkout with Node, pnpm, Go, and Playwright browsers installed:

```sh
pnpm install --no-frozen-lockfile
pnpm sync:docs
pnpm check:docs
pnpm check:matrix
pnpm check:roadmap
pnpm check:version
pnpm typecheck
pnpm test:js
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -race ./internal/build ./internal/bundler
go test ./internal/conformance
KRATE_BROWSER=chromium go test ./internal/build -run TestBrowserConformance -v
KRATE_BROWSER=chromium KRATE_REACT_EXIT=1 go test ./internal/build -run TestReactBrowser -v
pnpm --filter @krate/web build
```

Repeat browser conformance for Firefox and WebKit, and run the Preact matrix
(10.20.0, 10.24.0, 10.29.8) plus the Node 20/22/24/26 and Bun/Deno jobs. Check the
generated `dist` for a scaffolded app and the docs site before promotion.

## Release notes

Each release states:

- the version and dist-tag,
- compatibility matrix changes (newly stable rows, new limits, diagnostic
  codes),
- deprecations and their removal target,
- known divergences that remain (see
  `packages/compiler/internal/build/testdata/reactbrowser/divergences.json`),
- rollback instructions (the dist-tag and version to restore).
