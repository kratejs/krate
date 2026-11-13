// @build functions run once while the app is built, in a bare JavaScript VM
// (no fetch, timers, file system or SDK). The UI gets the baked value, with
// no host call at run time. Arguments must be literals. Keep this module free
// of imports that need a browser or Node API.
@build
export function buildInfo(label: string): string {
  return `${label} ${new Date().toISOString().slice(0, 10)}`;
}
