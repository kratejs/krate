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

export type WindowEventKind = "close-requested" | "closed" | "focus" | "blur";

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
  show() {
    return this.cmd("show");
  }
  hide() {
    return this.cmd("hide");
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
