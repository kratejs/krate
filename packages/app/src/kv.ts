// Key-value store in the app database. Values can be anything the host
// protocol carries: JSON values, Uint8Array, Date, Map, Set. Host ids
// krate/kv.*; permission kv.
import { call } from "./index.js";

// Resolves null when the key is missing (a stored null reads the same).
export function get<T = unknown>(key: string): Promise<T | null> {
  return call("krate/kv.get", [key], { dedupe: true });
}

export function set(key: string, value: unknown): Promise<void> {
  return call("krate/kv.set", [key, value]);
}

function del(key: string): Promise<void> {
  return call("krate/kv.delete", [key]);
}
export { del as delete };

// Keys that start with prefix, in byte order.
export function keys(prefix = ""): Promise<string[]> {
  return call("krate/kv.keys", prefix ? [prefix] : [], { dedupe: true });
}
