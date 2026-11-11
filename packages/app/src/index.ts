import { createClient, type Client } from "./client.js";
import type { CallOptions, ReadonlyHostSignal } from "./types.js";

export { KrateError, isKrateError } from "./errors.js";
export type { CallOptions, HostSignal, ReadonlyHostSignal } from "./types.js";
export { createClient } from "./client.js";
export type { Client, ClientOptions } from "./client.js";

let current: Client | undefined;

// getClient returns the page's shared client, creating it on first use.
export function getClient(): Client {
  return (current ??= createClient());
}

// setClient replaces the shared client, for example to pass createSignal from
// @krate/runtime so host signals are reactive.
export function setClient(c: Client): void {
  current = c;
}

export function call<T = unknown>(fn: string, args: unknown[] = [], opts?: CallOptions): Promise<T> {
  return getClient().call<T>(fn, args, opts);
}

export function stream<T = unknown>(fn: string, args: unknown[] = [], opts?: CallOptions): AsyncIterable<T> & AsyncIterator<T> {
  return getClient().stream<T>(fn, args, opts);
}

export const hostSignal = {
  signal<T = unknown>(name: string): ReadonlyHostSignal<T, T | undefined> {
    return getClient().signal<T>(name);
  },
};

export const signal = hostSignal.signal;

export const app = {
  get origin(): string {
    return typeof location !== "undefined" ? location.origin : "";
  },
  get native(): boolean {
    if (typeof location === "undefined") return false;
    return location.protocol === "krate:" || location.origin === "https://app.localhost";
  },
};
