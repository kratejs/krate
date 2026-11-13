// App directories, OS information, opening URLs and secrets.
// Host ids krate/os.*; permissions are in the table at the end of this file.
import { call } from "./index.js";

export interface OsInfo {
  // "linux", "darwin" or "windows".
  os: string;
  arch: string;
  // Human-readable OS version when available, else "".
  osVersion: string;
  hostname: string;
  // BCP 47 tag such as "en-US".
  locale: string;
  appId: string;
  appVersion: string;
}

// The directories are created on first use. Permission: os:info.
export function appDataDir(): Promise<string> {
  return call("krate/os.appDataDir", [], { dedupe: true });
}
export function cacheDir(): Promise<string> {
  return call("krate/os.cacheDir", [], { dedupe: true });
}
export function configDir(): Promise<string> {
  return call("krate/os.configDir", [], { dedupe: true });
}
export function logDir(): Promise<string> {
  return call("krate/os.logDir", [], { dedupe: true });
}
export function tempDir(): Promise<string> {
  return call("krate/os.tempDir", [], { dedupe: true });
}

// Permission: os:info.
export function info(): Promise<OsInfo> {
  return call("krate/os.info", [], { dedupe: true });
}

// Opens an http, https or mailto URL in the OS default handler.
// Permission: shell:openExternal.
export function openExternal(url: string): Promise<void> {
  return call("krate/os.openExternal", [url]);
}

// Secrets live in the OS credential store, scoped to the app. They reject with
// code "unsupported" where the platform has no store (for example Linux with
// no Secret Service provider).

// Permission: os:secrets.
export function secretSet(key: string, value: string): Promise<void> {
  return call("krate/os.secretSet", [key, value]);
}

// Resolves null when the key has no secret. Permission: os:secrets.read.
export function secretGet(key: string): Promise<string | null> {
  return call("krate/os.secretGet", [key]);
}

// Deleting a missing key succeeds. Permission: os:secrets.
export function secretDelete(key: string): Promise<void> {
  return call("krate/os.secretDelete", [key]);
}
