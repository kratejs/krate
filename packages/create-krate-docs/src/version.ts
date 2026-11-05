// The npm range generated projects use for every @krate/* package.
//
// The scaffolder and the packages it installs are released together, and its
// templates are tested against that version. Pointing a project at "latest" or
// a dist-tag instead lets an old scaffolder install a newer, incompatible
// compiler, so the range is the scaffolder's own version: caret for a stable
// release (patch and minor fixes within it), exact for a prerelease.
const SEMVER = /^(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

export function krateRange(version: string): string {
  const match = SEMVER.exec(version);
  // Not a version we can pin to (for example a local build): follow latest.
  if (!match) return 'latest';
  const release = `${match[1]}.${match[2]}.${match[3]}`;
  return match[4] ? `${release}${match[4]}` : `^${release}`;
}
