// In-memory implementation of the host side of PROTOCOL.md, as a fetch
// function. For tests of client code and of @krate/app itself.
import { KrateError } from "./errors.js";
import { FrameReader, encodeFrame } from "./wire.js";

export type FakeHandler = (args: any[], ctx: { signal: AbortSignal; id: number }) => unknown;

export interface FakeHost {
  fetch: (input: string, init?: RequestInit) => Promise<Response>;
  // Request log: "METHOD /path".
  requests: string[];
  // Number of frames in each POST /__krate/call body.
  batches: number[][];
  cancelledIds: number[];
  cancelledStreams: string[];
  // Streams whose consumer went away.
  closedStreams: string[];
  signalConnections: string[][];
  setSignal(name: string, v: unknown): void;
  // Ends all open signal connections, as if the server went away.
  dropSignals(): void;
  // Last x-krate-window header seen.
  windows: string[];
}


function framed(stream: ReadableStream<Uint8Array>, status = 200): Response {
  return new Response(stream, { status, headers: { "content-type": "application/x-krate-frames" } });
}

// createFakeHost builds a host from a registry keyed by "<module>.<Name>".
// A handler may return a value, a promise, or an async iterable (a stream).
// Throw KrateError to send an error frame.
export function createFakeHost(registry: Record<string, FakeHandler>, initialSignals: Record<string, unknown> = {}): FakeHost {
  const signals = new Map(Object.entries(initialSignals));
  const inflight = new Map<number, AbortController>();
  const streams = new Map<string, AsyncIterator<unknown>>();
  const sigConns = new Set<{ names: Set<string>; ctrl: ReadableStreamDefaultController<Uint8Array> }>();
  let nextStream = 1;

  const host: FakeHost = {
    requests: [],
    batches: [],
    cancelledIds: [],
    cancelledStreams: [],
    closedStreams: [],
    signalConnections: [],
    windows: [],
    setSignal(name, v) {
      signals.set(name, v);
      for (const c of sigConns) {
        if (c.names.has(name)) c.ctrl.enqueue(encodeFrame({ sig: name, v }));
      }
    },
    dropSignals() {
      for (const c of sigConns) {
        try {
          c.ctrl.close();
        } catch {
          // already closed
        }
      }
      sigConns.clear();
    },
    async fetch(input, init) {
      const url = new URL(input, "http://fake");
      const method = (init?.method ?? "GET").toUpperCase();
      host.requests.push(`${method} ${url.pathname}`);
      const h = new Headers(init?.headers);
      host.windows.push(h.get("x-krate-window") ?? "");
      const sig = init?.signal ?? undefined;

      if (url.pathname === "/__krate/call" && method === "POST") return handleCall(init!.body as Uint8Array);
      if (url.pathname === "/__krate/cancel" && method === "POST") {
        const f = await new FrameReader(new Response(init!.body as BodyInit).body!).next();
        const b = f!.value as { ids?: number[]; streams?: string[] };
        for (const id of b.ids ?? []) {
          host.cancelledIds.push(id);
          inflight.get(id)?.abort();
        }
        for (const s of b.streams ?? []) {
          host.cancelledStreams.push(s);
          const it = streams.get(s);
          if (it) {
            streams.delete(s);
            void it.return?.();
          }
        }
        return new Response(null, { status: 204 });
      }
      if (url.pathname.startsWith("/__krate/stream/")) return handleStream(url.pathname.slice("/__krate/stream/".length), sig);
      if (url.pathname === "/__krate/signals") return handleSignals(url.searchParams.get("names") ?? "", sig);
      return new Response("not found", { status: 404 });
    },
  };

  function handleCall(body: Uint8Array): Response {
    let calls: { id: number; fn: string; args: unknown[] }[];
    return framedBody(async (ctrl) => {
      try {
        const f = await new FrameReader(new Response(body as BodyInit).body!).next();
        calls = (f!.value as any).calls;
      } catch (e) {
        ctrl.enqueue(encodeFrame({ error: { code: "bad_request", message: String(e) } }));
        ctrl.close();
        return;
      }
      host.batches.push(calls.map((c) => c.id));
      let pending = calls.length;
      const finishOne = () => {
        if (--pending === 0) ctrl.close();
      };
      if (pending === 0) ctrl.close();
      for (const c of calls) {
        const h = registry[c.fn];
        const ac = new AbortController();
        inflight.set(c.id, ac);
        void (async () => {
          let out: unknown;
          try {
            if (!h) throw new KrateError("not_found", `unknown fn ${c.fn}`);
            const r = await h(c.args, { signal: ac.signal, id: c.id });
            if (r && typeof (r as any)[Symbol.asyncIterator] === "function") {
              const sid = "s" + nextStream++;
              streams.set(sid, (r as AsyncIterable<unknown>)[Symbol.asyncIterator]());
              out = { id: c.id, stream: sid };
            } else {
              out = { id: c.id, ok: r === undefined ? null : r };
            }
          } catch (e) {
            const ke = e instanceof KrateError ? e : new KrateError("internal", e instanceof Error ? e.message : String(e));
            out = { id: c.id, err: { code: ke.code, message: ke.message, data: ke.data } };
          }
          inflight.delete(c.id);
          try {
            ctrl.enqueue(encodeFrame(out));
          } catch {
            // response already torn down
          }
          finishOne();
        })();
      }
    });
  }

  function framedBody(start: (c: ReadableStreamDefaultController<Uint8Array>) => void | Promise<void>): Response {
    return framed(new ReadableStream<Uint8Array>({ start }));
  }

  function handleStream(sid: string, sig: AbortSignal | undefined): Response {
    const it = streams.get(sid);
    if (!it) return new Response("gone", { status: 404 });
    streams.delete(sid);
    let closed = false;
    const closeIt = () => {
      if (closed) return;
      closed = true;
      host.closedStreams.push(sid);
      void it.return?.();
    };
    sig?.addEventListener("abort", closeIt, { once: true });
    return framed(
      new ReadableStream<Uint8Array>({
        async pull(ctrl) {
          try {
            const r = await it.next();
            if (r.done) {
              ctrl.enqueue(encodeFrame({ end: true }));
              closed = true;
              ctrl.close();
            } else {
              ctrl.enqueue(encodeFrame({ v: r.value }));
            }
          } catch (e) {
            const ke = e instanceof KrateError ? e : new KrateError("internal", e instanceof Error ? e.message : String(e));
            closed = true;
            ctrl.enqueue(encodeFrame({ err: { code: ke.code, message: ke.message, data: ke.data } }));
            ctrl.close();
          }
        },
        cancel: closeIt,
      }),
    );
  }

  function handleSignals(namesParam: string, sig: AbortSignal | undefined): Response {
    const names = namesParam.split(",").filter(Boolean);
    host.signalConnections.push(names);
    let conn: { names: Set<string>; ctrl: ReadableStreamDefaultController<Uint8Array> } | undefined;
    return framed(
      new ReadableStream<Uint8Array>({
        start(ctrl) {
          conn = { names: new Set(names), ctrl };
          sigConns.add(conn);
          for (const n of names) if (signals.has(n)) ctrl.enqueue(encodeFrame({ sig: n, v: signals.get(n) }));
          sig?.addEventListener("abort", () => {
            sigConns.delete(conn!);
            try {
              ctrl.error(new DOMException("aborted", "AbortError"));
            } catch {
              // already closed
            }
          });
        },
        cancel() {
          if (conn) sigConns.delete(conn);
        },
      }),
    );
  }

  return host;
}
