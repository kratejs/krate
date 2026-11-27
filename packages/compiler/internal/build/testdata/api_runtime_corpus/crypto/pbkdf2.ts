function hex(b: ArrayBuffer): string {
  return Array.from(new Uint8Array(b), (x) => x.toString(16).padStart(2, "0")).join("");
}
async function errName(fn: () => Promise<unknown>): Promise<string> {
  try { await fn(); return "ok"; } catch (e) { return (e as Error).name; }
}
export async function GET() {
  const enc = new TextEncoder();
  const out: Record<string, unknown> = {};
  const base = await crypto.subtle.importKey("raw", enc.encode("password"), "PBKDF2", false, ["deriveBits", "deriveKey"]);
  for (const hash of ["SHA-1", "SHA-256", "SHA-384", "SHA-512"]) {
    out[hash] = hex(await crypto.subtle.deriveBits({ name: "PBKDF2", salt: enc.encode("salt"), iterations: 1000, hash }, base, 256));
  }
  const hmac = await crypto.subtle.deriveKey({ name: "PBKDF2", salt: new Uint8Array(16), iterations: 10, hash: "SHA-256" }, base, { name: "HMAC", hash: "SHA-256" }, true, ["sign"]);
  out.derived = [hmac.algorithm.name, (hmac.algorithm as any).length, hmac.extractable, hmac.usages, hex(await crypto.subtle.exportKey("raw", hmac))];
  out.signed = hex(await crypto.subtle.sign("HMAC", hmac, enc.encode("message")));
  out.key = [base.type, base.extractable, base.usages, base.algorithm.name];
  out.zeroIterations = await errName(() => crypto.subtle.deriveBits({ name: "PBKDF2", salt: new Uint8Array(1), iterations: 0, hash: "SHA-256" }, base, 256));
  out.oddLength = await errName(() => crypto.subtle.deriveBits({ name: "PBKDF2", salt: new Uint8Array(1), iterations: 1, hash: "SHA-256" }, base, 7));
  out.extractable = await errName(() => crypto.subtle.importKey("raw", enc.encode("pw"), "PBKDF2", true, ["deriveBits"]));
  out.noUsage = await errName(async () => {
    const k = await crypto.subtle.importKey("raw", enc.encode("pw"), "PBKDF2", false, ["deriveKey"]);
    return crypto.subtle.deriveBits({ name: "PBKDF2", salt: new Uint8Array(1), iterations: 1, hash: "SHA-256" }, k, 256);
  });
  return Response.json(out);
}
