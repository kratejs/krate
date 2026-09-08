/**
 * The `krate` object passed as the third argument to every JS plugin hook.
 *
 * Currently Krate passes `{ root, outDir, version }` (assembled in
 * internal/plugin/jsplugin.go). This type reflects that today; richer fields
 * will be added alongside the engine as they land.
 */
export interface Krate {
  /** Absolute path to the project root. */
  root: string;
  /** Absolute path to the output directory. */
  outDir: string;
  /** Krate compiler version. */
  version: string;
}
