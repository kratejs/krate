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
  const out: Record<string, unknown> = {};
  for (const s of [undefined, 301, 302, 303, 307, 308]) { const r = Response.redirect("http://x.example/a b", s); out[String(s)] = [r.status, r.headers.get("location"), r.body === null]; }
  out.badStatus = errOf(() => Response.redirect("http://x.example/", 200));
  out.relative = errOf(() => Response.redirect("/relative"));
  return Response.json(out);
}
