// Application menu bar and context menus. Needs the "menu" permission.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type ItemKind = "normal" | "check" | "radio" | "separator" | "submenu";

export interface MenuItem {
  // Identifies the item in click events.
  id?: string;
  // "&File" marks the mnemonic.
  label?: string;
  kind?: ItemKind;
  // For example "CmdOrCtrl+Shift+S".
  accelerator?: string;
  disabled?: boolean;
  checked?: boolean;
  submenu?: MenuItem[];
}

export interface MenuEvent {
  id: string;
  // SDK id of the window whose menu was used ("main" for the first).
  window: string;
  // New state of a check or radio item.
  checked: boolean;
}

const KINDS: Record<ItemKind, number> = { normal: 0, check: 1, radio: 2, separator: 3, submenu: 4 };

export function toWire(items: MenuItem[]): unknown[] {
  return items.map((it) => ({
    id: it.id ?? "",
    label: it.label ?? "",
    kind: KINDS[it.kind ?? (it.submenu ? "submenu" : "normal")],
    accelerator: it.accelerator ?? "",
    disabled: it.disabled ?? false,
    checked: it.checked ?? false,
    submenu: toWire(it.submenu ?? []),
  }));
}

// setApp installs the menu bar on every window; [] removes it.
export function setApp(items: MenuItem[]): Promise<void> {
  return call("krate/menu.setApp", [toWire(items)]);
}

export interface PopupOptions {
  window?: string;
  // Window coordinates; negative or omitted means the pointer.
  x?: number;
  y?: number;
}

export function popup(items: MenuItem[], o: PopupOptions = {}): Promise<void> {
  return call("krate/menu.popup", [o.window ?? "", o.x ?? -1, o.y ?? -1, toWire(items)]);
}

// onClick calls fn for every menu click; the returned function stops it.
export function onClick(fn: (e: MenuEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<MenuEvent>("krate/menu.events", fn, opts);
}

// onMenu calls fn when the item with this id is clicked.
export function onMenu(id: string, fn: (e: MenuEvent) => void, opts?: SubscribeOptions): () => void {
  return onClick((e) => {
    if (e.id === id) fn(e);
  }, opts);
}
