function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function POST(request) {
  const b = new Uint8Array(await request.arrayBuffer());
  let ok = true;
  for (let i = 0; i < b.length; i++) if (b[i] !== (i & 255)) { ok = false; break; }
  return Response.json({ len: b.length, ok, sha: hex(await crypto.subtle.digest("SHA-256", b)) });
}
