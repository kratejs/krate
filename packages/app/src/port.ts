// Message channels between windows, views and workers. Needs the "port"
// permission to create channels and the "port:receive" permission to use an
// end that was handed to this page.
//
// The engines give two top-level webviews no way to share a MessagePort, so a
// Port here is an object backed by the shell: messages go to the other end
// directly, without host code relaying them, and carry the same values as host
// calls (JSON, Uint8Array, Date, Map, Set). Inside one page the browser's own
// MessageChannel works as usual and is not replaced.
import { call, stream } from "./index.js";
import { isKrateError } from "./errors.js";

export interface PortEnds {
  a: string;
  b: string;
}

export interface PortMessageEvent<T = unknown> {
  type: "message";
  data: T;
}

export interface PortCloseEvent {
  type: "close";
}

type Listener<E> = (e: E) => void;

// A Port is one end of a channel. Setting onmessage or calling start() begins
// delivery; messages posted by the other end before that are queued by the
// shell (up to 1024 messages or 16 MiB).
export class Port {
  readonly id: string;
  onmessage: Listener<PortMessageEvent> | null = null;
  onclose: Listener<PortCloseEvent> | null = null;
  #listeners = { message: new Set<Listener<PortMessageEvent>>(), close: new Set<Listener<PortCloseEvent>>() };
  #abort: AbortController | undefined;
  #closed = false;

  constructor(id: string) {
    this.id = id;
    // MessagePort starts when onmessage is first assigned; poll cheaply via a
    // property so the common pattern port.onmessage = fn just works.
    let handler: Listener<PortMessageEvent> | null = null;
    Object.defineProperty(this, "onmessage", {
      get: () => handler,
      set: (fn: Listener<PortMessageEvent> | null) => {
        handler = fn;
        if (fn) this.start();
      },
      enumerable: true,
    });
  }

  get closed(): boolean {
    return this.#closed;
  }

  // postMessage sends a value to the other end. The promise rejects when the
  // channel is closed, this page does not hold the end, or the other end has
  // too many unread messages (code "port_full").
  postMessage(data: unknown): Promise<void> {
    return call<void>("krate/port.post", [this.id, data]);
  }

  // start begins delivering messages. It is safe to call more than once.
  start(): void {
    if (this.#abort || this.#closed) return;
    const ac = (this.#abort = new AbortController());
    void (async () => {
      let sawClose = false;
      try {
        for await (const ev of stream<{ data?: unknown; closed?: boolean }>("krate/port.listen", [this.id], { signal: ac.signal })) {
          if (ev.closed) {
            sawClose = true;
            break;
          }
          this.#dispatchMessage(ev.data);
        }
      } catch (e) {
        if (!ac.signal.aborted && !(isKrateError(e) && e.code === "not_found")) console.error(e);
      }
      // A stream that ended without a close means the end moved away or the
      // channel is gone; treat both as closed for this page.
      if (!ac.signal.aborted || sawClose) this.#finish();
    })();
  }

  #dispatchMessage(data: unknown): void {
    const ev: PortMessageEvent = { type: "message", data };
    for (const fn of [...this.#listeners.message, ...(this.onmessage ? [this.onmessage] : [])]) {
      try {
        fn(ev);
      } catch (e) {
        console.error(e);
      }
    }
  }

  #finish(): void {
    if (this.#closed) return;
    this.#closed = true;
    const ev: PortCloseEvent = { type: "close" };
    for (const fn of [...this.#listeners.close, ...(this.onclose ? [this.onclose] : [])]) {
      try {
        fn(ev);
      } catch (e) {
        console.error(e);
      }
    }
  }

  addEventListener(type: "message", fn: Listener<PortMessageEvent>): void;
  addEventListener(type: "close", fn: Listener<PortCloseEvent>): void;
  addEventListener(type: "message" | "close", fn: Listener<never>): void {
    (this.#listeners[type] as Set<Listener<never>>).add(fn);
    if (type === "message") this.start();
  }

  removeEventListener(type: "message" | "close", fn: Listener<never>): void {
    (this.#listeners[type] as Set<Listener<never>>).delete(fn);
  }

  // close ends the channel for both ends. The other end reads what is queued,
  // then sees the close.
  async close(): Promise<void> {
    if (this.#closed) return;
    this.#abort?.abort();
    this.#finish();
    try {
      await call<void>("krate/port.close", [this.id]);
    } catch (e) {
      if (!(isKrateError(e) && e.code === "not_found")) throw e;
    }
  }

  // detach stops this page from listening without closing the channel. It is
  // what transfer() does before the end moves.
  detach(): void {
    this.#abort?.abort();
    this.#abort = undefined;
    this.#closed = true;
  }
}

// createChannel makes a channel; this page holds both ends. Hand one to a
// window, view or worker with transferPort and keep the other.
export async function createChannel(): Promise<{ port1: Port; port2: Port }> {
  const ends = await call<PortEnds>("krate/port.channel", []);
  return { port1: new Port(ends.a), port2: new Port(ends.b) };
}

// transferPort hands an end to a window or view (its id from window.list and
// window.views, or window.thisView in that page), a worker ("worker:<id>") or
// "" for host code. Afterwards this page can no longer use the end. Tell the
// receiver the port's id (for example with window.postMessage) and have it call
// portFromId. The receiver must have the "port:receive" permission.
export async function transferPort(port: Port | string, to: string): Promise<void> {
  const id = typeof port === "string" ? port : port.id;
  await call<void>("krate/port.transfer", [id, to]);
  if (typeof port !== "string") port.detach();
}

// portFromId wraps an end that was transferred to this page.
export function portFromId(id: string): Port {
  return new Port(id);
}
