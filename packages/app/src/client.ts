// Transport client for the host call protocol (see packages/native/PROTOCOL.md).
import { KrateError } from "./errors.js";
import { findMock, mockFn, mockSignal } from "./mock.js";
import type { CallOptions, ReadonlyHostSignal } from "./types.js";
import { FrameReader, buildFrame, encodeFrame, encodeValue, type EncodeCtx } from "./wire.js";

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export interface ClientOptions {
  fetch?: FetchLike;
  // Prefix for request URLs. Empty (default) means same-origin relative URLs.
  origin?: string;
  windowId?: string;
  // Optional reactive signal factory, for example createSignal from
  // @krate/runtime. When set, host signals are backed by it so reads inside
  // effects and JSX are tracked.
  createSignal?: <T>(initial: T) => [() => T, (v: T) => void];
}

export interface Client {
  call<T = unknown>(fn: string, args?: unknown[], opts?: CallOptions): Promise<T>;
  stream<T = unknown>(fn: string, args?: unknown[], opts?: CallOptions): AsyncIterable<T> & AsyncIterator<T>;
  signal<T = unknown>(name: string): ReadonlyHostSignal<T, T | undefined>;
  close(): void;
}

const CT = "application/x-krate-frames";
let nextId = 1;

interface Waiter {
  resolve(v: unknown): void;
  reject(e: unknown): void;
  signal?: AbortSignal;
  onAbort?: () => void;
  rec: Rec;
}

interface Rec {
  id: number;
  fn: string;
  args: unknown[];
  kind: "call" | "stream";
  dedupe: boolean;
  waiters: Waiter[];
  sent: boolean;
  settled: boolean;
  key?: string;
}

function abortReason(s: AbortSignal): unknown {
  return s.reason !== undefined ? s.reason : new DOMException("This operation was aborted", "AbortError");
}

function transport(e: unknown): KrateError {
  if (e instanceof KrateError) return e;
  return new KrateError("transport", e instanceof Error ? e.message : String(e));
}

function toKrateError(err: any): KrateError {
  return new KrateError(String(err?.code ?? "internal"), String(err?.message ?? ""), err?.data ?? null);
}

