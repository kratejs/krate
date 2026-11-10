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
  const r = new Response("body text", { status: 201, statusText: "Made", headers: { "x-a": "1" } });
  const c = r.clone();
  const a = await r.text();
  const b = await c.text();
  const afterRead = errOf(() => r.clone());
  return Response.json({ a, b, status: c.status, statusText: c.statusText, h: c.headers.get("x-a"), afterRead });
}
