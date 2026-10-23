// Shared event log for the React browser differential. Fixtures import it as
// "../lib/log" on both sides (React and Krate), and the harness reads
// window.__log after every step, so the order of effects, cleanups,
// lifecycles, ref callbacks and handlers is compared exactly.

function fmt(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === undefined) return "undefined";
  if (typeof value === "number") {
    if (Object.is(value, -0)) return "-0";
    return String(value);
  }
  if (typeof value === "function") return "fn";
  if (value instanceof Element) return "<" + value.tagName.toLowerCase() + ">";
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export function log(...parts: unknown[]): void {
  if (typeof window === "undefined") return;
  const w = window as unknown as { __log?: string[] };
  if (!w.__log) w.__log = [];
  w.__log.push(parts.map(fmt).join(" "));
}

// A small external store for useSyncExternalStore fixtures. The harness can
// drive it with an "eval" step: window.__store.set(n).
type Listener = () => void;

export function createStore<T>(initial: T) {
  let value = initial;
  const listeners = new Set<Listener>();
  const store = {
    get: () => value,
    set: (next: T) => {
      value = next;
      listeners.forEach((l) => l());
    },
    subscribe: (l: Listener) => {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
  };
  return store;
}

// Resolves after ms milliseconds with value. Used for async actions and
// Suspense data so both sides see the same timing.
export function delay<T>(ms: number, value: T): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}

// A promise that resolves when open() is called, so a fixture can hold
// Suspense data or an async action pending until the harness releases it (a
// click or an eval step) instead of racing a timer.
export function createGate() {
  let release: () => void = () => {};
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, open: () => release() };
}
