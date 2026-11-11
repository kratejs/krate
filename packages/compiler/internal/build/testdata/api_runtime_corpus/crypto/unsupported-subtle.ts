function kind(e: any): string {
  if (typeof DOMException === "function" && e instanceof DOMException) return "DOMException:" + e.name;
  if (e instanceof TypeError) return "TypeError";
  if (e instanceof RangeError) return "RangeError";
  if (e instanceof SyntaxError) return "SyntaxError";
  if (e instanceof Error) return "Error:" + e.name;
  return "thrown:" + typeof e;
}
function errOf(fn: () => unknown): string {
  try { fn(); return "ok"; } catch (e) { return kind(e); }
}
async function errOfAsync(fn: () => unknown): Promise<string> {
  try { await fn(); return "ok"; } catch (e) { return kind(e); }
}
export async function GET() {
  return Response.json({
    generateKey: await errOfAsync(() => crypto.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, true, ["sign"])),
    aes: await errOfAsync(() => crypto.subtle.importKey("raw", new Uint8Array(16), "AES-GCM", false, ["encrypt"])),
  });
}
