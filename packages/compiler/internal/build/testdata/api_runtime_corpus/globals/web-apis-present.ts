const names = ["URL", "URLSearchParams", "Headers", "Request", "Response", "fetch", "Blob", "File", "FormData", "ReadableStream", "ReadableStreamDefaultReader", "TextEncoder", "TextDecoder", "AbortController", "AbortSignal", "Event", "EventTarget", "DOMException", "structuredClone", "atob", "btoa", "setTimeout", "clearTimeout", "setInterval", "clearInterval", "queueMicrotask", "crypto", "CryptoKey", "performance", "console"];
export async function GET() {
  return Response.json(Object.fromEntries(names.map((n) => [n, typeof (globalThis as any)[n]])));
}
