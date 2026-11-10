function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const enc = new TextEncoder();
  const mid = new Uint8Array(1 << 16); for (let i = 0; i < mid.length; i++) mid[i] = i & 255;
  const out: Record<string, unknown> = {};
  for (const alg of ["SHA-1", "SHA-256", "SHA-384", "SHA-512"]) {
    out[alg] = [hex(await crypto.subtle.digest(alg, enc.encode("abc"))), hex(await crypto.subtle.digest({ name: alg }, new Uint8Array(0))), hex(await crypto.subtle.digest(alg, mid))];
  }
  out.lower = hex(await crypto.subtle.digest("sha-256", enc.encode("abc")));
  out.isAB = (await crypto.subtle.digest("SHA-256", enc.encode(""))) instanceof ArrayBuffer;
  out.inputs = [hex(await crypto.subtle.digest("SHA-1", enc.encode("abc").buffer)), hex(await crypto.subtle.digest("SHA-1", new DataView(enc.encode("xabc").buffer, 1))), hex(await crypto.subtle.digest("SHA-1", new Uint16Array([0x6261])))];
  return Response.json(out);
}
