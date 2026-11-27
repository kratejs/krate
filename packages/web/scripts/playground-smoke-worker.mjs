// Node worker for playground-smoke.mjs: runs the engine the way a browser
// Web Worker would, with file: URLs readable through fetch.
import { readFile } from "node:fs/promises";
import { parentPort } from "node:worker_threads";
import { fileURLToPath } from "node:url";

const realFetch = globalThis.fetch;
globalThis.fetch = async (url) => {
  const u = new URL(String(url));
  if (u.protocol !== "file:") return realFetch(url);
  try {
    const buf = await readFile(fileURLToPath(u));
    return new Response(buf, { headers: { "content-length": String(buf.length) } });
  } catch (err) {
    return new Response(String(err.message), { status: 404 });
  }
};

const { attach } = await import("../public/playground/engine.js");
attach({
  postMessage: (msg) => parentPort.postMessage(msg),
  get onmessage() {
    return null;
  },
  set onmessage(fn) {
    parentPort.on("message", (data) => fn({ data }));
  },
});
parentPort.postMessage({ type: "worker-ready" });
