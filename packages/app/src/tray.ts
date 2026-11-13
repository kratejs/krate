// System tray icon. Needs the "tray" permission. Linux needs a
// StatusNotifier host; without one set() rejects with code "unsupported".
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";
import { toWire, type MenuItem } from "./menu.js";

export interface TrayOptions {
  tooltip?: string;
  title?: string;
  // PNG image.
  icon?: Uint8Array;
  // Item clicks arrive as tray events of kind "menu" and as menu events
  // (onMenu in @krate/app/menu).
  menu?: MenuItem[];
}

export type TrayEventKind = "click" | "middle-click" | "right-click" | "menu";

export interface TrayEvent {
  kind: TrayEventKind;
  // Menu item id for kind "menu".
  id: string;
}

// set shows or updates the tray icon.
export function set(o: TrayOptions): Promise<void> {
  return call("krate/tray.set", [
    { tooltip: o.tooltip ?? "", title: o.title ?? "", icon: o.icon ?? new Uint8Array(0), menu: toWire(o.menu ?? []) },
  ]);
}

export function remove(): Promise<void> {
  return call("krate/tray.remove");
}

export function onEvent(fn: (e: TrayEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<TrayEvent>("krate/tray.events", fn, opts);
}

export function onClick(fn: () => void, opts?: SubscribeOptions): () => void {
  return onEvent((e) => {
    if (e.kind === "click") fn();
  }, opts);
}
