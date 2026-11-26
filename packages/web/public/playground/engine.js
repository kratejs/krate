// Krate playground engine: the Krate compiler (Go, built to WebAssembly)
// running in a Web Worker.
//
// Start it as a module worker:
//
//   const worker = new Worker("/playground/engine.js", { type: "module" });
//   worker.postMessage({ id: 1, type: "init" });
//   worker.postMessage({ id: 2, type: "build", files: { "src/pages/index.tsx": "..." }, opts: {} });
//
// Requests are { id, type: "init" | "build", ... }. Every request gets exactly
// one reply, { id, ok: true, type, result } or { id, ok: false, error: { message, stack } },
// and "init" also streams { type: "progress", id, stage, loaded, total } events.
// The same module can be imported to drive the engine directly (createEngine).
//
// The engine mutates built-in prototypes (see the build-time evaluator in
// packages/native/jsruntime), so run it in a dedicated worker, not in the page.

import { createMemFS } from "./memfs.js";

const PROJECT_ROOT = "/app";

/**
 * @param {{ base?: URL | string, fetch?: typeof fetch, onProgress?: (p: {stage: string, loaded?: number, total?: number}) => void, onLog?: (line: string) => void }} [options]
 */
export function createEngine(options = {}) {
  const base = new URL(options.base ?? ".", import.meta.url);
  const doFetch = options.fetch ?? ((url) => fetch(url));
  const progress = options.onProgress ?? (() => {});
  const onLog = options.onLog ?? (() => {});

  let ready = null;
  let memfs = null;
  let version = "";

  async function fetchOk(name) {
    const res = await doFetch(new URL(name, base));
    if (!res.ok) throw new Error(`could not load ${name}: HTTP ${res.status}`);
    return res;
  }

  /** Streams a response body, reporting download progress. */
  function withProgress(res, stage) {
    const total = Number(res.headers.get("x-krate-size") || res.headers.get("content-length") || 0);
    if (!res.body) return res;
    let loaded = 0;
    const counter = new TransformStream({
      transform(chunk, controller) {
        loaded += chunk.length;
        progress({ stage, loaded, total });
        controller.enqueue(chunk);
      },
    });
    return new Response(res.body.pipeThrough(counter), { headers: res.headers });
  }

  async function load() {
    memfs = createMemFS({ onOutput: (fd, text) => onLog(text) });
    memfs.install();
    for (const dir of [PROJECT_ROOT, "/tmp", "/home"]) memfs.mkdirp(dir);

    progress({ stage: "manifest" });
    const manifest = await (await fetchOk("manifest.json")).json();
    version = manifest.version ?? "";

    // wasm_exec.js is a classic script that defines globalThis.Go.
    await import(/* @vite-ignore */ new URL(manifest.wasmExec, base).href);
    if (typeof globalThis.Go !== "function") throw new Error("wasm_exec.js did not define Go");

    // The packages the compiler resolves from the project's node_modules.
    const { packages } = await (await fetchOk(manifest.packages)).json();
    for (const [pkg, { files }] of Object.entries(packages)) {
      for (const [name, text] of Object.entries(files)) memfs.writeFile(`${PROJECT_ROOT}/node_modules/${pkg}/${name}`, text);
    }

    progress({ stage: "download", loaded: 0, total: manifest.wasmSize ?? 0 });
    let res = withProgress(await fetchOk(manifest.wasm), "download");
    if (manifest.wasmEncoding === "gzip") {
      res = new Response(res.body.pipeThrough(new DecompressionStream("gzip")), {
        headers: { "content-type": "application/wasm" },
      });
    } else {
      res = new Response(res.body, { headers: { "content-type": "application/wasm" } });
    }

    const go = new globalThis.Go();
    go.env = { HOME: "/home", TMPDIR: "/tmp", PWD: "/app", KRATE_NO_COLOR: "1", NO_COLOR: "1", TERM: "dumb" };
    const readyPromise = new Promise((resolve) => {
      globalThis.__krateReady = resolve;
    });
    progress({ stage: "compile" });
    const { instance } = await WebAssembly.instantiateStreaming(res, go.importObject);
    progress({ stage: "start" });
    // Resolves when the Go program exits, which it only does on a fatal error.
    go.run(instance).then(
      () => {
        ready = Promise.reject(new Error("the Krate compiler exited"));
        ready.catch(() => {});
      },
      (err) => {
        ready = Promise.reject(err);
        ready.catch(() => {});
      },
    );
    await readyPromise;
    progress({ stage: "ready" });
    return { version: globalThis.krate.version || version };
  }

  return {
    /** Loads the compiler. Safe to call more than once. */
    init() {
      ready ??= load();
      return ready;
    },

    /**
     * Compiles a project. files maps project paths ("src/pages/index.tsx")
     * to text, or to Uint8Array for binary assets.
     * Resolves to { ok, files, diagnostics, timingsMs, log }.
     */
    async build(files, opts = {}) {
      await this.init();
      const input = { ...files };
      if (!("package.json" in input)) input["package.json"] = '{"name":"playground","private":true,"type":"module"}';
      return globalThis.krate.build(input, opts);
    },
  };
}

/** Wires an engine to a message port: { postMessage(msg), onmessage = fn }. */
export function attach(port, options = {}) {
  let current = 0;
  const engine = createEngine({
    ...options,
    onProgress: (p) => port.postMessage({ type: "progress", id: current, ...p }),
  });
  port.onmessage = async (event) => {
    const msg = event.data ?? event;
    const { id, type } = msg;
    try {
      let result;
      if (type === "init") {
        current = id;
        result = await engine.init();
      } else if (type === "build") {
        result = await engine.build(msg.files, msg.opts);
      } else {
        throw new Error(`unknown request type: ${type}`);
      }
      port.postMessage({ id, ok: true, type, result });
    } catch (err) {
      port.postMessage({ id, ok: false, error: { message: String(err?.message ?? err), stack: String(err?.stack ?? "") } });
    }
  };
}

// Started as a worker: serve requests.
if (typeof WorkerGlobalScope !== "undefined" && globalThis instanceof WorkerGlobalScope) {
  attach(globalThis);
}
