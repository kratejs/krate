/**
 * Shared description of the per-platform binary packages (@krate/core-<platform>).
 * Used by build-platform-packages.mjs to generate them and by check-release.mjs
 * to prove the generated manifest stays publishable.
 */

export const repo = {
  url: 'https://github.com/kratejs/krate.git',
  homepage: 'https://github.com/kratejs/krate',
  bugs: 'https://github.com/kratejs/krate/issues',
};

export const platforms = [
  { pkg: 'core-win32-x64', os: 'win32', cpu: 'x64', goos: 'windows', goarch: 'amd64', ext: '.exe' },
  { pkg: 'core-win32-arm64', os: 'win32', cpu: 'arm64', goos: 'windows', goarch: 'arm64', ext: '.exe' },
  { pkg: 'core-linux-x64', os: 'linux', cpu: 'x64', goos: 'linux', goarch: 'amd64', ext: '' },
  { pkg: 'core-linux-x64-musl', os: 'linux', cpu: 'x64', goos: 'linux', goarch: 'amd64', ext: '', musl: true },
  { pkg: 'core-linux-arm64', os: 'linux', cpu: 'arm64', goos: 'linux', goarch: 'arm64', ext: '' },
  { pkg: 'core-darwin-x64', os: 'darwin', cpu: 'x64', goos: 'darwin', goarch: 'amd64', ext: '' },
  { pkg: 'core-darwin-arm64', os: 'darwin', cpu: 'arm64', goos: 'darwin', goarch: 'arm64', ext: '' },
];

/**
 * The package.json of one platform package.
 *
 * It must not declare `bin`. Every platform package is an optional dependency
 * of @krate/core, and on any machine most of them do not match os/cpu/libc.
 * npm 10 rolls those back as failed optional dependencies, and the rollback
 * removes the bin link they would have created. That link is
 * node_modules/.bin/krate, the same path @krate/core links its own bin to, so
 * the install ends with no krate command at all. @krate/core finds the binary
 * by path (<pkg>/bin/krate), so nothing needs the link.
 *
 * Without `bin`, the binaries need publishConfig.executableFiles: pnpm packs every
 * other file as 0644 whatever its mode on disk, and a binary with no exec bit
 * fails with EACCES on every install outside Windows.
 *
 * The package holds two binaries side by side: krate, and krate-shell, the
 * prebuilt Krate Native shell. The compiler finds the shell by looking next to
 * its own executable, so both must stay in bin/.
 */
export function platformManifest(target, version) {
  return {
    name: `@krate/${target.pkg}`,
    version,
    description: `The Krate compiler binary and the Krate Native shell (krate-shell) for ${target.os} ${target.cpu}${target.musl ? ' (musl)' : ''}.`,
    license: 'Apache-2.0',
    os: [target.os],
    cpu: [target.cpu],
    homepage: repo.homepage,
    repository: { type: 'git', url: repo.url },
    bugs: { url: repo.bugs },
    keywords: ['krate', 'ssg', 'compiler', target.os, target.cpu],
    files: ['bin'],
    publishConfig: { executableFiles: [`./bin/krate${target.ext}`, `./bin/krate-shell${target.ext}`] },
  };
}
