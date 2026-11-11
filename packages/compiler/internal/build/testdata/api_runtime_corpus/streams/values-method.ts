export async function GET() {
  const s = new ReadableStream({ start(c) { c.enqueue(1); c.close(); } });
  if (typeof (s as any).values !== "function") return Response.json({ missing: true });
  const got: number[] = [];
  for await (const x of (s as any).values()) got.push(x);
  return Response.json({ got });
}
