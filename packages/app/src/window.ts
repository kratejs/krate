// Open and control windows. Needs the "window" permission. The first window
// has the id "main". Always-on-top and absolute positions are ignored by
// Wayland compositors.
import { call } from "./index.js";
import { subscribe, type SubscribeOptions } from "./events.js";

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
  onCloseRequested(fn: () => void, opts?: SubscribeOptions): () => void {
    return onEvent((e) => {
      if (e.window === this.id && e.kind === "close-requested") fn();
    }, opts);
  }
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
