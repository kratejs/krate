// docfind.js — minimal browser glue for the docfind search WASM module
// (raw C-ABI, no wasm-bindgen). Written by krate's docs plugin build and
// served alongside docfind_bg.wasm (the module produced at build time with
// the docs index embedded into it).
//
// Usage:
//   import search from "/docs/search/docfind.js";
//   const docs = await search("signals", 8);

let instance = null;
let memory = null;
let initPromise = null;

function alloc(len) {
  return instance.exports.docfind_alloc(len);
}

function free(ptr, len) {
  if (ptr) instance.exports.docfind_free(ptr, len);
}

function writeString(str) {
  const bytes = new TextEncoder().encode(str);
  const ptr = alloc(bytes.byteLength);
  new Uint8Array(memory.buffer, ptr, bytes.byteLength).set(bytes);
  return { ptr, len: bytes.byteLength };
}

function readString(ptr, len) {
  if (!ptr || !len) return "";
  return new TextDecoder("utf-8").decode(new Uint8Array(memory.buffer, ptr, len));
}

async function instantiate(url) {
  if (typeof WebAssembly.instantiateStreaming === "function") {
    try {
      const res = await fetch(url);
      if (res.ok && (res.headers.get("Content-Type") || "").indexOf("application/wasm") === 0) {
        return await WebAssembly.instantiateStreaming(res);
      }
      return await WebAssembly.instantiate(await res.arrayBuffer());
    } catch (e) {
      // fall back to non-streaming below
    }
  }
  const res = await fetch(url);
  return WebAssembly.instantiate(await res.arrayBuffer());
}

/** Initialize the WASM module (idempotent). Returns the instance. */
export async function init(input) {
  if (instance) return instance;
  if (!initPromise) {
    initPromise = (async () => {
      const url = input || new URL("docfind_bg.wasm", import.meta.url).href;
      const { instance: inst } = await instantiate(url);
      instance = inst;
      memory = inst.exports.memory;
      return instance;
    })();
  }
  return initPromise;
}

/**
 * Search the embedded index for `query`. Returns a Promise of a ranked array
 * of `{ title, category, href, body }` documents. `maxResults` defaults to 8.
 */
export default async function search(query, maxResults) {
  await init();
  const max = maxResults || 8;
  const { ptr, len } = writeString(String(query || ""));
  const outPtr = alloc(8);
  try {
    const code = instance.exports.docfind_search(ptr, len, max, outPtr, outPtr + 4);
    if (code !== 0) throw new Error("docfind search failed");
    // Memory may have grown during the call — always re-read the buffer.
    const view = new DataView(memory.buffer);
    const resPtr = view.getUint32(outPtr, true);
    const resLen = view.getUint32(outPtr + 4, true);
    const payload = readString(resPtr, resLen);
    const docs = JSON.parse(payload || "[]");
    for (const d of docs) {
      if (d.body && d.body.length > 220) d.body = d.body.slice(0, 220) + "…";
    }
    return docs;
  } finally {
    free(ptr, len);
    free(outPtr, 8);
  }
}
