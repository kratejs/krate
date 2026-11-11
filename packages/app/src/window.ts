// Open and control windows. Needs the "window" permission. The first window
// has the id "main". Always-on-top and absolute positions are ignored by
// Wayland compositors.
import { call } from "./index.js";
import { clean, subscribe, type SubscribeOptions } from "./events.js";

export interface WindowOptions {
  title?: string;
  width?: number;
  height?: number;
  // Route to open, for example "/settings".
  path?: string;
  // Not resizable.
  fixed?: boolean;
  frameless?: boolean;
  alwaysOnTop?: boolean;
  fullscreen?: boolean;
  maximized?: boolean;
  position?: { x: number; y: number };
  // Deliver "close-requested" instead of closing; call close() to close.
  preventClose?: boolean;
  // Narrow the SDK permissions the page of this window may use to these
  // names (a subset of the opening window's). An empty list leaves the page
  // nothing. Without it the window gets the opening window's permissions.
  permissions?: string[];
}

export interface HideOptions {
  // Also destroy the window's webview. Ignored where the platform cannot.
  release?: boolean;
}

export interface WindowState {
  x: number;
  y: number;
  width: number;
  height: number;
  maximized: boolean;
  minimized: boolean;
  fullscreen: boolean;
  alwaysOnTop: boolean;
  decorated: boolean;
  visible: boolean;
  focused: boolean;
  title: string;
}

// "released": the window gave up its webview (hide({ release: true }), or the
// last window closing under window.keepAlive); "restored": show() brought it back.
export type WindowEventKind = "close-requested" | "closed" | "focus" | "blur" | "released" | "restored";

export interface WindowEvent {
  window: string;
  kind: WindowEventKind;
}

export class Window {
  constructor(readonly id: string) {}

  private cmd(name: string, ...args: unknown[]): Promise<void> {
    return call(`krate/window.${name}`, [this.id, ...args]);
  }
  close() {
    return this.cmd("close");
  }
  setTitle(title: string) {
    return this.cmd("setTitle", title);
  }
  setSize(width: number, height: number) {
    return this.cmd("setSize", width, height);
  }
  setPosition(x: number, y: number) {
    return this.cmd("setPosition", x, y);
  }
  setAlwaysOnTop(on: boolean) {
    return this.cmd("setAlwaysOnTop", on);
  }
  setFullscreen(on: boolean) {
    return this.cmd("setFullscreen", on);
  }
  setFrameless(on: boolean) {
    return this.cmd("setFrameless", on);
  }
  minimize() {
    return this.cmd("minimize");
  }
  maximize() {
    return this.cmd("maximize");
  }
  restore() {
    return this.cmd("restore");
  }
  focus() {
    return this.cmd("focus");
  }
  // Shows a hidden window. A window hidden with { release: true } gets its
  // webview back on the route and scroll position it had; the page reloads
  // and re-subscribes to host signals, which resend their current values.
  show() {
    return this.cmd("show");
  }
  // Hides the window. With { release: true } the webview is destroyed too:
  // when every window is released the web process exits, which suits a tray
  // app running in the background. Without it the page stays and gets a
  // "krate:memory-pressure" event (see onMemoryPressure).
  hide(opts: HideOptions = {}) {
    return opts.release ? this.cmd("hide", true) : this.cmd("hide");
  }
  setPreventClose(on: boolean) {
    return this.cmd("setPreventClose", on);
  }
  navigate(path: string) {
    return this.cmd("navigate", path);
  }
  state(): Promise<WindowState> {
    return call("krate/window.state", [this.id]);
  }
  // Adds a view (a second webview) to the window, split off another view of it.
  addView(o: ViewOptions = {}): Promise<View> {
    const { beside, ...rest } = o;
    return call<string>("krate/window.addView", [
      this.id,
      clean({ ...rest, beside: beside === undefined ? undefined : typeof beside === "string" ? beside : beside.id }),
    ]).then((id) => new View(id));
  }
  // The window's views, the primary one first.
  views(): Promise<ViewInfo[]> {
    return call("krate/window.views", [this.id]);
  }
  // Replaces the layout. It must name every view of the window exactly once.
  setLayout(layout: Layout): Promise<void> {
    return call("krate/window.setLayout", [this.id, layoutToWire(layout)]);
  }
  // The layout as it is now, with the ratios the user dragged the dividers to.
  layout(): Promise<Layout> {
    return call("krate/window.layout", [this.id]);
  }
  onCloseRequested(fn: () => void, opts?: SubscribeOptions): () => void {
    return onEvent((e) => {
      if (e.window === this.id && e.kind === "close-requested") fn();
    }, opts);
  }
}

export type Side = "right" | "left" | "top" | "bottom";

export interface ViewOptions {
  // Names the view; default "v1", "v2", ... Letters, digits, "_" and "-".
  id?: string;
  // Route the view opens. Default "/".
  path?: string;
  // The view to split. Default: the window's primary view.
  beside?: string | View;
  // Where the new view goes relative to "beside". Default "right".
  side?: Side;
  // The share of the split the new view gets, between 0 and 1. Default 0.5.
  size?: number;
  // Smallest size of the new pane, in pixels.
  minWidth?: number;
  minHeight?: number;
  // Narrow the SDK permissions the view's page may use to these names (a
  // subset of the calling view's). Without it the view gets the caller's.
  permissions?: string[];
}

