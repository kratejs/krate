// Crash reports and crash events. Needs the "crash" permission.
//
// The shell writes a JSON report under <logDir>/crashes when a Go panic or a
// fatal runtime error ends the shell or the Go host, and when a webview
// process crashes. Reports from a run that ended in a crash are listed on the
// next launch, so the app can upload them. Nothing leaves the machine by
// itself.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type CrashKind = "panic" | "fatal" | "webview";

export interface CrashSummary {
  id: string;
  kind: CrashKind;
  // RFC 3339 time.
  time: string;
  reason: string;
}

export interface CrashReport extends CrashSummary {
  appId: string;
  appVersion: string;
  os: string;
  arch: string;
  osVersion: string;
  goVersion: string;
  shellVersion: string;
  // "shell", "host" or "webview".
  process: string;
  window?: string;
  view?: string;
  // The stack of a Go crash, as the runtime printed it.
  stack?: string;
}

export interface CrashEvent {
  kind: CrashKind;
  reason: string;
  window: string;
  view: string;
  // The report written for it, "" if writing failed.
  reportId: string;
  // True when the shell reloaded the view.
  reloaded: boolean;
}

export function list(): Promise<CrashSummary[]> {
  return call("krate/crash.list", []);
}

export function read(id: string): Promise<CrashReport> {
  return call("krate/crash.read", [id]);
}

// remove deletes a report (deleting a missing one succeeds).
export function remove(id: string): Promise<void> {
  return call("krate/crash.delete", [id]);
}

// setAutoReload makes the shell reload a webview whose process crashed. It
// gives up on a window that crashes again within seconds, so a broken page
// cannot loop.
export function setAutoReload(on: boolean): Promise<void> {
  return call("krate/crash.setAutoReload", [on]);
}

export function onCrash(fn: (e: CrashEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<CrashEvent>("krate/crash.events", fn, opts);
}
