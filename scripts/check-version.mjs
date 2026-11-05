#!/usr/bin/env node
/**
 * Fails when publishable packages drift out of lockstep. Run before tagging a
 * release and in CI so a missed `set-version.mjs` is caught early.
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const manifests = [
  "packages/core/package.json",
  "packages/runtime/package.json",
  "packages/components/package.json",
  "packages/plugin/package.json",
  "packages/ts-plugin/package.json",
  "packages/base-docs-theme/package.json",
  "packages/simple-docs-theme/package.json",
  "packages/create-krate-app/package.json",
  "packages/create-krate-docs/package.json",
];

const failures = [];
const packages = manifests.map((file) => {
  const pkg = JSON.parse(readFileSync(join(root, file), "utf8"));
  return { file, pkg };
});
const version = packages[0].pkg.version;
if (!version) failures.push(`${packages[0].file}: missing version`);

for (const { file, pkg } of packages) {
  if (pkg.version !== version) {
    failures.push(`${pkg.name}: ${pkg.version} != ${version}`);
  }
  if (pkg.name === "@krate/core") {
    for (const [dep, range] of Object.entries(pkg.optionalDependencies ?? {})) {
      if (dep.startsWith("@krate/core-") && range !== version) {
        failures.push(`${dep}: ${range} != ${version}`);
      }
    }
  }
  if (pkg.name === "@krate/components") {
    const range = pkg.peerDependencies?.["@krate/runtime"];
    if (range !== `^${version}`) {
      failures.push(`@krate/components peer @krate/runtime: ${range ?? "(missing)"} != ^${version}`);
    }
  }
}

// The docs site pins are updated manually and deliberately track the published
// alpha/stable train rather than the workspace version. Still require every
// @krate dependency to be an exact, consistent version.
const web = JSON.parse(readFileSync(join(root, "packages/web/package.json"), "utf8"));
const webKrate = Object.entries(web.devDependencies ?? {}).filter(([name]) => name.startsWith("@krate/") && name !== "@krate/base-docs-theme");
if (webKrate.length !== 3) failures.push("packages/web: expected components/core/runtime pins");
const webVersions = new Set(webKrate.map(([, range]) => range));
if (webVersions.size > 1) failures.push("packages/web: @krate dependency versions disagree");
for (const [name, range] of webKrate) {
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(range)) {
    failures.push(`packages/web ${name}: must be an exact version, got ${range}`);
  }
}

for (const file of [
  "packages/create-krate-app/templates/app/package.json",
  "packages/create-krate-docs/templates/docs/package.json",
]) {
  const template = JSON.parse(readFileSync(join(root, file), "utf8"));
  for (const [name, range] of Object.entries(template.dependencies ?? {})) {
    if (name.startsWith("@krate/") && range !== "__KRATE_VERSION__") {
      failures.push(`${file} ${name}: expected __KRATE_VERSION__, got ${range}`);
    }
  }
}

if (failures.length) {
  console.error("check-version: publishable packages are out of sync");
  for (const failure of failures) console.error(`  ${failure}`);
  console.error("Run `node scripts/set-version.mjs <version>` before releasing.");
  process.exit(1);
}
console.log(`check-version: ${packages.length} publishable packages at ${version}`);
