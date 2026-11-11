export async function GET() {
  const a = new AbortController(), b = new AbortController();
  const s = AbortSignal.any([a.signal, b.signal]);
  const before = s.aborted;
  let fired = 0; s.addEventListener("abort", () => fired++);
  b.abort("from-b"); a.abort("from-a");
  const pre = AbortSignal.any([new AbortController().signal, AbortSignal.abort("already")]);
  const empty = AbortSignal.any([]);
  return Response.json({ before, after: s.aborted, reason: s.reason, fired, pre: [pre.aborted, pre.reason], empty: empty.aborted });
}
