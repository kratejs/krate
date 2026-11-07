import { basename } from 'node:path';

/** A package.json name made from a directory name. */
export function toValidPackageName(name: string, fallback: string): string {
  const cleaned = name
    .trim()
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-_]/g, '')
    .replace(/^[^a-z]+/, '');
  return cleaned || fallback;
}

/** A title made from a directory name. */
export function toDisplayName(name: string, fallback: string): string {
  return (
    name
      .split(/[-_ ]+/)
      .filter(Boolean)
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ') || fallback
  );
}

/**
 * The names a project gets. They come from the directory that is created, not
 * from the text typed: `.` and `..` name no project, and used to give a
 * project titled "." in a package called after the fallback.
 */
export function projectNames(targetDir: string, fallbacks: { pkg: string; display: string }): { pkg: string; display: string } {
  const dirName = basename(targetDir);
  return {
    pkg: toValidPackageName(dirName, fallbacks.pkg),
    display: toDisplayName(dirName, fallbacks.display),
  };
}
