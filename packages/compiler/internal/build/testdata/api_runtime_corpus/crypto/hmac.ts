function hex(b: ArrayBuffer | ArrayBufferView): string {
  const u = b instanceof ArrayBuffer ? new Uint8Array(b) : new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  return Array.from(u, (x) => x.toString(16).padStart(2, "0")).join("");
}
export async function GET() {
  const enc = new TextEncoder();
  const out: Record<string, unknown> = {};
  for (const hash of ["SHA-1", "SHA-256", "SHA-384", "SHA-512"]) {
    const key = await crypto.subtle.importKey("raw", enc.encode("secret-key"), { name: "HMAC", hash }, true, ["sign", "verify"]);
    const sig = await crypto.subtle.sign("HMAC", key, enc.encode("message"));
    const ok = await crypto.subtle.verify("HMAC", key, sig, enc.encode("message"));
    const bad = await crypto.subtle.verify({ name: "HMAC" }, key, new Uint8Array(sig.byteLength), enc.encode("message"));
    const short = await crypto.subtle.verify("HMAC", key, new Uint8Array(3), enc.encode("message"));
    out[hash] = [hex(sig), ok, bad, short];
  }
  const k = await crypto.subtle.importKey("raw", new Uint8Array(16), { name: "HMAC", hash: { name: "SHA-256" } }, false, ["sign"]);
  out.key = [k.type, k.extractable, k.usages, (k.algorithm as any).name, (k.algorithm as any).hash.name, (k.algorithm as any).length, k instanceof CryptoKey];
  out.emptyMsg = hex(await crypto.subtle.sign("HMAC", k, new Uint8Array(0)));
  return Response.json(out);
}
