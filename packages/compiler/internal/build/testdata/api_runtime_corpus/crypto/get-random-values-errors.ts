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
    float32: errOf(() => crypto.getRandomValues(new Float32Array(1) as any)),
    float64: errOf(() => crypto.getRandomValues(new Float64Array(1) as any)), dataView: errOf(() => crypto.getRandomValues(new DataView(new ArrayBuffer(4)) as any)),
    plain: errOf(() => crypto.getRandomValues([1, 2] as any)), u16TooBig: errOf(() => crypto.getRandomValues(new Uint16Array(32769))),
  });
}
