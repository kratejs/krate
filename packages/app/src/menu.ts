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

// Webview context menu.
//
// By default a right click shows the engine's own menu. setContextMenu
// changes that for a window or one view: "none" shows nothing, "custom" shows
// nothing and reports the click (onContextMenu answers it with items drawn by
// the native menu code), "extend" adds items to the engine's menu.
export type ContextMenuMode = "default" | "none" | "custom" | "extend";

export interface ContextMenuOptions {
  mode: ContextMenuMode;
  // Added to the engine's menu; "extend" mode only. Clicks arrive through
  // onClick like any other menu item.
  items?: MenuItem[];
}

export type ContextKind = "page" | "link" | "image" | "media" | "editable" | "selection";

export interface ContextMenuEvent {
  // SDK ids; the primary view has its window's id.
  window: string;
  view: string;
  // View client coordinates (a DOM MouseEvent's clientX and clientY).
  x: number;
  y: number;
  // Window coordinates, what popup takes.
  windowX: number;
  windowY: number;
  kinds: ContextKind[];
  linkUrl: string;
  imageUrl: string;
  mediaUrl: string;
  selectionText: string;
  // Set when native.features.spellcheck is on and the engine found a
  // misspelled word under the pointer.
  misspelled: string;
  suggestions: string[];
  // True in dev builds, which have developer tools.
  inspectable: boolean;
}

export interface ContextMenuTarget {
  // Window id; "" is the main window and "*" every window without a setting
  // of its own. Ignored when view is set.
  window?: string;
  // A view id; the setting then applies to that view only.
  view?: string;
}

// setContextMenu sets the context menu behavior of a window or view.
export function setContextMenu(options: ContextMenuOptions, target: ContextMenuTarget = {}): Promise<void> {
  return call("krate/menu.setContextMenu", [
    target.window ?? "",
    target.view ?? "",
    { mode: options.mode, items: toWire(options.items ?? []) },
  ]);
}

// onContext calls fn for every right click in a view whose mode is "custom".
export function onContext(fn: (e: ContextMenuEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<ContextMenuEvent>("krate/menu.contextEvents", fn, opts);
}

// onContextMenu answers right clicks in "custom" views: fn returns the items
// (or nothing, for no menu) and they pop up at the click. Start from
// defaultContextItems to keep the usual entries.
export function onContextMenu(
  fn: (e: ContextMenuEvent) => MenuItem[] | null | undefined | Promise<MenuItem[] | null | undefined>,
  opts?: SubscribeOptions,
): () => void {
  return onContext(async (e) => {
    const items = await fn(e);
    if (items && items.length > 0) await popup(items, { window: e.window, x: e.windowX, y: e.windowY });
  }, opts);
}

// Ids of the built-in actions: the shell runs them on the view that showed
// the menu instead of reporting a click.
export const actions = {
  undo: "krate:undo",
  redo: "krate:redo",
  cut: "krate:cut",
  copy: "krate:copy",
  paste: "krate:paste",
  selectAll: "krate:selectAll",
  delete: "krate:delete",
  inspectElement: "krate:inspectElement",
  addToDictionary: "krate:addToDictionary",
  replaceMisspelling: (word: string) => `krate:replaceMisspelling:${word}`,
  copyLink: (url: string) => `krate:copyLink:${url}`,
  copyImageUrl: (url: string) => `krate:copyImageUrl:${url}`,
};

// defaultContextItems is the menu a browser would show for e (English
// labels): in editable fields spelling suggestions with "Add to Dictionary"
// (when the engine reported a misspelling), undo, redo, cut, copy, paste and
// select all; "Copy" for a selection; "Copy Link Address" for a link; and, in
// dev builds, "Inspect Element".
export function defaultContextItems(e: ContextMenuEvent): MenuItem[] {
  const items: MenuItem[] = [];
  const sep = () => {
    if (items.length > 0 && items[items.length - 1].kind !== "separator") items.push({ kind: "separator" });
  };
  const has = (k: ContextKind) => e.kinds.includes(k);
  const editable = has("editable");
  if (editable && e.misspelled) {
    for (const s of e.suggestions) items.push({ id: actions.replaceMisspelling(s), label: s });
    if (e.suggestions.length === 0) items.push({ label: "No Guesses Found", disabled: true });
    items.push({ id: actions.addToDictionary, label: "Add to Dictionary" });
    sep();
  }
  if (has("link") && e.linkUrl) {
    items.push({ id: actions.copyLink(e.linkUrl), label: "Copy Link Address" });
    sep();
  }
  if (has("image") && e.imageUrl) {
    items.push({ id: actions.copyImageUrl(e.imageUrl), label: "Copy Image Address" });
    sep();
  }
  if (editable) {
    items.push(
      { id: actions.undo, label: "Undo", accelerator: "CmdOrCtrl+Z" },
      { id: actions.redo, label: "Redo", accelerator: "CmdOrCtrl+Shift+Z" },
      { kind: "separator" },
      { id: actions.cut, label: "Cut", accelerator: "CmdOrCtrl+X" },
      { id: actions.copy, label: "Copy", accelerator: "CmdOrCtrl+C" },
      { id: actions.paste, label: "Paste", accelerator: "CmdOrCtrl+V" },
      { id: actions.selectAll, label: "Select All", accelerator: "CmdOrCtrl+A" },
    );
  } else if (has("selection")) {
    items.push(
      { id: actions.copy, label: "Copy", accelerator: "CmdOrCtrl+C" },
      { id: actions.selectAll, label: "Select All", accelerator: "CmdOrCtrl+A" },
    );
  }
  if (e.inspectable) {
    sep();
    items.push({ id: actions.inspectElement, label: "Inspect Element" });
  }
  while (items.length > 0 && items[items.length - 1].kind === "separator") items.pop();
  return items;
}
