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
  const r = new Response("x");
  const before = r.bodyUsed;
  await r.text();
  return Response.json({ before, after: r.bodyUsed, again: await errOfAsync(() => r.text()), ab: await errOfAsync(() => r.arrayBuffer()), nullBody: [new Response(null).body === null, new Response(null).bodyUsed, await new Response(null).text()] });
}
