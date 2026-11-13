// Background worker processes (Electron's utilityProcess.fork). Needs the
// "worker" permission. Workers are declared in native.workers; this module
// starts them by name and talks to them. See worker-host for the code that
// runs inside a worker.
import { call, stream } from "./index.js";
import { isKrateError } from "./errors.js";

export interface WorkerStartOptions {
  // Appended to the worker's declared arguments.
  args?: string[];
  // Added to the environment of this instance.
  env?: Record<string, string>;
}

export interface WorkerRestart {
  policy?: "never" | "onFailure" | "always";
  max?: number;
  backoffMs?: number;
}

export interface WorkerSpec {
  name: string;
  runtime: "node" | "bun" | "deno" | "go" | "exec";
  entry: string;
  permissions?: string[];
  restart?: WorkerRestart;
  autostart?: boolean;
}

export type WorkerState = "starting" | "running" | "restarting" | "stopped" | "failed";

export interface WorkerInfo {
  id: string;
  name: string;
  state: WorkerState;
  pid: number;
  restarts: number;
  exitCode?: number;
  error?: string;
}

export interface WorkerEvent {
  kind: "started" | "exit" | "crash" | "restarting" | "stopped" | "failed" | "message";
  id: string;
  name: string;
  pid?: number;
  exitCode?: number;
  error?: string;
  attempt?: number;
  delayMs?: number;
  // The message of a "message" event.
  data?: unknown;
}

export function workerSpecs(): Promise<WorkerSpec[]> {
  return call("krate/worker.specs", []);
}

export function listWorkers(): Promise<WorkerInfo[]> {
  return call("krate/worker.list", []);
}

// onWorkerEvent calls fn for the events of one instance (all when id is
// omitted) until the returned function is called.
export function onWorkerEvent(fn: (e: WorkerEvent) => void, id?: string): () => void {
  const ac = new AbortController();
  void (async () => {
    try {
      for await (const ev of stream<WorkerEvent>("krate/worker.events", id === undefined ? [] : [id], { signal: ac.signal })) {
        try {
          fn(ev);
        } catch (e) {
          console.error(e);
        }
      }
    } catch (e) {
      if (!ac.signal.aborted && !(isKrateError(e) && e.code === "not_found")) console.error(e);
    }
  })();
  return () => ac.abort();
}

// A WorkerHandle is one running instance.
export class WorkerHandle {
  readonly id: string;
  readonly name: string;
  onmessage: ((data: unknown) => void) | null = null;
  // onexit runs each time the process ends, with the exit code (-1 for a
  // signal) and whether it crashed. A restart policy may start it again.
  onexit: ((e: { code: number; crashed: boolean; error?: string }) => void) | null = null;
  onrestart: ((e: { attempt: number; delayMs: number }) => void) | null = null;
  #off: () => void;

  constructor(id: string, name: string) {
    this.id = id;
    this.name = name;
    this.#off = onWorkerEvent((ev) => {
      if (ev.kind === "message") this.onmessage?.(ev.data);
      else if (ev.kind === "exit" || ev.kind === "crash") this.onexit?.({ code: ev.exitCode ?? 0, crashed: ev.kind === "crash", error: ev.error });
      else if (ev.kind === "restarting") this.onrestart?.({ attempt: ev.attempt ?? 0, delayMs: ev.delayMs ?? 0 });
    }, id);
  }

  // The id to give transferPort to hand this worker an end of a channel.
  get portOwner(): string {
    return `worker:${this.id}`;
  }

  postMessage(data: unknown): Promise<void> {
    return call<void>("krate/worker.post", [this.id, data]);
  }

  // terminate stops the worker and keeps it from restarting.
  async terminate(): Promise<void> {
    this.#off();
    await call<void>("krate/worker.stop", [this.id]);
  }
}

// startWorker starts an instance of a declared worker and resolves when its
// process is ready.
export async function startWorker(name: string, options?: WorkerStartOptions): Promise<WorkerHandle> {
  const id = await call<string>("krate/worker.start", options ? [name, options] : [name]);
  return new WorkerHandle(id, name);
}
