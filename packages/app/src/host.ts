// Host-side signals for TS host code (marked @host).
import type { HostSignal } from "./types.js";
export type { HostSignal, ReadonlyHostSignal } from "./types.js";

export function hostSignal<T>(initial: T): HostSignal<T> {
  let value = initial;
  const subs = new Set<(v: T) => void>();
  const sig: HostSignal<T> = {
    get: () => value,
    set(v) {
      value = v;
      for (const fn of [...subs]) fn(v);
    },
    update(f) {
      sig.set(f(value));
    },
    subscribe(fn) {
      subs.add(fn);
      return () => {
        subs.delete(fn);
      };
    },
  };
  (globalThis as any).__krateHost?.registerSignal?.(sig);
  return sig;
}
