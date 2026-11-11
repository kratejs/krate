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
  const s = new ReadableStream({ start(c) { c.enqueue("a"); c.enqueue("b"); c.close(); } });
  const locked0 = s.locked;
  const r = s.getReader();
  const rd = async () => { const x = await r.read(); return [x.value ?? null, x.done]; };
  const reads = [await rd(), await rd(), await rd(), await rd()];
  const locked1 = s.locked;
  r.releaseLock();
  const relocked = errOf(() => { const r2 = s.getReader(); r2.releaseLock(); });
  const s2 = new ReadableStream();
  s2.getReader();
  const second = errOf(() => s2.getReader());
  return Response.json({ locked0, locked1, after: s.locked, reads, relocked, second });
}
