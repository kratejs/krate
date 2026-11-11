// Shared by the UI half of the SDK: turn a host event stream into callbacks.
import { stream } from "./index.js";
import { isKrateError } from "./errors.js";

export interface SubscribeOptions {
  // Called with an error that ends the subscription (for example the
  // permission was not granted or the platform lacks the feature).
  onError?: (e: unknown) => void;
}

const RETRY_MS = 250;

// subscribe calls fn for every value of the host stream `id` until the
// returned function is called. A dropped stream is reopened; errors that will
// not go away (not_found for a missing permission, unsupported) stop it.
export function subscribe<T>(id: string, fn: (v: T) => void, opts: SubscribeOptions = {}): () => void {
  const ac = new AbortController();
  void (async () => {
    while (!ac.signal.aborted) {
      try {
        for await (const v of stream<T>(id, [], { signal: ac.signal })) {
          if (ac.signal.aborted) return;
          try {
            fn(v);
          } catch (e) {
            // A throwing listener must not end the subscription.
            console.error(e);
          }
        }
      } catch (e) {
        if (ac.signal.aborted) return;
        if (isKrateError(e) && (e.code === "not_found" || e.code === "unsupported" || e.code === "bad_args")) {
          opts.onError?.(e);
          return;
        }
      }
      if (ac.signal.aborted) return;
      await new Promise((r) => setTimeout(r, RETRY_MS));
    }
  })();
  return () => ac.abort();
}

// drop undefined values so Go sees the field as absent.
export function clean<T extends Record<string, unknown>>(o: T): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(o)) if (v !== undefined) out[k] = v;
  return out;
}
