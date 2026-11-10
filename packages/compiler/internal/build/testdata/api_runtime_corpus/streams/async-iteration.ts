export async function GET() {
  const s = new ReadableStream({ start(c) { for (const x of [1, 2, 3]) c.enqueue(x); c.close(); } });
  const got: number[] = [];
  for await (const x of s) got.push(x);
  let cancelled = "";
  const s2 = new ReadableStream({ pull(c) { c.enqueue(9); }, cancel(r) { cancelled = "cancel:" + String(r); } });
  for await (const x of s2) { got.push(x); break; }
  return Response.json({ got, cancelled, lockedAfterBreak: s2.locked });
}
