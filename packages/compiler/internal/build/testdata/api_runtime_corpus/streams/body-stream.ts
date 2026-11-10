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
export async function POST(request) {
  const r = request.body.getReader();
  const chunks: number[] = [];
  for (;;) { const x = await r.read(); if (x.done) break; chunks.push(...x.value); }
  const resp = new Response("resp-body");
  const rr = resp.body.getReader();
  let text = "";
  const dec = new TextDecoder();
  for (;;) { const x = await rr.read(); if (x.done) break; text += dec.decode(x.value, { stream: true }); }
  return Response.json({ len: chunks.length, sum: chunks.reduce((a, b) => a + b, 0), text, again: await errOfAsync(() => request.text()) });
}
