export async function GET() {
  const r = new ReadableStream({ start(c) { c.enqueue("a"); c.close(); } }).getReader();
  const a = await r.read();
  const b = await r.read();
  return new Response(JSON.stringify([a, b, Object.keys(a), Object.keys(b), "value" in b]), { headers: { "content-type": "application/json" } });
}
