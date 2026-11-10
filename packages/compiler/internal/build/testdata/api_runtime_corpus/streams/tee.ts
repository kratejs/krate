export async function GET() {
  const [a, b] = new ReadableStream({ start(c) { c.enqueue("x"); c.close(); } }).tee();
  const ra = await a.getReader().read();
  const rb = await b.getReader().read();
  return Response.json({ a: ra.value, b: rb.value });
}
