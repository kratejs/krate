// The system appearance and the app's title bars. Needs the "theme" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type ThemeSource = "system" | "light" | "dark";

export interface Theme {
  // The appearance the app's native chrome uses now.
  scheme: "light" | "dark";
  highContrast: boolean;
  // What the user's system is set to, regardless of the override.
  system: "light" | "dark";
  source: ThemeSource;
}

export function get(): Promise<Theme> {
  return call("krate/theme.get", []);
}

// set makes native title bars and dialogs light, dark or follow the system.
export function set(source: ThemeSource): Promise<void> {
  return call("krate/theme.set", [source]);
}

export function onChange(fn: (t: Theme) => void, opts?: SubscribeOptions): () => void {
  return subscribe<Theme>("krate/theme.events", fn, opts);
}
