import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { platforms } from './platform-package.mjs';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

/**
 * The npm packages a release publishes, by directory under packages/, in the
 * order they are published: a package comes after the @krate packages it
 * depends on, and @krate/core last, once its platform packages and
 * @krate/plugin exist.
 *
 * set-version.mjs, check-version.mjs and smoke-install.mjs read this list;
 * check-release.mjs fails when the release workflow's publish step or release
 * notes disagree with it, or when a package that is not private is missing.
 */
export const releasePackages = [
  'runtime',
  'components',
  'plugin',
  'ts-plugin',
  'base-docs-theme',
  'simple-docs-theme',
  'auth',
  'three',
  'konva',
  'app',
  'create-krate-app',
  'create-krate-docs',
  'core',
];

/**
 * The package.json that `krate native init` writes. Its @krate dependencies
 * are exact versions, kept at the release version by set-version.mjs, since the
 * template is embedded in the krate binary.
 */
export const nativeTemplate = 'packages/compiler/internal/native/scaffold/templates/app/package.json.tmpl';

/** Matches each `"@krate/<name>": "<range>"` entry in the native template. */
export const nativeTemplateKrateDep = /("(@krate\/[a-z0-9-]+)"\s*:\s*")([^"]*)(")/g;

/** The native template text with every @krate dependency pinned to version. */
export function pinNativeTemplate(text, version) {
  return text.replace(nativeTemplateKrateDep, (_, open, _name, _range, close) => `${open}${version}${close}`);
}

/**
 * The workflow npm trusts to publish every package (trusted publishing). npm
 * matches both fields exactly, so they must name this repository and the
 * release workflow file.
 */
export const trustedPublisher = { repository: 'kratejs/krate', workflow: 'release.yml' };

/** Every package name a release publishes: the platform packages, then releasePackages in order. */
export function npmPackageNames(root = repoRoot) {
  return [
    ...platforms.map((p) => `@krate/${p.pkg}`),
    ...releasePackages.map((dir) => JSON.parse(readFileSync(join(root, 'packages', dir, 'package.json'), 'utf8')).name),
  ];
}
