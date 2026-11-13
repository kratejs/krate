// In-app updates: check a feed, download with progress, apply and relaunch.
// Needs the "updater" permission and native.updater.{url,publicKey} in the
// app configuration. Host ids krate/updater.*.
import { call, stream } from "./index.js";

export interface UpdateInfo {
  version: string;
  notes: string;
  // RFC 3339 publication date from the feed, "" when absent.
  pubDate: string;
  // Size of the full update file in bytes, 0 when the feed does not say.
  size: number;
  // The feed marks the update critical (it bypasses a staged rollout).
  critical: boolean;
  // The running version is older than the feed's min_version: the app should
  // not let the user skip this update.
  required: boolean;
  // A delta update (a patch from the running version) is offered; download()
  // falls back to the full file when it cannot be applied.
  delta: boolean;
  // Bytes download() fetches: the patch size with a delta, size otherwise.
  downloadSize: number;
}

export interface RollbackEvent {
  // The version that failed to start properly and was rolled back.
  failed: string;
  // The version that runs again.
  restored: string;
  reason: string;
  // RFC 3339.
  at: string;
}

export interface UpdaterStatus {
  version: string;
  channel: string;
  // This version was applied by an update and has not confirmed a healthy
  // start yet.
  unconfirmed: boolean;
  // Set until ackRollback() when the previous launch rolled an update back.
  rollback: RollbackEvent | null;
}

export interface DownloadProgress {
  done: number;
  // 0 when the size is unknown.
  total: number;
}

// Asks the update feed. Resolves null when the app is up to date.
// Rejects with code "unsupported" when native.updater is not configured.
export function check(): Promise<UpdateInfo | null> {
  return call("krate/updater.check");
}

// Downloads and verifies the update found by check(). onProgress is called
// as bytes arrive. Rejects with code "bad_signature" when the file does not
// match the signature in the feed; the file is then deleted. An interrupted
// download resumes on the next call.
export async function download(onProgress?: (p: DownloadProgress) => void): Promise<void> {
  for await (const p of stream<DownloadProgress>("krate/updater.download")) {
    onProgress?.(p);
  }
}

// Replaces the installed app with the downloaded update. The running process
// keeps running the old code until relaunch(). Rejects with code
// "not_downloaded" before download() has finished.
export function apply(): Promise<void> {
  return call("krate/updater.apply");
}

// Starts the new version and quits this process.
export function relaunch(): Promise<void> {
  return call("krate/updater.relaunch");
}

// Confirms that this version started fine. The shell does this by itself a
// few seconds after the first page was served; call it yourself when
// native.updater.confirm is "app". Until it runs, a crash or exit makes the
// next launch restore the previous version.
export function markHealthy(): Promise<void> {
  return call("krate/updater.markHealthy");
}

// The running version, the update channel and, after a rollback, the event.
export function status(): Promise<UpdaterStatus> {
  return call("krate/updater.status");
}

// Clears the rollback event reported by status().
export function ackRollback(): Promise<void> {
  return call("krate/updater.ackRollback");
}

// Chooses the update channel ("stable", "beta", ...) for this install; it is
// kept across runs and fills {channel} in native.updater.url.
export function setChannel(channel: string): Promise<void> {
  return call("krate/updater.setChannel", [channel]);
}

// check, download, apply: resolves the applied version, or null when up to
// date. The caller decides when to relaunch.
export async function update(onProgress?: (p: DownloadProgress) => void): Promise<UpdateInfo | null> {
  const info = await check();
  if (!info) return null;
  await download(onProgress);
  await apply();
  return info;
}
