export async function GET() {
  const a = new AbortController();
  const r = new Request("http://x.example/", { signal: a.signal });
  a.abort("stop");
  return Response.json({ aborted: r.signal.aborted, reason: r.signal.reason });
}
