export async function GET() {
  const a = new AbortController();
  const log: string[] = [];
  a.signal.onabort = (e) => log.push("onabort:" + e.type);
  a.signal.addEventListener("abort", (e) => log.push("listener:" + e.type + ":" + (e.target === a.signal)));
  a.signal.addEventListener("abort", () => log.push("once"), { once: true });
  a.abort();
  a.abort();
  return Response.json(log);
}
