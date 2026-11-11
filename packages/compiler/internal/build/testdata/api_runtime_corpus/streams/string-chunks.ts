export async function GET() {
  const s = new ReadableStream({ start(c) { c.enqueue(new TextEncoder().encode("é")); c.enqueue(new TextEncoder().encode("😀")); c.close(); } });
  return new Response(s, { headers: { "content-type": "text/plain; charset=utf-8" } });
}
