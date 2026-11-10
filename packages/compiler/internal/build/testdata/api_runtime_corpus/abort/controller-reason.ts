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
  const a = new AbortController();
  const before = [a.signal.aborted, a.signal.reason === undefined];
  a.abort();
  const r: any = a.signal.reason;
  const b = new AbortController(); b.abort("custom");
  const c = new AbortController(); const err = new Error("e"); c.abort(err);
  const d = new AbortController(); d.abort(1); d.abort(2);
  return Response.json({
    before, defaultReason: [r instanceof DOMException, r.name, r.code], custom: b.signal.reason, errSame: c.signal.reason === err, first: d.signal.reason,
    throwIf: (() => { try { b.signal.throwIfAborted(); return "no"; } catch (e) { return e; } })(),
    notAborted: errOf(() => new AbortController().signal.throwIfAborted()),
    ctor: errOf(() => new (AbortSignal as any)()),
  });
}
