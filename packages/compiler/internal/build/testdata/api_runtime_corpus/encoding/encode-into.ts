function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const e = new TextEncoder();
  const out: Record<string, unknown> = {};
  for (const [s, n] of [["abc", 5], ["abc", 2], ["é€", 2], ["é€", 3], ["é€", 4], ["😀x", 3], ["😀x", 4], ["😀x", 5], ["", 1], ["\ud800", 3]] as [string, number][]) {
    const d = new Uint8Array(n).fill(0xaa);
    const r = e.encodeInto(s, d);
    out[JSON.stringify(s) + "/" + n] = [r.read, r.written, hex(d)];
  }
  const big = new Uint8Array(8);
  const r2 = e.encodeInto("hi", big.subarray(3, 6));
  out.subarray = [r2.read, r2.written, hex(big)];
  return Response.json(out);
}
