// Application lifecycle: quit, relaunch, capability checks and second-instance
// events. Needs the "app" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export interface SecondInstance {
  args: string[];
  dir: string;
  // Arguments that look like deep links (scheme://...).
  urls: string[];
}

export function quit(): Promise<void> {
  return call("krate/app.quit");
}

export function relaunch(): Promise<void> {
  return call("krate/app.relaunch");
}

// supports reports whether a capability works right now: "dialog",
// "clipboard", "menu", "tray", "notify", "shortcut", "media", "window",
// "window:alwaysOnTop", "window:fullscreen", "window:frameless",
// "window:position", "window:release".
export function supports(capability: string): Promise<boolean> {
  return call("krate/app.supports", [capability], { dedupe: true });
}

export function onSecondInstance(fn: (e: SecondInstance) => void, opts?: SubscribeOptions): () => void {
  return subscribe<SecondInstance>("krate/app.events", fn, opts);
}

export function onOpenURL(fn: (url: string) => void, opts?: SubscribeOptions): () => void {
  return onSecondInstance((e) => {
    for (const u of e.urls) fn(u);
  }, opts);
}
