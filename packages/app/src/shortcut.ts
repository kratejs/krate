// Global (system-wide) keyboard shortcuts. Needs the "shortcut" permission.
// Works on X11; on Wayland it needs the GlobalShortcuts portal and otherwise
// register() rejects with code "unsupported".
import { call } from "./index.js";
import { subscribe } from "./events.js";

const handlers = new Map<string, () => void>();
let stop: (() => void) | undefined;
let counter = 0;

function ensureSubscribed(): void {
  if (stop) return;
  stop = subscribe<{ id: string }>("krate/shortcut.events", (e) => handlers.get(e.id)?.(), {
    onError: () => {
      stop = undefined;
    },
  });
}

function newHandle(): string {
  const c = (globalThis as { crypto?: { randomUUID?: () => string } }).crypto;
  return "ui-" + ++counter + "-" + (c?.randomUUID?.() ?? Math.random().toString(36).slice(2));
}

// register grabs the accelerator ("Ctrl+Shift+K", "Alt+F4") and calls fn on
// each press. It resolves to a function that releases the shortcut.
export async function register(accelerator: string, fn: () => void): Promise<() => Promise<void>> {
  const handle = newHandle();
  handlers.set(handle, fn);
  ensureSubscribed();
  try {
    await call("krate/shortcut.register", [handle, accelerator]);
  } catch (e) {
    handlers.delete(handle);
    throw e;
  }
  return async () => {
    if (!handlers.delete(handle)) return;
    await call("krate/shortcut.unregister", [handle]);
    if (handlers.size === 0) {
      stop?.();
      stop = undefined;
    }
  };
}
