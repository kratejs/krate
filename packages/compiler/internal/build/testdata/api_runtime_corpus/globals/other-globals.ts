const names = ["CustomEvent", "MessageChannel", "BroadcastChannel", "WritableStream", "TransformStream", "TextEncoderStream", "CompressionStream", "WebSocket", "Intl", "WebAssembly", "navigator", "Buffer", "global", "setImmediate", "SharedArrayBuffer", "WeakRef", "FinalizationRegistry"];
export async function GET() {
  return Response.json(Object.fromEntries(names.map((n) => [n, typeof (globalThis as any)[n]])));
}
