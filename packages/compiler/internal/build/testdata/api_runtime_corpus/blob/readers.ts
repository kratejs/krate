function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const b = new Blob([new Uint8Array([0xe2, 0x82, 0xac, 0xff])]);
  const ab = await b.arrayBuffer();
  const reader = b.stream().getReader();
  const chunks: number[] = [];
  for (;;) { const r = await reader.read(); if (r.done) break; chunks.push(...r.value); }
  return Response.json({ text: Array.from(await b.text(), (c) => c.codePointAt(0)!.toString(16)), ab: [ab instanceof ArrayBuffer, hex(ab)], stream: chunks, bytes: typeof (b as any).bytes === "function" ? hex(await (b as any).bytes()) : "missing" });
}
