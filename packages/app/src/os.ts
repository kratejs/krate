// Preliminary: backed by the Go SDK later.
import { call } from "./index.js";

export function openExternal(url: string): Promise<void> {
  return call("krate/os.OpenExternal", [url]);
}

export function appDataDir(): Promise<string> {
  return call("krate/os.AppDataDir", [], { dedupe: true });
}
