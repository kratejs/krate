export async function GET() {
  const enc = new TextEncoder();
  const stream = new ReadableStream({
    async start(c) { for (let i = 0; i < 3; i++) { c.enqueue(enc.encode("chunk" + i + ";")); await new Promise((r) => setTimeout(r, 5)); } c.close(); },
  });
  return new Response(stream, { headers: { "content-type": "text/plain" } });
}
