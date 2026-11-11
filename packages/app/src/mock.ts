// Registry of in-process host implementations used by @krate/app/testing.
import { KrateError } from "./errors.js";
import type { ReadonlyHostSignal, Impl } from "./types.js";

const mocks = new Map<string, Impl>();

export function setMock(moduleId: string, impl: Impl): void {
  mocks.set(moduleId, impl);
}

export function clearMocks(): void {
  mocks.clear();
}

export function hasMocks(): boolean {
  return mocks.size > 0;
}

function split(fn: string): [string, string] {
  const i = fn.lastIndexOf(".");
  return i < 0 ? ["", fn] : [fn.slice(0, i), fn.slice(i + 1)];
}

export function findMock(fn: string): { impl: Impl; name: string } | undefined {
  if (mocks.size === 0) return undefined;
  const [mod, name] = split(fn);
  const impl = mocks.get(mod);
  return impl ? { impl, name } : undefined;
}

export function mockFn(m: { impl: Impl; name: string }, fn: string): (...args: unknown[]) => unknown {
  const f = m.impl[m.name];
  if (typeof f !== "function") throw new KrateError("not_found", `no mock for ${fn}`);
  return f;
}

// mockSignal returns a read-only view when the signal's module is mocked. The
// impl value is either a host signal (get/subscribe) or a plain value.
export function mockSignal(name: string): ReadonlyHostSignal<unknown> | undefined {
  const m = findMock(name);
  if (!m) return undefined;
  const v = m.impl[m.name];
  if (v && typeof v === "object" && typeof v.get === "function" && typeof v.subscribe === "function") {
    const read = (() => v.get()) as ReadonlyHostSignal<unknown>;
    read.subscribe = (fn) => v.subscribe(fn);
    return read;
  }
  if (typeof v === "function") throw new KrateError("not_found", `mock ${name} is a function, not a signal`);
  const read = (() => v) as ReadonlyHostSignal<unknown>;
  read.subscribe = () => () => {};
  return read;
}
