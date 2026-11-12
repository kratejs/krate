// Displays and window placement. Needs the "screen" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export interface Display {
  // Stable while the display configuration does not change.
  id: string;
  name: string;
  // Logical pixels, in the coordinates of window.state and window.setPosition.
  x: number;
  y: number;
  width: number;
  height: number;
  // The part of the display not covered by docks, taskbars and menu bars.
  workX: number;
  workY: number;
  workWidth: number;
  workHeight: number;
  // Device pixels per logical pixel (2 on a retina display).
  scale: number;
  primary: boolean;
  // 0 when unknown.
  refreshHz: number;
}

export interface ScreenEvent {
  // The display list after the change.
  displays: Display[];
}

export function listDisplays(): Promise<Display[]> {
  return call("krate/screen.list", []);
}

export function primaryDisplay(): Promise<Display> {
  return call("krate/screen.primary", []);
}

// ofWindow resolves the display most of the window is on ("" is the main window).
export function displayOfWindow(window = ""): Promise<Display> {
  return call("krate/screen.ofWindow", [window]);
}

// centerWindow moves the window to the middle of the work area of a display
// (the one it is on when display is omitted).
export function centerWindow(window = "", display = ""): Promise<void> {
  return call("krate/screen.centerWindow", [window, display]);
}

// onChange calls fn when a display is added, removed or changes geometry or
// scale.
export function onDisplaysChanged(fn: (e: ScreenEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<ScreenEvent>("krate/screen.events", fn, opts);
}
