// Window chrome: title bar styles and drag regions, backdrops and vibrancy,
// the caption buttons over a hidden title bar, traffic lights, document
// state, sheets, native tabs, taskbar thumbnail buttons and jump lists, the
// share menu and the dock. Window operations need the "window" permission,
// jump lists and the dock "app", and the share menu "share".
//
// Most of it belongs to one platform. An operation the platform lacks rejects
// with code "unsupported"; ask first with app.supports("chrome:<feature>").
//
// Drag regions are marked in the page: add the data-krate-drag attribute to
// an element, or the CSS property -webkit-app-region (or app-region) with the
// value drag. no-drag (data-krate-drag="false") keeps a part clickable.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

export type TitleBarStyle = "native" | "hidden" | "none";
export type Backdrop = "none" | "mica" | "micaAlt" | "acrylic";
export type Vibrancy =
  | "titlebar"
  | "selection"
  | "menu"
  | "popover"
  | "sidebar"
  | "headerView"
  | "sheet"
  | "windowBackground"
  | "hudWindow"
  | "fullScreenUI"
  | "tooltip"
  | "contentBackground"
  | "underWindowBackground"
  | "underPageBackground";
export type TabbingMode = "auto" | "preferred" | "disallowed";
export type ActivationPolicy = "regular" | "accessory" | "prohibited";

// Caption buttons the OS draws over a hidden title bar (Windows). Colors are
// "#rgb", "#rrggbb" or "#rrggbbaa"; omit one to keep the system's. height is
// the height of the draggable strip in pixels (default 32).
export interface TitleBarOverlay {
  color?: string;
  symbolColor?: string;
  height?: number;
}

export interface ChromeState {
  titleBar: TitleBarStyle;
  transparent: boolean;
  // Windows backdrop and macOS vibrancy material; "" when unset.
  backdrop: string;
  vibrancy: string;
  overlay: TitleBarOverlay | null;
  trafficLight: { x: number; y: number } | null;
  documentEdited: boolean;
  representedFile: string;
  tabbingId: string;
  tabbingMode: string;
  // Windows in this window's tab group; 1 when alone.
  tabs: number;
  sheet: boolean;
  thumbButtons: number;
  // The web engine honors the CSS app-region property itself (WebView2).
  nativeRegions: boolean;
  // What this window supports: titleBar, transparent, backdrop, vibrancy,
  // titleBarOverlay, trafficLights, drag, resize, document, sheets, tabs,
  // thumbButtons.
  features: string[];
}

export interface ThumbButton {
  id: string;
  tooltip?: string;
  // A PNG image of about 16x16 pixels.
  icon?: Uint8Array;
  disabled?: boolean;
  hidden?: boolean;
  dismissOnClick?: boolean;
}

export interface JumpTask {
  title: string;
  // Arguments the app starts with; a running instance gets them as a
  // second-instance event.
  args?: string[];
  description?: string;
  // Absolute path of an .exe or .ico file, and the icon index in it.
  iconPath?: string;
  iconIndex?: number;
}

export interface ShareItems {
  text?: string;
  // http, https and mailto URLs.
  urls?: string[];
  // Absolute file paths.
  files?: string[];
}

// "new-tab": the user asked for a new tab in a tabbed window (macOS): open a
// window with the same tabbing identifier. "tabs-changed": a window joined or
// left a tab group. "thumb-click": a taskbar thumbnail button was clicked (id
// names it). "sheet-ended": a sheet closed.
export type ChromeEventKind = "new-tab" | "tabs-changed" | "thumb-click" | "sheet-ended";

export interface ChromeEvent {
  window: string;
  kind: ChromeEventKind;
  id: string;
}

// ChromeWindow controls the chrome of one window by its id.
export class ChromeWindow {
  constructor(readonly id: string) {}

