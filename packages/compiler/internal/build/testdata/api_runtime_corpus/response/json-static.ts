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
  const r = Response.json({ a: 1 }, { status: 202, headers: { "x-custom": "j" } });
  const typed = Response.json([1], { headers: { "content-type": "application/vnd.x+json" } });
  return Response.json({
    status: r.status, ct: r.headers.get("content-type"), x: r.headers.get("x-custom"), body: await r.text(), typed: typed.headers.get("content-type"),
    undef: errOf(() => Response.json(undefined)), fn: errOf(() => Response.json(() => 1)), big: errOf(() => Response.json(1n as any)),
    str: await Response.json("s").text(), nul: await Response.json(null).text(),
  });
}
