function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const e = new TextEncoder();
  return Response.json({
    encoding: e.encoding,
    empty: e.encode().length,
    emptyStr: e.encode("").length,
    ascii: hex(e.encode("Az09")),
    twoByte: hex(e.encode("é")),
    threeByte: hex(e.encode("€")),
    astral: hex(e.encode("😀")),
    loneHigh: hex(e.encode("a\ud800b")),
    loneLow: hex(e.encode("\udc00")),
    reversedPair: hex(e.encode("\ude00\ud83d")),
    nul: hex(e.encode("\u0000")),
    num: hex(e.encode(123 as any)),
    isU8: e.encode("x") instanceof Uint8Array,
  });
}