function randomId(): string {
  const c = (globalThis as any).crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

export function createClient(options: ClientOptions = {}): Client {
  const fetchFn: FetchLike = options.fetch ?? ((i, init) => (globalThis as any).fetch(i, init));
  const origin = options.origin ?? "";
  const windowId: string = options.windowId ?? (globalThis as any).__KRATE_WINDOW__ ?? randomId();
  const hdr = { "x-krate-window": windowId };
  let closed = false;

  // ---- calls ----
  let queue: Rec[] = [];
  const byId = new Map<number, Rec>();
  const inflight = new Map<string, Rec>();

  function postCancel(body: { ids?: number[]; streams?: string[] }): void {
    if (closed) return;
    try {
      fetchFn(origin + "/__krate/cancel", {
        method: "POST",
        headers: { ...hdr, "content-type": CT },
        body: encodeFrame(body) as BodyInit,
      }).catch(() => {});
    } catch {
      // best effort
    }
  }

  function detach(w: Waiter): void {
    if (w.signal && w.onAbort) w.signal.removeEventListener("abort", w.onAbort);
  }

  function abandon(rec: Rec): void {
    if (rec.key && inflight.get(rec.key) === rec) inflight.delete(rec.key);
    if (rec.sent && !rec.settled) postCancel({ ids: [rec.id] });
  }

  function addWaiter(rec: Rec, resolve: (v: any) => void, reject: (e: unknown) => void, signal?: AbortSignal): void {
    const w: Waiter = { resolve, reject, signal, rec };
    if (signal) {
      w.onAbort = () => {
        const r = w.rec;
        const i = r.waiters.indexOf(w);
        if (i < 0) return;
        r.waiters.splice(i, 1);
        reject(abortReason(signal));
        if (r.waiters.length === 0) abandon(r);
      };
      signal.addEventListener("abort", w.onAbort, { once: true });
    }
    rec.waiters.push(w);
  }

  function finish(rec: Rec, how: "resolve" | "reject", v: unknown): void {
    if (rec.settled) return;
    rec.settled = true;
    byId.delete(rec.id);
    if (rec.key && inflight.get(rec.key) === rec) inflight.delete(rec.key);
    const ws = rec.waiters;
    rec.waiters = [];
    for (const w of ws) {
      detach(w);
      w[how](v);
    }
  }

  function enqueue(
    kind: Rec["kind"],
    fn: string,
    args: unknown[],
    opts: CallOptions | undefined,
    resolve: (v: any) => void,
    reject: (e: unknown) => void,
  ): void {
    const signal = opts?.signal;
    if (signal?.aborted) {
      reject(abortReason(signal));
      return;
    }
    const rec: Rec = {
      id: nextId++,
      fn,
      args,
      kind,
      dedupe: kind === "call" && !!opts?.dedupe,
      waiters: [],
      sent: false,
      settled: false,
    };
    addWaiter(rec, resolve, reject, signal);
    queue.push(rec);
    if (queue.length === 1) queueMicrotask(flush);
  }

  function flush(): void {
    const batch = queue;
    queue = [];
    const ctx: EncodeCtx = { blobs: [] };
    const calls: unknown[] = [];
    const recs: Rec[] = [];
    for (const rec of batch) {
      if (rec.waiters.length === 0) continue;
      const mark = ctx.blobs.length;
      let tree: unknown[];
      try {
        tree = rec.args.map((a, i) => encodeValue(a, ctx, `argument ${i}`));
      } catch (e) {
        ctx.blobs.length = mark;
        finish(rec, "reject", e);
        continue;
      }
      if (rec.dedupe && ctx.blobs.length === mark) {
        const key = rec.fn + "\0" + JSON.stringify(tree);
        const ex = inflight.get(key);
        if (ex && !ex.settled) {
          for (const w of rec.waiters) {
            w.rec = ex;
            ex.waiters.push(w);
          }
          rec.waiters = [];
          rec.settled = true;
          continue;
        }
        rec.key = key;
        inflight.set(key, rec);
      }
      rec.sent = true;
      byId.set(rec.id, rec);
      calls.push({ id: rec.id, fn: rec.fn, args: tree });
      recs.push(rec);
    }
    if (recs.length === 0) return;
    let body: Uint8Array;
    try {
      body = buildFrame({ calls }, ctx.blobs);
    } catch (e) {
      for (const r of recs) finish(r, "reject", e);
      return;
    }
    void send(recs, body);
  }

  function failAll(recs: Rec[], e: KrateError): void {
    for (const r of recs) finish(r, "reject", e);
  }

  async function send(recs: Rec[], body: Uint8Array): Promise<void> {
    try {
      const res = await fetchFn(origin + "/__krate/call", {
        method: "POST",
        headers: { ...hdr, "content-type": CT },
        body: body as BodyInit,
      });
      if (!res.ok || !res.body) {
        let e = new KrateError("transport", `host returned HTTP ${res.status}`);
        if (res.body) {
          try {
            const f = await new FrameReader(res.body).next();
            const err = (f?.value as any)?.error;
            if (err) e = toKrateError(err);
          } catch {
            // keep the HTTP error
          }
        }
        failAll(recs, e);
        return;
      }
      const reader = new FrameReader(res.body);
      for (;;) {
        const f = await reader.next();
        if (!f) break;
        const m = f.value as any;
        if (m && m.error && m.id === undefined) {
          failAll(recs, toKrateError(m.error));
          return;
        }
        const rec = m && typeof m.id === "number" ? byId.get(m.id) : undefined;
        if (!rec) continue;
        if ("err" in m) finish(rec, "reject", toKrateError(m.err));
        else if ("stream" in m) {
          const sid = String(m.stream);
          if (rec.kind === "stream") {
            if (rec.waiters.length === 0) postCancel({ streams: [sid] });
            finish(rec, "resolve", sid);
          } else {
            postCancel({ streams: [sid] });
            finish(rec, "reject", new KrateError("protocol", `${rec.fn} returned a stream; use stream()`));
          }
        } else if ("ok" in m) {
          if (rec.kind === "stream") finish(rec, "reject", new KrateError("protocol", `${rec.fn} did not return a stream`));
          else finish(rec, "resolve", m.ok);
        }
      }
      for (const r of recs) finish(r, "reject", new KrateError("transport", "no result"));
    } catch (e) {
      failAll(recs, transport(e));
    }
  }

  function call<T = unknown>(fn: string, args: unknown[] = [], opts?: CallOptions): Promise<T> {
    const m = findMock(fn);
    if (m) {
      return new Promise<T>((resolve, reject) => {
        if (opts?.signal?.aborted) return reject(abortReason(opts.signal));
        try {
          const f = mockFn(m, fn);
          args.forEach((a, i) => encodeValue(a, { blobs: [] }, `argument ${i}`));
          Promise.resolve(f(...args)).then(resolve as (v: unknown) => void, reject);
        } catch (e) {
          reject(e);
        }
      });
    }
    return new Promise<T>((resolve, reject) => enqueue("call", fn, args, opts, resolve, reject));
  }

  // ---- streams ----
  function stream<T = unknown>(fn: string, args: unknown[] = [], opts?: CallOptions): AsyncIterable<T> & AsyncIterator<T> {
    const m = findMock(fn);
    if (m) return mockStream<T>(m, fn, args, opts);

    let sid: string | undefined;
    let reader: FrameReader | undefined;
    let ctrl: AbortController | undefined;
    let done = false;
    let ended = false;
    let reason: { v: unknown } | undefined;
    let chain: Promise<unknown> = Promise.resolve();
    const signal = opts?.signal;

    function close(): void {
      if (done) return;
      done = true;
      signal?.removeEventListener("abort", onAbort);
      ctrl?.abort();
      void reader?.cancel();
      if (sid && !ended) postCancel({ streams: [sid] });
    }
    function onAbort(): void {
      reason = { v: abortReason(signal!) };
      close();
    }
    signal?.addEventListener("abort", onAbort, { once: true });

    async function open(): Promise<void> {
      sid = await new Promise<string>((resolve, reject) => enqueue("stream", fn, args, { signal }, resolve, reject));
      if (done) {
        // closed while the call was in flight
        postCancel({ streams: [sid] });
        return;
      }
      ctrl = new AbortController();
      const res = await fetchFn(origin + "/__krate/stream/" + encodeURIComponent(sid), {
        method: "GET",
        headers: hdr,
        signal: ctrl.signal,
      });
      if (!res.ok || !res.body) throw new KrateError("transport", `stream returned HTTP ${res.status}`);
      reader = new FrameReader(res.body);
    }

    async function step(): Promise<IteratorResult<T>> {
      if (done) {
        if (reason) throw reason.v;
        return { done: true, value: undefined };
      }
      try {
        if (!reader) await open();
        if (done || !reader) {
          if (reason) throw reason.v;
          return { done: true, value: undefined };
        }
        const f = await reader.next();
        if (done) {
          if (reason) throw reason.v;
          return { done: true, value: undefined };
        }
        if (!f) throw new KrateError("transport", "stream closed before end");
        const v = f.value as any;
        if (v && "v" in v) return { done: false, value: v.v as T };
        if (v && v.end) {
          ended = true;
          close();
          return { done: true, value: undefined };
        }
        if (v && v.err) {
          ended = true;
          close();
          throw toKrateError(v.err);
        }
        throw new KrateError("protocol", "unexpected stream frame");
      } catch (e) {
        if (!reason && signal?.aborted) reason = { v: abortReason(signal) };
        if (reason) {
          close();
          throw reason.v;
        }
        if (done && !(e instanceof KrateError)) return { done: true, value: undefined };
        ended = e instanceof KrateError && e.code !== "transport";
        close();
        throw transport(e);
      }
    }

    const it = {
      [Symbol.asyncIterator]() {
        return it;
      },
      next(): Promise<IteratorResult<T>> {
        const p = chain.then(step);
        chain = p.catch(() => {});
        return p;
      },
      return(): Promise<IteratorResult<T>> {
        close();
        return Promise.resolve({ done: true, value: undefined });
      },
    };
    return it;
  }

  function mockStream<T>(m: { impl: any; name: string }, fn: string, args: unknown[], opts?: CallOptions): AsyncIterable<T> & AsyncIterator<T> {
    let inner: AsyncIterator<T> | undefined;
    const open = async (): Promise<AsyncIterator<T>> => {
      const r: any = await mockFn(m, fn)(...args);
      if (r && typeof r[Symbol.asyncIterator] === "function") return r[Symbol.asyncIterator]();
      if (r && typeof r[Symbol.iterator] === "function") {
        const sync = r[Symbol.iterator]() as Iterator<T>;
        return { next: async () => sync.next(), return: async () => ({ done: true, value: undefined }) };
      }
      throw new KrateError("protocol", `mock ${fn} did not return an iterable`);
    };
    const it = {
      [Symbol.asyncIterator]() {
        return it;
      },
      async next(): Promise<IteratorResult<T>> {
        if (opts?.signal?.aborted) throw abortReason(opts.signal);
        inner ??= await open();
        return inner.next();
      },
      async return(): Promise<IteratorResult<T>> {
        await inner?.return?.();
        return { done: true, value: undefined };
      },
    };
    return it;
  }

  // ---- signals ----
  interface Entry {
    value: unknown;
    has: boolean;
    subs: Set<(v: any) => void>;
    write?: (v: unknown) => void;
    read: ReadonlyHostSignal<unknown>;
  }
  const sigs = new Map<string, Entry>();
  let sigCtrl: AbortController | undefined;
  let sigGen = 0;
  let sigTimer: ReturnType<typeof setTimeout> | undefined;
  let backoff = 100;
  let connectPending = false;

  function signal<T = unknown>(name: string): ReadonlyHostSignal<T, T | undefined> {
    const mocked = mockSignal(name);
    if (mocked) return mocked as ReadonlyHostSignal<T, T | undefined>;
    let e = sigs.get(name);
    if (!e) {
      const entry: Entry = { value: undefined, has: false, subs: new Set(), read: undefined as any };
      let read: () => unknown;
      if (options.createSignal) {
        const [r, w] = options.createSignal<unknown>(undefined);
        read = r;
        entry.write = w;
      } else {
        read = () => entry.value;
      }
      const sig = read as ReadonlyHostSignal<unknown>;
      sig.subscribe = (fn) => {
        entry.subs.add(fn);
        return () => {
          entry.subs.delete(fn);
        };
      };
      entry.read = sig;
      sigs.set(name, entry);
      e = entry;
      scheduleConnect();
    }
    return e.read as ReadonlyHostSignal<T, T | undefined>;
  }

  function scheduleConnect(): void {
    if (connectPending || closed) return;
    connectPending = true;
    queueMicrotask(() => {
      connectPending = false;
      void connect();
    });
  }

  function apply(name: string, v: unknown): void {
    const e = sigs.get(name);
    if (!e) return;
    if (e.has && Object.is(e.value, v)) return;
    e.has = true;
    e.value = v;
    e.write?.(v);
    for (const fn of [...e.subs]) fn(v);
  }

  async function connect(): Promise<void> {
    if (closed || sigs.size === 0) return;
    if (sigTimer !== undefined) clearTimeout(sigTimer);
    sigTimer = undefined;
    sigCtrl?.abort();
    const my = ++sigGen;
    const ctrl = (sigCtrl = new AbortController());
    try {
      const names = [...sigs.keys()].map(encodeURIComponent).join(",");
      const res = await fetchFn(origin + "/__krate/signals?names=" + names, { headers: hdr, signal: ctrl.signal });
      if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`);
      const reader = new FrameReader(res.body);
      for (;;) {
        const f = await reader.next();
        if (!f) break;
        if (my !== sigGen) return;
        const m = f.value as any;
        if (m && typeof m.sig === "string") {
          backoff = 100;
          apply(m.sig, m.v);
        }
      }
    } catch {
      // fall through to reconnect
    }
    if (my !== sigGen || closed) return;
    sigTimer = setTimeout(() => void connect(), backoff);
    backoff = Math.min(backoff * 2, 5000);
  }

  function close(): void {
    closed = true;
    sigGen++;
    sigCtrl?.abort();
    if (sigTimer !== undefined) clearTimeout(sigTimer);
  }

  return { call, stream, signal, close };
}
