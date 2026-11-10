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
  const h = new Headers();
  return Response.json({
    badName: errOf(() => h.set("bad name", "v")), emptyName: errOf(() => h.set("", "v")), colon: errOf(() => h.append("a:b", "v")),
    newline: errOf(() => h.set("a", "x\ny")), cr: errOf(() => h.set("a", "x\ry")), nul: errOf(() => h.set("a", "x\u0000y")),
    getBad: errOf(() => h.get("bad name")), hasBad: errOf(() => h.has("bad name")),
    tab: errOf(() => h.set("a", "x\ty")),
    badInit: errOf(() => new Headers([["only-one"]] as any)), badInitType: errOf(() => new Headers(5 as any)),
  });
}