  private cmd(name: string, ...args: unknown[]): Promise<void> {
    return call(`krate/chrome.${name}`, [this.id, ...args]);
  }
  state(): Promise<ChromeState> {
    return call("krate/chrome.state", [this.id]);
  }
  setTitleBar(style: TitleBarStyle) {
    return this.cmd("setTitleBar", style);
  }
  // Windows 11 system backdrop. Leave the page background transparent for it
  // to show.
  setBackdrop(name: Backdrop) {
    return this.cmd("setBackdrop", name);
  }
  // macOS vibrancy material behind the page; "" removes it.
  setVibrancy(material: Vibrancy | "") {
    return this.cmd("setVibrancy", material);
  }
  // The OS caption buttons over a hidden title bar (Windows); null removes.
  setTitleBarOverlay(overlay: TitleBarOverlay | null) {
    return this.cmd("setTitleBarOverlay", overlay === null ? null : { ...overlay });
  }
  // Moves the macOS window buttons.
  setTrafficLightPosition(x: number, y: number) {
    return this.cmd("setTrafficLightPosition", x, y);
  }
  // Starts a native window move from the press that is happening. Pages
  // normally mark drag regions in markup instead.
  startDrag() {
    return this.cmd("startDrag");
  }
  toggleMaximize() {
    return this.cmd("toggleMaximize");
  }
  // macOS: the dot in the close button for unsaved changes.
  setDocumentEdited(on: boolean) {
    return this.cmd("setDocumentEdited", on);
  }
  // macOS: the file whose icon the title bar shows; "" clears it.
  setRepresentedFile(path: string) {
    return this.cmd("setRepresentedFile", path);
  }
  // macOS: shows another window of the app as a sheet attached to this one.
  attachSheet(child: string | ChromeWindow) {
    return this.cmd("attachSheet", typeof child === "string" ? child : child.id);
  }
  endSheet() {
    return this.cmd("endSheet");
  }
  // macOS: windows with the same identifier tab together.
  setTabbing(identifier: string, mode: TabbingMode = "preferred") {
    return this.cmd("setTabbing", identifier, mode);
  }
  mergeAllWindows() {
    return this.cmd("mergeAllWindows");
  }
  moveTabToNewWindow() {
    return this.cmd("moveTabToNewWindow");
  }
  // Ids of the windows in this window's tab group, in tab order.
  tabs(): Promise<string[]> {
    return call("krate/chrome.tabs", [this.id]);
  }
  // Windows: replaces the taskbar thumbnail buttons (at most 7). Clicks
  // arrive as "thumb-click" events.
  setThumbButtons(buttons: ThumbButton[]) {
    return this.cmd("setThumbButtons", buttons.map((b) => ({ ...b, icon: b.icon ?? new Uint8Array(0) })));
  }
  onNewTab(fn: () => void, opts?: SubscribeOptions): () => void {
    return onEvent((e) => {
      if (e.window === this.id && e.kind === "new-tab") fn();
    }, opts);
  }
  onThumbClick(fn: (id: string) => void, opts?: SubscribeOptions): () => void {
    return onEvent((e) => {
      if (e.window === this.id && e.kind === "thumb-click") fn(e.id);
    }, opts);
  }
}

export function main(): ChromeWindow {
  return new ChromeWindow("main");
}

export function get(id: string): ChromeWindow {
  return new ChromeWindow(id);
}

// Shows the system share menu for items, anchored on a window (macOS). It
// resolves when the menu is shown. Needs the "share" permission.
export function share(window: string | ChromeWindow, items: ShareItems): Promise<void> {
  const id = typeof window === "string" ? window : window.id;
  return call("krate/chrome.share", [id, { text: "", urls: [], files: [], ...items }]);
}

// Windows: replaces the tasks of the taskbar jump list (at most 20).
export function setJumpList(tasks: JumpTask[]): Promise<void> {
  return call(
    "krate/chrome.setJumpList",
    [tasks.map((t) => ({ args: [], description: "", iconPath: "", iconIndex: 0, ...t }))],
  );
}

export function clearJumpList(): Promise<void> {
  return call("krate/chrome.clearJumpList");
}

// macOS: "accessory" makes a menu-bar-only app with no dock icon.
export function setActivationPolicy(policy: ActivationPolicy): Promise<void> {
  return call("krate/chrome.setActivationPolicy", [policy]);
}

// macOS: show or hide the dock icon.
export function setDockVisible(on: boolean): Promise<void> {
  return call("krate/chrome.setDockVisible", [on]);
}

export function onEvent(fn: (e: ChromeEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<ChromeEvent>("krate/chrome.events", fn, opts);
}
