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
  return Response.json({ immutable: errOf(() => Response.redirect("http://x.example/").headers.set("x", "1")) });
}
