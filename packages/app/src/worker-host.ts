// The side of a worker that runs inside the worker process: receive the
// messages the app posts and send messages back. Import it from the module
// listed in native.workers; it needs no permission.
//
//   import { onMessage, postMessage } from "@krate/app/worker-host";
//   onMessage(async (job) => postMessage({ done: job.id }));
//
// Messages are JSON values or binary (Uint8Array), Date, Map and Set. A
// channel end handed to the worker (Port.transferPort(port, "worker:<id>"))
// is used with portFromId from "@krate/app/port".
import { call } from "./index.js";

type Handler = (data: unknown) => void | Promise<void>;

interface HostRegistry {
  register(id: string, fn: (...args: unknown[]) => unknown, kind?: "func" | "stream"): void;
}

let registered = false;
let handler: Handler | undefined;

// onMessage sets the function that receives each message the app posts to
// this worker, one at a time and in order. Calling it again replaces the
// handler.
export function onMessage(fn: Handler): void {
  handler = fn;
  if (registered) return;
  const host = (globalThis as { __krateHost?: HostRegistry }).__krateHost;
  if (!host) throw new Error("worker-host: onMessage must run inside a Krate worker process");
  registered = true;
  host.register("__worker.message", async (data: unknown) => {
    await handler?.(data);
    return null;
  });
}

// postMessage sends a value to the app (it arrives as a "message" event of the
// worker's handle).
export function postMessage(data: unknown): Promise<void> {
  return call<void>("krate/worker.emit", [data]);
}

// The id of this worker instance, for ports: transferPort(port, `worker:${workerId}`).
export const workerId: string = ((globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env?.KRATE_WORKER_ID) ?? "";
