// Preliminary: backed by the Go SDK later.
import { call } from "./index.js";

export function get<T = unknown>(key: string): Promise<T | null> {
  return call("krate/kv.Get", [key], { dedupe: true });
}

export function set(key: string, value: unknown): Promise<void> {
  return call("krate/kv.Set", [key, value]);
}

function del(key: string): Promise<void> {
  return call("krate/kv.Delete", [key]);
}
export { del as delete };

export function keys(prefix = ""): Promise<string[]> {
  return call("krate/kv.Keys", [prefix], { dedupe: true });
}
