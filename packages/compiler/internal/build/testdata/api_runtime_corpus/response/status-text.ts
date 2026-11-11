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
  const a = new Response(null, { status: 201, statusText: "Made" });
  const b = new Response("x");
  return Response.json({
    a: [a.status, a.statusText, a.ok], b: [b.status, JSON.stringify(b.statusText), b.ok, b.type, b.url, b.redirected],
    s299: new Response(null, { status: 299 }).ok, s300: new Response(null, { status: 300 }).ok,
    low: errOf(() => new Response(null, { status: 199 })), high: errOf(() => new Response(null, { status: 600 })),
    nullBodyStatus: errOf(() => new Response("x", { status: 204 })), nullBody304: errOf(() => new Response("x", { status: 304 })),
  });
}
