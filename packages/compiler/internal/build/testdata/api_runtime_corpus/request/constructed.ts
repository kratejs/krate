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
  const r = new Request("http://x.example/a?b=1", { method: "post", headers: { "X-A": "1" }, body: "payload" });
  const p = new Request("http://x.example/", { method: "patch" });
  const derived = new Request(r, { headers: { "x-b": "2" } });
  return Response.json({
    method: r.method, patch: p.method, url: r.url, ct: r.headers.get("content-type"), sourceUsed: r.bodyUsed,
    derived: [derived.method, derived.headers.get("x-a"), derived.headers.get("x-b"), derived.url, await derived.text()],
    reread: await errOfAsync(() => r.text()),
    getBody: errOf(() => new Request("http://x.example/", { body: "x" })), headBody: errOf(() => new Request("http://x.example/", { method: "HEAD", body: "x" })),
    relative: errOf(() => new Request("/relative")), creds: errOf(() => new Request("http://u:p@x.example/")),
    badMethod: errOf(() => new Request("http://x.example/", { method: "bad method" })), connect: errOf(() => new Request("http://x.example/", { method: "CONNECT" })),
    defaults: [p.redirect, p.cache, p.credentials, p.mode],
  });
}
