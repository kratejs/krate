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
    md5: await errOfAsync(() => crypto.subtle.digest("MD5", new Uint8Array(1))), str: await errOfAsync(() => crypto.subtle.digest("SHA-256", "abc" as any)),
    missing: await errOfAsync(() => crypto.subtle.digest(undefined as any, new Uint8Array(1))),
  });
}
