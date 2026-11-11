export async function GET() {
  let n = 0;
  const pulls: number[] = [];
  const s = new ReadableStream({ pull(c) { pulls.push(c.desiredSize); n++; if (n > 3) c.close(); else c.enqueue(n); } }, { highWaterMark: 2 });
  const out: number[] = [];
  const r = s.getReader();
  for (;;) { const x = await r.read(); if (x.done) break; out.push(x.value); }
  return Response.json({ out, pullCount: pulls.length > 0 });
}
