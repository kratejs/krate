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
  let ctrl: any;
  const s = new ReadableStream({ start(c) { ctrl = c; } });
  const r = s.getReader();
  ctrl.enqueue("x");
  const desired = ctrl.desiredSize;
  ctrl.close();
  const afterClose = errOf(() => ctrl.enqueue("y"));
  const closeTwice = errOf(() => ctrl.close());
  const reads = [await r.read(), await r.read()].map((x) => [x.value ?? null, x.done]);
  let ec: any;
  const e = new ReadableStream({ start(c) { ec = c; } });
  const er = e.getReader();
  const pending = er.read().then(() => "resolved", (x) => "rejected:" + String(x));
  ec.error(new Error("boom"));
  return Response.json({ desired, afterClose, closeTwice, reads, pending: await pending, later: await er.read().then(() => "resolved", (x) => "rejected:" + x.message), closed: await er.closed.then(() => "ok", () => "rejected") });
}
