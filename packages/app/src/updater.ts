// In-app updates: check a feed, download with progress, apply and relaunch.
// Needs the "updater" permission and native.updater.{url,publicKey} in the
// app configuration. Host ids krate/updater.*.
import { call, stream } from "./index.js";

export interface UpdateInfo {
  version: string;
  notes: string;
  // RFC 3339 publication date from the feed, "" when absent.
  pubDate: string;
  // Size of the update file in bytes, 0 when the feed does not say.
  size: number;
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

// check, download, apply: resolves the applied version, or null when up to
// date. The caller decides when to relaunch.
export async function update(onProgress?: (p: DownloadProgress) => void): Promise<UpdateInfo | null> {
  const info = await check();
  if (!info) return null;
  await download(onProgress);
  await apply();
  return info;
}
