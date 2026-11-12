// Badge and progress on the app icon (dock, taskbar, launcher).
// Needs the "badge" permission.
import { call } from "./index.js";

export type ProgressState = "none" | "normal" | "indeterminate" | "error" | "paused";

export interface BadgeProgress {
  state: ProgressState;
  // 0..1; 0 for "none" and "indeterminate".
  value: number;
}

// set shows a count or short text on the app icon; "", 0 or null removes it.
// Linux launchers can only show numbers and reject other text with code
// "unsupported". Windows shows up to two or three glyphs ("99+" above 99).
export function set(badge: string | number | null): Promise<void> {
  const text = badge === null || badge === 0 ? "" : String(badge);
  return call("krate/badge.set", [text]);
}

export function clear(): Promise<void> {
  return set("");
}

// get resolves the text last set, "" when none.
export function get(): Promise<string> {
  return call("krate/badge.get", []);
}

// setProgress shows progress. value is 0..1 for "normal", "error" and
// "paused"; macOS draws error and paused like normal, Linux launchers show
// every determinate state as a plain bar.
export function setProgress(state: ProgressState, value = 0): Promise<void> {
  return call("krate/badge.setProgress", [state, value]);
}

export function clearProgress(): Promise<void> {
  return setProgress("none");
}

export function progress(): Promise<BadgeProgress> {
  return call("krate/badge.progress", []);
}

// setUrgent bounces the dock icon, flashes the taskbar button or marks the
// launcher entry urgent. false cancels it.
export function setUrgent(on: boolean): Promise<void> {
  return call("krate/badge.setUrgent", [on]);
}
