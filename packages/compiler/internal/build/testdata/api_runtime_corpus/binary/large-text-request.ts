function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function POST(request) {
  const t = await request.text();
  return Response.json({ len: t.length, cps: Array.from(t).length, head: t.slice(0, 6), sha: hex(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(t))) });
}
