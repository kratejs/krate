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
  const cases: Record<string, () => unknown> = {
    fn: () => structuredClone(() => 1), method: () => structuredClone({ f() {} }), symbol: () => structuredClone(Symbol("s")),
    symbolProp: () => structuredClone({ s: Symbol("x") }), weakMap: () => structuredClone(new WeakMap()), promise: () => structuredClone(Promise.resolve()),
  };
  for (const [k, f] of Object.entries(cases)) out[k] = errOf(f);
  return Response.json(out);
}
