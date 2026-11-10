function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const inner = new Blob(["in"]);
  const b = new Blob(["a", new Uint8Array([98]), new Uint16Array([0x6463]).buffer, inner, new DataView(new Uint8Array([101]).buffer), "\ud800", 7 as any]);
  return Response.json({ size: b.size, text: await b.text(), hex: hex(await b.arrayBuffer()), empty: [new Blob().size, new Blob([]).type], isBlob: b instanceof Blob });
}