export interface ViewInfo {
  id: string;
  window: string;
  path: string;
  primary: boolean;
}

// A layout tree. A pane shows a view; a split puts two layouts side by side
// ("horizontal": first on the left) or one above the other ("vertical": first
// on top). "ratio" is the share of the space "first" gets, default 0.5.
export type Layout =
  | { view: string | View; minWidth?: number; minHeight?: number }
  | { direction: "horizontal" | "vertical"; ratio?: number; first: Layout; second: Layout };

// "added": a view was created; "navigated": a page finished loading in it;
// "removed": remove() took it out; "closed": its page called window.close() or
// its window closed (a window's views are announced before the window).
export type ViewEventKind = "added" | "removed" | "navigated" | "closed";

export interface ViewEvent {
  view: string;
  window: string;
  kind: ViewEventKind;
  // The route, for "added" and "navigated".
  path: string;
}

export interface ViewMessage {
  // The id of the view that sent it, set by the shell; "" when host code sent it.
  from: string;
  data: unknown;
}

function layoutToWire(l: Layout): unknown {
  if ("direction" in l) {
    return clean({ direction: l.direction, ratio: l.ratio, first: layoutToWire(l.first), second: layoutToWire(l.second) });
  }
  return clean({ view: typeof l.view === "string" ? l.view : l.view.id, minWidth: l.minWidth, minHeight: l.minHeight });
}

// A pane of a layout showing a view.
export function pane(view: string | View, min: { minWidth?: number; minHeight?: number } = {}): Layout {
  return { view, ...min };
}

// A split of two layouts.
export function split(direction: "horizontal" | "vertical", first: Layout, second: Layout, ratio?: number): Layout {
  return { direction, first, second, ...(ratio === undefined ? {} : { ratio }) };
}

// A handle to one view of a window. The first view of a window has the window's id.
export class View {
  constructor(readonly id: string) {}

  // Removes the view; its neighbour takes the space. A window's first view
  // cannot be removed (close the window).
  remove(): Promise<void> {
    return call("krate/window.removeView", [this.id]);
  }
  focus(): Promise<void> {
    return call("krate/window.focusView", [this.id]);
  }
  navigate(path: string): Promise<void> {
    return call("krate/window.navigateView", [this.id, path]);
  }
  // Sends JSON-compatible data to the view; its page receives a "krate:view-message"
  // event (see onViewMessage). The sender's id arrives as "from".
  post(data: unknown): Promise<void> {
    const json = JSON.stringify(data);
    if (json === undefined) return Promise.reject(new TypeError("view.post: data is not JSON-compatible"));
    return call("krate/window.postMessage", [this.id, JSON.parse(json)]);
  }
}

export function getView(id: string): View {
  return new View(id);
}

// Every view of every window.
export function views(): Promise<ViewInfo[]> {
  return call("krate/window.views", [""]);
}

export function onViewEvent(fn: (e: ViewEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<ViewEvent>("krate/window.viewEvents", fn, opts);
}

// Calls fn for a message another view posted to this page (View.post). Returns
// a function that stops listening. Outside a page it does nothing.
export function onViewMessage(fn: (m: ViewMessage) => void): () => void {
  const target = globalThis as unknown as {
    addEventListener?: (type: string, fn: (e: { detail: ViewMessage }) => void) => void;
    removeEventListener?: (type: string, fn: (e: { detail: ViewMessage }) => void) => void;
  };
  if (typeof target.addEventListener !== "function") return () => {};
  const h = (e: { detail: ViewMessage }) => fn(e.detail);
  target.addEventListener("krate:view-message", h);
  return () => target.removeEventListener?.("krate:view-message", h);
}

export function main(): Window {
  return new Window("main");
}

export function get(id: string): Window {
  return new Window(id);
}

export async function open(o: WindowOptions = {}): Promise<Window> {
  const { position, ...rest } = o;
  const id = await call<string>("krate/window.open", [
    { ...rest, ...(position ? { x: position.x, y: position.y, hasPosition: true } : {}) },
  ]);
  return new Window(id);
}

export function list(): Promise<string[]> {
  return call("krate/window.list");
}

export function onEvent(fn: (e: WindowEvent) => void, opts?: SubscribeOptions): () => void {
  return subscribe<WindowEvent>("krate/window.events", fn, opts);
}

// Calls fn when the shell hides or minimizes this window, so the page can drop
// caches and large buffers. It listens for the "krate:memory-pressure" DOM
// event, which the shell dispatches before it returns memory to the system.
// Returns a function that stops listening. Outside a page it does nothing.
export function onMemoryPressure(fn: () => void): () => void {
  const target = globalThis as unknown as {
    addEventListener?: (type: string, fn: () => void) => void;
    removeEventListener?: (type: string, fn: () => void) => void;
  };
  if (typeof target.addEventListener !== "function") return () => {};
  target.addEventListener("krate:memory-pressure", fn);
  return () => target.removeEventListener?.("krate:memory-pressure", fn);
}
