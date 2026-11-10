export async function GET() {
  const s = new ReadableStream({ async start(c) { c.enqueue(new TextEncoder().encode("partial;")); await new Promise((r) => setTimeout(r, 5)); c.error(new Error("stream failed")); } });
  return new Response(s, { headers: { "content-type": "text/plain" } });
}
